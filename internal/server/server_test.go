package server

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vocalis/internal/feed"
	"vocalis/internal/store"
)

type fixture struct {
	ts        *httptest.Server
	bookID    string
	chapters  [][]byte
	chapterID []string
	dataDir   string
}

func newFixture(t *testing.T, token string) *fixture {
	t.Helper()
	return newFixtureCfg(t, Config{LibraryTitle: "测试书架", Token: token, Language: "zh-cn"})
}

func newFixtureCfg(t *testing.T, cfg Config) *fixture {
	t.Helper()
	libDir := t.TempDir()
	dataDir := t.TempDir()

	bookDir := filepath.Join(libDir, "测试书（作者 著）")
	if err := os.MkdirAll(bookDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var chapters [][]byte
	var ids []string
	for i := 1; i <= 2; i++ {
		body := testMP3(300 + i*137)
		p := filepath.Join(bookDir, fmt.Sprintf("0%d.mp3", i))
		if err := os.WriteFile(p, body, 0o644); err != nil {
			t.Fatal(err)
		}
		chapters = append(chapters, body)
	}

	st := store.New(libDir, dataDir, "auto")
	if err := st.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := st.Rescan(); err != nil {
		t.Fatalf("Rescan: %v", err)
	}
	books := st.Books()
	if len(books) != 1 {
		t.Fatalf("期望扫到 1 本书，实际 %d", len(books))
	}
	for _, c := range books[0].Chapters {
		ids = append(ids, c.ID)
	}

	if cfg.LibraryTitle == "" {
		cfg.LibraryTitle = "测试书架"
	}
	if cfg.Language == "" {
		cfg.Language = "zh-cn"
	}
	srv, err := New(cfg, st, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return &fixture{ts: ts, bookID: books[0].ID, chapters: chapters, chapterID: ids, dataDir: dataDir}
}

// testMP3 builds a valid MPEG frame with a Xing header so that the scanner can
// read a real duration out of it.
func testMP3(frames int) []byte {
	b := []byte{0xFF, 0xFB, 0x90, 0x00}
	b = append(b, make([]byte, 32)...)
	b = append(b, "Xing"...)
	b = append(b, 0, 0, 0, 1)
	b = binary.BigEndian.AppendUint32(b, uint32(frames))
	pad := make([]byte, 2000)
	for i := range pad {
		pad[i] = byte(i % 251)
	}
	return append(b, pad...)
}

func get(t *testing.T, url string, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, body
}

func TestBookFeedListsEveryChapter(t *testing.T) {
	f := newFixture(t, "")
	resp, body := get(t, f.ts.URL+"/feed/book/"+f.bookID+".xml", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	xml := string(body)
	if n := strings.Count(xml, "<item>"); n != 2 {
		t.Fatalf("期望 2 个 episode，实际 %d\n%s", n, xml)
	}
	for _, id := range f.chapterID {
		if !strings.Contains(xml, "/audio/"+id) {
			t.Errorf("feed 中缺少章节 %s", id)
		}
	}
	if !strings.Contains(xml, "<itunes:duration>") {
		t.Error("feed 缺少 itunes:duration")
	}
}

// The default library feed keeps every chapter as its own episode so a single
// subscription can still pick "which episode of this book".
func TestLibraryFeedChapterMode(t *testing.T) {
	f := newFixtureCfg(t, Config{LibraryTitle: "测试书架", LibraryFeedMode: feed.LibraryModeChapters})
	_, body := get(t, f.ts.URL+"/feed/library.xml", nil)
	xml := string(body)
	if n := strings.Count(xml, "<item>"); n != 2 {
		t.Fatalf("chapters 模式应有 2 集，实际 %d", n)
	}
	for _, id := range f.chapterID {
		if !strings.Contains(xml, "/audio/"+id) {
			t.Errorf("chapters 模式缺少章节 %s", id)
		}
	}
	if !strings.Contains(xml, "<itunes:season>1</itunes:season>") {
		t.Error("chapters 模式需要用 itunes:season 标出书名分组")
	}
	if !strings.Contains(xml, "<itunes:episode>2</itunes:episode>") {
		t.Error("chapters 模式缺少章节序号")
	}
}

func TestLibraryFeedBooksModeUsesMergedStream(t *testing.T) {
	f := newFixtureCfg(t, Config{LibraryTitle: "测试书架", LibraryFeedMode: feed.LibraryModeBooks})
	_, body := get(t, f.ts.URL+"/feed/library.xml", nil)
	if !strings.Contains(string(body), "/stream/"+f.bookID) {
		t.Fatalf("books 模式应使用合并音频流：\n%s", body)
	}
}

// TestMergedStreamRange exercises the byte range logic that podcast clients
// rely on to seek and to remember the playback position.
func TestMergedStreamRange(t *testing.T) {
	f := newFixture(t, "")
	want := bytes.Join(f.chapters, nil)
	url := f.ts.URL + "/stream/" + f.bookID

	resp, body := get(t, url, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if !bytes.Equal(body, want) {
		t.Fatalf("合并流内容不一致: got %d bytes, want %d", len(body), len(want))
	}

	// A range that starts inside the first file and ends inside the second.
	start, end := int64(len(f.chapters[0])-50), int64(len(f.chapters[0])+60)
	resp, body = get(t, url, map[string]string{
		"Range": fmt.Sprintf("bytes=%d-%d", start, end),
	})
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("Range 请求状态 = %d，期望 206", resp.StatusCode)
	}
	if !bytes.Equal(body, want[start:end+1]) {
		t.Fatalf("跨文件 Range 数据不一致")
	}
	if got := resp.Header.Get("Content-Range"); got != fmt.Sprintf("bytes %d-%d/%d", start, end, len(want)) {
		t.Errorf("Content-Range = %q", got)
	}

	// A range in the middle of the second file.
	s2 := int64(len(f.chapters[0]) + 100)
	resp, body = get(t, url, map[string]string{"Range": fmt.Sprintf("bytes=%d-%d", s2, s2+9)})
	if resp.StatusCode != http.StatusPartialContent || !bytes.Equal(body, want[s2:s2+10]) {
		t.Fatalf("第二段 Range 失败: status=%d len=%d", resp.StatusCode, len(body))
	}
}

func TestChapterAudioServesFileAndRange(t *testing.T) {
	f := newFixture(t, "")
	url := f.ts.URL + "/audio/" + f.chapterID[1]
	resp, body := get(t, url, nil)
	if resp.StatusCode != http.StatusOK || !bytes.Equal(body, f.chapters[1]) {
		t.Fatalf("整段下载失败: status=%d", resp.StatusCode)
	}
	resp, body = get(t, url, map[string]string{"Range": "bytes=10-19"})
	if resp.StatusCode != http.StatusPartialContent || !bytes.Equal(body, f.chapters[1][10:20]) {
		t.Fatalf("章节 Range 失败: status=%d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "audio/mpeg" {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestCoverIsRasterImage(t *testing.T) {
	f := newFixture(t, "")
	resp, body := get(t, f.ts.URL+"/cover/"+f.bookID, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if !bytes.HasPrefix(body, []byte{0x89, 'P', 'N', 'G'}) {
		t.Fatalf("没有封面的书应该返回 PNG 占位图")
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestChaptersJSON(t *testing.T) {
	f := newFixture(t, "")
	resp, body := get(t, f.ts.URL+"/chapters/"+f.bookID+".json", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var doc struct {
		Version  string `json:"version"`
		Chapters []struct {
			StartTime int    `json:"startTime"`
			Title     string `json:"title"`
		} `json:"chapters"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("章节 JSON 解析失败: %v\n%s", err, body)
	}
	if len(doc.Chapters) != 2 {
		t.Fatalf("章节数 = %d", len(doc.Chapters))
	}
}

func TestTokenAuth(t *testing.T) {
	f := newFixture(t, "s3cret")

	resp, _ := get(t, f.ts.URL+"/feed/library.xml", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("无 token 应返回 401，实际 %d", resp.StatusCode)
	}

	resp, body := get(t, f.ts.URL+"/feed/library.xml?token=s3cret", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("带 token 应返回 200，实际 %d", resp.StatusCode)
	}
	// Generated URLs must carry the token so podcast clients can follow them.
	if !strings.Contains(string(body), "token=s3cret") {
		t.Fatalf("feed 里的地址应带上 token")
	}

	resp, _ = get(t, f.ts.URL+"/audio/"+f.chapterID[0], map[string]string{
		"Authorization": "Bearer s3cret",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Bearer token 应通过鉴权，实际 %d", resp.StatusCode)
	}
}

func TestPagesRender(t *testing.T) {
	f := newFixture(t, "")
	for _, p := range []string{"/", "/book/" + f.bookID, "/healthz"} {
		resp, body := get(t, f.ts.URL+p, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s -> %d", p, resp.StatusCode)
		}
		if len(body) == 0 {
			t.Fatalf("%s 返回空内容", p)
		}
	}
}

func TestUnknownBookIs404(t *testing.T) {
	f := newFixture(t, "")
	for _, p := range []string{"/feed/book/nope.xml", "/audio/nope", "/stream/nope", "/cover/nope"} {
		resp, _ := get(t, f.ts.URL+p, nil)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s -> %d，期望 404", p, resp.StatusCode)
		}
	}
}

// TestSubscribeLinkIsWellFormed guards the bug where a missing token left the
// generated link as "/subscribe&u=..." instead of "/subscribe?u=...".
func TestSubscribeLinkIsWellFormed(t *testing.T) {
	f := newFixture(t, "")
	_, body := get(t, f.ts.URL+"/", nil)
	page := string(body)
	if !strings.Contains(page, "/subscribe?u=") {
		t.Fatalf("首页的订阅链接格式不对：\n%s", excerpt(page, "/subscribe", 120))
	}
	if strings.Contains(page, "/subscribe&u=") {
		t.Fatalf("首页的订阅链接缺少 ? 分隔符")
	}

	// And with a token the query string must be extended, not restarted.
	f2 := newFixture(t, "abc123")
	_, body = get(t, f2.ts.URL+"/?token=abc123", nil)
	page = string(body)
	// html/template escapes the & separator as &amp; inside attributes, which
	// is what a browser expects to receive.
	if !strings.Contains(page, "/subscribe?token=abc123&amp;u=http") {
		t.Fatalf("带 token 的订阅链接格式不对：\n%s", excerpt(page, "/subscribe", 140))
	}
}

func TestSubscribePageQRCarriesToken(t *testing.T) {
	f := newFixture(t, "abc123")
	resp, body := get(t, f.ts.URL+"/subscribe?token=abc123&u=http%3A%2F%2Fexample.com%2Ffeed.xml&t=T", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), "/qr?u=http://example.com/feed.xml&amp;token=abc123") {
		t.Fatalf("订阅页的二维码地址没有带 token：\n%s", excerpt(string(body), "/qr?", 160))
	}
	// The QR image itself must be reachable with the token.
	resp, qr := get(t, f.ts.URL+"/qr?u=http%3A%2F%2Fexample.com%2Ffeed.xml&token=abc123", nil)
	if resp.StatusCode != http.StatusOK || !bytes.HasPrefix(qr, []byte{0x89, 'P', 'N', 'G'}) {
		t.Fatalf("二维码不可用: status=%d", resp.StatusCode)
	}
}

// TestLoginGeneratesStableToken checks that protecting the UI with a password
// still yields a shareable feed URL.
func TestLoginGeneratesStableToken(t *testing.T) {
	f := newFixtureCfg(t, Config{Username: "admin", Password: "pw", LibraryTitle: "书架"})

	raw, err := os.ReadFile(filepath.Join(f.dataDir, "token.txt"))
	if err != nil {
		t.Fatalf("应该自动生成 token.txt: %v", err)
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		t.Fatal("生成的 token 为空")
	}

	resp, _ := get(t, f.ts.URL+"/feed/library.xml", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("没有凭证应 401，实际 %d", resp.StatusCode)
	}
	resp, body := get(t, f.ts.URL+"/feed/library.xml?token="+token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("带 token 应 200，实际 %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), "token="+token) {
		t.Error("feed 里的音频地址应带上 token")
	}

	// Static assets stay public so the login page still renders correctly.
	resp, _ = get(t, f.ts.URL+"/static/style.css", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("静态资源应公开，实际 %d", resp.StatusCode)
	}
}

func excerpt(s, marker string, n int) string {
	i := strings.Index(s, marker)
	if i < 0 {
		return "(未找到 " + marker + ")"
	}
	end := i + n
	if end > len(s) {
		end = len(s)
	}
	return s[i:end]
}
