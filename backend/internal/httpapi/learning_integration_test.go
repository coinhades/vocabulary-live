//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"vocabulary.live/internal/domain"
	"vocabulary.live/internal/learning"
	"vocabulary.live/internal/testsupport"
)

type inspectedProvider struct {
	base   *testsupport.Provider
	change func(string, []byte) []byte
	inputs []string
}

func (p *inspectedProvider) ValidateCredentials(ctx context.Context) error {
	return p.base.ValidateCredentials(ctx)
}

func (p *inspectedProvider) Text(ctx context.Context, r learning.TextRequest) ([]byte, error) {
	raw, _ := json.Marshal(r.Input)
	p.inputs = append(p.inputs, string(raw))
	b, err := p.base.Text(ctx, r)
	if err == nil && p.change != nil {
		b = p.change(r.Action, b)
	}
	return b, err
}
func (p *inspectedProvider) Speech(ctx context.Context, r learning.SpeechRequest) ([]byte, error) {
	return p.base.Speech(ctx, r)
}

func TestStructurallyValidButUnsupportedLearningOutputAndPrivateInputs(t *testing.T) {
	for _, kind := range []string{"meaning", "markup", "refused-reference", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			a, fake := learningApp(t)
			p := newPlayer(t, a)
			st := joinPlayer(t, p, a, "VOCAB-DEMO")
			request(t, p, a.http.URL, "POST", "/api/quizzes/VOCAB-DEMO/answers", command(st.Epoch, 1, "b"), 200)
			before, _ := a.store.Snapshot(context.Background(), st.QuizID, p.pid)
			inspected := &inspectedProvider{base: fake, change: func(_ string, b []byte) []byte {
				var v learning.Explanation
				json.Unmarshal(b, &v)
				switch kind {
				case "meaning":
					v.CanonicalMeaning = "Unsupported meaning"
				case "markup":
					v.Explanation = "<script>alert(1)</script>"
				case "refused-reference":
					v.Supported = false
				case "oversized":
					v.Explanation = strings.Repeat("x", learning.MaxTextBytes+1)
				}
				raw, _ := json.Marshal(v)
				return raw
			}}
			a.app.Learning.Provider = inspected
			_, err := a.app.Learning.Explanation(context.Background(), p.pid, st.QuizID, "vocab-q1", learning.LessonRequest{Epoch: st.Epoch, ClientActionID: "00000000-0000-4000-8000-000000009000"})
			if domain.Code(err) != "AI_INVALID_OUTPUT" {
				t.Fatal("invalid enrichment accepted", err)
			}
			for _, raw := range inspected.inputs {
				if strings.Contains(raw, p.pid) || strings.Contains(raw, "displayName") || strings.Contains(raw, "onerror") || strings.Contains(raw, "vocab_session") {
					t.Fatal("personal input reached provider")
				}
			}
			after, _ := a.store.Snapshot(context.Background(), st.QuizID, p.pid)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("rejected output mutated quiz")
			}
		})
	}
	a, fake := learningApp(t)
	p := newPlayer(t, a)
	st := joinPlayer(t, p, a, "VOCAB-DEMO")
	finishAttempt(t, a, p, st, false)
	a.app.Learning.Provider = &inspectedProvider{base: fake, change: func(_ string, b []byte) []byte {
		var v learning.Reflection
		json.Unmarshal(b, &v)
		v.Suggestions[0].QuestionID = "not-in-this-attempt"
		raw, _ := json.Marshal(v)
		return raw
	}}
	_, err := a.app.Learning.Reflection(context.Background(), p.pid, st.QuizID, learning.LessonRequest{Epoch: st.Epoch, ClientActionID: "00000000-0000-4000-8000-000000009001"})
	if domain.Code(err) != "AI_INVALID_OUTPUT" {
		t.Fatal("unknown reflection reference accepted", err)
	}
}

func learningApp(t *testing.T) (*testApp, *testsupport.Provider) {
	a := newTestApp(t, "", "", nil, nil, func(n int) (int, error) { return n - 1, nil })
	fake := &testsupport.Provider{Audio: []byte{0xff, 0xfb, 0x90, 0, 0, 0, 0, 0}}
	a.app.Learning.Close()
	c := learning.Config{Key: "test-only", TextModel: "test", SpeechModel: "test", Voice: "test", LearningEnabled: true, SpeechEnabled: true}
	a.app.Learning = learning.New(a.store, c, fake, nil)
	return a, fake
}

func TestRejectedCredentialsHideCapabilitiesAndBlockCachedLearning(t *testing.T) {
	a, fake := learningApp(t)
	p := newPlayer(t, a)
	st := joinPlayer(t, p, a, "VOCAB-DEMO")
	request(t, p, a.http.URL, "POST", "/api/quizzes/VOCAB-DEMO/answers", command(st.Epoch, 1, "b"), 200)
	before, _ := a.store.Snapshot(context.Background(), st.QuizID, p.pid)
	action := learning.LessonRequest{Epoch: st.Epoch, ClientActionID: "00000000-0000-4000-8000-000000009321"}
	path := "/api/quizzes/VOCAB-DEMO/questions/vocab-q1/explanation"
	request(t, p, a.http.URL, "POST", path, action, 200)
	calls := fake.Count()
	fake.Set("invalid-key", 0)
	raw := request(t, p, a.http.URL, "GET", "/api/learning-capabilities", nil, 200)
	var caps learning.Capabilities
	if json.Unmarshal(raw, &caps) != nil || caps.TextConfigured || caps.SpeechConfigured {
		t.Fatalf("rejected key enabled learning: %s", raw)
	}
	for _, endpoint := range []string{path, "/api/quizzes/VOCAB-DEMO/questions/vocab-q1/pronunciation"} {
		body := any(action)
		if strings.HasSuffix(endpoint, "pronunciation") {
			body = map[string]string{"epoch": st.Epoch}
		}
		raw = request(t, p, a.http.URL, "POST", endpoint, body, 503)
		if !strings.Contains(string(raw), "AI_CREDENTIALS_INVALID") {
			t.Fatalf("wrong credential error: %s", raw)
		}
	}
	after, _ := a.store.Snapshot(context.Background(), st.QuizID, p.pid)
	if !reflect.DeepEqual(before, after) || fake.Count() != calls {
		t.Fatal("credential rejection changed quiz or generated output")
	}
	fake.Set("", 0)
	request(t, p, a.http.URL, "POST", path, action, 200)
	if fake.Count() != calls {
		t.Fatal("cached learning regenerated after recovery")
	}
}

func finishAttempt(t *testing.T, a *testApp, p *player, state domain.State, allCorrect bool) domain.State {
	t.Helper()
	for i, q := range a.store.Quizzes[state.QuizID].Questions {
		option := q.CorrectOptionID
		if !allCorrect {
			for _, o := range q.Options {
				if o.ID != q.CorrectOptionID {
					option = o.ID
					break
				}
			}
		}
		request(t, p, a.http.URL, "POST", "/api/quizzes/"+state.QuizID+"/answers", domain.Answer{SubmissionID: fmt.Sprintf("00000000-0000-4000-8000-%012d", i+1), Epoch: state.Epoch, QuestionID: q.ID, OptionID: option}, 200)
	}
	result, err := a.store.Snapshot(context.Background(), state.QuizID, p.pid)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func TestReviewIsolationFrozenPlanConcurrentChecksHintsAndCrossInstance(t *testing.T) {
	a, fake := learningApp(t)
	p := newPlayer(t, a)
	state := joinPlayer(t, p, a, "VOCAB-DEMO")
	start := learning.ReviewStart{Epoch: state.Epoch, ClientActionID: "00000000-0000-4000-8000-000000000081", UseAI: true}
	request(t, p, a.http.URL, "POST", "/api/quizzes/VOCAB-DEMO/reviews", start, 403)
	if fake.Count() != 0 {
		t.Fatal("precompletion generated")
	}
	before := finishAttempt(t, a, p, state, false)
	if before.CorrectCount != 0 || before.Accuracy != 0 {
		t.Fatal("incorrect server metrics")
	}
	raw := request(t, p, a.http.URL, "POST", "/api/quizzes/VOCAB-DEMO/reviews", start, 200)
	var review learning.ReviewState
	_ = json.Unmarshal(raw, &review)
	if review.TotalCards != 3 || review.CurrentCard.Source != "ai" {
		t.Fatalf("review plan: %s", raw)
	}
	again := request(t, p, a.http.URL, "POST", "/api/quizzes/VOCAB-DEMO/reviews", start, 200)
	if string(raw) != string(again) {
		t.Fatal("start replay changed plan")
	}
	b := newTestApp(t, a.store.Namespace, "", nil, nil)
	b.app.Learning.Close()
	b.app.Learning = learning.New(b.store, a.app.Learning.Config, fake, nil)
	other, err := b.app.Learning.GetReview(context.Background(), p.pid, review.ReviewSessionID)
	if err != nil || !reflect.DeepEqual(review, other) {
		t.Fatal("second instance inconsistent", err)
	}
	outsider := newPlayer(t, a)
	request(t, outsider, a.http.URL, "GET", "/api/reviews/"+review.ReviewSessionID, nil, 404)
	card := review.CurrentCard
	hinted, err := a.app.Learning.ReviewHint(context.Background(), p.pid, review.ReviewSessionID, card.ID, state.Epoch)
	if err != nil || hinted.Hint == nil {
		t.Fatal("hint failed", err)
	}
	calls := fake.Count()
	_, err = b.app.Learning.ReviewHint(context.Background(), p.pid, review.ReviewSessionID, card.ID, state.Epoch)
	if err != nil || fake.Count() != calls {
		t.Fatal("hint replay regenerated")
	}
	source, _ := a.store.Quizzes["VOCAB-DEMO"].Question(card.QuestionID)
	wrong := ""
	for _, o := range card.Options {
		if o.ID != source.CorrectOptionID {
			wrong = o.ID
			break
		}
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, e := b.app.Learning.ReviewAction(context.Background(), p.pid, review.ReviewSessionID, card.ID, "answers", learning.ReviewAction{Epoch: state.Epoch, ClientActionID: fmt.Sprintf("00000000-0000-4000-8000-%012d", 100+i), OptionID: wrong})
			if e != nil {
				t.Error(e)
			}
		}(i)
	}
	wg.Wait()
	current, err := a.app.Learning.GetReview(context.Background(), p.pid, review.ReviewSessionID)
	if err != nil || current.Checked != 1 || current.TotalCards != 4 || current.Feedback == nil || !current.Feedback.HintUsed {
		t.Fatal("duplicate review progress", current, err)
	}
	if _, err = a.app.Learning.ReviewHint(context.Background(), p.pid, review.ReviewSessionID, card.ID, state.Epoch); domain.Code(err) != "LEARNING_INELIGIBLE" {
		t.Fatal("hint after check", err)
	}
	after, _ := a.store.Snapshot(context.Background(), state.QuizID, p.pid)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("review changed competitive state")
	}
	if err = a.store.Reset(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = a.app.Learning.GetReview(context.Background(), p.pid, review.ReviewSessionID); err == nil {
		t.Fatal("review crossed reset")
	}
}
func TestReflectionAllCorrectAndCanonicalReviewWithoutProvider(t *testing.T) {
	a, fake := learningApp(t)
	p := newPlayer(t, a)
	before := finishAttempt(t, a, p, joinPlayer(t, p, a, "VOCAB-DEMO"), true)
	if before.CorrectCount != 8 || before.Accuracy != 100 {
		t.Fatal("wrong server metrics")
	}
	req := learning.LessonRequest{Epoch: before.Epoch, ClientActionID: "00000000-0000-4000-8000-000000000098"}
	raw := request(t, p, a.http.URL, "POST", "/api/quizzes/VOCAB-DEMO/learning-reflection", req, 200)
	var aReflection learning.Artifact
	_ = json.Unmarshal(raw, &aReflection)
	var reflection learning.Reflection
	_ = json.Unmarshal(aReflection.Data, &reflection)
	if reflection.Summary != "In this practice, no words were missed. Extra practice is optional." {
		t.Fatal("all-correct invented weakness")
	}
	start := learning.ReviewStart{Epoch: before.Epoch, ClientActionID: "00000000-0000-4000-8000-000000000099"}
	request(t, p, a.http.URL, "POST", "/api/quizzes/VOCAB-DEMO/reviews", start, 403)
	a.app.Learning.Config.Key = ""
	start.ExtraPractice = true
	start.UseAI = true
	calls := fake.Count()
	raw = request(t, p, a.http.URL, "POST", "/api/quizzes/VOCAB-DEMO/reviews", start, 200)
	var review learning.ReviewState
	_ = json.Unmarshal(raw, &review)
	if review.CurrentCard.Source != "canonical" || review.Fallback == "" || fake.Count() != calls {
		t.Fatal("missing key did not use canonical review")
	}
	after, _ := a.store.Snapshot(context.Background(), before.QuizID, p.pid)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("reflection/review changed score")
	}
}
func TestLearningVisibilityGroundingIdempotencyAndNoCompetitiveMutation(t *testing.T) {
	a, fake := learningApp(t)
	p := newPlayer(t, a)
	state := joinPlayer(t, p, a, "VOCAB-DEMO")
	other := newPlayer(t, a)
	path := "/api/quizzes/VOCAB-DEMO/questions/vocab-q1/"
	req := learning.LessonRequest{Epoch: state.Epoch, ClientActionID: "00000000-0000-4000-8000-000000000010"}
	request(t, other, a.http.URL, "POST", path+"explanation", req, 403)
	request(t, p, a.http.URL, "POST", path+"explanation", req, 403)
	request(t, p, a.http.URL, "POST", "/api/quizzes/VOCAB-DEMO/questions/vocab-q2/pronunciation", map[string]string{"epoch": state.Epoch}, 403)
	request(t, p, a.http.URL, "POST", path+"pronunciation", map[string]string{"epoch": domain.ID()}, 409)
	request(t, p, a.http.URL, "POST", path+"pronunciation", map[string]string{"epoch": state.Epoch, "text": "hidden answer"}, 400)
	if fake.Count() != 0 {
		t.Fatal("unauthorized action reached provider")
	}
	request(t, p, a.http.URL, "POST", path+"pronunciation", map[string]string{"epoch": state.Epoch}, 200)
	request(t, p, a.http.URL, "POST", "/api/quizzes/VOCAB-DEMO/answers", command(state.Epoch, 1, "a"), 200)
	before, err := a.store.Snapshot(context.Background(), "VOCAB-DEMO", p.pid)
	if err != nil {
		t.Fatal(err)
	}
	raw := request(t, p, a.http.URL, "POST", path+"explanation", req, 200)
	var artifact learning.Artifact
	_ = json.Unmarshal(raw, &artifact)
	var explanation learning.Explanation
	_ = json.Unmarshal(artifact.Data, &explanation)
	if artifact.AcceptedReceiptID != before.Receipts[0].SubmissionID || explanation.Contrast == "" || artifact.Source != "ai" {
		t.Fatal("not grounded in stored wrong choice")
	}
	calls := fake.Count()
	raw2 := request(t, p, a.http.URL, "POST", path+"explanation", req, 200)
	if string(raw) != string(raw2) || fake.Count() != calls {
		t.Fatal("retry regenerated")
	}
	ex := req
	ex.Context = "travel"
	ex.ClientActionID = "00000000-0000-4000-8000-000000000011"
	request(t, p, a.http.URL, "POST", path+"examples", ex, 200)
	ex.Context = "work"
	request(t, p, a.http.URL, "POST", path+"examples", ex, 409)
	ex.Variant = 3
	request(t, p, a.http.URL, "POST", path+"examples", ex, 400)
	request(t, other, a.http.URL, "POST", "/api/learning-artifacts/"+artifact.ArtifactID+"/reports", map[string]string{"reason": "confusing"}, 404)
	request(t, p, a.http.URL, "POST", "/api/learning-artifacts/"+artifact.ArtifactID+"/reports", map[string]string{"reason": "confusing"}, 200)
	after, err := a.store.Snapshot(context.Background(), "VOCAB-DEMO", p.pid)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("learning mutated competition")
	}
}
func TestLearningOutputAfterResetIsDiscardedAndMissingKeyWorks(t *testing.T) {
	a, fake := learningApp(t)
	p := newPlayer(t, a)
	state := joinPlayer(t, p, a, "VOCAB-DEMO")
	request(t, p, a.http.URL, "POST", "/api/quizzes/VOCAB-DEMO/answers", command(state.Epoch, 1, "b"), 200)
	fake.Set("", 120*time.Millisecond)
	done := make(chan error, 1)
	go func() {
		_, err := a.app.Learning.Explanation(context.Background(), p.pid, "VOCAB-DEMO", "vocab-q1", learning.LessonRequest{Epoch: state.Epoch, ClientActionID: "00000000-0000-4000-8000-000000000020"})
		done <- err
	}()
	deadline := time.Now().Add(time.Second)
	for fake.Count() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("provider not started")
		}
		time.Sleep(time.Millisecond)
	}
	if err := a.store.Reset(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-done; domain.Code(err) != "NOT_JOINED" && domain.Code(err) != "EPOCH_MISMATCH" {
		t.Fatalf("old generation returned: %v", err)
	}
	plain := newTestApp(t, "", "", nil, nil)
	person := newPlayer(t, plain)
	fresh := joinPlayer(t, person, plain, "VOCAB-DEMO")
	request(t, person, plain.http.URL, "POST", "/api/quizzes/VOCAB-DEMO/answers", command(fresh.Epoch, 1, "b"), 200)
	request(t, person, plain.http.URL, "POST", "/api/quizzes/VOCAB-DEMO/questions/vocab-q1/explanation", learning.LessonRequest{Epoch: fresh.Epoch, ClientActionID: fmt.Sprintf("00000000-0000-4000-8000-%012d", 1)}, 503)
	request(t, person, plain.http.URL, "GET", "/readyz", nil, 200)
}
