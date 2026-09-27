// Package config reads settings from flags and environment variables.
package config

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds every runtime setting.
type Config struct {
	LibraryDir     string
	DataDir        string
	Addr           string
	BaseURL        string
	Username       string
	Password       string
	Token          string
	Title          string
	Language       string
	Country        string
	Layout         string
	ScanInterval   time.Duration
	LogLevel       string
	GoogleBooksKey string
	// BookTitle names the single book when layout=single.
	BookTitle string
	// LibraryFeedMode is books|chapters.
	LibraryFeedMode string
}

// Load parses command line flags, falling back to VOCALIS_* environment
// variables and finally to built in defaults.
func Load(args []string) (Config, error) {
	fs := flag.NewFlagSet("vocalis", flag.ContinueOnError)
	var (
		libraryDir = fs.String("library-dir", env("VOCALIS_LIBRARY_DIR", "/audiobooks"), "有声书目录")
		dataDir    = fs.String("data-dir", env("VOCALIS_DATA_DIR", "/data"), "数据与缓存目录")
		addr       = fs.String("addr", env("VOCALIS_ADDR", ":8080"), "监听地址")
		baseURL    = fs.String("base-url", env("VOCALIS_BASE_URL", ""), "对外访问地址，例如 https://book.example.com")
		username   = fs.String("username", env("VOCALIS_USERNAME", ""), "Web UI 登录用户名")
		password   = fs.String("password", env("VOCALIS_PASSWORD", ""), "Web UI 登录密码")
		token      = fs.String("token", env("VOCALIS_TOKEN", ""), "订阅地址访问令牌，推荐在公网使用时设置")
		title      = fs.String("title", env("VOCALIS_TITLE", "Vocalis"), "播客标题 / 网页标题")
		language   = fs.String("language", env("VOCALIS_LANGUAGE", "zh-cn"), "播客语言")
		country    = fs.String("country", env("VOCALIS_COUNTRY", "cn"), "刮削优先使用的区域")
		layout     = fs.String("layout", env("VOCALIS_LAYOUT", "auto"), "目录解析模式：auto|flat|nested|single")
		interval   = fs.String("scan-interval", env("VOCALIS_SCAN_INTERVAL", "0"), "自动重新扫描间隔，如 6h，0 表示关闭")
		logLevel   = fs.String("log-level", env("VOCALIS_LOG_LEVEL", "info"), "日志级别：debug|info|warn|error")
		gbKey      = fs.String("google-books-key", env("VOCALIS_GOOGLE_BOOKS_KEY", ""), "可选的 Google Books API Key，用于提高刮削配额")
		bookTitle  = fs.String("book-title", env("VOCALIS_BOOK_TITLE", ""), "layout=single 时这本书的标题（容器里挂载点名字没意义）")
		libFeed    = fs.String("library-feed", env("VOCALIS_LIBRARY_FEED", "chapters"), "整库订阅组织方式：chapters（每章一集，书=季）| books（每本书一集）")
	)
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	d, err := time.ParseDuration(strings.TrimSpace(*interval))
	if err != nil && strings.TrimSpace(*interval) != "0" {
		return Config{}, fmt.Errorf("scan-interval 格式不正确: %w", err)
	}
	layoutNorm := strings.ToLower(strings.TrimSpace(*layout))
	switch layoutNorm {
	case "auto", "flat", "nested", "single":
	default:
		return Config{}, fmt.Errorf("layout 只能是 auto、flat、nested 或 single，收到 %q", *layout)
	}
	return Config{
		LibraryDir:      strings.TrimSpace(*libraryDir),
		DataDir:         strings.TrimSpace(*dataDir),
		Addr:            strings.TrimSpace(*addr),
		BaseURL:         strings.TrimRight(strings.TrimSpace(*baseURL), "/"),
		Username:        strings.TrimSpace(*username),
		Password:        *password,
		Token:           strings.TrimSpace(*token),
		Title:           strings.TrimSpace(*title),
		Language:        strings.TrimSpace(*language),
		Country:         strings.TrimSpace(*country),
		Layout:          layoutNorm,
		ScanInterval:    d,
		LogLevel:        strings.ToLower(strings.TrimSpace(*logLevel)),
		GoogleBooksKey:  strings.TrimSpace(*gbKey),
		BookTitle:       strings.TrimSpace(*bookTitle),
		LibraryFeedMode: strings.ToLower(strings.TrimSpace(*libFeed)),
	}, nil
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return v
	}
	return def
}

// EnvBool is a small helper for boolean environment variables.
func EnvBool(key string, def bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return def
	}
	b, err := strconv.ParseBool(strings.TrimSpace(v))
	if err != nil {
		return def
	}
	return b
}
