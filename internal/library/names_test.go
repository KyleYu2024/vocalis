package library

import "testing"

func TestParseBookName(t *testing.T) {
	cases := []struct {
		in          string
		title, auth string
	}{
		{"三体（刘慈欣 著）", "三体", "刘慈欣"},
		{"《活着》作者：余华", "活着", "余华"},
		{"刘慈欣 - 三体", "三体", "刘慈欣"},
		{"三体 - 刘慈欣", "三体", "刘慈欣"},
		{"【有声书】明朝那些事儿 全集", "明朝那些事儿", ""},
		{"平凡的世界", "平凡的世界", ""},
		{"刘慈欣 - 球状闪电", "球状闪电", "刘慈欣"},
		{"三体 作者：刘慈欣", "三体", "刘慈欣"},
		{"The Hobbit - Tolkien", "The Hobbit", "Tolkien"},
		{"原著书名", "原著书名", ""},
		{"朗读版", "朗读版", ""},
		{"作者：刘慈欣", "刘慈欣", ""},
		{"道德经（作者 著）", "道德经", ""},
		{"01上下五千年【更新中】", "上下五千年", ""},
		{"007. 三体", "三体", ""},
		{"1984", "1984", ""},
		{"007", "007", ""},
	}
	for _, c := range cases {
		title, author := ParseBookName(c.in)
		if title != c.title || author != c.auth {
			t.Errorf("ParseBookName(%q) = (%q, %q), want (%q, %q)", c.in, title, author, c.title, c.auth)
		}
	}
}

func TestChapterTitle(t *testing.T) {
	cases := map[string]string{
		"001.mp3":        "001",
		"01. 第一章 觉醒.mp3": "第一章 觉醒",
		"CD1-03 夜航.flac": "夜航",
		"序章.m4a":         "序章",
		"第1章 三体世界.mp3":   "第1章 三体世界",
	}
	for in, want := range cases {
		if got := ChapterTitle(in); got != want {
			t.Errorf("ChapterTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLooksLikePart(t *testing.T) {
	for _, s := range []string{"CD1", "Disc 2", "第二部", "上卷", "1", "CD1 上", "CD2 下", "第三部 下"} {
		if !LooksLikePart(s) {
			t.Errorf("LooksLikePart(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"三体", "呐喊", "Book 1", "球状闪电"} {
		if LooksLikePart(s) {
			t.Errorf("LooksLikePart(%q) = true, want false", s)
		}
	}
}
