// Package feed renders podcast RSS documents.
package feed

import (
	"encoding/xml"
	"fmt"
	"sort"
	"strings"
	"time"

	"vocalis/internal/media"
	"vocalis/internal/model"
	"vocalis/internal/natsort"
)

// Linker turns a server relative path into an absolute, token carrying URL.
type Linker interface {
	URL(path string) string
}

// Item is one RSS episode.
type Item struct {
	Title       string
	GUID        string
	URL         string
	MIME        string
	Length      int64
	PubDate     string
	Duration    string
	Episode     int
	Season      int
	EpisodeType string
	Description string
	CoverURL    string
	ChaptersURL string
	// Compact drops the duplicated summary/content fields, which keeps a feed
	// with thousands of episodes from ballooning.
	Compact bool
}

// Feed is a whole podcast channel.
type Feed struct {
	Title       string
	Link        string
	Description string
	Language    string
	Author      string
	OwnerEmail  string
	CoverURL    string
	Explicit    string
	Type        string
	SelfURL     string
	Category    []string
	Items       []Item
}

// Mergeable reports whether the chapters can be streamed as one continuous
// MPEG audio file, which lets a whole book show up as a single episode.
func Mergeable(b *model.Book) bool {
	if len(b.Chapters) == 0 {
		return false
	}
	for _, c := range b.Chapters {
		if !strings.EqualFold(ext(c.RelPath), ".mp3") {
			return false
		}
	}
	return true
}

func ext(p string) string {
	i := strings.LastIndexByte(p, '.')
	if i < 0 {
		return ""
	}
	return p[i:]
}

// BookFeed renders one book as its own podcast, with one episode per chapter.
func BookFeed(b *model.Book, base *LinkTarget, lang string) *Feed {
	cover := base.coverURL(b)
	f := &Feed{
		Title:       b.Title,
		Link:        base.URL("/"),
		Description: bookDescription(b),
		Language:    firstNonEmpty(lang, b.Language, "zh-cn"),
		Author:      firstNonEmpty(b.Author, "未知作者"),
		CoverURL:    cover,
		Explicit:    "no",
		Type:        "serial",
		SelfURL:     base.URL("/feed/book/" + b.ID + ".xml"),
		Category:    []string{"Arts", "Books"},
	}
	start := b.AddedAt
	if start.IsZero() {
		start = time.Now().UTC()
	}
	for i, c := range b.Chapters {
		f.Items = append(f.Items, Item{
			Title:       chapterLabel(b, c),
			GUID:        "vocalis:chapter:" + c.ID,
			URL:         base.URL("/audio/" + c.ID),
			MIME:        firstNonEmpty(c.MIME, media.MIMEFor(c.RelPath)),
			Length:      c.Size,
			PubDate:     start.Add(time.Duration(i) * time.Minute).Format(time.RFC1123Z),
			Duration:    durationString(c.Duration),
			Episode:     i + 1,
			Season:      1,
			EpisodeType: "full",
			Description: chapterDescription(b, c, i),
			CoverURL:    cover,
		})
	}
	return f
}

// Library feed layouts.
const (
	// LibraryModeBooks puts one whole book into one episode.
	LibraryModeBooks = "books"
	// LibraryModeChapters keeps every chapter as its own episode and groups
	// them with itunes:season, so a subscription can still browse chapter by
	// chapter and switch between books with the season picker.
	LibraryModeChapters = "chapters"
)

// LibraryOptions configures the library wide feed.
type LibraryOptions struct {
	Title       string
	Description string
	Language    string
	Mode        string
}

// LibraryFeed renders the whole library as one podcast.
func LibraryFeed(books []*model.Book, base *LinkTarget, opt LibraryOptions) *Feed {
	if opt.Mode == "" {
		opt.Mode = LibraryModeChapters
	}
	f := &Feed{
		Title:       opt.Title,
		Link:        base.URL("/"),
		Description: opt.Description,
		Language:    firstNonEmpty(opt.Language, "zh-cn"),
		Author:      opt.Title,
		Explicit:    "no",
		Type:        "serial",
		SelfURL:     base.URL("/feed/library.xml"),
		Category:    []string{"Arts", "Books"},
	}
	sorted := append([]*model.Book(nil), books...)
	sortBooks(sorted)
	if len(sorted) > 0 {
		f.CoverURL = base.coverURL(sorted[0])
	}
	if opt.Mode == LibraryModeChapters {
		buildChapterEpisodes(f, sorted, base)
		return f
	}
	buildBookEpisodes(f, sorted, base)
	return f
}

// buildChapterEpisodes emits one episode per chapter, using itunes:season to
// keep the books apart.
func buildChapterEpisodes(f *Feed, sorted []*model.Book, base *LinkTarget) {
	// Publication times must be strictly increasing across the whole feed,
	// otherwise podcast clients cannot tell which episode follows which (this
	// is what makes "play the next episode" work). We lay the episodes out on
	// a timeline using their real playtime, starting far enough in the past
	// that the newest chapter still looks recent.
	at := feedStart(sorted)
	for bi, b := range sorted {
		for i, c := range b.Chapters {
			pub := at
			step := c.Duration
			if step <= 0 {
				step = 60
			}
			at = at.Add(time.Duration(step * float64(time.Second)))
			f.Items = append(f.Items, Item{
				Title:       libraryChapterTitle(b, c),
				GUID:        "vocalis:chapter:" + c.ID,
				URL:         base.URL("/audio/" + c.ID),
				MIME:        firstNonEmpty(c.MIME, media.MIMEFor(c.RelPath)),
				Length:      c.Size,
				PubDate:     pub.UTC().Format(time.RFC1123Z),
				Duration:    durationString(c.Duration),
				Episode:     i + 1,
				Season:      bi + 1,
				EpisodeType: "full",
				Description: shortDescription(b, c, i),
				Compact:     true,
			})
		}
	}
}

// feedStart picks the oldest timestamp that still ends up close to "now" once
// every chapter is laid out on the timeline.
func feedStart(books []*model.Book) time.Time {
	var total float64
	earliest := time.Now().UTC()
	for _, b := range books {
		total += b.TotalDuration
		if !b.AddedAt.IsZero() && b.AddedAt.Before(earliest) {
			earliest = b.AddedAt
		}
	}
	start := time.Now().UTC().Add(-time.Duration(total * float64(time.Second)))
	if !earliest.IsZero() && earliest.Before(start) {
		return earliest
	}
	return start
}

// buildBookEpisodes puts a whole book into a single episode.
func buildBookEpisodes(f *Feed, sorted []*model.Book, base *LinkTarget) {
	episode := 0
	for _, b := range sorted {
		cover := base.coverURL(b)
		if Mergeable(b) {
			episode++
			f.Items = append(f.Items, Item{
				Title:       b.Title,
				GUID:        "vocalis:book:" + b.ID,
				URL:         base.URL("/stream/" + b.ID),
				MIME:        "audio/mpeg",
				Length:      mergedLength(b),
				PubDate:     b.AddedAt.UTC().Format(time.RFC1123Z),
				Duration:    durationString(b.TotalDuration),
				Episode:     episode,
				Season:      1,
				EpisodeType: "full",
				Description: bookDescription(b),
				CoverURL:    cover,
				ChaptersURL: chaptersURL(base, b),
			})
			continue
		}
		for i, c := range b.Chapters {
			episode++
			f.Items = append(f.Items, Item{
				Title:       b.Title + " · " + chapterLabel(b, c),
				GUID:        "vocalis:chapter:" + c.ID,
				URL:         base.URL("/audio/" + c.ID),
				MIME:        firstNonEmpty(c.MIME, media.MIMEFor(c.RelPath)),
				Length:      c.Size,
				PubDate:     b.AddedAt.Add(time.Duration(i) * time.Minute).UTC().Format(time.RFC1123Z),
				Duration:    durationString(c.Duration),
				Episode:     episode,
				Season:      1,
				EpisodeType: "full",
				Description: chapterDescription(b, c, i),
				CoverURL:    cover,
			})
		}
	}
}

// libraryChapterTitle prefixes the book name so that a flat episode list (for
// example the "latest episodes" screen) still says which book it belongs to.
func libraryChapterTitle(b *model.Book, c model.Chapter) string {
	title := chapterLabel(b, c)
	if strings.Contains(title, b.Title) {
		return title
	}
	return b.Title + " · " + title
}

func shortDescription(b *model.Book, c model.Chapter, i int) string {
	return fmt.Sprintf("%s 第 %d / %d 集", b.Title, i+1, len(b.Chapters))
}

func chaptersURL(base *LinkTarget, b *model.Book) string {
	if b.TotalDuration <= 0 || len(b.Chapters) < 2 {
		return ""
	}
	return base.URL("/chapters/" + b.ID + ".json")
}

func mergedLength(b *model.Book) int64 {
	var n int64
	for _, c := range b.Chapters {
		n += c.Size
	}
	return n
}

func sortBooks(books []*model.Book) {
	sort.Slice(books, func(i, j int) bool { return natsort.Less(books[i].RelDir, books[j].RelDir) })
}

func nowRFC1123Z() string { return time.Now().UTC().Format(time.RFC1123Z) }

func bookDescription(b *model.Book) string {
	parts := []string{}
	if b.Author != "" {
		parts = append(parts, "作者："+b.Author)
	}
	if b.Narrator != "" {
		parts = append(parts, "演播："+b.Narrator)
	}
	if b.Publisher != "" {
		parts = append(parts, "出版："+b.Publisher)
	}
	if b.Year != "" {
		parts = append(parts, "年份："+b.Year)
	}
	if n := len(b.Chapters); n > 0 {
		parts = append(parts, fmt.Sprintf("共 %d 个音频文件", n))
	}
	head := strings.Join(parts, " · ")
	if b.Description != "" {
		if head != "" {
			return head + "\n\n" + b.Description
		}
		return b.Description
	}
	if head == "" {
		return b.Title
	}
	return head
}

func chapterDescription(b *model.Book, c model.Chapter, i int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s · 第 %d / %d 章", b.Title, i+1, len(b.Chapters))
	if b.Author != "" {
		sb.WriteString(" · 作者：" + b.Author)
	}
	if c.Duration > 0 {
		fmt.Fprintf(&sb, " · 时长：%s", durationString(c.Duration))
	}
	return sb.String()
}

func chapterLabel(b *model.Book, c model.Chapter) string {
	title := strings.TrimSpace(c.Title)
	if title == "" {
		return fmt.Sprintf("第 %d 章", c.Index+1)
	}
	if title == b.Title {
		return fmt.Sprintf("第 %d 章", c.Index+1)
	}
	return title
}

// durationString formats seconds the way podcast clients expect.
func durationString(seconds float64) string {
	if seconds <= 0 {
		return ""
	}
	return fmt.Sprintf("%d", int64(seconds+0.5))
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// ---------------------------------------------------------------- rendering

// Escape XML-escapes a string for use in text or attributes.
func Escape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// ChapterMark is one entry of a Podcasting 2.0 chapters document.
type ChapterMark struct {
	StartTime int    `json:"startTime"`
	Title     string `json:"title"`
	Img       string `json:"img,omitempty"`
}

// ChaptersDoc is the JSON document behind <podcast:chapters>.
type ChaptersDoc struct {
	Version  string        `json:"version"`
	Chapters []ChapterMark `json:"chapters"`
}

// BuildChapters turns chapter durations into absolute start times.
func BuildChapters(b *model.Book, coverURL string) ChaptersDoc {
	doc := ChaptersDoc{Version: "1.2.0"}
	t := 0.0
	for _, c := range b.Chapters {
		doc.Chapters = append(doc.Chapters, ChapterMark{
			StartTime: int(t + 0.5),
			Title:     chapterLabel(b, c),
			Img:       coverURL,
		})
		t += c.Duration
	}
	return doc
}

// LinkTarget renders absolute URLs for a given base address.
type LinkTarget struct {
	Base  string
	Token string
}

// URL joins the base address with a server path and appends the access token.
func (l *LinkTarget) URL(path string) string {
	base := strings.TrimRight(l.Base, "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u := base + path
	if l.Token != "" {
		sep := "?"
		if strings.Contains(u, "?") {
			sep = "&"
		}
		u += sep + "token=" + l.Token
	}
	return u
}

func (l *LinkTarget) coverURL(b *model.Book) string {
	return l.URL("/cover/" + b.ID)
}
