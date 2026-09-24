//go:build integration

package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"vocabulary.live/internal/domain"
	"vocabulary.live/internal/fixtures"
)

func setup(t *testing.T, capacity int) *Store {
	t.Helper()
	url := os.Getenv("REDIS_URL")
	if url == "" {
		url = "redis://127.0.0.1:6379/0"
	}
	o, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	o.MaxRetries = 0
	o.ReadTimeout = time.Second
	o.WriteTimeout = time.Second
	c := redis.NewClient(o)
	ctx := context.Background()
	if err := c.Ping(ctx).Err(); err != nil {
		t.Fatalf("REQUIRED real Redis unavailable (not skipped): %v", err)
	}
	s, err := New(c, "test-"+domain.ID(), fixtures.All(), capacity)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for id := range s.Quizzes {
			if err := c.Del(ctx, s.Keys(id)...).Err(); err != nil {
				t.Error(err)
			}
		}
		_ = c.Close()
	})
	if err := s.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	return s
}
func join(t *testing.T, s *Store, id, pid string) domain.State {
	t.Helper()
	if _, _, err := s.Join(context.Background(), id, pid, "Player "+pid); err != nil {
		t.Fatal(err)
	}
	// Existing concurrency/scoring cases exercise answers inside their window.
	// Dedicated timer tests start individual questions and control expiry.
	initial, err := s.Snapshot(context.Background(), id, pid)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range s.Quizzes[id].Questions {
		if _, err := s.StartQuestion(context.Background(), id, pid, domain.StartQuestion{Epoch: initial.Epoch, QuestionID: q.ID}); err != nil {
			t.Fatal(err)
		}
	}
	v, err := s.Snapshot(context.Background(), id, pid)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func answer(state domain.State, n int, option string) domain.Answer {
	return domain.Answer{SubmissionID: fmt.Sprintf("00000000-0000-4000-8000-%012d", n+1), Epoch: state.Epoch, QuestionID: fmt.Sprintf("vocab-q%d", n), OptionID: option}
}
func assertCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil || domain.Code(err) != code {
		t.Fatalf("want %s got %v", code, err)
	}
}
func TestJoinCapacityAndConcurrentResume(t *testing.T) {
	s := setup(t, 2)
	ctx := context.Background()
	v := join(t, s, "VOCAB-DEMO", "p1")
	a := answer(v, 1, "b")
	if _, err := s.Answer(ctx, v.QuizID, "p1", a); err != nil {
		t.Fatal(err)
	}
	join(t, s, v.QuizID, "p2")
	_, _, err := s.Join(ctx, v.QuizID, "p3", "Third")
	assertCode(t, err, "QUIZ_FULL")
	var wg sync.WaitGroup
	for i := 0; i < 60; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, changed, err := s.Join(ctx, v.QuizID, "p1", "Renamed"); err != nil || changed {
				t.Errorf("resume changed: %v %v", changed, err)
			}
		}()
	}
	wg.Wait()
	current, err := s.Snapshot(ctx, v.QuizID, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if current.Version != 3 || current.ParticipantCount != 2 || current.Leaderboard[0].Score != 100 || current.Leaderboard[0].DisplayName != "Player p1" {
		t.Fatalf("state reset: %+v", current)
	}
	_, _, err = s.Join(ctx, "UNKNOWN", "p1", "Name")
	assertCode(t, err, "QUIZ_NOT_FOUND")
}

func TestConcurrentFirstJoinsAndCapacity(t *testing.T) {
	s := setup(t, 2)
	ctx := context.Background()
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 60; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, changed, err := s.Join(ctx, "VOCAB-DEMO", "shared", "First name")
			if err != nil {
				t.Error(err)
			}
			if changed {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("first membership accepted %d times", accepted.Load())
	}
	accepted.Store(0)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, changed, err := s.Join(ctx, "VOCAB-DEMO", fmt.Sprintf("contender-%d", i), "Next")
			if err != nil && domain.Code(err) != "QUIZ_FULL" {
				t.Error(err)
			}
			if changed {
				accepted.Add(1)
			}
		}(i)
	}
	wg.Wait()
	current, err := s.Snapshot(ctx, "VOCAB-DEMO", "shared")
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Load() != 1 || current.ParticipantCount != 2 || current.Version != 2 {
		t.Fatalf("capacity race: accepted=%d state=%+v", accepted.Load(), current)
	}
}
func TestAcceptanceReplayAndUniqueness(t *testing.T) {
	s := setup(t, 200)
	ctx := context.Background()
	v := join(t, s, "VOCAB-DEMO", "p1")
	a := answer(v, 1, "b")
	first, err := s.Answer(ctx, v.QuizID, "p1", a)
	if err != nil || first.Outcome != "accepted" || first.Receipt.PointsAwarded != 100 {
		t.Fatal(first, err)
	}
	second := answer(v, 2, "b")
	wrong, err := s.Answer(ctx, v.QuizID, "p1", second)
	if err != nil || wrong.Receipt.Correctness || wrong.Receipt.PointsAwarded != 0 || wrong.Receipt.AcceptedVersion != 3 {
		t.Fatal(wrong, err)
	}
	replay, err := s.Answer(ctx, v.QuizID, "p1", a)
	if err != nil || replay.Outcome != "replayed" || !reflect.DeepEqual(first.Receipt, replay.Receipt) {
		t.Fatal(replay, err)
	}
	conflict := a
	conflict.OptionID = "a"
	_, err = s.Answer(ctx, v.QuizID, "p1", conflict)
	assertCode(t, err, "IDEMPOTENCY_CONFLICT")
	conflict.QuestionID = "vocab-q3"
	_, err = s.Answer(ctx, v.QuizID, "p1", conflict)
	assertCode(t, err, "IDEMPOTENCY_CONFLICT")
	second.SubmissionID = "00000000-0000-4000-8000-000000000099"
	second.OptionID = "a"
	_, err = s.Answer(ctx, v.QuizID, "p1", second)
	assertCode(t, err, "ALREADY_ANSWERED")
	receiptErr := err.(*domain.Error)
	if !reflect.DeepEqual(*receiptErr.Receipt, wrong.Receipt) {
		t.Fatal("original receipt changed")
	}
	newer := answer(v, 3, "c")
	if _, err := s.Answer(ctx, v.QuizID, "p1", newer); err != nil {
		t.Fatal(err)
	}
	replay, err = s.Answer(ctx, v.QuizID, "p1", a)
	if err != nil || replay.Receipt.TotalScoreAtAcceptance != 100 {
		t.Fatal(replay, err)
	}
	current, err := s.Snapshot(ctx, v.QuizID, "p1")
	if err != nil || current.Version != 4 || current.Leaderboard[0].Score != 200 || len(current.Receipts) != 3 {
		t.Fatal(current, err)
	}
}
func TestInvalidDoesNotConsumeAndIsolation(t *testing.T) {
	s := setup(t, 200)
	ctx := context.Background()
	v := join(t, s, "VOCAB-DEMO", "p1")
	a := answer(v, 1, "z")
	_, err := s.Answer(ctx, v.QuizID, "p1", a)
	assertCode(t, err, "INVALID_OPTION")
	a.OptionID = "b"
	a.QuestionID = "travel-q1"
	_, err = s.Answer(ctx, v.QuizID, "p1", a)
	assertCode(t, err, "QUESTION_NOT_FOUND")
	a.QuestionID = "missing"
	_, err = s.Answer(ctx, v.QuizID, "p1", a)
	assertCode(t, err, "QUESTION_NOT_FOUND")
	a.QuestionID = "vocab-q1"
	if result, err := s.Answer(ctx, v.QuizID, "p1", a); err != nil || result.Outcome != "accepted" {
		t.Fatal(result, err)
	}
	_, err = s.Answer(ctx, v.QuizID, "outsider", a)
	assertCode(t, err, "NOT_JOINED")
	_, err = s.Snapshot(ctx, "TRAVEL-DEMO", "p1")
	assertCode(t, err, "NOT_JOINED")
	other := join(t, s, "TRAVEL-DEMO", "p1")
	if other.Version != 1 || other.Leaderboard[0].Score != 0 || len(other.Receipts) != 0 {
		t.Fatal("quiz state leaked", other)
	}
}
func TestSixtyConcurrentDuplicateAnswers(t *testing.T) {
	s := setup(t, 200)
	v := join(t, s, "VOCAB-DEMO", "p1")
	a := answer(v, 1, "b")
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 60; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := s.Answer(context.Background(), v.QuizID, "p1", a)
			if err != nil {
				t.Error(err)
			}
			if r.Outcome == "accepted" {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	current, err := s.Snapshot(context.Background(), v.QuizID, "p1")
	if err != nil || accepted.Load() != 1 || current.Version != 2 || current.Leaderboard[0].Score != 100 || len(current.Receipts) != 1 {
		t.Fatal(accepted.Load(), current, err)
	}
}
func TestConcurrentCompetingOptionsAndDifferentQuestions(t *testing.T) {
	s := setup(t, 200)
	v := join(t, s, "VOCAB-DEMO", "p1")
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			a := answer(v, 1, string(rune('a'+i%4)))
			a.SubmissionID = fmt.Sprintf("00000000-0000-4000-8000-%012d", i+1)
			r, err := s.Answer(context.Background(), v.QuizID, "p1", a)
			if err != nil && domain.Code(err) != "ALREADY_ANSWERED" {
				t.Error(err)
			}
			if r.Outcome == "accepted" {
				accepted.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatal("multiple winners", accepted.Load())
	}
	for n, opt := range map[int]string{2: "a", 3: "c", 4: "d", 5: "a", 6: "c", 7: "b", 8: "d"} {
		wg.Add(1)
		go func(n int, opt string) {
			defer wg.Done()
			a := answer(v, n, opt)
			a.SubmissionID = fmt.Sprintf("00000000-0000-4000-8000-%012d", n+100)
			if _, err := s.Answer(context.Background(), v.QuizID, "p1", a); err != nil {
				t.Error(err)
			}
		}(n, opt)
	}
	wg.Wait()
	current, err := s.Snapshot(context.Background(), v.QuizID, "p1")
	if err != nil {
		t.Fatal(err)
	}
	want := 700 + current.Receipts[0].PointsAwarded
	if current.Leaderboard[0].Score != want || len(current.Receipts) != 8 || current.Version != 9 {
		t.Fatal(current)
	}
}
func TestSnapshotConsistentRanksAndZeroPlayers(t *testing.T) {
	s := setup(t, 200)
	ctx := context.Background()
	for _, p := range []string{"p1", "p2", "p3", "p4"} {
		v := join(t, s, "VOCAB-DEMO", p)
		if p == "p4" {
			continue
		}
		for n, opt := range map[int]string{1: "b", 2: "a"} {
			if p == "p3" && n == 2 {
				continue
			}
			if _, err := s.Answer(ctx, v.QuizID, p, answer(v, n, opt)); err != nil {
				t.Fatal(err)
			}
		}
	}
	v, err := s.Snapshot(ctx, "VOCAB-DEMO", "")
	if err != nil {
		t.Fatal(err)
	}
	for i, rank := range []int{1, 1, 3, 4} {
		if v.Leaderboard[i].Rank != rank {
			t.Fatal(v)
		}
	}
	if len(v.Receipts) != 0 || v.Version != 9 {
		t.Fatal(v)
	}
	// Every accepted operation changes the version and summed progress together.
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			state, err := s.Snapshot(ctx, v.QuizID, "")
			if err != nil {
				t.Error(err)
				return
			}
			count := state.ParticipantCount
			for _, row := range state.Leaderboard {
				count += row.AnsweredCount
			}
			if int64(count) != state.Version {
				t.Errorf("torn snapshot: %+v", state)
			}
		}()
	}
	if _, err := s.Answer(ctx, v.QuizID, "p3", answer(v, 2, "a")); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
}
func TestRepeatedSeedResetEpochAndStoragePreflight(t *testing.T) {
	s := setup(t, 200)
	ctx := context.Background()
	v := join(t, s, "VOCAB-DEMO", "p1")
	a := answer(v, 1, "b")
	if _, err := s.Answer(ctx, v.QuizID, "p1", a); err != nil {
		t.Fatal(err)
	}
	before, err := s.Snapshot(ctx, v.QuizID, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := s.Snapshot(ctx, v.QuizID, "p1")
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("seed changed state", err)
	}
	if err := s.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	next := join(t, s, v.QuizID, "p1")
	if next.Epoch == v.Epoch {
		t.Fatal("epoch reused")
	}
	_, err = s.Answer(ctx, v.QuizID, "p1", a)
	assertCode(t, err, "EPOCH_MISMATCH")
	keys := s.Keys(v.QuizID)
	if err := s.Client.Set(ctx, keys[4], "corrupt", 0).Err(); err != nil {
		t.Fatal(err)
	}
	a.Epoch = next.Epoch
	if _, err := s.Answer(ctx, v.QuizID, "p1", a); err == nil {
		t.Fatal("accepted corrupt key")
	}
	if score := s.Client.ZScore(ctx, keys[2], "p1").Val(); score != 0 {
		t.Fatal("partial score mutation")
	}
	if version := s.Client.HGet(ctx, keys[0], "version").Val(); version != "1" {
		t.Fatal("partial version mutation")
	}
	if err := s.Client.Del(ctx, keys[4]).Err(); err != nil {
		t.Fatal(err)
	}
	if err := s.Client.HSet(ctx, keys[0], "version", "not-a-number").Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Answer(ctx, v.QuizID, "p1", a); err == nil {
		t.Fatal("accepted bad version")
	}
	if s.Client.HLen(ctx, keys[3]).Val() != 0 {
		t.Fatal("partial receipt mutation")
	}
}
func TestSessionDigestAndExpiry(t *testing.T) {
	s := setup(t, 200)
	ctx := context.Background()
	credential, pid, err := s.CreateSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	key := s.Namespace + ":session:" + digest(credential)
	defer s.Client.Del(ctx, key)
	if got, err := s.Session(ctx, credential); err != nil || got != pid {
		t.Fatal(got, err)
	}
	ttl, err := s.SessionTTL(ctx, credential)
	if err != nil || ttl < 6*24*time.Hour {
		t.Fatal(ttl, err)
	}
	if s.Client.Exists(ctx, s.Namespace+":session:"+credential).Val() != 0 {
		t.Fatal("raw credential stored")
	}
	s.Client.Del(ctx, key)
	_, err = s.Session(ctx, credential)
	assertCode(t, err, "UNAUTHENTICATED")
}
func TestPublicContractHasNoGradingData(t *testing.T) {
	s := setup(t, 200)
	v := join(t, s, "VOCAB-DEMO", "p1")
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		t.Fatal(err)
	}
	if v.CurrentQuestion == nil || len(v.CurrentQuestion.Options) != 4 || v.TotalQuestions != 8 || v.QuestionNumber != 1 || v.Policies != domain.LivePractice() {
		t.Fatal(v)
	}
	if _, leaked := fields["questions"]; leaked {
		t.Fatal("full question sequence leaked into state")
	}
	if len(v.Receipts) != 0 {
		t.Fatal("unanswered feedback leaked")
	}
}
