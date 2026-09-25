//go:build integration

package realtime

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"vocabulary.live/internal/domain"
	"vocabulary.live/internal/fixtures"
	"vocabulary.live/internal/observability"
	"vocabulary.live/internal/store"
)

func TestSlowQueueReplacesSnapshotWithoutBlockingRoom(t *testing.T) {
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://127.0.0.1:6379/0"
	}
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	rc := redis.NewClient(options)
	defer rc.Close()
	s, err := store.New(rc, "queue-test-"+domain.ID(), fixtures.All(), 200)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.Seed(ctx); err != nil {
		t.Fatalf("REQUIRED Redis unavailable: %v", err)
	}
	defer func() {
		for id := range s.Quizzes {
			rc.Del(ctx, s.Keys(id)...)
		}
	}()
	change, _, err := s.Join(ctx, "VOCAB-DEMO", "p1", "Slow player")
	if err != nil {
		t.Fatal(err)
	}
	m := observability.New()
	h := New(s, m, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Hour, time.Hour)
	// Inject a receiver that never drains its queue, at the actual queue boundary.
	slow := &client{pid: "p1", epoch: change.Epoch, queue: make(chan *broadcast, 1)}
	fast := &client{pid: "p1", epoch: change.Epoch, queue: make(chan *broadcast, 1)}
	h.mu.Lock()
	h.rooms[change.QuizID].clients[slow] = true
	h.rooms[change.QuizID].clients[fast] = true
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.rooms[change.QuizID].clients, slow)
		delete(h.rooms[change.QuizID].clients, fast)
		h.mu.Unlock()
		h.Close()
	}()
	h.refresh(change.QuizID)
	<-fast.queue
	answer := domain.Answer{SubmissionID: "00000000-0000-4000-8000-000000000001", Epoch: change.Epoch, QuestionID: "vocab-q1", OptionID: "b"}
	if _, err := s.Answer(ctx, change.QuizID, "p1", answer); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { h.refresh(change.QuizID); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("slow consumer blocked room")
	}
	if len(slow.queue) != 1 {
		t.Fatal("queue not bounded")
	}
	fastEvent := <-fast.queue
	if fastEvent.event.Snapshot.Version != 2 {
		t.Fatal("fast receiver missed update")
	}
	if event := <-slow.queue; event.event.Snapshot.Version != 2 || event.message != fastEvent.message {
		t.Fatal("stale queue not replaced")
	}
	h.refresh(change.QuizID)
	if event := <-fast.queue; event != fastEvent {
		t.Fatal("unchanged reconciliation re-encoded the room snapshot")
	}
}

type waitingRepository struct {
	started    chan struct{}
	finished   chan struct{}
	once       sync.Once
	finishOnce sync.Once
}

func (r *waitingRepository) Get(ctx context.Context, id string) (domain.Quiz, error) {
	r.once.Do(func() { close(r.started) })
	<-ctx.Done()
	r.finishOnce.Do(func() { close(r.finished) })
	return domain.Quiz{}, ctx.Err()
}

func TestSlowRoomDoesNotDelayIndependentRoom(t *testing.T) {
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://127.0.0.1:6379/0"
	}
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	rc := redis.NewClient(options)
	defer rc.Close()
	s, err := store.New(rc, "room-workers-"+domain.ID(), fixtures.All(), 200)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.Seed(ctx); err != nil {
		t.Fatalf("REQUIRED Redis unavailable: %v", err)
	}
	defer func() {
		for _, id := range []string{"VOCAB-DEMO", "TRAVEL-DEMO"} {
			rc.Del(ctx, s.Keys(id)...)
		}
	}()
	fastChange, _, err := s.Join(ctx, "VOCAB-DEMO", "p1", "Fast")
	if err != nil {
		t.Fatal(err)
	}
	slowChange, _, err := s.Join(ctx, "TRAVEL-DEMO", "p2", "Slow")
	if err != nil {
		t.Fatal(err)
	}
	repository := &waitingRepository{started: make(chan struct{}), finished: make(chan struct{})}
	delete(s.Quizzes, "TRAVEL-DEMO")
	s.Repository = repository
	h := New(s, observability.New(), slog.New(slog.NewTextHandler(io.Discard, nil)), 10*time.Millisecond, time.Hour)
	fast := &client{pid: "p1", epoch: fastChange.Epoch, queue: make(chan *broadcast, 1)}
	slow := &client{pid: "p2", epoch: slowChange.Epoch, queue: make(chan *broadcast, 1)}
	h.mu.Lock()
	h.rooms["TRAVEL-DEMO"] = &room{clients: map[*client]bool{slow: true}, dirty: true}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.rooms["TRAVEL-DEMO"].clients, slow)
		delete(h.rooms["VOCAB-DEMO"].clients, fast)
		h.mu.Unlock()
		h.Close()
	}()
	select {
	case <-repository.started:
	case <-time.After(2 * time.Second):
		t.Fatal("slow read never started")
	}
	h.mu.Lock()
	h.rooms["VOCAB-DEMO"].clients[fast] = true
	h.rooms["VOCAB-DEMO"].dirty = true
	h.mu.Unlock()
	select {
	case message := <-fast.queue:
		if message.event.Snapshot == nil || message.event.Snapshot.QuizID != "VOCAB-DEMO" {
			t.Fatal("invalid independent room update")
		}
	case <-repository.finished:
		t.Fatal("healthy room waited for another room's dependency timeout")
	case <-time.After(2 * time.Second):
		t.Fatal("healthy room did not synchronize")
	}
}
