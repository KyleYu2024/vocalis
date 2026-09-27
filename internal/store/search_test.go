package store

import (
	"os"
	"path/filepath"
	"testing"
)

func newSearchStore(t *testing.T) *Store {
	t.Helper()
	libDir := t.TempDir()
	dataDir := t.TempDir()
	books := map[string][]string{
		"上下五千年": {"001前言.mp3", "002盘古开天地.mp3", "003神农尝百草.mp3"},
		"讲历史":   {"001夏商周.mp3", "002春秋战国.mp3"},
	}
	for book, files := range books {
		dir := filepath.Join(libDir, book)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	st := New(libDir, dataDir, "auto")
	if _, err := st.Rescan(); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestSearchFindsBooksAndChapters(t *testing.T) {
	st := newSearchStore(t)

	res := st.Search("上下五千年", 50)
	if len(res.Books) != 1 || res.Books[0].Title != "上下五千年" {
		t.Fatalf("书名匹配失败: %+v", res.Books)
	}

	// Chapters whose title or file name contains the term.
	res = st.Search("盘古", 50)
	if res.ChapterTotal != 1 || len(res.Chapters) != 1 {
		t.Fatalf("章节匹配失败: total=%d hits=%d", res.ChapterTotal, len(res.Chapters))
	}
	if res.Chapters[0].Book.Title != "上下五千年" {
		t.Errorf("归属书错误: %q", res.Chapters[0].Book.Title)
	}

	// Numeric prefixes are part of the file name, so "002" works too.
	if res := st.Search("002", 50); res.ChapterTotal != 2 {
		t.Errorf("按序号搜索应命中 2 集，实际 %d", res.ChapterTotal)
	}

	if res := st.Search("不存在的东西", 50); len(res.Books) != 0 || res.ChapterTotal != 0 {
		t.Errorf("不该有结果: %+v", res)
	}
	if res := st.Search("  ", 50); len(res.Books) != 0 {
		t.Errorf("空查询不该有结果")
	}
}

func TestSearchLimit(t *testing.T) {
	st := newSearchStore(t)
	res := st.Search(".mp3", 2)
	if len(res.Chapters) != 2 {
		t.Fatalf("limit 未生效: %d", len(res.Chapters))
	}
	if res.ChapterTotal != 5 {
		t.Errorf("ChapterTotal = %d，期望 5", res.ChapterTotal)
	}
}

func TestSearchMultipleTerms(t *testing.T) {
	st := newSearchStore(t)
	res := st.Search("上下五千年 盘古", 50)
	if res.ChapterTotal != 1 {
		t.Fatalf("多词应取交集，实际 %d", res.ChapterTotal)
	}
	res = st.Search("上下五千年 共产党", 50)
	if res.ChapterTotal != 0 {
		t.Fatalf("不该命中，实际 %d", res.ChapterTotal)
	}
}
