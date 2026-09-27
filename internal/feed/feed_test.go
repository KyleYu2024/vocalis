package feed

import (
	"strings"
	"testing"
	"time"

	"vocalis/internal/model"
)

func book(id, title string, added time.Time, chapters ...model.Chapter) *model.Book {
	var total float64
	for i := range chapters {
		chapters[i].Index = i
		chapters[i].Size = 1000
		total += chapters[i].Duration
	}
	return &model.Book{
		ID:            id,
		Title:         title,
		AddedAt:       added,
		Chapters:      chapters,
		TotalDuration: total,
	}
}

func chapter(id, title string, dur float64) model.Chapter {
	return model.Chapter{ID: id, Title: title, Duration: dur, RelPath: id + ".mp3", MIME: "audio/mpeg"}
}

// Podcast clients decide "what comes next" from the episode order, so the
// publication times have to increase monotonically across the whole feed.
// They used to restart for every book, which made the order ambiguous and
// broke auto play.
func TestLibraryFeedPubDatesIncreaseAcrossBooks(t *testing.T) {
	added := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	books := []*model.Book{
		book("a", "第一本", added,
			chapter("a1", "甲", 600), chapter("a2", "乙", 700), chapter("a3", "丙", 500)),
		book("b", "第二本", added,
			chapter("b1", "丁", 300), chapter("b2", "戊", 400)),
	}
	f := LibraryFeed(books, &LinkTarget{Base: "http://nas"}, LibraryOptions{Title: "书架"})
	if len(f.Items) != 5 {
		t.Fatalf("episodes = %d", len(f.Items))
	}

	var prev time.Time
	for i, it := range f.Items {
		ts, err := time.Parse(time.RFC1123Z, it.PubDate)
		if err != nil {
			t.Fatalf("第 %d 集的时间格式错误: %v", i, err)
		}
		if i > 0 && !ts.After(prev) {
			t.Fatalf("第 %d 集(%s) 的时间没有晚于上一集(%s)", i, ts, prev)
		}
		prev = ts
	}

	// Seasons follow the book order, episodes follow the chapter order.
	wantSeason := []int{1, 1, 1, 2, 2}
	wantEpisode := []int{1, 2, 3, 1, 2}
	for i, it := range f.Items {
		if it.Season != wantSeason[i] || it.Episode != wantEpisode[i] {
			t.Errorf("第 %d 集 = S%dE%d，期望 S%dE%d",
				i, it.Season, it.Episode, wantSeason[i], wantEpisode[i])
		}
	}
	if f.Items[0].Title != "第一本 · 甲" {
		t.Errorf("标题 = %q", f.Items[0].Title)
	}
}

func TestLibraryFeedBooksModeMergesToStream(t *testing.T) {
	added := time.Now().UTC().Add(-time.Hour)
	books := []*model.Book{book("a", "一本书", added,
		chapter("a1", "第一章", 60), chapter("a2", "第二章", 60))}
	f := LibraryFeed(books, &LinkTarget{Base: "http://nas"}, LibraryOptions{
		Title: "书架", Mode: LibraryModeBooks,
	})
	if len(f.Items) != 1 {
		t.Fatalf("episodes = %d", len(f.Items))
	}
	if !strings.Contains(f.Items[0].URL, "/stream/a") {
		t.Fatalf("books 模式应该用合并流，实际 %s", f.Items[0].URL)
	}
	if f.Items[0].ChaptersURL == "" {
		t.Error("合并成一集时应该带上章节标记")
	}
}

func TestBookFeedOneEpisodePerChapter(t *testing.T) {
	added := time.Now().UTC().Add(-time.Hour)
	b := book("a", "一本书", added, chapter("a1", "第一章", 60), chapter("a2", "第二章", 60))
	f := BookFeed(b, &LinkTarget{Base: "http://nas"}, "zh-cn")
	if len(f.Items) != 2 {
		t.Fatalf("episodes = %d", len(f.Items))
	}
	if !strings.Contains(f.Items[1].URL, "/audio/a2") {
		t.Errorf("单本 feed 每章一集，实际 %s", f.Items[1].URL)
	}
	if f.Type != "serial" {
		t.Errorf("单本 feed 应该是 serial，实际 %q", f.Type)
	}
}
