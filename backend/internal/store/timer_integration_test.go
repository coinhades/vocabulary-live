//go:build integration

package store

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"vocabulary.live/internal/domain"
)

func joinWithoutClock(t *testing.T, s *Store, pid string) domain.State {
	t.Helper()
	ctx := context.Background()
	if _, _, err := s.Join(ctx, "VOCAB-DEMO", pid, "Timer learner"); err != nil {
		t.Fatal(err)
	}
	v, err := s.Snapshot(ctx, "VOCAB-DEMO", pid)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func expireQuestion(t *testing.T, s *Store, pid, qid string) int64 {
	t.Helper()
	ctx := context.Background()
	now, err := s.Client.Time(ctx).Result()
	if err != nil {
		t.Fatal(err)
	}
	deadline := now.UnixMilli()
	// Only the isolated test namespace is changed. Production exposes no clock override.
	if err := s.Client.HSet(ctx, s.Keys("VOCAB-DEMO")[6], pid+":"+qid, deadline).Err(); err != nil {
		t.Fatal(err)
	}
	return deadline
}

func TestQuestionClockIsSharedIdempotentAndDoesNotAdvance(t *testing.T) {
	s := setup(t, 200)
	ctx := context.Background()
	v := joinWithoutClock(t, s, "p1")
	other, err := New(s.Client, s.Namespace, s.Quizzes, s.Capacity)
	if err != nil {
		t.Fatal(err)
	}
	q := v.CurrentQuestion
	command := domain.StartQuestion{Epoch: v.Epoch, QuestionID: q.ID}
	first, err := s.StartQuestion(ctx, v.QuizID, "p1", command)
	if err != nil || first.DeadlineMS-first.ServerTimeMS != int64(q.Timing.DurationSeconds*1000) {
		t.Fatal(first, err)
	}
	var wg sync.WaitGroup
	for range 30 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			clock, err := other.StartQuestion(ctx, v.QuizID, "p1", command)
			if err != nil || clock.DeadlineMS != first.DeadlineMS || clock.ServerTimeMS < first.ServerTimeMS {
				t.Errorf("renewed clock: %+v %v", clock, err)
			}
		}()
	}
	wg.Wait()
	after, err := other.Snapshot(ctx, v.QuizID, "p1")
	if err != nil || !reflect.DeepEqual(v, after) {
		t.Fatal("clock changed progress or order", err)
	}
	deadline := expireQuestion(t, s, "p1", q.ID)
	clock, err := other.StartQuestion(ctx, v.QuizID, "p1", command)
	if err != nil || clock.DeadlineMS != deadline || clock.ServerTimeMS < deadline {
		t.Fatal("expired clock renewed", clock, err)
	}
	if size := s.Client.HLen(ctx, s.Keys(v.QuizID)[6]).Val(); size != 1 {
		t.Fatal("future questions started", size)
	}
}

func TestTimedScoringAndImmutableReceiptReplay(t *testing.T) {
	s := setup(t, 200)
	ctx := context.Background()
	v := joinWithoutClock(t, s, "p1")
	firstCommand := answer(v, 1, "b")
	clock, err := s.StartQuestion(ctx, v.QuizID, "p1", domain.StartQuestion{Epoch: v.Epoch, QuestionID: firstCommand.QuestionID})
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.Answer(ctx, v.QuizID, "p1", firstCommand)
	if err != nil || first.Receipt.PointsAwarded != 100 || first.Receipt.TimedOut || first.Receipt.AcceptedAtMS >= clock.DeadlineMS {
		t.Fatal(first, err)
	}
	expireQuestion(t, s, "p1", firstCommand.QuestionID)
	replay, err := s.Answer(ctx, v.QuizID, "p1", firstCommand)
	if err != nil || replay.Outcome != "replayed" || !reflect.DeepEqual(first.Receipt, replay.Receipt) {
		t.Fatal("100-point receipt changed after expiry", replay, err)
	}
	lateCommand := answer(v, 2, "a")
	deadline := expireQuestion(t, s, "p1", lateCommand.QuestionID)
	late, err := s.Answer(ctx, v.QuizID, "p1", lateCommand)
	if err != nil || !late.Receipt.Correctness || !late.Receipt.TimedOut || late.Receipt.PointsAwarded != 50 || late.Receipt.DeadlineMS != deadline || late.Receipt.AcceptedAtMS < deadline {
		t.Fatal(late, err)
	}
	if late.Receipt.TotalScoreAtAcceptance != 150 {
		t.Fatal(late)
	}
	wrongCommand := answer(v, 3, "a")
	expireQuestion(t, s, "p1", wrongCommand.QuestionID)
	wrong, err := s.Answer(ctx, v.QuizID, "p1", wrongCommand)
	if err != nil || wrong.Receipt.Correctness || !wrong.Receipt.TimedOut || wrong.Receipt.PointsAwarded != 0 {
		t.Fatal(wrong, err)
	}
	// Native clients cannot skip /start to get the full 100 points.
	unstarted, err := s.Answer(ctx, v.QuizID, "p1", answer(v, 4, "d"))
	if err != nil || !unstarted.Receipt.TimedOut || unstarted.Receipt.PointsAwarded != 50 {
		t.Fatal(unstarted, err)
	}
	_, err = s.StartQuestion(ctx, v.QuizID, "p1", domain.StartQuestion{Epoch: v.Epoch, QuestionID: "vocab-q4"})
	assertCode(t, err, "QUESTION_ALREADY_ANSWERED")
	for range 2 {
		r, err := s.Answer(ctx, v.QuizID, "p1", lateCommand)
		if err != nil || !reflect.DeepEqual(late.Receipt, r.Receipt) {
			t.Fatal("50-point receipt changed", r, err)
		}
	}
	state, err := s.Snapshot(ctx, v.QuizID, "p1")
	if err != nil || state.Score != 200 || state.AnsweredCount != 4 || state.Version != 5 || len(state.Receipts) != 4 {
		t.Fatal(state, err)
	}
}

func TestConcurrentLateAnswersScoreOnlyOnce(t *testing.T) {
	s := setup(t, 200)
	ctx := context.Background()
	v := joinWithoutClock(t, s, "p1")
	a := answer(v, 1, "b")
	expireQuestion(t, s, "p1", a.QuestionID)
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for range 60 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := s.Answer(ctx, v.QuizID, "p1", a)
			if err != nil {
				t.Error(err)
				return
			}
			if r.Receipt.PointsAwarded != 50 || !r.Receipt.TimedOut {
				t.Errorf("late receipt: %+v", r)
			}
			if r.Outcome == "accepted" {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	state, err := s.Snapshot(ctx, v.QuizID, "p1")
	if err != nil || accepted.Load() != 1 || state.Score != 50 || state.Version != 2 || len(state.Receipts) != 1 {
		t.Fatal(state, accepted.Load(), err)
	}
}

func TestInvalidClockCommandsCannotCreateOrConsumeTimers(t *testing.T) {
	s := setup(t, 200)
	ctx := context.Background()
	v := joinWithoutClock(t, s, "p1")
	for _, c := range []struct{ pid, epoch, qid, code string }{
		{"outsider", v.Epoch, "vocab-q1", "NOT_JOINED"},
		{"p1", strings.Repeat("f", 32), "vocab-q1", "EPOCH_MISMATCH"},
		{"p1", v.Epoch, "travel-q1", "QUESTION_NOT_FOUND"},
		{"p1", "bad", "vocab-q1", "VALIDATION_ERROR"},
	} {
		_, err := s.StartQuestion(ctx, v.QuizID, c.pid, domain.StartQuestion{Epoch: c.epoch, QuestionID: c.qid})
		assertCode(t, err, c.code)
	}
	keys := s.Keys(v.QuizID)
	if s.Client.Exists(ctx, keys[6]).Val() != 0 {
		t.Fatal("rejected command started clock")
	}
	if err := s.Client.HSet(ctx, keys[6], "p1:vocab-q1", "corrupt").Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Answer(ctx, v.QuizID, "p1", answer(v, 1, "b")); err == nil {
		t.Fatal("accepted corrupt timer")
	}
	if _, err := s.StartQuestion(ctx, v.QuizID, "p1", domain.StartQuestion{Epoch: v.Epoch, QuestionID: "vocab-q1"}); err == nil {
		t.Fatal("renewed corrupt timer")
	}
	after, err := s.Snapshot(ctx, v.QuizID, "p1")
	if err != nil || !reflect.DeepEqual(v, after) {
		t.Fatal("invalid timer consumed answer", err)
	}
}

func TestPriorScoringUpgradePreservesOrdersScoresAndReceiptBytes(t *testing.T) {
	s := setup(t, 200)
	ctx := context.Background()
	v := join(t, s, "VOCAB-DEMO", "p1")
	a := answer(v, 1, "b")
	if _, err := s.Answer(ctx, v.QuizID, "p1", a); err != nil {
		t.Fatal(err)
	}
	keys := s.Keys(v.QuizID)
	// Recreate the preceding persisted schema, including its receipt byte shape.
	raw := s.Client.HGet(ctx, keys[3], "p1:vocab-q1").Val()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"timedOut", "deadlineMs", "acceptedAtMs"} {
		delete(fields, name)
	}
	legacyReceipt, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	for key, field := range map[string]string{keys[3]: "p1:vocab-q1", keys[4]: "p1:" + a.SubmissionID} {
		if err := s.Client.HSet(ctx, key, field, string(legacyReceipt)).Err(); err != nil {
			t.Fatal(err)
		}
	}
	var plan domain.AttemptPlan
	if err := json.Unmarshal([]byte(s.Client.HGet(ctx, keys[5], "p1").Val()), &plan); err != nil {
		t.Fatal(err)
	}
	oldOrders := plan
	plan.ContentVersion = s.Quizzes[v.QuizID].LegacyContentVersion
	encoded, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Client.HSet(ctx, keys[5], "p1", string(encoded)).Err(); err != nil {
		t.Fatal(err)
	}
	if err := s.Client.HSet(ctx, keys[0], "content", plan.ContentVersion).Err(); err != nil {
		t.Fatal(err)
	}
	if err := s.Client.Del(ctx, keys[6]).Err(); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.Seed(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var upgraded domain.AttemptPlan
	if err := json.Unmarshal([]byte(s.Client.HGet(ctx, keys[5], "p1").Val()), &upgraded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(oldOrders, upgraded) {
		t.Fatal("migration changed question or option orders")
	}
	after, err := s.Snapshot(ctx, v.QuizID, "p1")
	if err != nil || after.Epoch != v.Epoch || after.Version != 2 || after.Score != 100 || after.AnsweredCount != 1 {
		t.Fatal(after, err)
	}
	if s.Client.HGet(ctx, keys[3], "p1:vocab-q1").Val() != string(legacyReceipt) || s.Client.HGet(ctx, keys[4], "p1:"+a.SubmissionID).Val() != string(legacyReceipt) {
		t.Fatal("migration rewrote immutable receipt")
	}
	r, err := s.Answer(ctx, v.QuizID, "p1", a)
	if err != nil || r.Outcome != "replayed" || r.Receipt.PointsAwarded != 100 || r.Receipt.TimedOut {
		t.Fatal(r, err)
	}
	clock, err := s.StartQuestion(ctx, v.QuizID, "p1", domain.StartQuestion{Epoch: v.Epoch, QuestionID: "vocab-q2"})
	if err != nil || clock.DeadlineMS <= clock.ServerTimeMS {
		t.Fatal(clock, err)
	}
}

func TestPolicyUpgradeRejectsUnknownOrCorruptStateBeforeWriting(t *testing.T) {
	for _, kind := range []string{"unknown", "corrupt-plan"} {
		t.Run(kind, func(t *testing.T) {
			s := setup(t, 200)
			ctx := context.Background()
			v := joinWithoutClock(t, s, "p1")
			keys := s.Keys(v.QuizID)
			old := "unrecognized-version"
			if kind == "corrupt-plan" {
				old = s.Quizzes[v.QuizID].LegacyContentVersion
			}
			if err := s.Client.HSet(ctx, keys[0], "content", old).Err(); err != nil {
				t.Fatal(err)
			}
			before := s.Client.HGetAll(ctx, keys[5]).Val()
			if err := s.Seed(ctx); err == nil {
				t.Fatal("accepted unknown or inconsistent previous state")
			}
			if s.Client.HGet(ctx, keys[0], "content").Val() != old || !reflect.DeepEqual(before, s.Client.HGetAll(ctx, keys[5]).Val()) {
				t.Fatal("failed upgrade partially mutated state")
			}
		})
	}
}
