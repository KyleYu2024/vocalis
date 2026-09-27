// Command vocalis serves a folder of audiobooks as podcast feeds so that
// ordinary podcast clients (including Apple Podcasts on iOS) can play them and
// remember the playback position of every book.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"vocalis/internal/config"
	"vocalis/internal/library"
	"vocalis/internal/server"
	"vocalis/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "启动失败:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		return err
	}
	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	if cfg.LibraryDir == "" || cfg.DataDir == "" {
		return errors.New("library-dir 与 data-dir 都不能为空")
	}
	if st, err := os.Stat(cfg.LibraryDir); err != nil {
		logger.Warn("有声书目录不存在，先创建空书库", "dir", cfg.LibraryDir, "err", err)
	} else if !st.IsDir() {
		return fmt.Errorf("library-dir 不是目录: %s", cfg.LibraryDir)
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return fmt.Errorf("创建数据目录失败: %w", err)
	}

	st := store.NewWithOptions(cfg.LibraryDir, cfg.DataDir, cfg.Layout, library.Options{
		Root:        cfg.LibraryDir,
		Layout:      cfg.Layout,
		SingleTitle: cfg.BookTitle,
	})
	st.SetLogger(logger)
	if err := st.Load(); err != nil {
		logger.Warn("读取缓存索引失败，将重新扫描", "err", err)
	}
	// The first scan can take minutes on a big library, so serve the cached
	// index immediately and refresh in the background instead of blocking.
	if st.Stats().Books == 0 {
		logger.Info("首次扫描书库，服务已提前就绪，扫描完成后网页会自动出现书目",
			"library", cfg.LibraryDir)
	}
	go func() {
		start := time.Now()
		logger.Info("开始扫描书库", "library", cfg.LibraryDir)
		n, err := st.Rescan()
		if err != nil {
			logger.Error("扫描书库失败", "err", err)
			return
		}
		logger.Info("扫描完成", "books", n, "took", time.Since(start).Round(time.Millisecond).String())
	}()

	srv, err := server.New(server.Config{
		Addr:         cfg.Addr,
		BaseURL:      cfg.BaseURL,
		Username:     cfg.Username,
		Password:     cfg.Password,
		Token:        cfg.Token,
		LibraryTitle: cfg.Title,
		Language:     cfg.Language,
	}, st, logger)
	if err != nil {
		return err
	}

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if cfg.ScanInterval > 0 {
		go func() {
			t := time.NewTicker(cfg.ScanInterval)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					if n, err := st.Rescan(); err != nil {
						logger.Error("定时扫描失败", "err", err)
					} else {
						logger.Info("定时扫描完成", "books", n)
					}
				}
			}
		}()
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("开始监听", "addr", cfg.Addr, "library", cfg.LibraryDir, "data", cfg.DataDir)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		logger.Info("收到退出信号，正在关闭")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpSrv.Shutdown(shutdownCtx)
}

func newLogger(level string) *slog.Logger {
	var lv slog.Level
	switch level {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: lv}))
}
