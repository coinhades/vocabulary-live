// learningtest is a dedicated local browser-test executable. It is not included
// in the application Docker image and cannot contact a real AI provider.
package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"vocabulary.live/internal/fixtures"
	"vocabulary.live/internal/httpapi"
	"vocabulary.live/internal/learning"
	"vocabulary.live/internal/store"
	"vocabulary.live/internal/testsupport"
)

func main() {
	options, err := redis.ParseURL(os.Getenv("REDIS_URL"))
	if err != nil {
		panic("test Redis URL required")
	}
	client := redis.NewClient(options)
	defer client.Close()
	// Only this dedicated executable owns this exact test namespace. Remove
	// its previous drafts/publications too, so repeated runs cannot select an
	// old published fixture. Never touch the reviewer demo or other keys.
	var cursor uint64
	for {
		keys, next, scanErr := client.Scan(context.Background(), cursor, "vocab-ai-e2e:*", 100).Result()
		if scanErr != nil {
			panic("test namespace cleanup failed")
		}
		if len(keys) > 0 {
			if err := client.Del(context.Background(), keys...).Err(); err != nil {
				panic("test namespace cleanup failed")
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	s, err := store.New(client, "vocab-ai-e2e", fixtures.All(), 200)
	if err != nil {
		panic(err)
	}
	if err = s.Reset(context.Background()); err != nil {
		panic(err)
	}
	audio, err := os.ReadFile("frontend/tests/fixtures/test-tone.mp3")
	if err != nil {
		panic(err)
	}
	fake := &testsupport.Provider{Audio: audio}
	ai := learning.Config{Key: "test-only", SpeechModel: "test-speech", TextModel: "test-text", Voice: "test-voice", LearningEnabled: true, SpeechEnabled: true, AuthoringEnabled: true, AdminSecret: "test-only-operator-secret-7b210415ac74e91f"}
	app := httpapi.New(s, httpapi.Config{Origins: []string{"http://127.0.0.1:18082"}, Rate: 1000, Burst: 2000, StaticDir: "frontend/dist", AI: ai, Provider: fake}, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	defer app.Close()
	mux := http.NewServeMux()
	mux.Handle("/", app.Handler())
	mux.HandleFunc("POST /__test/provider", func(w http.ResponseWriter, r *http.Request) {
		var c struct {
			Mode    string
			DelayMS int
		}
		if json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&c) != nil || c.DelayMS < 0 || c.DelayMS > 30000 {
			w.WriteHeader(400)
			return
		}
		fake.Set(c.Mode, time.Duration(c.DelayMS)*time.Millisecond)
		w.WriteHeader(204)
	})
	mux.HandleFunc("GET /__test/provider", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]int{"calls": fake.Count()})
	})
	server := &http.Server{Addr: "127.0.0.1:18082", Handler: mux, ReadHeaderTimeout: 3 * time.Second}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go func() {
		if e := server.ListenAndServe(); e != nil && e != http.ErrServerClosed {
			panic(e)
		}
	}()
	<-ctx.Done()
	stop, c := context.WithTimeout(context.Background(), 5*time.Second)
	defer c()
	_ = server.Shutdown(stop)
}
