package server

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"vocalis/internal/feed"
	"vocalis/internal/model"
	"vocalis/internal/scrape"
)

type bookCard struct {
	ID           string
	Title        string
	Author       string
	Narrator     string
	Chapters     int
	Duration     string
	Size         string
	CoverURL     string
	FeedURL      string
	SubscribeURL string
	RelDir       string
	Source       string
	Mergeable    bool
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
			ID:           b.ID,
			Title:        b.Title,
			Author:       b.Author,
			Narrator:     b.Narrator,
			Chapters:     len(b.Chapters),
			Duration:     humanDuration(b.TotalDuration),
			Size:         humanSize(b.TotalSize),
			CoverURL:     link.URL("/cover/" + b.ID),
			FeedURL:      feedURL,
			SubscribeURL: subscribeURL(link, feedURL, b.Title),
			RelDir:       b.RelDir,
			Source:       b.ScrapeSource,
			Mergeable:    feed.Mergeable(b),
		})
	}
	libraryFeed := link.URL("/feed/library.xml")
	s.render(w, "index.html", map[string]any{
		"Title":          s.cfg.LibraryTitle,
		"Stats":          st,
		"Books":          cards,
		"LibraryFeedURL": libraryFeed,
		"SubscribeURL":   subscribeURL(link, libraryFeed, "全部有声书"),
		"Msg":            r.URL.Query().Get("msg"),
		"Err":            r.URL.Query().Get("err"),
	})
}

func subscribeURL(link *feed.LinkTarget, feedURL, title string) string {
	u := link.URL("/subscribe")
	sep := "?"
	if strings.Contains(u, "?") {
		sep = "&"
	}
	return u + sep + "u=" + url.QueryEscape(feedURL) + "&t=" + url.QueryEscape(title)
}

func (s *Server) handleBookPage(w http.ResponseWriter, r *http.Request) {
	b, ok := s.store.Book(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.renderBook(w, r, b, nil, r.URL.Query().Get("msg"), r.URL.Query().Get("err"))
}

type searchChapterRow struct {
	BookID    string
	BookTitle string
	Title     string
	Path      string
	Season    int
	Episode   int
	Duration  string
	Size      string
	AudioURL  string
}

func (s *Server) handleSearchPage(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	res := s.store.Search(q, 400)
	link := s.linker(r)

	// Season numbers match the library feed so the two views agree.
	seasonOf := map[string]int{}
	for i, b := range s.store.Books() {
		seasonOf[b.ID] = i + 1
	}
	rows := make([]searchChapterRow, 0, len(res.Chapters))
	for _, m := range res.Chapters {
		rows = append(rows, searchChapterRow{
			BookID:    m.Book.ID,
			BookTitle: m.Book.Title,
			Title:     chapterTitle(m.Book, m.Chapter),
			Path:      m.Chapter.RelPath,
			Season:    seasonOf[m.Book.ID],
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
			ID:           b.ID,
			Title:        b.Title,
			Author:       b.Author,
			Narrator:     b.Narrator,
			Chapters:     len(b.Chapters),
			Duration:     humanDuration(b.TotalDuration),
			Size:         humanSize(b.TotalSize),
			CoverURL:     link.URL("/cover/" + b.ID),
			FeedURL:      feedURL,
			SubscribeURL: subscribeURL(link, feedURL, b.Title),
			RelDir:       b.RelDir,
			Source:       b.ScrapeSource,
			Mergeable:    feed.Mergeable(b),
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

func (s *Server) renderBook(w http.ResponseWriter, r *http.Request, b *model.Book, cands []scrape.Candidate, msg, errMsg string) {
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
		"AppLink":      appLink(feedURL),
		"SubscribeURL": subscribeURL(link, feedURL, b.Title),
		"LibraryTitle": s.cfg.LibraryTitle,
		"StreamURL":    streamURL,
		"Mergeable":    feed.Mergeable(b),
		"Candidates":   cands,
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
		CoverURL:    strings.TrimSpace(r.FormValue("coverUrl")),
	}
	if g := strings.TrimSpace(r.FormValue("genres")); g != "" {
		for _, part := range strings.FieldsFunc(g, func(r rune) bool { return r == ',' || r == '，' || r == '/' }) {
			if t := strings.TrimSpace(part); t != "" {
				ov.Genres = append(ov.Genres, t)
			}
		}
	}
	if r.FormValue("lock") != "" {
		ov.Locked = lockedFields(ov)
	}
	if err := s.store.SetOverride(id, ov); err != nil {
		s.redirect(w, r, "/book/"+id+"?err="+url.QueryEscape(err.Error()))
		return
	}
	s.redirect(w, r, "/book/"+id+"?msg="+url.QueryEscape("已保存"))
}

func lockedFields(ov *model.Override) []string {
	var out []string
	add := func(name, val string) {
		if val != "" {
			out = append(out, name)
		}
	}
	add("title", ov.Title)
	add("author", ov.Author)
	add("narrator", ov.Narrator)
	add("description", ov.Description)
	add("publisher", ov.Publisher)
	add("year", ov.Year)
	add("language", ov.Language)
	add("series", ov.Series)
	add("coverUrl", ov.CoverURL)
	if len(ov.Genres) > 0 {
		out = append(out, "genres")
	}
	return out
}

func (s *Server) handleBookScrape(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	b, ok := s.store.Book(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	q := scrape.Query{Title: b.Title, Author: b.Author, Language: s.cfg.Language, Country: s.cfg.Country}
	if t := strings.TrimSpace(r.FormValue("q")); t != "" {
		q.Title = t
	}
	if a := strings.TrimSpace(r.FormValue("author")); a != "" {
		q.Author = a
	}

	ctx, cancel := contextWithTimeout(r, 30*time.Second)
	defer cancel()
	best, err := s.scrape.Best(ctx, q)
	if err != nil {
		s.redirect(w, r, "/book/"+id+"?err="+url.QueryEscape(err.Error()))
		return
	}
	if best == nil {
		s.redirect(w, r, "/book/"+id+"?err="+url.QueryEscape("没有找到足够接近的结果，可以试试手动填写书名再刮削"))
		return
	}
	if err := s.applyCandidate(id, *best, false); err != nil {
		s.redirect(w, r, "/book/"+id+"?err="+url.QueryEscape(err.Error()))
		return
	}
	msg := "已应用 " + best.Source + " 的元数据：《" + best.Title + "》"
	s.redirect(w, r, "/book/"+id+"?msg="+url.QueryEscape(msg))
}

func (s *Server) handleBookSearch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	b, ok := s.store.Book(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	q := scrape.Query{
		Title:    firstNonEmpty(strings.TrimSpace(r.FormValue("q")), b.Title),
		Author:   strings.TrimSpace(r.FormValue("author")),
		Language: s.cfg.Language,
		Country:  s.cfg.Country,
	}
	ctx, cancel := contextWithTimeout(r, 30*time.Second)
	defer cancel()
	cands, err := s.scrape.Search(ctx, q)
	if err != nil {
		s.renderBook(w, r, b, nil, "", err.Error())
		return
	}
	if len(cands) > 8 {
		cands = cands[:8]
	}
	s.renderBook(w, r, b, cands, "找到 "+itoa(len(cands))+" 个候选", "")
}

func (s *Server) handleBookApply(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.store.Book(id); !ok {
		http.NotFound(w, r)
		return
	}
	cand := scrape.Candidate{
		Title:       strings.TrimSpace(r.FormValue("cand_title")),
		Author:      strings.TrimSpace(r.FormValue("cand_author")),
		Narrator:    strings.TrimSpace(r.FormValue("cand_narrator")),
		Description: strings.TrimSpace(r.FormValue("cand_description")),
		Publisher:   strings.TrimSpace(r.FormValue("cand_publisher")),
		Year:        strings.TrimSpace(r.FormValue("cand_year")),
		Language:    strings.TrimSpace(r.FormValue("cand_language")),
		CoverURL:    strings.TrimSpace(r.FormValue("cand_coverUrl")),
		Genres:      splitGenres(r.FormValue("cand_genres")),
		Source:      firstNonEmpty(strings.TrimSpace(r.FormValue("cand_source")), "手动选择"),
	}
	if cand.Title == "" {
		s.redirect(w, r, "/book/"+id+"?err="+url.QueryEscape("候选数据不完整"))
		return
	}
	if err := s.applyCandidate(id, cand, false); err != nil {
		s.redirect(w, r, "/book/"+id+"?err="+url.QueryEscape(err.Error()))
		return
	}
	s.redirect(w, r, "/book/"+id+"?msg="+url.QueryEscape("已应用选中的元数据"))
}

// applyCandidate merges a scraped result into the stored override, leaving
// user locked fields untouched.
func (s *Server) applyCandidate(id string, c scrape.Candidate, lock bool) error {
	ov := s.store.Override(id)
	if ov == nil {
		ov = &model.Override{}
	}
	locked := ov.LockedSet()

	set := func(field, cur string, next string) string {
		if locked[field] || strings.TrimSpace(next) == "" {
			return cur
		}
		return next
	}
	ov.Title = set("title", ov.Title, c.Title)
	ov.Author = set("author", ov.Author, c.Author)
	ov.Narrator = set("narrator", ov.Narrator, c.Narrator)
	ov.Description = set("description", ov.Description, c.Description)
	ov.Publisher = set("publisher", ov.Publisher, c.Publisher)
	ov.Year = set("year", ov.Year, c.Year)
	ov.Language = set("language", ov.Language, c.Language)
	ov.CoverURL = set("coverUrl", ov.CoverURL, c.CoverURL)
	if len(c.Genres) > 0 && !locked["genres"] {
		ov.Genres = c.Genres
	}
	ov.Source = firstNonEmpty(c.Source, ov.Source)
	ov.ScrapedAt = time.Now().UTC()
	if lock {
		ov.Locked = lockedFields(ov)
	}
	return s.store.SetOverride(id, ov)
}

func splitGenres(s string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '，' || r == '/' }) {
		if t := strings.TrimSpace(part); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func (s *Server) redirect(w http.ResponseWriter, r *http.Request, to string) {
	http.Redirect(w, r, to, http.StatusSeeOther)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
