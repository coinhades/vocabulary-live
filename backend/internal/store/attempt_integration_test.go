//go:build integration

package store

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"testing"

	"vocabulary.live/internal/domain"
)

func savedPlan(t *testing.T, s *Store, pid string) (domain.AttemptPlan, string) {
	t.Helper()
	raw, err := s.Client.HGet(context.Background(), s.Keys("VOCAB-DEMO")[5], pid).Result()
	if err != nil {
		t.Fatal(err)
	}
	var plan domain.AttemptPlan
	if err := json.Unmarshal([]byte(raw), &plan); err != nil {
		t.Fatal(err)
	}
	return plan, raw
}

func TestStudyUpgradePreservesStoredReceiptsAndAttempts(t *testing.T) {
	s := setup(t, 200)
	ctx := context.Background()
	initial := join(t, s, "VOCAB-DEMO", "learner")
	question, _ := s.Quizzes[initial.QuizID].Question(initial.CurrentQuestion.ID)
	intent := domain.Answer{SubmissionID: "00000000-0000-4000-8000-000000000001", Epoch: initial.Epoch, QuestionID: question.ID, OptionID: question.CorrectOptionID}
	accepted, err := s.Answer(ctx, initial.QuizID, "learner", intent)
	if err != nil {
		t.Fatal(err)
	}
	keys := s.Keys(initial.QuizID)
	stored, err := s.Client.HGet(ctx, keys[3], "learner:"+question.ID).Result()
	if err != nil {
		t.Fatal(err)
	}
	var legacy domain.Receipt
	if err := json.Unmarshal([]byte(stored), &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.Vocabulary != nil || legacy.Question.Timing != nil {
		t.Fatal("supplemental teaching was written to immutable receipt")
	}
	_, planBefore := savedPlan(t, s, "learner")
	if err := s.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	replayed, err := s.Answer(ctx, initial.QuizID, "learner", intent)
	if err != nil || !reflect.DeepEqual(replayed.Receipt, accepted.Receipt) {
		t.Fatal("replay lost teaching or changed receipt", err)
	}
	state, err := s.Snapshot(ctx, initial.QuizID, "learner")
	if err != nil || !reflect.DeepEqual(state.Receipts[0], accepted.Receipt) {
		t.Fatal("reload lost teaching", err)
	}
	if state.Receipts[0].Vocabulary.Word != question.Study.Vocabulary.Word || state.Receipts[0].Vocabulary.PartOfSpeech == "" || state.Receipts[0].Vocabulary.Pronunciation != question.Study.Vocabulary.Pronunciation {
		t.Fatal("missing vocabulary")
	}
	after, err := s.Client.HGet(ctx, keys[3], "learner:"+question.ID).Result()
	if err != nil || after != stored {
		t.Fatal("teaching mutated stored grading receipt", err)
	}
	_, planAfter := savedPlan(t, s, "learner")
	if planAfter != planBefore || state.Epoch != initial.Epoch || state.Score != 100 || state.AnsweredCount != 1 {
		t.Fatal("upgrade changed saved progress")
	}
}

func TestPersistedRandomizedAttemptsAndMaximumScore(t *testing.T) {
	s := setup(t, 200)
	ctx := context.Background()
	s.random = func(int) (int, error) { return 0, nil }
	a := join(t, s, "VOCAB-DEMO", "a")
	second, err := NewWithRandom(s.Client, s.Namespace, s.Quizzes, 200, func(n int) (int, error) { return n - 1, nil })
	if err != nil {
		t.Fatal(err)
	}
	b := join(t, second, "VOCAB-DEMO", "b")
	pa, rawA := savedPlan(t, s, "a")
	pb, rawB := savedPlan(t, s, "b")
	if reflect.DeepEqual(pa.QuestionOrder, pb.QuestionOrder) {
		t.Fatal("deterministic sources should produce different orders")
	}
	sa, sb := append([]string(nil), pa.QuestionOrder...), append([]string(nil), pb.QuestionOrder...)
	sort.Strings(sa)
	sort.Strings(sb)
	if !reflect.DeepEqual(sa, sb) || len(sa) != 8 {
		t.Fatal("question sets differ")
	}
	for _, q := range s.Quizzes[a.QuizID].Questions {
		if reflect.DeepEqual(pa.OptionOrders[q.ID], pb.OptionOrders[q.ID]) {
			t.Fatal("option permutations did not differ")
		}
	}
	// Simulate a restarted backend: seeding, joining and reading existing players
	// must work with a source that cannot generate another plan.
	restarted, err := NewWithRandom(s.Client, s.Namespace, s.Quizzes, 200, func(int) (int, error) { t.Error("resume invoked RNG"); return 0, fmt.Errorf("no RNG on resume") })
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	for _, pid := range []string{"a", "b"} {
		before, err := s.Snapshot(ctx, a.QuizID, pid)
		if err != nil {
			t.Fatal(err)
		}
		after := join(t, restarted, a.QuizID, pid)
		if !reflect.DeepEqual(before, after) {
			t.Fatal("another instance changed private state")
		}
	}
	for pid, initial := range map[string]domain.State{"a": a, "b": b} {
		plan, _ := savedPlan(t, s, pid)
		current, err := s.Snapshot(ctx, initial.QuizID, pid)
		if err != nil {
			t.Fatal(err)
		}
		for i, qid := range plan.QuestionOrder {
			if current.CurrentQuestion == nil || current.CurrentQuestion.ID != qid || current.QuestionNumber != i+1 {
				t.Fatalf("wrong server cursor: %+v", current)
			}
			q, _ := s.Quizzes[initial.QuizID].Question(qid)
			if !reflect.DeepEqual(*current.CurrentQuestion, plan.Present(s.Quizzes[initial.QuizID], qid)) {
				t.Fatal("option order drifted")
			}
			intent := domain.Answer{SubmissionID: fmt.Sprintf("00000000-0000-4000-8000-%012d", i+1), Epoch: initial.Epoch, QuestionID: qid, OptionID: q.CorrectOptionID}
			accepted, err := restarted.Answer(ctx, initial.QuizID, pid, intent)
			if err != nil || accepted.Receipt.PointsAwarded != 100 || accepted.Receipt.QuestionNumber != i+1 || !reflect.DeepEqual(accepted.Receipt.Question, *current.CurrentQuestion) {
				t.Fatal(accepted, err)
			}
			current, err = s.Snapshot(ctx, initial.QuizID, pid)
			if err != nil {
				t.Fatal(err)
			}
		}
		if !current.Completed || current.CurrentQuestion != nil || current.Score != 800 || current.AnsweredCount != 8 {
			t.Fatal(current)
		}
		// Reviewing receipts and retrying with a new ID cannot change competition.
		for _, receipt := range current.Receipts {
			_, err := s.Answer(ctx, initial.QuizID, pid, domain.Answer{SubmissionID: "00000000-0000-4000-8000-000000000999", Epoch: initial.Epoch, QuestionID: receipt.QuestionID, OptionID: receipt.CorrectOptionID})
			assertCode(t, err, "ALREADY_ANSWERED")
		}
		after, err := s.Snapshot(ctx, initial.QuizID, pid)
		if err != nil || !reflect.DeepEqual(current, after) {
			t.Fatal("review mutated state", err)
		}
	}
	_, afterA := savedPlan(t, s, "a")
	_, afterB := savedPlan(t, s, "b")
	if afterA != rawA || afterB != rawB {
		t.Fatal("persisted plans were mutated")
	}
}

func TestSimultaneousFirstJoinChoosesOneImmutablePlan(t *testing.T) {
	s := setup(t, 200)
	ctx := context.Background()
	draw := 0
	s.random = func(n int) (int, error) { draw++; return draw % n, nil }
	var wg sync.WaitGroup
	states := make(chan domain.State, 60)
	for i := 0; i < 60; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := s.Join(ctx, "VOCAB-DEMO", "same", "Same"); err != nil {
				t.Error(err)
				return
			}
			state, err := s.Snapshot(ctx, "VOCAB-DEMO", "same")
			if err != nil {
				t.Error(err)
				return
			}
			states <- state
		}()
	}
	wg.Wait()
	close(states)
	plan, raw := savedPlan(t, s, "same")
	for state := range states {
		if state.Version != 1 || state.ParticipantCount != 1 || !reflect.DeepEqual(*state.CurrentQuestion, plan.Present(s.Quizzes[state.QuizID], plan.QuestionOrder[0])) {
			t.Fatal("racing join returned another plan", state)
		}
	}
	if s.Client.HLen(ctx, s.Keys("VOCAB-DEMO")[5]).Val() != 1 {
		t.Fatal("multiple attempt plans")
	}
	join(t, s, "VOCAB-DEMO", "same")
	_, after := savedPlan(t, s, "same")
	if raw != after {
		t.Fatal("resume replaced plan")
	}
}

func TestAttemptGenerationFailureAndCorruptionConsumeNothing(t *testing.T) {
	s := setup(t, 200)
	ctx := context.Background()
	s.random = func(int) (int, error) { return 0, fmt.Errorf("injected RNG failure") }
	if _, _, err := s.Join(ctx, "VOCAB-DEMO", "p", "Player"); err == nil {
		t.Fatal("accepted failed plan")
	}
	state, err := s.Snapshot(ctx, "VOCAB-DEMO", "")
	if err != nil || state.Version != 0 || state.ParticipantCount != 0 {
		t.Fatal(state, err)
	}
	s.random = func(n int) (int, error) { return n - 1, nil }
	state = join(t, s, "VOCAB-DEMO", "p")
	plan, _ := savedPlan(t, s, "p")
	plan.QuestionOrder[1] = plan.QuestionOrder[0]
	b, _ := json.Marshal(plan)
	if err := s.Client.HSet(ctx, s.Keys(state.QuizID)[5], "p", b).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Answer(ctx, state.QuizID, "p", answer(state, 1, "b")); err == nil {
		t.Fatal("accepted corrupt plan")
	}
	if s.Client.ZScore(ctx, s.Keys(state.QuizID)[2], "p").Val() != 0 || s.Client.HLen(ctx, s.Keys(state.QuizID)[3]).Val() != 0 || s.Client.HGet(ctx, s.Keys(state.QuizID)[0], "version").Val() != "1" {
		t.Fatal("corruption caused partial mutation")
	}
}
