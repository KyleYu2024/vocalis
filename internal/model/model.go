package model

import "time"

// Chapter is a single audio file that belongs to a book.
type Chapter struct {
	ID       string  `json:"id"`
	Index    int     `json:"index"`
	Title    string  `json:"title"`
	RelPath  string  `json:"relPath"`
	Size     int64   `json:"size"`
	Duration float64 `json:"duration"`
	MIME     string  `json:"mime"`
	ModTime  int64   `json:"modTime"`
}

// Book is one audiobook, made of one or more chapters.
type Book struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Author      string   `json:"author,omitempty"`
	Narrator    string   `json:"narrator,omitempty"`
	Description string   `json:"description,omitempty"`
	Publisher   string   `json:"publisher,omitempty"`
	Year        string   `json:"year,omitempty"`
	Language    string   `json:"language,omitempty"`
	Series      string   `json:"series,omitempty"`
	SeriesIndex int      `json:"seriesIndex,omitempty"`
	Genres      []string `json:"genres,omitempty"`

	// CoverFile is an absolute path to a local cover image, CoverURL a remote
	// image that gets downloaded and cached on first use.
	CoverFile string `json:"coverFile,omitempty"`
	CoverURL  string `json:"coverUrl,omitempty"`
	// CoverMIME describes the bytes behind CoverFile/CoverURL once resolved.
	CoverMIME string `json:"coverMime,omitempty"`

	RelDir string `json:"relDir"`

	Chapters      []Chapter `json:"chapters"`
	TotalDuration float64   `json:"totalDuration"`
	TotalSize     int64     `json:"totalSize"`

	// ScrapeSource/ScrapedAt are legacy fields kept so that override files
	// written by older versions (when online scraping existed) still load.
	ScrapeSource string    `json:"scrapeSource,omitempty"`
	ScrapedAt    time.Time `json:"scrapedAt,omitempty"`
	AddedAt      time.Time `json:"addedAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	// Fingerprint changes whenever the file layout changes, used to skip rescans.
	Fingerprint string `json:"fingerprint"`
}

// ChapterCount returns how many audio files the book has.
func (b *Book) ChapterCount() int { return len(b.Chapters) }

// SingleFile reports whether the whole book lives in one audio file.
func (b *Book) SingleFile() bool { return len(b.Chapters) == 1 }

// Override carries user edits that must survive a rescan of the library
// directory. Only non-empty fields win over scanned values.
type Override struct {
	Title       string    `json:"title,omitempty"`
	Author      string    `json:"author,omitempty"`
	Narrator    string    `json:"narrator,omitempty"`
	Description string    `json:"description,omitempty"`
	Publisher   string    `json:"publisher,omitempty"`
	Year        string    `json:"year,omitempty"`
	Language    string    `json:"language,omitempty"`
	Series      string    `json:"series,omitempty"`
	SeriesIndex int       `json:"seriesIndex,omitempty"`
	Genres      []string  `json:"genres,omitempty"`
	CoverURL    string    `json:"coverUrl,omitempty"`
	Source      string    `json:"source,omitempty"`
	ScrapedAt   time.Time `json:"scrapedAt,omitempty"`

	// Locked is a legacy field from the scraping era; it is only read back for
	// compatibility with older override files.
	Locked []string `json:"locked,omitempty"`
}

// LockedSet returns the locked fields as a set.
func (o *Override) LockedSet() map[string]bool {
	if o == nil {
		return nil
	}
	m := make(map[string]bool, len(o.Locked))
	for _, f := range o.Locked {
		m[f] = true
	}
	return m
}

// Apply merges the override into a book.
func (o *Override) Apply(b *Book) {
	if o == nil {
		return
	}
	if o.Title != "" {
		b.Title = o.Title
	}
	if o.Author != "" {
		b.Author = o.Author
	}
	if o.Narrator != "" {
		b.Narrator = o.Narrator
	}
	if o.Description != "" {
		b.Description = o.Description
	}
	if o.Publisher != "" {
		b.Publisher = o.Publisher
	}
	if o.Year != "" {
		b.Year = o.Year
	}
	if o.Language != "" {
		b.Language = o.Language
	}
	if o.Series != "" {
		b.Series = o.Series
	}
	if o.SeriesIndex != 0 {
		b.SeriesIndex = o.SeriesIndex
	}
	if len(o.Genres) > 0 {
		b.Genres = o.Genres
	}
	if o.CoverURL != "" {
		b.CoverURL = o.CoverURL
	}
	if o.Source != "" {
		b.ScrapeSource = o.Source
	}
	if !o.ScrapedAt.IsZero() {
		b.ScrapedAt = o.ScrapedAt
	}
}
