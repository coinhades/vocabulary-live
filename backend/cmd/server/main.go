package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"vocabulary.live/internal/config"
	"vocabulary.live/internal/fixtures"
	"vocabulary.live/internal/httpapi"
	"vocabulary.live/internal/learning"
	"vocabulary.live/internal/store"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	reset := flag.Bool("demo-reset", false, "development only: reset the configured namespace's two quiz rooms")
	health := flag.Bool("healthcheck", false, "check the local readiness endpoint and exit")
	flag.Parse()
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	if *health {
		c := http.Client{Timeout: 2 * time.Second}
		r, err := c.Get(cfg.HealthURL())
		if err != nil {
			return err
		}
		defer r.Body.Close()
		if r.StatusCode != 200 {
			return fmt.Errorf("not ready: %d", r.StatusCode)
		}
		return nil
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	if cfg.Production() {
		if info, err := os.Stat(filepath.Join(cfg.StaticDir, "index.html")); err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("STATIC_DIR must contain the built frontend index.html in production")
		}
	}
	client := redis.NewClient(cfg.Redis)
	defer client.Close()
	s, err := store.New(client, cfg.Namespace, fixtures.All(), 200)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if *reset {
		if cfg.Environment != "development" || os.Getenv("ALLOW_DEMO_RESET") != "yes" {
			return fmt.Errorf("reset requires APP_ENV=development and ALLOW_DEMO_RESET=yes")
		}
		return s.Reset(ctx)
	}
	if err := s.Seed(ctx); err != nil {
		return err
	}
	ai := learning.FromEnv()
	logger.Info("optional learning configuration", "speechConfigured", ai.SpeechAvailable(), "textConfigured", ai.TextAvailable(), "studioEnabled", ai.StudioAvailable())
	app := httpapi.New(s, httpapi.Config{Origins: cfg.Origins, SecureCookie: cfg.Production(), StaticDir: cfg.StaticDir, Rate: cfg.Rate, Burst: cfg.Burst, TrustedProxies: cfg.TrustedProxies, AI: ai}, logger, nil)
	server := &http.Server{Addr: cfg.HTTPAddr, Handler: app.Handler(), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 55 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	sig, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() { logger.Info("listening", "address", server.Addr); done <- server.ListenAndServe() }()
	select {
	case err := <-done:
		app.Close()
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-sig.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		app.Close()
		if err := server.Shutdown(shutdown); err != nil {
			return err
		}
	}
	return nil
}
