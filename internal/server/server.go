// Package server exposes the library as podcast feeds and a small web UI.
package server

import (
	"crypto/subtle"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"vocalis/internal/feed"
	"vocalis/internal/store"
)

//go:embed web/*
var webFS embed.FS

// Config holds the runtime settings of the HTTP server.
type Config struct {
	Addr         string
	BaseURL      string
	Username     string
	Password     string
	Token        string
	LibraryTitle string
	Language     string
}

// Server serves feeds, audio and the web UI.
type Server struct {
	cfg   Config
	store *store.Store
	tmpl  *template.Template
	mux   *http.ServeMux
	log   *slog.Logger
}

// New builds a server for the given store.
func New(cfg Config, st *store.Store, log *slog.Logger) (*Server, error) {
	if cfg.LibraryTitle == "" {
		cfg.LibraryTitle = "我的有声书"
	}
	if cfg.Language == "" {
		cfg.Language = "zh-cn"
	}
	tmpl, err := template.New("").Funcs(template.FuncMap{
		"safeURL": func(s string) template.URL { return template.URL(s) },
		"dur":     humanDuration,
		"size":    humanSize,
		"date":    func(t time.Time) string { return t.Local().Format("2006-01-02 15:04") },
	}).ParseFS(webFS, "web/*.html")
	if err != nil {
		return nil, fmt.Errorf("解析页面模板失败: %w", err)
	}
	s := &Server{
		cfg:   cfg,
		store: st,
		tmpl:  tmpl,
		mux:   http.NewServeMux(),
		log:   log,
	}
	// A login protects the browser UI, but podcast clients cannot answer a
	// Basic auth prompt reliably. When only a login is configured we mint a
	// stable token so that feed URLs can still be protected and shared.
	if s.cfg.Username != "" && s.cfg.Token == "" {
		tok, err := ensureToken(st.DataDir())
		if err != nil {
			return nil, fmt.Errorf("生成访问令牌失败: %w", err)
		}
		s.cfg.Token = tok
		if log != nil {
			log.Info("已自动生成访问令牌，订阅地址会带上它",
				"file", filepath.Join(st.DataDir(), "token.txt"))
		}
	}
	s.routes()
	return s, nil
}

// Handler returns the root http.Handler including auth middleware.
func (s *Server) Handler() http.Handler {
	return s.withLogging(s.withAuth(s.mux))
}

func (s *Server) routes() {
	static, _ := fs.Sub(webFS, "web")
	s.mux.Handle("GET /static/", noCache(http.StripPrefix("/static/", http.FileServer(http.FS(static)))))

	s.mux.HandleFunc("GET /{$}", s.handleIndex)
	s.mux.HandleFunc("GET /book/{id}", s.handleBookPage)
	s.mux.HandleFunc("POST /book/{id}/save", s.handleBookSave)
	s.mux.HandleFunc("POST /rescan", s.handleRescanPage)
	s.mux.HandleFunc("GET /search", s.handleSearchPage)

	s.mux.HandleFunc("GET /feed/book/{name}", s.handleBookFeed)
	s.mux.HandleFunc("GET /chapters/{name}", s.handleChaptersJSON)
	s.mux.HandleFunc("GET /cover/{id}", s.handleCover)
	s.mux.HandleFunc("GET /audio/{id}", s.handleAudio)
	s.mux.HandleFunc("HEAD /audio/{id}", s.handleAudio)
	s.mux.HandleFunc("GET /stream/{id}", s.handleStream)
	s.mux.HandleFunc("HEAD /stream/{id}", s.handleStream)

	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, "ok")
	})

	s.mux.HandleFunc("GET /api/stats", s.handleAPIStats)
	s.mux.HandleFunc("GET /api/books", s.handleAPIBooks)
	s.mux.HandleFunc("POST /api/rescan", s.handleAPIRescan)
	s.mux.HandleFunc("GET /api/find", s.handleAPIFind)
}

// noCache makes browsers revalidate these responses instead of reusing a
// cached copy. Pages are dynamic and the embedded static files carry neither
// Last-Modified nor ETag (they are baked into the binary with a zero mod
// time), so without this header a browser can keep running an old app.js
// after an upgrade.
func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		next.ServeHTTP(w, r)
	})
}

// --------------------------------------------------------------- middleware

func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if s.log != nil {
			s.log.Debug("http",
				"method", r.Method, "path", r.URL.Path, "status", rec.status,
				"bytes", rec.bytes, "dur", time.Since(start).String())
		}
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	n, err := r.ResponseWriter.Write(b)
	r.bytes += int64(n)
	return n, err
}

func (s *Server) authEnabled() bool {
	return s.cfg.Username != "" || s.cfg.Token != ""
}

func (s *Server) authorized(r *http.Request) bool {
	if !s.authEnabled() {
		return true
	}
	if s.cfg.Token != "" {
		tok := r.URL.Query().Get("token")
		if tok == "" {
			tok = strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		}
		if subtle.ConstantTimeCompare([]byte(tok), []byte(s.cfg.Token)) == 1 {
			return true
		}
	}
	if s.cfg.Username != "" {
		u, p, ok := r.BasicAuth()
		if ok &&
			subtle.ConstantTimeCompare([]byte(u), []byte(s.cfg.Username)) == 1 &&
			subtle.ConstantTimeCompare([]byte(p), []byte(s.cfg.Password)) == 1 {
			return true
		}
	}
	return false
}

func (s *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Static assets and the health probe stay open; everything else,
		// including feeds and audio, needs the token or the login.
		if publicPath(r.URL.Path) || s.authorized(r) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Basic realm="vocalis"`)
		http.Error(w, "需要登录：请在 URL 后添加 ?token=... 或使用 Basic Auth", http.StatusUnauthorized)
	})
}

func publicPath(p string) bool {
	return p == "/healthz" || strings.HasPrefix(p, "/static/")
}

// ------------------------------------------------------------------- links

func (s *Server) baseURL(r *http.Request) string {
	if s.cfg.BaseURL != "" {
		return strings.TrimRight(s.cfg.BaseURL, "/")
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		scheme = firstHeaderValue(p)
	}
	host := r.Host
	if h := r.Header.Get("X-Forwarded-Host"); h != "" {
		host = firstHeaderValue(h)
	}
	return scheme + "://" + host
}

func firstHeaderValue(v string) string {
	if i := strings.IndexByte(v, ','); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

func (s *Server) linker(r *http.Request) *feed.LinkTarget {
	token := ""
	if s.cfg.Token != "" {
		token = s.cfg.Token
	}
	return &feed.LinkTarget{Base: s.baseURL(r), Token: token}
}

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if err := s.tmpl.ExecuteTemplate(w, name, data); err != nil && s.log != nil {
		s.log.Error("渲染模板失败", "template", name, "err", err)
	}
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	writeJSON(w, status, v)
}

func humanDuration(seconds float64) string {
	if seconds <= 0 {
		return "--"
	}
	total := int64(seconds + 0.5)
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%d 小时 %d 分", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%d 分 %d 秒", m, s)
	}
	return fmt.Sprintf("%d 秒", s)
}

func humanSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
