// Package scrape looks up book metadata on public, key free APIs.
package scrape

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Query describes what we are looking for.
type Query struct {
	Title    string
	Author   string
	Language string
	Country  string
}

// Candidate is one metadata hit.
type Candidate struct {
	Title       string   `json:"title"`
	Author      string   `json:"author,omitempty"`
	Narrator    string   `json:"narrator,omitempty"`
	Description string   `json:"description,omitempty"`
	Publisher   string   `json:"publisher,omitempty"`
	Year        string   `json:"year,omitempty"`
	Language    string   `json:"language,omitempty"`
	Genres      []string `json:"genres,omitempty"`
	CoverURL    string   `json:"coverUrl,omitempty"`
	Source      string   `json:"source"`
	Score       float64  `json:"score"`
}

// Client queries the metadata providers.
type Client struct {
	hc        *http.Client
	userAgent string
	apiKey    string
	logf      func(string, ...any)
}

// SetGoogleBooksKey enables higher Google Books quotas.
func (c *Client) SetGoogleBooksKey(key string) { c.apiKey = strings.TrimSpace(key) }

// SetLogger receives non fatal provider errors.
func (c *Client) SetLogger(fn func(string, ...any)) { c.logf = fn }

func (c *Client) logfMsg(format string, args ...any) {
	if c.logf != nil {
		c.logf(format, args...)
	}
}

// New returns a client with sane timeouts.
func New() *Client {
	return &Client{
		hc:        &http.Client{Timeout: 15 * time.Second},
		userAgent: "vocalis/1.0 (+https://github.com/)",
	}
}

// Search fans out to every provider and returns the candidates, best first.
func (c *Client) Search(ctx context.Context, q Query) ([]Candidate, error) {
	if strings.TrimSpace(q.Title) == "" {
		return nil, errors.New("缺少书名，无法刮削")
	}
	type res struct {
		name  string
		items []Candidate
		err   error
	}
	ch := make(chan res, 3)
	go func() { items, err := c.googleBooks(ctx, q); ch <- res{"googlebooks", items, err} }()
	go func() { items, err := c.iTunes(ctx, q, "ebook"); ch <- res{"itunes-ebook", items, err} }()
	go func() { items, err := c.iTunes(ctx, q, "audiobook"); ch <- res{"itunes-audiobook", items, err} }()

	var all []Candidate
	var errs []string
	for i := 0; i < 3; i++ {
		r := <-ch
		if r.err != nil {
			errs = append(errs, r.name+": "+r.err.Error())
			c.logfMsg("刮削源失败", "source", r.name, "err", r.err)
			continue
		}
		all = append(all, r.items...)
	}
	if len(all) == 0 && len(errs) > 0 {
		return nil, fmt.Errorf("刮削失败: %s", strings.Join(errs, "; "))
	}
	for i := range all {
		all[i].Score = score(q, all[i])
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Score > all[j].Score })
	return dedupe(all), nil
}

// Best returns the highest scoring candidate, or nil when nothing looks close
// enough to be worth applying automatically.
func (c *Client) Best(ctx context.Context, q Query) (*Candidate, error) {
	items, err := c.Search(ctx, q)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 || items[0].Score < 0.55 {
		if len(items) == 0 {
			return nil, nil
		}
		return nil, nil
	}
	best := items[0]
	return &best, nil
}

// FetchImage downloads a cover image, refusing anything unreasonably large.
func (c *Client) FetchImage(ctx context.Context, rawURL string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", c.userAgent)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("封面下载失败: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 12<<20))
	if err != nil {
		return nil, "", err
	}
	if len(data) < 512 {
		return nil, "", errors.New("封面数据不完整")
	}
	mime := resp.Header.Get("Content-Type")
	if mime == "" || !strings.HasPrefix(mime, "image/") {
		mime = http.DetectContentType(data)
	}
	return data, mime, nil
}

// ------------------------------------------------------------ google books

type googleBooksResponse struct {
	Items []struct {
		VolumeInfo struct {
			Title         string   `json:"title"`
			Subtitle      string   `json:"subtitle"`
			Authors       []string `json:"authors"`
			Publisher     string   `json:"publisher"`
			PublishedDate string   `json:"publishedDate"`
			Description   string   `json:"description"`
			Categories    []string `json:"categories"`
			Language      string   `json:"language"`
			ImageLinks    struct {
				Thumbnail string `json:"thumbnail"`
				Small     string `json:"smallThumbnail"`
			} `json:"imageLinks"`
		} `json:"volumeInfo"`
	} `json:"items"`
}

func (c *Client) googleBooks(ctx context.Context, q Query) ([]Candidate, error) {
	parts := []string{}
	if t := strings.TrimSpace(q.Title); t != "" {
		parts = append(parts, "intitle:"+t)
	}
	if a := strings.TrimSpace(q.Author); a != "" {
		parts = append(parts, "inauthor:"+a)
	}
	v := url.Values{}
	v.Set("q", strings.Join(parts, " "))
	v.Set("maxResults", "10")
	v.Set("printType", "books")
	if c.apiKey != "" {
		v.Set("key", c.apiKey)
	}
	u := "https://www.googleapis.com/books/v1/volumes?" + v.Encode()

	var out googleBooksResponse
	if err := c.getJSON(ctx, u, &out); err != nil {
		return nil, err
	}
	var items []Candidate
	for _, it := range out.Items {
		vi := it.VolumeInfo
		title := strings.TrimSpace(vi.Title)
		if title == "" {
			continue
		}
		cand := Candidate{
			Title:       title,
			Author:      strings.Join(vi.Authors, " / "),
			Publisher:   strings.TrimSpace(vi.Publisher),
			Description: CleanDescription(vi.Description),
			Language:    strings.TrimSpace(vi.Language),
			Genres:      trimAll(vi.Categories),
			CoverURL:    httpsUpgrade(strings.TrimSpace(firstNonEmpty(vi.ImageLinks.Thumbnail, vi.ImageLinks.Small))),
			Source:      "googlebooks",
		}
		if len(vi.PublishedDate) >= 4 {
			cand.Year = vi.PublishedDate[:4]
		}
		items = append(items, cand)
	}
	return items, nil
}

// ---------------------------------------------------------------- itunes

type iTunesResponse struct {
	Results []struct {
		TrackName        string `json:"trackName"`
		CollectionName   string `json:"collectionName"`
		ArtistName       string `json:"artistName"`
		Description      string `json:"description"`
		ReleaseDate      string `json:"releaseDate"`
		PrimaryGenreName string `json:"primaryGenreName"`
		ArtworkUrl100    string `json:"artworkUrl100"`
		ArtworkUrl60     string `json:"artworkUrl60"`
		Country          string `json:"country"`
		Language         string `json:"language"`
	} `json:"results"`
}

func (c *Client) iTunes(ctx context.Context, q Query, media string) ([]Candidate, error) {
	term := strings.TrimSpace(q.Title)
	if a := strings.TrimSpace(q.Author); a != "" {
		term += " " + a
	}
	countries := []string{firstNonEmpty(q.Country, "cn"), "us"}
	var items []Candidate
	var lastErr error
	for _, country := range countries {
		v := url.Values{}
		v.Set("term", term)
		v.Set("media", media)
		v.Set("entity", media)
		v.Set("limit", "10")
		v.Set("country", country)
		u := "https://itunes.apple.com/search?" + v.Encode()

		var out iTunesResponse
		if err := c.getJSON(ctx, u, &out); err != nil {
			lastErr = err
			continue
		}
		for _, r := range out.Results {
			title := firstNonEmpty(r.TrackName, r.CollectionName)
			if strings.TrimSpace(title) == "" {
				continue
			}
			cand := Candidate{
				Title:       strings.TrimSpace(title),
				Author:      strings.TrimSpace(r.ArtistName),
				Description: CleanDescription(r.Description),
				CoverURL:    artworkLarge(r.ArtworkUrl100, r.ArtworkUrl60),
				Language:    strings.TrimSpace(r.Language),
				Source:      "itunes-" + media,
			}
			if r.PrimaryGenreName != "" {
				cand.Genres = []string{r.PrimaryGenreName}
			}
			if len(r.ReleaseDate) >= 4 {
				cand.Year = r.ReleaseDate[:4]
			}
			items = append(items, cand)
		}
		if len(items) > 0 {
			break
		}
	}
	if len(items) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return items, nil
}

// ----------------------------------------------------------------- helpers

func (c *Client) getJSON(ctx context.Context, u string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(body, dst)
}

func score(q Query, c Candidate) float64 {
	titleSim := similarity(q.Title, c.Title)
	if titleSim < 0.5 {
		return 0
	}
	if strings.TrimSpace(q.Author) == "" {
		return 0.85*titleSim + 0.15
	}
	authorSim := similarity(q.Author, c.Author)
	s := 0.7*titleSim + 0.3*authorSim
	if strings.EqualFold(strings.TrimSpace(q.Author), strings.TrimSpace(c.Author)) {
		s += 0.05
	}
	if s > 1 {
		s = 1
	}
	return s
}

func similarity(a, b string) float64 {
	x, y := normalize(a), normalize(b)
	if x == "" || y == "" {
		return 0
	}
	if x == y {
		return 1
	}
	if strings.Contains(x, y) || strings.Contains(y, x) {
		short, long := len([]rune(x)), len([]rune(y))
		if short > long {
			short, long = long, short
		}
		return 0.75 + 0.25*float64(short)/float64(long)
	}
	d := levenshtein([]rune(x), []rune(y))
	maxLen := len([]rune(x))
	if n := len([]rune(y)); n > maxLen {
		maxLen = n
	}
	if maxLen == 0 {
		return 0
	}
	return 1 - float64(d)/float64(maxLen)
}

func normalize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func levenshtein(a, b []rune) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = minInt(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

func minInt(vals ...int) int {
	m := vals[0]
	for _, v := range vals[1:] {
		if v < m {
			m = v
		}
	}
	return m
}

func dedupe(items []Candidate) []Candidate {
	seen := map[string]bool{}
	out := items[:0]
	for _, it := range items {
		key := normalize(it.Title) + "|" + normalize(it.Author)
		if key == "|" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, it)
	}
	return out
}

func trimAll(in []string) []string {
	var out []string
	for _, s := range in {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func httpsUpgrade(u string) string {
	u = strings.Replace(u, "http://", "https://", 1)
	return strings.Replace(u, "zoom=0", "zoom=1", 1)
}

var (
	brRe    = regexp.MustCompile(`(?i)<\s*br\s*/?\s*>`)
	blockRe = regexp.MustCompile(`(?i)</\s*(p|div|li|h[1-6])\s*>`)
	tagRe   = regexp.MustCompile(`<[^>]*>`)
)

// CleanDescription turns the HTML blurb returned by the APIs into plain text
// so that podcast clients never show raw markup.
func CleanDescription(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = brRe.ReplaceAllString(s, "\n")
	s = blockRe.ReplaceAllString(s, "\n")
	s = tagRe.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = strings.ReplaceAll(s, "\u00a0", " ")

	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			if blank {
				continue
			}
			blank = true
		} else {
			blank = false
		}
		out = append(out, l)
	}
	s = strings.TrimSpace(strings.Join(out, "\n"))
	const maxLen = 2000
	if r := []rune(s); len(r) > maxLen {
		s = strings.TrimSpace(string(r[:maxLen])) + "…"
	}
	return s
}

// artworkLarge rewrites Apple's 100x100 artwork URL to a 1024x1024 one.
func artworkLarge(urls ...string) string {
	for _, u := range urls {
		if strings.TrimSpace(u) == "" {
			continue
		}
		u = strings.Replace(u, "100x100bb", "1024x1024bb", 1)
		u = strings.Replace(u, "60x60bb", "1024x1024bb", 1)
		return u
	}
	return ""
}
