package server

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"vocalis/internal/feed"
	"vocalis/internal/media"
	"vocalis/internal/model"
	"vocalis/internal/scrape"

	"github.com/skip2/go-qrcode"
)

// ---------------------------------------------------------------- podcast

func (s *Server) handleLibraryFeed(w http.ResponseWriter, r *http.Request) {
	books := s.store.Books()
	f := feed.LibraryFeed(books, s.linker(r), feed.LibraryOptions{
		Title:       s.cfg.LibraryTitle,
		Description: fmt.Sprintf("%s · 共 %d 本有声书", s.cfg.LibraryTitle, len(books)),
		Language:    s.cfg.Language,
		Mode:        s.cfg.LibraryFeedMode,
	})
	s.writeFeed(w, f)
}

func (s *Server) handleBookFeed(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSuffix(r.PathValue("name"), ".xml")
	b, ok := s.store.Book(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.writeFeed(w, feed.BookFeed(b, s.linker(r), s.cfg.Language))
}

func (s *Server) writeFeed(w http.ResponseWriter, f *feed.Feed) {
	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	if err := feed.Render(w, f); err != nil && s.log != nil {
		s.log.Error("渲染 feed 失败", "err", err)
	}
}

func (s *Server) handleChaptersJSON(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSuffix(r.PathValue("name"), ".json")
	b, ok := s.store.Book(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	cover := ""
	if b.CoverFile != "" || b.CoverURL != "" {
		cover = s.linker(r).URL("/cover/" + b.ID)
	}
	s.writeJSON(w, http.StatusOK, feed.BuildChapters(b, cover))
}

// ------------------------------------------------------------------ cover

func (s *Server) handleCover(w http.ResponseWriter, r *http.Request) {
	b, ok := s.store.Book(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")

	if b.CoverFile != "" {
		if st, err := os.Stat(b.CoverFile); err == nil && !st.IsDir() {
			w.Header().Set("Content-Type", firstNonEmpty(b.CoverMIME, media.MIMEFor(b.CoverFile)))
			http.ServeFile(w, r, b.CoverFile)
			return
		}
	}
	if b.CoverURL != "" {
		if p, mime, err := s.localCover(b, r.Context()); err == nil && p != "" {
			w.Header().Set("Content-Type", mime)
			http.ServeFile(w, r, p)
			return
		}
	}
	// No artwork anywhere: draw a deterministic gradient instead.
	png := placeholderCover(b.ID)
	if png == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Length", strconv.Itoa(len(png)))
	_, _ = w.Write(png)
}

// localCover downloads and caches a remote cover image.
func (s *Server) localCover(b *model.Book, ctx context.Context) (string, string, error) {
	dir := filepath.Join(s.store.DataDir(), "covers")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	sum := sha1.Sum([]byte(b.CoverURL))
	key := hex.EncodeToString(sum[:])[:8]

	// Reuse a previously downloaded copy of this exact URL.
	for _, ext := range []string{".jpg", ".png", ".webp", ".gif"} {
		p := filepath.Join(dir, "remote-"+b.ID+"-"+key+ext)
		if st, err := os.Stat(p); err == nil && st.Size() > 0 {
			return p, media.MIMEFor(p), nil
		}
	}

	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	data, mime, err := s.scrape.FetchImage(ctx, b.CoverURL)
	if err != nil {
		return "", "", err
	}
	ext := extForMIME(mime)
	if ext == "" {
		return "", "", errors.New("未知的封面格式")
	}
	p := filepath.Join(dir, "remote-"+b.ID+"-"+key+ext)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return "", "", err
	}
	if err := os.Rename(tmp, p); err != nil {
		return "", "", err
	}
	return p, media.MIMEFor(p), nil
}

func extForMIME(mime string) string {
	switch {
	case strings.HasPrefix(mime, "image/jpeg"):
		return ".jpg"
	case strings.HasPrefix(mime, "image/png"):
		return ".png"
	case strings.HasPrefix(mime, "image/webp"):
		return ".webp"
	case strings.HasPrefix(mime, "image/gif"):
		return ".gif"
	}
	return ""
}

// ------------------------------------------------------------------ audio

func (s *Server) handleAudio(w http.ResponseWriter, r *http.Request) {
	b, ch, ok := s.store.Chapter(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	abs := s.store.AbsPath(ch.RelPath)
	st, err := os.Stat(abs)
	if err != nil || st.IsDir() {
		http.Error(w, "音频文件不存在（可能已被移动或删除，请重新扫描）", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", firstNonEmpty(ch.MIME, media.MIMEFor(abs)))
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("Content-Disposition", "inline")
	_ = b
	http.ServeFile(w, r, abs)
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	b, ok := s.store.Book(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	if !feed.Mergeable(b) {
		http.Error(w, "这本书包含非 MP3 章节，请改用逐章的订阅地址", http.StatusConflict)
		return
	}
	paths := make([]string, 0, len(b.Chapters))
	for _, c := range b.Chapters {
		paths = append(paths, s.store.AbsPath(c.RelPath))
	}
	cr, err := newConcatReader(paths)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	defer cr.Close()

	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeContent(w, r, b.Title+".mp3", bookModTime(b), cr)
}

// bookModTime is the newest chapter modification time, used as the
// Last-Modified value so that caching stays correct when files change.
func bookModTime(b *model.Book) time.Time {
	var newest int64
	for _, c := range b.Chapters {
		if c.ModTime > newest {
			newest = c.ModTime
		}
	}
	if newest == 0 {
		return b.AddedAt
	}
	return time.Unix(newest, 0).UTC()
}

// -------------------------------------------------------------------- qr

func (s *Server) handleSubscribePage(w http.ResponseWriter, r *http.Request) {
	feedURL := r.URL.Query().Get("u")
	title := r.URL.Query().Get("t")
	if feedURL == "" {
		http.Error(w, "缺少 u 参数", http.StatusBadRequest)
		return
	}
	s.render(w, "subscribe.html", map[string]any{
		"Title":   firstNonEmpty(title, "订阅"),
		"FeedURL": feedURL,
		"AppLink": appLink(feedURL),
		"QR":      s.linker(r).URL("/qr?u=" + urlQueryEscape(feedURL)),
	})
}

// appLink returns the URL scheme that asks iOS to open the Podcasts app and
// subscribe to the feed.
func appLink(feedURL string) string {
	trimmed := strings.TrimPrefix(strings.TrimPrefix(feedURL, "https://"), "http://")
	return "pcast://" + trimmed
}

func (s *Server) handleQRCode(w http.ResponseWriter, r *http.Request) {
	u := r.URL.Query().Get("u")
	if u == "" {
		http.Error(w, "缺少 u 参数", http.StatusBadRequest)
		return
	}
	png, err := qrcode.Encode(u, qrcode.Medium, 512)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(png)
}

// -------------------------------------------------------------------- api

func (s *Server) handleAPIStats(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, s.store.Stats())
}

func (s *Server) handleAPIBooks(w http.ResponseWriter, r *http.Request) {
	books := s.store.Books()
	base := s.linker(r)
	type bookJSON struct {
		*model.Book
		FeedURL     string   `json:"feedUrl"`
		StreamURL   string   `json:"streamUrl"`
		ChapterURLs []string `json:"chapterUrls,omitempty"`
	}
	out := make([]bookJSON, 0, len(books))
	for _, b := range books {
		bj := bookJSON{Book: b, FeedURL: base.URL("/feed/book/" + b.ID + ".xml")}
		if feed.Mergeable(b) {
			bj.StreamURL = base.URL("/stream/" + b.ID)
		}
		for _, c := range b.Chapters {
			bj.ChapterURLs = append(bj.ChapterURLs, base.URL("/audio/"+c.ID))
		}
		out = append(out, bj)
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"libraryFeedUrl": base.URL("/feed/library.xml"),
		"books":          out,
	})
}

func (s *Server) handleAPIRescan(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	n, err := s.store.Rescan()
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"books":  n,
		"tookMs": time.Since(start).Milliseconds(),
		"stats":  s.store.Stats(),
	})
}

// handleRescanPage rescans and bounces back to the book list.
func (s *Server) handleRescanPage(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.Rescan()
	if err != nil {
		s.redirect(w, r, "/?err="+urlQueryEscape("扫描失败: "+err.Error()))
		return
	}
	s.redirect(w, r, "/?msg="+urlQueryEscape(fmt.Sprintf("扫描完成，共 %d 本有声书", n)))
}

func (s *Server) handleAPISearch(w http.ResponseWriter, r *http.Request) {
	q := scrape.Query{
		Title:    r.URL.Query().Get("q"),
		Author:   r.URL.Query().Get("author"),
		Language: firstNonEmpty(r.URL.Query().Get("lang"), s.cfg.Language),
		Country:  s.cfg.Country,
	}
	if strings.TrimSpace(q.Title) == "" {
		http.Error(w, "缺少 q 参数", http.StatusBadRequest)
		return
	}
	items, err := s.scrape.Search(r.Context(), q)
	if err != nil {
		s.writeJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"ok": true, "candidates": items})
}

// -------------------------------------------------------------------- util

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func contextWithTimeout(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}

func atoiFallback(s string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && n > 0 {
		return n
	}
	return def
}

// handleAPIFind is the JSON form of the site wide search.
func (s *Server) handleAPIFind(w http.ResponseWriter, r *http.Request) {
	res := s.store.Search(r.URL.Query().Get("q"), atoiFallback(r.URL.Query().Get("limit"), 200))
	link := s.linker(r)
	type hit struct {
		BookID    string  `json:"bookId"`
		BookTitle string  `json:"bookTitle"`
		Title     string  `json:"title"`
		Index     int     `json:"index"`
		Duration  float64 `json:"duration"`
		URL       string  `json:"url"`
	}
	out := make([]hit, 0, len(res.Chapters))
	for _, m := range res.Chapters {
		out = append(out, hit{
			BookID:    m.Book.ID,
			BookTitle: m.Book.Title,
			Title:     m.Chapter.Title,
			Index:     m.Chapter.Index,
			Duration:  m.Chapter.Duration,
			URL:       link.URL("/audio/" + m.Chapter.ID),
		})
	}
	books := make([]map[string]any, 0, len(res.Books))
	for _, b := range res.Books {
		books = append(books, map[string]any{
			"id": b.ID, "title": b.Title, "author": b.Author,
			"chapters": len(b.Chapters), "feedUrl": link.URL("/feed/book/" + b.ID + ".xml"),
		})
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"query":        res.Query,
		"books":        books,
		"chapters":     out,
		"chapterTotal": res.ChapterTotal,
	})
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func urlQueryEscape(s string) string {
	var b strings.Builder
	for _, r := range []byte(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.', r == '~', r == '/', r == ':':
			b.WriteByte(r)
		default:
			b.WriteString("%")
			const hexDigits = "0123456789ABCDEF"
			b.WriteByte(hexDigits[r>>4])
			b.WriteByte(hexDigits[r&0x0F])
		}
	}
	return b.String()
}
