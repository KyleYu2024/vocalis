package natsort

import (
	"sort"
	"testing"
)

func TestLess(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"第2章", "第10章", true},
		{"第10章", "第2章", false},
		{"01.mp3", "02.mp3", true},
		{"track1", "track1", false},
		{"a", "b", true},
		{"B", "a", false},
		{"a", "B", true},
		{"第007章", "第8章", true},
	}
	for _, c := range cases {
		if got := Less(c.a, c.b); got != c.want {
			t.Errorf("Less(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestSortOrder(t *testing.T) {
	in := []string{"第10章.mp3", "第2章.mp3", "第1章.mp3", "第20章.mp3"}
	sort.Slice(in, func(i, j int) bool { return Less(in[i], in[j]) })
	want := []string{"第1章.mp3", "第2章.mp3", "第10章.mp3", "第20章.mp3"}
	for i := range want {
		if in[i] != want[i] {
			t.Fatalf("got %v, want %v", in, want)
		}
	}
}
