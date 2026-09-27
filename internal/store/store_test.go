package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vocalis/internal/model"
)

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	libDir := t.TempDir()
	dataDir := t.TempDir()
	bookDir := filepath.Join(libDir, "原著书名")
	if err := os.MkdirAll(bookDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bookDir, "01.mp3"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := New(libDir, dataDir, "auto")
	if _, err := st.Rescan(); err != nil {
		t.Fatalf("Rescan: %v", err)
	}
	return st, dataDir
}

func TestOverrideSurvivesRescanAndCanBeCleared(t *testing.T) {
	st, dataDir := newTestStore(t)
	books := st.Books()
	if len(books) != 1 {
		t.Fatalf("books = %d", len(books))
	}
	id := books[0].ID
	if books[0].Title != "原著书名" {
		t.Fatalf("初始书名 = %q", books[0].Title)
	}

	// A user edit wins.
	if err := st.SetOverride(id, &model.Override{Title: "手改书名", Author: "某人"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := st.Book(id); b.Title != "手改书名" || b.Author != "某人" {
		t.Fatalf("override 未生效: %+v", b)
	}

	// Rescanning must keep the override.
	if _, err := st.Rescan(); err != nil {
		t.Fatal(err)
	}
	if b, _ := st.Book(id); b.Title != "手改书名" {
		t.Fatalf("重新扫描后 override 丢失: %q", b.Title)
	}

	// Clearing the override falls back to what is on disk.
	if err := st.SetOverride(id, &model.Override{}); err != nil {
		t.Fatal(err)
	}
	if b, _ := st.Book(id); b.Title != "原著书名" || b.Author != "" {
		t.Fatalf("清空 override 后应回落到扫描结果，实际 %+v", b)
	}

	// A fresh store loading the same data directory agrees.
	st2 := New(st.LibraryDir(), dataDir, "auto")
	if err := st2.Load(); err != nil {
		t.Fatal(err)
	}
	if b, ok := st2.Book(id); !ok || b.Title != "原著书名" {
		t.Fatalf("重新加载后书名 = %+v", b)
	}
}

func TestScrapedFieldsAreNotBakedIntoTheIndex(t *testing.T) {
	st, dataDir := newTestStore(t)
	id := st.Books()[0].ID
	if err := st.SetOverride(id, &model.Override{
		Title:    "刮削书名",
		CoverURL: "https://example.com/c.jpg",
		Source:   "itunes-ebook",
	}); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(dataDir, "library.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(raw); strings.Contains(got, "刮削书名") {
		t.Fatalf("library.json 里不应该写入手动/刮削后的值:\n%s", got)
	}
}
