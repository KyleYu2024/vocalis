// Package store keeps the scanned library in memory and on disk.
package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"vocalis/internal/library"
	"vocalis/internal/model"
	"vocalis/internal/natsort"
)

// ChapterRef points at one chapter of one book.
type ChapterRef struct {
	BookID string
	Index  int
}

// Store holds the current library snapshot.
type Store struct {
	mu sync.RWMutex

	libraryDir string
	dataDir    string
	layout     string
	scanner    *library.Scanner
	log        *slog.Logger

	books     map[string]*model.Book
	scanned   map[string]*model.Book
	byRelDir  map[string]*model.Book
	chapters  map[string]ChapterRef
	overrides map[string]*model.Override

	lastScan time.Time
	lastErr  string
}

// New creates a store for the given directories.
func New(libraryDir, dataDir, layout string) *Store {
	return NewWithOptions(libraryDir, dataDir, layout, library.Options{
		Root:     libraryDir,
		Layout:   layout,
		CoverDir: filepath.Join(dataDir, "covers"),
	})
}

// NewWithOptions creates a store with a fully configured scanner.
func NewWithOptions(libraryDir, dataDir, layout string, opt library.Options) *Store {
	if opt.Root == "" {
		opt.Root = libraryDir
	}
	if opt.Layout == "" {
		opt.Layout = layout
	}
	if opt.CoverDir == "" {
		opt.CoverDir = filepath.Join(dataDir, "covers")
	}
	s := &Store{
		libraryDir: libraryDir,
		dataDir:    dataDir,
		layout:     layout,
		books:      map[string]*model.Book{},
		scanned:    map[string]*model.Book{},
		byRelDir:   map[string]*model.Book{},
		chapters:   map[string]ChapterRef{},
		overrides:  map[string]*model.Override{},
	}
	opt.Progress = func(n int) {
		if s.log != nil {
			s.log.Info("扫描进行中", "已处理章节", n)
		}
	}
	s.scanner = library.NewScanner(opt)
	return s
}

// SetLogger attaches a logger used for scan progress.
func (s *Store) SetLogger(l *slog.Logger) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log = l
}

func (s *Store) indexPath() string    { return filepath.Join(s.dataDir, "library.json") }
func (s *Store) overridesDir() string { return filepath.Join(s.dataDir, "overrides") }
func (s *Store) overridePath(id string) string {
	return filepath.Join(s.overridesDir(), id+".json")
}

type indexFile struct {
	Version   int           `json:"version"`
	ScannedAt time.Time     `json:"scannedAt"`
	Books     []*model.Book `json:"books"`
}

// Load reads the cached index and overrides from disk. It is safe to call
// before the first scan.
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.loadOverridesLocked(); err != nil {
		return err
	}
	data, err := os.ReadFile(s.indexPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var idx indexFile
	if err := json.Unmarshal(data, &idx); err != nil {
		return fmt.Errorf("解析 library.json 失败: %w", err)
	}
	s.reindexLocked(idx.Books)
	s.lastScan = idx.ScannedAt
	return nil
}

func (s *Store) loadOverridesLocked() error {
	entries, err := os.ReadDir(s.overridesDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		data, err := os.ReadFile(filepath.Join(s.overridesDir(), e.Name()))
		if err != nil {
			continue
		}
		var ov model.Override
		if err := json.Unmarshal(data, &ov); err != nil {
			continue
		}
		s.overrides[id] = &ov
	}
	return nil
}

// Rescan walks the library directory and refreshes the in-memory index.
func (s *Store) Rescan() (int, error) {
	s.mu.Lock()
	prev := make(map[string]*model.Book, len(s.byRelDir))
	for rel, b := range s.byRelDir {
		prev[rel] = b
	}
	s.mu.Unlock()

	res, err := s.scanner.Scan(prev)
	if err != nil {
		s.mu.Lock()
		s.lastErr = err.Error()
		s.mu.Unlock()
		return 0, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.reindexLocked(res.Books)
	s.lastScan = time.Now().UTC()
	s.lastErr = ""

	if err := s.saveIndexLocked(); err != nil {
		return len(res.Books), err
	}
	return len(res.Books), nil
}

func (s *Store) applyOverrideLocked(b *model.Book) {
	if ov, ok := s.overrides[b.ID]; ok {
		ov.Apply(b)
	}
}

// reindexLocked takes the raw scan result, keeps it as the source of truth and
// derives the effective books by layering the user overrides on top. Keeping
// the two apart means clearing an override always falls back to what is
// actually on disk.
func (s *Store) reindexLocked(raw []*model.Book) {
	s.scanned = make(map[string]*model.Book, len(raw))
	s.books = make(map[string]*model.Book, len(raw))
	s.byRelDir = make(map[string]*model.Book, len(raw))
	s.chapters = make(map[string]ChapterRef)
	for _, b := range raw {
		s.scanned[b.ID] = b
		eff := cloneBook(b)
		s.applyOverrideLocked(eff)
		s.books[b.ID] = eff
		if eff.RelDir != "" {
			s.byRelDir[eff.RelDir] = eff
		}
		for i := range eff.Chapters {
			s.chapters[eff.Chapters[i].ID] = ChapterRef{BookID: b.ID, Index: i}
		}
	}
}

func (s *Store) saveIndexLocked() error {
	if err := os.MkdirAll(s.dataDir, 0o755); err != nil {
		return err
	}
	// Only the scan result is persisted; overrides live in their own files.
	books := make([]*model.Book, 0, len(s.scanned))
	for _, b := range s.scanned {
		books = append(books, b)
	}
	sort.Slice(books, func(i, j int) bool { return natsort.Less(books[i].RelDir, books[j].RelDir) })

	data, err := marshalIndent(indexFile{
		Version:   1,
		ScannedAt: s.lastScan,
		Books:     books,
	})
	if err != nil {
		return err
	}
	tmp := s.indexPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.indexPath())
}

// ------------------------------------------------------------------ readers

// Books returns every book, ordered by directory name.
func (s *Store) Books() []*model.Book {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*model.Book, 0, len(s.books))
	for _, b := range s.books {
		out = append(out, cloneBook(b))
	}
	sort.Slice(out, func(i, j int) bool { return natsort.Less(out[i].RelDir, out[j].RelDir) })
	return out
}

// Book returns a single book by ID.
func (s *Store) Book(id string) (*model.Book, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.books[id]
	if !ok {
		return nil, false
	}
	return cloneBook(b), true
}

// Chapter resolves a chapter ID to its book and chapter.
func (s *Store) Chapter(chapterID string) (*model.Book, *model.Chapter, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ref, ok := s.chapters[chapterID]
	if !ok {
		return nil, nil, false
	}
	b, ok := s.books[ref.BookID]
	if !ok || ref.Index >= len(b.Chapters) {
		return nil, nil, false
	}
	cb := cloneBook(b)
	return cb, &cb.Chapters[ref.Index], true
}

// AbsPath returns the absolute path of a chapter's file.
func (s *Store) AbsPath(rel string) string {
	if rel == "" {
		return ""
	}
	if filepath.IsAbs(rel) {
		return rel
	}
	return filepath.Join(s.libraryDir, filepath.FromSlash(rel))
}

// LibraryDir returns the configured library root.
func (s *Store) LibraryDir() string { return s.libraryDir }

// DataDir returns the configured data directory.
func (s *Store) DataDir() string { return s.dataDir }

// Override returns the stored override for a book.
func (s *Store) Override(id string) *model.Override {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if ov, ok := s.overrides[id]; ok {
		cp := *ov
		return &cp
	}
	return nil
}

// SetOverride stores (and persists) an override and rebuilds the book entry.
func (s *Store) SetOverride(id string, ov *model.Override) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.books[id]; !ok {
		return fmt.Errorf("未找到这本书: %s", id)
	}
	if err := os.MkdirAll(s.overridesDir(), 0o755); err != nil {
		return err
	}
	data, err := marshalIndent(ov)
	if err != nil {
		return err
	}
	tmp := s.overridePath(id) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.overridePath(id)); err != nil {
		return err
	}
	s.overrides[id] = ov

	// Rebuild this book from the last raw scan so the override takes effect
	// immediately and nothing from a previous override lingers.
	raw := s.scanned[id]
	if raw == nil {
		return nil
	}
	eff := cloneBook(raw)
	ov.Apply(eff)
	s.books[id] = eff
	if eff.RelDir != "" {
		s.byRelDir[eff.RelDir] = eff
	}
	return s.saveIndexLocked()
}

// Stats summarises the library for the web UI.
type Stats struct {
	Books         int       `json:"books"`
	Chapters      int       `json:"chapters"`
	TotalBytes    int64     `json:"totalBytes"`
	TotalDuration float64   `json:"totalDuration"`
	LastScan      time.Time `json:"lastScan"`
	LastError     string    `json:"lastError,omitempty"`
	LibraryDir    string    `json:"libraryDir"`
	DataDir       string    `json:"dataDir"`
	Layout        string    `json:"layout"`
}

// Stats returns current library statistics.
func (s *Store) Stats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := Stats{
		Books:      len(s.books),
		LastScan:   s.lastScan,
		LastError:  s.lastErr,
		LibraryDir: s.libraryDir,
		DataDir:    s.dataDir,
		Layout:     s.layout,
	}
	for _, b := range s.books {
		st.Chapters += len(b.Chapters)
		st.TotalBytes += b.TotalSize
		st.TotalDuration += b.TotalDuration
	}
	return st
}

func cloneBook(b *model.Book) *model.Book {
	if b == nil {
		return nil
	}
	cp := *b
	cp.Chapters = append([]model.Chapter(nil), b.Chapters...)
	cp.Genres = append([]string(nil), b.Genres...)
	return &cp
}

// marshalIndent renders JSON without escaping <, > and & so the files stay
// readable when a description contains HTML entities.
func marshalIndent(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
