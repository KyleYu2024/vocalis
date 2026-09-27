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
