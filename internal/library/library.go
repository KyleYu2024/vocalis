// Package library turns a directory tree of audio files into a list of books.
package library

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"vocalis/internal/media"
	"vocalis/internal/model"
	"vocalis/internal/natsort"
)

// Layout selects how directories are interpreted.
const (
	LayoutAuto   = "auto"   // guess from the folder structure
	LayoutFlat   = "flat"   // every top level folder is one book
	LayoutNested = "nested" // every folder that directly holds audio is a book
	LayoutSingle = "single" // the library root itself is one single book
)

// Options configures a Scanner.
type Options struct {
	Root     string
	Layout   string
	CoverDir string
	MaxDepth int
	// SingleTitle overrides the book title when Layout is LayoutSingle. Inside
	// a container the root folder is usually just "/audiobooks", which would
	// otherwise become the book name.
	SingleTitle string
	// Progress is called periodically while scanning, with the number of
	// chapters processed so far.
	Progress func(chapters int)
}

// Scanner walks a library root and produces books.
type Scanner struct {
	opt       Options
	root      string
	processed int
}

func (s *Scanner) tick() {
	s.processed++
	if s.opt.Progress != nil && s.processed%200 == 0 {
		s.opt.Progress(s.processed)
	}
}

// NewScanner returns a scanner for the given options.
func NewScanner(opt Options) *Scanner {
	if opt.Layout == "" {
		opt.Layout = LayoutAuto
	}
	if opt.MaxDepth <= 0 {
		opt.MaxDepth = 8
	}
	return &Scanner{opt: opt, root: filepath.Clean(opt.Root)}
}

// Result is what a scan produced.
type Result struct {
	Books    []*model.Book
	Warnings []string
}

// Scan walks the library. prev may hold books from an earlier scan so that
// unchanged chapter metadata (tags, duration) does not have to be re-read.
func (s *Scanner) Scan(prev map[string]*model.Book) (*Result, error) {
	res := &Result{}
	if prev == nil {
		prev = map[string]*model.Book{}
	}
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, fmt.Errorf("read library root: %w", err)
	}

	var roots []string     // directories that each hold one book
	var loneFiles []string // audio files sitting directly in the root
	if s.opt.Layout == LayoutSingle {
		if s.subtreeHasAudio(s.root, 0) {
			roots = append(roots, s.root)
		}
		res.Books = s.buildRoots(roots, loneFiles, prev)
		return res, nil
	}
	for _, e := range entries {
		if ignoreName(e.Name()) {
			continue
		}
		full := filepath.Join(s.root, e.Name())
		if e.IsDir() {
			switch s.opt.Layout {
			case LayoutFlat:
				if s.subtreeHasAudio(full, 0) {
					roots = append(roots, full)
				}
			default:
				roots = append(roots, s.bookRootsFrom(full, 0)...)
			}
			continue
		}
		if media.IsAudio(full) {
			loneFiles = append(loneFiles, full)
		}
	}

	res.Books = s.buildRoots(roots, loneFiles, prev)
	return res, nil
}

func (s *Scanner) buildRoots(roots, loneFiles []string, prev map[string]*model.Book) []*model.Book {
	sort.Strings(roots)
	sort.Strings(loneFiles)
	var out []*model.Book
	for _, r := range roots {
		if b := s.buildBook(r, prev); b != nil {
			out = append(out, b)
		}
	}
	for _, f := range loneFiles {
		if b := s.buildLoneBook(f, prev); b != nil {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return natsort.Less(out[i].RelDir, out[j].RelDir) })
	return out
}

// bookRootsFrom decides which directories below dir each hold one book.
func (s *Scanner) bookRootsFrom(dir string, depth int) []string {
	files, dirs := listDir(dir)
	for _, f := range files {
		if media.IsAudio(f) {
			return []string{dir}
		}
	}
	if depth >= s.opt.MaxDepth {
		return nil
	}

	var withAudio []string
	for _, d := range dirs {
		if s.subtreeHasAudio(d, depth+1) {
			withAudio = append(withAudio, d)
		}
	}
	if len(withAudio) == 0 {
		return nil
	}
	if len(withAudio) == 1 {
		child := withAudio[0]
		name := filepath.Base(child)
		// "书名/CD1" or "书名/正文" means the parent is the book; anything
		// else is treated as a container such as "作者/书名", so we keep
		// descending until we reach the folder that holds the audio.
		if isGenericDir(name) || LooksLikePart(name) {
			return []string{dir}
		}
		return s.bookRootsFrom(child, depth+1)
	}
	allParts := true
	for _, c := range withAudio {
		if !LooksLikePart(filepath.Base(c)) {
			allParts = false
			break
		}
	}
	if allParts {
		return []string{dir}
	}
	// "×××【全集】/勇气篇/坚韧篇/…" is one book split into thematic parts.
	if IsCollectionDir(filepath.Base(dir)) {
		return []string{dir}
	}
	var out []string
	for _, d := range dirs {
		out = append(out, s.bookRootsFrom(d, depth+1)...)
	}
	return out
}

func (s *Scanner) subtreeHasAudio(dir string, depth int) bool {
	if depth > s.opt.MaxDepth {
		return false
	}
	files, dirs := listDir(dir)
	for _, f := range files {
		if media.IsAudio(f) {
			return true
		}
	}
	for _, d := range dirs {
		if s.subtreeHasAudio(d, depth+1) {
			return true
		}
	}
	return false
}

// collectAudio returns every audio file below dir, ordered naturally by path.
func (s *Scanner) collectAudio(dir string, depth int) []string {
	if depth > s.opt.MaxDepth {
		return nil
	}
	files, dirs := listDir(dir)
	var out []string
	for _, f := range files {
		if media.IsAudio(f) {
			out = append(out, f)
		}
	}
	for _, d := range dirs {
		out = append(out, s.collectAudio(d, depth+1)...)
	}
	sort.Slice(out, func(i, j int) bool { return natsort.Less(out[i], out[j]) })
	return out
}

func (s *Scanner) buildBook(dir string, prev map[string]*model.Book) *model.Book {
	relDir, err := filepath.Rel(s.root, dir)
	if err != nil {
		return nil
	}
	if relDir == "." {
		// layout=single: the library root itself is the book, so its relative
		// path is empty and the title comes from the root folder name.
		relDir = ""
	}
	abs := s.collectAudio(dir, 0)
	if len(abs) == 0 {
		return nil
	}
	id := shortID("dir:" + filepath.ToSlash(relDir))
	old := prev[filepath.ToSlash(relDir)]
	oldChapters := chapterIndex(old)

	b := &model.Book{
		ID:        id,
		RelDir:    filepath.ToSlash(relDir),
		AddedAt:   nowUTC(),
		UpdatedAt: nowUTC(),
		Language:  "",
	}
	if old != nil {
		b.AddedAt = old.AddedAt
	}
	b.Chapters = s.buildChapters(id, abs, oldChapters)
	b.Title, b.Author = ParseBookName(filepath.Base(dir))
	if relDir == "" && s.opt.SingleTitle != "" {
		b.Title, b.Author = ParseBookName(s.opt.SingleTitle)
	}
	s.applyEmbedded(b, abs)
	s.applyLocalMeta(b, dir)
	s.resolveCover(b, dir, abs)
	finalize(b)
	return b
}

func (s *Scanner) buildLoneBook(file string, prev map[string]*model.Book) *model.Book {
	rel, err := filepath.Rel(s.root, file)
	if err != nil {
		return nil
	}
	rel = filepath.ToSlash(rel)
	id := shortID("file:" + rel)
	old := prev[rel]
	oldChapters := chapterIndex(old)

	b := &model.Book{
		ID:        id,
		RelDir:    "",
		AddedAt:   nowUTC(),
		UpdatedAt: nowUTC(),
	}
	if old != nil {
		b.AddedAt = old.AddedAt
	}
	b.Chapters = s.buildChapters(id, []string{file}, oldChapters)
	b.Title, b.Author = ParseBookName(strings.TrimSuffix(filepath.Base(file), filepath.Ext(file)))
	s.applyEmbedded(b, []string{file})
	finalize(b)
	return b
}

func (s *Scanner) buildChapters(bookID string, abs []string, reuse map[string]model.Chapter) []model.Chapter {
	out := make([]model.Chapter, 0, len(abs))
	for i, p := range abs {
		rel, err := filepath.Rel(s.root, p)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		if old, ok := reuse[rel]; ok && old.Size == st.Size() && old.ModTime == st.ModTime().Unix() {
			old.Index = i
			out = append(out, old)
			continue
		}
		s.tick()
		c := model.Chapter{
			ID:      shortID("ch:" + rel),
			Index:   i,
			Title:   ChapterTitle(filepath.Base(p)),
			RelPath: rel,
			Size:    st.Size(),
			MIME:    media.MIMEFor(p),
			ModTime: st.ModTime().Unix(),
		}
		if emb, err := media.ReadEmbedded(p); err == nil && emb.Title != "" && emb.Title != emb.Album {
			c.Title = ChapterTitle(emb.Title)
		}
		c.Duration = round2(media.Duration(p))
		out = append(out, c)
	}
	return out
}

func (s *Scanner) applyEmbedded(b *model.Book, abs []string) {
	for _, p := range abs[:min(3, len(abs))] {
		emb, err := media.ReadEmbedded(p)
		if err != nil {
			continue
		}
		if b.Title == "" {
			for _, cand := range []string{emb.Album, emb.Title} {
				if t := cleanTag(cand); t != "" && t != b.Title {
					b.Title = t
					break
				}
			}
		}
		if b.Author == "" {
			b.Author = cleanTag(firstNonEmpty(emb.AlbumArtist, emb.Artist))
		}
		if b.Narrator == "" {
			b.Narrator = cleanTag(emb.Narrator)
		}
		if b.Year == "" && emb.Year != "" {
			b.Year = emb.Year
		}
		if len(b.Genres) == 0 && emb.Genre != "" {
			b.Genres = []string{cleanTag(emb.Genre)}
		}
		if b.Description == "" && len([]rune(emb.Comment)) >= 20 {
			b.Description = cleanTag(emb.Comment)
		}
	}
}

func (s *Scanner) resolveCover(b *model.Book, dir string, abs []string) {
	// 1. A dedicated image file in the book folder.
	if p := findLocalCover(dir); p != "" {
		b.CoverFile = p
		b.CoverMIME = media.MIMEFor(p)
		return
	}
	// 2. Artwork embedded in one of the first audio files.
	for _, p := range abs[:min(3, len(abs))] {
		emb, err := media.ReadEmbedded(p)
		if err != nil || len(emb.Picture) < 512 {
			continue
		}
		ext := extForImageMIME(emb.PictureMIME, emb.Picture)
		if ext == "" {
			ext = ".jpg"
		}
		if s.opt.CoverDir == "" {
			return
		}
		if err := os.MkdirAll(s.opt.CoverDir, 0o755); err != nil {
			return
		}
		dst := filepath.Join(s.opt.CoverDir, "embed-"+b.ID+ext)
		if st, err := os.Stat(dst); err != nil || st.Size() != int64(len(emb.Picture)) {
			if err := os.WriteFile(dst, emb.Picture, 0o644); err != nil {
				return
			}
		}
		b.CoverFile = dst
		b.CoverMIME = media.MIMEFor(dst)
		return
	}
}

var coverNames = []string{"cover", "folder", "front", "album", "poster", "artwork", "封面", "专辑", "海报"}

func findLocalCover(dir string) string {
	files, _ := listDir(dir)
	var images []string
	for _, f := range files {
		if media.IsImage(f) {
			images = append(images, f)
		}
	}
	if len(images) == 0 {
		return ""
	}
	sort.Slice(images, func(i, j int) bool { return natsort.Less(images[i], images[j]) })
	for _, name := range coverNames {
		for _, p := range images {
			if baseClean(strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))) == name {
				return p
			}
		}
	}
	// Fall back to the biggest image, which is usually the cover.
	best, bestSize := images[0], int64(-1)
	for _, p := range images {
		if st, err := os.Stat(p); err == nil && st.Size() > bestSize {
			best, bestSize = p, st.Size()
		}
	}
	return best
}

// applyLocalMeta reads an optional metadata.json / book.json / .vocalis.json.
func (s *Scanner) applyLocalMeta(b *model.Book, dir string) {
	m, _ := readLocalMeta(dir)
	if m == nil {
		return
	}
	if m.Title != "" {
		b.Title = m.Title
	}
	if m.Author != "" {
		b.Author = m.Author
	}
	if m.Narrator != "" {
		b.Narrator = m.Narrator
	}
	if m.Description != "" {
		b.Description = m.Description
	}
	if m.Publisher != "" {
		b.Publisher = m.Publisher
	}
	if m.Year != "" {
		b.Year = m.Year
	}
	if m.Language != "" {
		b.Language = m.Language
	}
	if m.Series != "" {
		b.Series = m.Series
	}
	if m.SeriesIndex != 0 {
		b.SeriesIndex = m.SeriesIndex
	}
	if len(m.Genres) > 0 {
		b.Genres = m.Genres
	}
	if m.Cover != "" {
		p := m.Cover
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			b.CoverFile = p
			b.CoverMIME = media.MIMEFor(p)
		}
	}
}

func finalize(b *model.Book) {
	b.Title = strings.TrimSpace(b.Title)
	if b.Title == "" {
		b.Title = filepath.Base(b.RelDir)
	}
	if b.Title == "" || b.Title == "." {
		b.Title = "未命名有声书"
	}
	var total float64
	var size int64
	for _, c := range b.Chapters {
		total += c.Duration
		size += c.Size
	}
	b.TotalDuration = round2(total)
	b.TotalSize = size
	b.Fingerprint = fingerprint(b)
	b.UpdatedAt = nowUTC()
}

func fingerprint(b *model.Book) string {
	h := sha1.New()
	fmt.Fprintf(h, "%s|%s|%s|", b.RelDir, b.Title, b.Author)
	for _, c := range b.Chapters {
		fmt.Fprintf(h, "%s:%d:%d|", c.RelPath, c.Size, c.ModTime)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// ------------------------------------------------------------------ helpers

var ignoredDirNames = map[string]bool{
	"@eadir":                    true,
	"@recycle":                  true,
	"#recycle":                  true,
	"#snapshot":                 true,
	".@__thumb":                 true,
	"$recycle.bin":              true,
	"system volume information": true,
	"lost+found":                true,
	"node_modules":              true,
	"__macosx":                  true,
	"@tmp":                      true,
	"@appstore":                 true,
	"@database":                 true,
	"@synologydrive":            true,
}

func ignoreName(name string) bool {
	if name == "" {
		return true
	}
	if strings.HasPrefix(name, ".") {
		return true
	}
	if strings.HasPrefix(name, "._") {
		return true
	}
	return ignoredDirNames[strings.ToLower(name)]
}

func listDir(dir string) (files []string, dirs []string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}
	for _, e := range entries {
		if ignoreName(e.Name()) {
			continue
		}
		full := filepath.Join(dir, e.Name())
		if e.IsDir() {
			dirs = append(dirs, full)
			continue
		}
		files = append(files, full)
	}
	sort.Slice(files, func(i, j int) bool { return natsort.Less(files[i], files[j]) })
	sort.Slice(dirs, func(i, j int) bool { return natsort.Less(dirs[i], dirs[j]) })
	return files, dirs
}

func chapterIndex(b *model.Book) map[string]model.Chapter {
	if b == nil {
		return nil
	}
	m := make(map[string]model.Chapter, len(b.Chapters))
	for _, c := range b.Chapters {
		m[c.RelPath] = c
	}
	return m
}

func shortID(seed string) string {
	sum := sha1.Sum([]byte(seed))
	return hex.EncodeToString(sum[:])[:12]
}

func cleanTag(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "\x00")
	return strings.Join(strings.Fields(s), " ")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func extForImageMIME(mime string, data []byte) string {
	switch {
	case len(data) >= 8 && string(data[1:4]) == "PNG":
		return ".png"
	case len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8:
		return ".jpg"
	case len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return ".webp"
	case len(data) >= 4 && string(data[0:4]) == "GIF8":
		return ".gif"
	}
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	}
	return ""
}

func round2(f float64) float64 {
	return float64(int64(f*100+0.5)) / 100
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
