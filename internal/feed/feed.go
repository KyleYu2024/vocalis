// Package feed renders podcast RSS documents.
package feed

import (
	"encoding/xml"
	"fmt"
	"strings"
	"time"

	"vocalis/internal/media"
	"vocalis/internal/model"
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
