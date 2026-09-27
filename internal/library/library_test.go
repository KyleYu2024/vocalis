package library

import (
	"os"
	"path/filepath"
	"testing"

	"vocalis/internal/model"
)

// layoutCase describes a directory tree and the books we expect to find.
type layoutCase struct {
	name  string
	dirs  []string
	files []string
	want  []string // expected RelDir values
}

func TestScanLayouts(t *testing.T) {
	cases := []layoutCase{
		{
			name:  "one folder per book",
			dirs:  []string{"三体"},
			files: []string{"三体/01.mp3", "三体/02.mp3"},
			want:  []string{"三体"},
		},
		{
			name:  "author slash title",
			dirs:  []string{"刘慈欣/三体"},
			files: []string{"刘慈欣/三体/01.mp3"},
			want:  []string{"刘慈欣/三体"},
		},
		{
			name:  "disc folders belong to one book",
			dirs:  []string{"球状闪电/CD1 上", "球状闪电/CD2 下"},
			files: []string{"球状闪电/CD1 上/01.mp3", "球状闪电/CD2 下/01.mp3"},
			want:  []string{"球状闪电"},
		},
		{
			name:  "numbered disc folders",
			dirs:  []string{"呐喊/CD1", "呐喊/CD2"},
			files: []string{"呐喊/CD1/01.mp3", "呐喊/CD2/01.mp3"},
			want:  []string{"呐喊"},
		},
		{
			name:  "generic audio folder is not a book",
			dirs:  []string{"活着/正文"},
			files: []string{"活着/正文/01.mp3"},
			want:  []string{"活着"},
		},
		{
			name:  "several books under one author",
			dirs:  []string{"余华/活着", "余华/许三观"},
			files: []string{"余华/活着/01.mp3", "余华/许三观/01.mp3"},
			want:  []string{"余华/活着", "余华/许三观"},
		},
		{
			name:  "loose files become single file books",
			files: []string{"呐喊 - 鲁迅.mp3", "朝花夕拾 - 鲁迅.m4a"},
			want:  []string{"", ""},
		},
		{
			name:  "synology metadata folders are ignored",
			dirs:  []string{"三体", "@eaDir/x"},
			files: []string{"三体/01.mp3", "@eaDir/x/thumb.mp3", "._noise.mp3"},
			want:  []string{"三体"},
		},
		{
			name:  "collection folder with thematic sub folders stays one book",
			dirs:  []string{"名人传【全集】/勇气篇", "名人传【全集】/坚韧篇"},
			files: []string{"名人传【全集】/勇气篇/01.mp3", "名人传【全集】/坚韧篇/01.mp3"},
			want:  []string{"名人传【全集】"},
		},
		{
			name:  "numbered volumes named after the book merge",
			dirs:  []string{"战虫部队/01战虫部队 第1部", "战虫部队/02战虫部队 第2部"},
			files: []string{"战虫部队/01战虫部队 第1部/01.mp3", "战虫部队/02战虫部队 第2部/01.mp3"},
			want:  []string{"战虫部队"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for _, d := range tc.dirs {
				if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			for _, f := range tc.files {
				p := filepath.Join(root, f)
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte("fake audio"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			res, err := NewScanner(Options{Root: root, Layout: LayoutAuto}).Scan(nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Books) != len(tc.want) {
				t.Fatalf("扫到 %d 本书，期望 %d：%v", len(res.Books), len(tc.want), relDirs(res.Books))
			}
			got := relDirs(res.Books)
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("第 %d 本 = %q，期望 %q（全部：%v）", i, got[i], tc.want[i], got)
				}
			}
		})
	}
}

func relDirs(books []*model.Book) []string {
	out := make([]string, 0, len(books))
	for _, b := range books {
		out = append(out, b.RelDir)
	}
	return out
}

// TestSingleLayoutTreatsTheRootAsOneBook covers pointing the container at a
// single book folder, which is how a quick "just test this one" setup works.
func TestSingleLayoutTreatsTheRootAsOneBook(t *testing.T) {
	// t.TempDir() itself ends in "001", so build the real folder name inside it.
	root := filepath.Join(t.TempDir(), "01上下五千年【更新中】")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"001.mp3", "002.mp3", "010.mp3"} {
		if err := os.WriteFile(filepath.Join(root, n), []byte("fake audio"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res, err := NewScanner(Options{Root: root, Layout: LayoutSingle}).Scan(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Books) != 1 {
		t.Fatalf("期望 1 本书，实际 %d: %v", len(res.Books), relDirs(res.Books))
	}
	b := res.Books[0]
	if b.Title != "上下五千年" {
		t.Errorf("书名 = %q，期望 %q", b.Title, "上下五千年")
	}
	if len(b.Chapters) != 3 {
		t.Fatalf("章节数 = %d，期望 3", len(b.Chapters))
	}
	if b.Chapters[0].Title != "001" || b.Chapters[2].Title != "010" {
		t.Errorf("章节标题/顺序不对: %q %q %q",
			b.Chapters[0].Title, b.Chapters[1].Title, b.Chapters[2].Title)
	}
}

// Inside a container the mount point is usually just "/audiobooks", so the
// title has to come from the operator.
func TestSingleTitleOverride(t *testing.T) {
	root := filepath.Join(t.TempDir(), "audiobooks")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "01.mp3"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := NewScanner(Options{Root: root, Layout: LayoutSingle, SingleTitle: "上下五千年"}).Scan(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Books) != 1 {
		t.Fatalf("books = %d", len(res.Books))
	}
	if got := res.Books[0].Title; got != "上下五千年" {
		t.Fatalf("书名 = %q，期望 上下五千年", got)
	}
}

// The same tree under the default layout must not be mistaken for three books.
func TestSingleLayoutIsNotTheDefault(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"001.mp3", "002.mp3"} {
		if err := os.WriteFile(filepath.Join(root, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res, err := NewScanner(Options{Root: root, Layout: LayoutAuto}).Scan(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Books) != 2 {
		t.Fatalf("auto 模式下根目录的散落音频应各自成书，实际 %d", len(res.Books))
	}
}
