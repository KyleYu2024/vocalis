package server

import (
	"net/http"
	"net/url"
	"strings"

	"vocalis/internal/feed"
	"vocalis/internal/model"
)

type bookCard struct {
	ID        string
	Title     string
	Author    string
	Narrator  string
	Chapters  int
	Duration  string
	Size      string
	CoverURL  string
	FeedURL   string
	RelDir    string
	Mergeable bool
}

type chapterRow struct {
	Index    int
	Title    string
	Duration string
	Size     string
	AudioURL string
	Filename string
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	st := s.store.Stats()
	link := s.linker(r)
	books := s.store.Books()

	cards := make([]bookCard, 0, len(books))
	for _, b := range books {
		feedURL := link.URL("/feed/book/" + b.ID + ".xml")
		cards = append(cards, bookCard{
			ID:        b.ID,
			Title:     b.Title,
			Author:    b.Author,
			Narrator:  b.Narrator,
			Chapters:  len(b.Chapters),
			Duration:  humanDuration(b.TotalDuration),
			Size:      humanSize(b.TotalSize),
			CoverURL:  link.URL("/cover/" + b.ID),
			FeedURL:   feedURL,
			RelDir:    b.RelDir,
			Mergeable: feed.Mergeable(b),
		})
	}
	s.render(w, "index.html", map[string]any{
		"Title": s.cfg.LibraryTitle,
		"Stats": st,
		"Books": cards,
		"Msg":   r.URL.Query().Get("msg"),
		"Err":   r.URL.Query().Get("err"),
	})
}

func (s *Server) handleBookPage(w http.ResponseWriter, r *http.Request) {
	b, ok := s.store.Book(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.renderBook(w, r, b, r.URL.Query().Get("msg"), r.URL.Query().Get("err"))
}

type searchChapterRow struct {
	BookID    string
	BookTitle string
	Title     string
	Path      string
	Episode   int
	Duration  string
	Size      string
	AudioURL  string
}

func (s *Server) handleSearchPage(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	res := s.store.Search(q, 400)
	link := s.linker(r)

	rows := make([]searchChapterRow, 0, len(res.Chapters))
	for _, m := range res.Chapters {
		rows = append(rows, searchChapterRow{
			BookID:    m.Book.ID,
			BookTitle: m.Book.Title,
			Title:     chapterTitle(m.Book, m.Chapter),
			Path:      m.Chapter.RelPath,
			Episode:   m.Chapter.Index + 1,
			Duration:  humanDuration(m.Chapter.Duration),
			Size:      humanSize(m.Chapter.Size),
			AudioURL:  link.URL("/audio/" + m.Chapter.ID),
		})
	}
	cards := make([]bookCard, 0, len(res.Books))
	for _, b := range res.Books {
		feedURL := link.URL("/feed/book/" + b.ID + ".xml")
		cards = append(cards, bookCard{
			ID:        b.ID,
			Title:     b.Title,
			Author:    b.Author,
			Narrator:  b.Narrator,
			Chapters:  len(b.Chapters),
			Duration:  humanDuration(b.TotalDuration),
			Size:      humanSize(b.TotalSize),
			CoverURL:  link.URL("/cover/" + b.ID),
			FeedURL:   feedURL,
			RelDir:    b.RelDir,
			Mergeable: feed.Mergeable(b),
		})
	}
	s.render(w, "search.html", map[string]any{
		"Title":        "搜索 " + q,
		"LibraryTitle": s.cfg.LibraryTitle,
		"Stats":        s.store.Stats(),
		"Query":        q,
		"Books":        cards,
		"Chapters":     rows,
		"ChapterTotal": res.ChapterTotal,
		"Truncated":    res.ChapterTotal > len(rows),
	})
}

func (s *Server) renderBook(w http.ResponseWriter, r *http.Request, b *model.Book, msg, errMsg string) {
	link := s.linker(r)
	feedURL := link.URL("/feed/book/" + b.ID + ".xml")
	rows := make([]chapterRow, 0, len(b.Chapters))
	for _, c := range b.Chapters {
		rows = append(rows, chapterRow{
			Index:    c.Index + 1,
			Title:    chapterTitle(b, c),
			Duration: humanDuration(c.Duration),
			Size:     humanSize(c.Size),
			AudioURL: link.URL("/audio/" + c.ID),
			Filename: c.RelPath,
		})
	}
	streamURL := ""
	if feed.Mergeable(b) {
		streamURL = link.URL("/stream/" + b.ID)
	}
	ov := s.store.Override(b.ID)
	if ov == nil {
		ov = &model.Override{}
	}
	s.render(w, "book.html", map[string]any{
		"Title":        b.Title,
		"Book":         b,
		"Chapters":     rows,
		"CoverURL":     link.URL("/cover/" + b.ID),
		"FeedURL":      feedURL,
		"LibraryTitle": s.cfg.LibraryTitle,
		"StreamURL":    streamURL,
		"Mergeable":    feed.Mergeable(b),
		"Msg":          msg,
		"Err":          errMsg,
		"Stats":        s.store.Stats(),
		"Override":     ov,
		"Genres":       strings.Join(b.Genres, ", "),
	})
}

func chapterTitle(b *model.Book, c model.Chapter) string {
	if strings.TrimSpace(c.Title) == "" {
		return c.RelPath
	}
	return c.Title
}

func (s *Server) handleBookSave(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.store.Book(id); !ok {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "表单解析失败", http.StatusBadRequest)
		return
	}
	ov := &model.Override{
		Title:       strings.TrimSpace(r.FormValue("title")),
		Author:      strings.TrimSpace(r.FormValue("author")),
		Narrator:    strings.TrimSpace(r.FormValue("narrator")),
		Description: strings.TrimSpace(r.FormValue("description")),
		Publisher:   strings.TrimSpace(r.FormValue("publisher")),
		Year:        strings.TrimSpace(r.FormValue("year")),
		Language:    strings.TrimSpace(r.FormValue("language")),
		Series:      strings.TrimSpace(r.FormValue("series")),
	}
	// 封面只能来自本地文件；这里保留之前记录的封面地址，避免手动保存把它清掉。
	if prev := s.store.Override(id); prev != nil {
		ov.CoverURL = prev.CoverURL
	}
	if g := strings.TrimSpace(r.FormValue("genres")); g != "" {
		for _, part := range strings.FieldsFunc(g, func(r rune) bool { return r == ',' || r == '，' || r == '/' }) {
			if t := strings.TrimSpace(part); t != "" {
				ov.Genres = append(ov.Genres, t)
			}
		}
	}
	if err := s.store.SetOverride(id, ov); err != nil {
		s.redirect(w, r, "/book/"+id+"?err="+url.QueryEscape(err.Error()))
		return
	}
	s.redirect(w, r, "/book/"+id+"?msg="+url.QueryEscape("已保存"))
}

func (s *Server) redirect(w http.ResponseWriter, r *http.Request, to string) {
	http.Redirect(w, r, to, http.StatusSeeOther)
}
