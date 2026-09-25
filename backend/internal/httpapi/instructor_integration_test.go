//go:build integration

package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/redis/go-redis/v9"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"vocabulary.live/internal/domain"
	"vocabulary.live/internal/learning"
	"vocabulary.live/internal/testsupport"
)

const testOperatorSecret = "test-only-operator-secret-7b210415ac74e91f"

func studioApp(t *testing.T) (*testApp, *player) {
	a, _ := learningApp(t)
	a.app.Learning.Config.AuthoringEnabled = true
	a.app.Learning.Config.AdminSecret = testOperatorSecret
	p := newPlayer(t, a)
	request(t, p, a.http.URL, "POST", "/api/instructor/session", map[string]string{"secret": testOperatorSecret}, 200)
	return a, p
}
func decodeDraft(t *testing.T, b []byte) learning.Draft {
	t.Helper()
	var d learning.Draft
	if json.Unmarshal(b, &d) != nil || d.ID == "" {
		t.Fatalf("invalid draft %s", b)
	}
	return d
}
func createTestDraft(t *testing.T, a *testApp, p *player, count int) learning.Draft {
	return decodeDraft(t, request(t, p, a.http.URL, "POST", "/api/instructor/drafts", learning.DraftCreate{ClientActionID: "00000000-0000-4000-8000-000000005000", Brief: learning.AuthoringBrief{Topic: "Travel practice", Context: "travel", Level: "B1", Count: count, TargetWords: []string{}}}, 200))
}
func mutation(version, n int) learning.DraftMutation {
	return learning.DraftMutation{ExpectedVersion: version, ClientActionID: fmt.Sprintf("00000000-0000-4000-8000-%012d", n)}
}
func edits(d learning.Draft) learning.DraftSave {
	r := learning.DraftSave{DraftMutation: mutation(d.Version, 5100+d.Version), Title: d.Title, Items: []learning.DraftEditItem{}}
	for _, i := range d.Items {
		r.Items = append(r.Items, learning.DraftEditItem{ID: i.ID, Content: i.Content})
	}
	return r
}

func TestInstructorAuthorizationCookieOriginExpiryAndLimits(t *testing.T) {
	a, _ := learningApp(t)
	p := newPlayer(t, a)
	request(t, p, a.http.URL, "GET", "/api/instructor/drafts", nil, 404)
	a.app.Learning.Config.AuthoringEnabled = true
	request(t, p, a.http.URL, "POST", "/api/instructor/session", map[string]string{"secret": ""}, 404)
	a.app.Learning.Config.AdminSecret = testOperatorSecret
	request(t, p, a.http.URL, "GET", "/api/instructor/drafts", nil, 401)
	request(t, p, a.http.URL, "POST", "/api/instructor/drafts", map[string]string{"role": "admin"}, 401)
	req, _ := http.NewRequest("POST", a.http.URL+"/api/instructor/session", strings.NewReader(`{"secret":"`+testOperatorSecret+`"}`))
	req.Header.Set("Origin", "https://untrusted.invalid")
	r, e := p.client.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	io.Copy(io.Discard, r.Body)
	r.Body.Close()
	if r.StatusCode != 403 {
		t.Fatal("missing login CSRF guard", r.StatusCode)
	}
	req, _ = http.NewRequest("POST", a.http.URL+"/api/instructor/session", strings.NewReader(`{"secret":"`+testOperatorSecret+`"}`))
	req.Header.Set("Origin", origin)
	req.Header.Set("Content-Type", "application/json")
	r, e = p.client.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	io.Copy(io.Discard, r.Body)
	r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatal(r.StatusCode)
	}
	var cookie *http.Cookie
	for _, c := range r.Cookies() {
		if c.Name == operatorCookie {
			cookie = c
		}
	}
	if cookie == nil || !cookie.HttpOnly || cookie.Path != "/api/instructor" || cookie.MaxAge != 3600 || cookie.SameSite != http.SameSiteStrictMode || strings.Contains(cookie.Value, testOperatorSecret) {
		t.Fatal("unsafe operator cookie")
	}
	request(t, p, a.http.URL, "GET", "/api/instructor/session", nil, 200)
	request(t, p, a.http.URL, "GET", "/api/instructor/reports", nil, 200)
	key := a.store.Namespace + ":authoring:operator:" + learning.Hash(cookie.Value)
	a.store.Client.Del(context.Background(), key)
	request(t, p, a.http.URL, "GET", "/api/instructor/drafts", nil, 401)
	for i := 0; i < 4; i++ {
		request(t, p, a.http.URL, "POST", "/api/instructor/session", map[string]string{"secret": "wrong"}, 401)
	}
	request(t, p, a.http.URL, "POST", "/api/instructor/session", map[string]string{"secret": testOperatorSecret}, 429)
}

func TestInstructorDraftConflictApprovalAndAtomicPublication(t *testing.T) {
	a, op := studioApp(t)
	ctx := context.Background()
	learner := newPlayer(t, a)
	joinPlayer(t, learner, a, "VOCAB-DEMO")
	before := map[string]string{}
	for _, key := range a.store.Keys("VOCAB-DEMO") {
		v, _ := a.store.Client.Dump(ctx, key).Result()
		before[key] = v
	}
	d := createTestDraft(t, a, op, 4)
	again := createTestDraft(t, a, op, 4)
	if !reflect.DeepEqual(d, again) {
		t.Fatal("create replay changed draft")
	}
	if d.Status != "needs-review" || len(d.Items) != 4 {
		t.Fatal("draft auto approved")
	}
	path := "/api/instructor/drafts/" + d.ID
	request(t, learner, a.http.URL, "GET", path, nil, 401)
	request(t, op, a.http.URL, "POST", path+"/publish", learning.DraftPublish{DraftMutation: mutation(d.Version, 5001), FinalReviewed: true}, 409)
	malformed := edits(d)
	malformed.Items[0].Content.Word = "<script>alert(1)</script>"
	request(t, op, a.http.URL, "POST", path, malformed, 400)
	malformed = edits(d)
	malformed.Items[0].Content.Options = []string{"same", "same", "same", "same"}
	request(t, op, a.http.URL, "POST", path, malformed, 400)
	firstID := d.Items[0].ID
	d = decodeDraft(t, request(t, op, a.http.URL, "POST", path+"/questions/"+firstID+"/approve", mutation(d.Version, 5002), 200))
	old := d
	save := edits(d)
	save.Items[0].Content.Tags = []string{"travel"}
	save.Items[0].Content.Sense = "A reviewed travel sense"
	save.Items[0].Content.PronunciationVisible = true
	d = decodeDraft(t, request(t, op, a.http.URL, "POST", path, save, 200))
	if d.Items[0].Approval != nil {
		t.Fatal("edit retained approval")
	}
	stale := edits(old)
	stale.Title = "stale content"
	request(t, op, a.http.URL, "POST", path, stale, 409)
	raw := request(t, op, a.http.URL, "POST", path+"/questions/"+firstID+"/regenerate", mutation(d.Version, 5003), 200)
	var preview learning.RegenerationPreview
	json.Unmarshal(raw, &preview)
	if preview.Content.Word != d.Items[0].Content.Word || preview.Source != "ai" {
		t.Fatal("wrong preview")
	}
	unchanged := decodeDraft(t, request(t, op, a.http.URL, "GET", path, nil, 200))
	if !reflect.DeepEqual(d, unchanged) {
		t.Fatal("regeneration overwrote draft")
	}
	for i, item := range d.Items {
		d = decodeDraft(t, request(t, op, a.http.URL, "POST", path+"/questions/"+item.ID+"/approve", mutation(d.Version, 5010+i), 200))
	}
	request(t, op, a.http.URL, "POST", path+"/publish", learning.DraftPublish{DraftMutation: mutation(d.Version, 5020)}, 409)
	publish := learning.DraftPublish{DraftMutation: mutation(d.Version, 5021), FinalReviewed: true}
	var wg sync.WaitGroup
	results := make(chan learning.Draft, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := a.app.Learning.PublishDraft(ctx, d.ID, publish)
			if err != nil {
				t.Error(err)
			} else {
				results <- out
			}
		}()
	}
	wg.Wait()
	close(results)
	var published learning.Draft
	for result := range results {
		if published.ID != "" && !reflect.DeepEqual(published, result) {
			t.Fatal("publication replay mismatch")
		}
		published = result
	}
	if published.Status != "published" || published.PublishedQuizID == "" || a.store.Client.HLen(ctx, a.store.ContentKey()).Val() != 1 {
		t.Fatal("publication not unique")
	}
	replay := decodeDraft(t, request(t, op, a.http.URL, "POST", path+"/publish", publish, 200))
	if !reflect.DeepEqual(published, replay) {
		t.Fatal("HTTP publication replay")
	}
	for key, v := range before {
		after, _ := a.store.Client.Dump(ctx, key).Result()
		if v != after {
			t.Fatal("seeded quiz changed", key)
		}
	}
	b := newTestApp(t, a.store.Namespace, "", nil, nil)
	definition, err := b.store.QuizContext(ctx, published.PublishedQuizID)
	if err != nil || len(definition.Questions) != 4 {
		t.Fatal("repository reload", err)
	}
	if definition.Questions[0].Study.Vocabulary.Sense != "A reviewed travel sense" || len(definition.Questions[0].Study.Vocabulary.Tags) != 1 {
		t.Fatal("reviewed sense/tags lost")
	}
	participants := []*player{newPlayer(t, a), newPlayer(t, b)}
	for i, p := range participants {
		app := a
		if i == 1 {
			app = b
		}
		state := joinPlayer(t, p, app, definition.ID)
		live := connect(t, p, app, definition.ID)
		waitSnapshot(t, live, state.Version)
		if state.TotalQuestions != 4 || state.Policies.ScoringPolicy != "FIRST_ANSWER_TIMED_100_50" {
			t.Fatal("new quiz policies")
		}
		for qi, q := range definition.Questions {
			request(t, p, app.http.URL, "POST", "/api/quizzes/"+definition.ID+"/start", domain.StartQuestion{Epoch: state.Epoch, QuestionID: q.ID}, 200)
			request(t, p, app.http.URL, "POST", "/api/quizzes/"+definition.ID+"/answers", domain.Answer{Epoch: state.Epoch, QuestionID: q.ID, OptionID: q.CorrectOptionID, SubmissionID: fmt.Sprintf("00000000-0000-4000-8000-%012d", 6000+qi)}, 200)
		}
		final, err := app.store.Snapshot(ctx, definition.ID, p.pid)
		if err != nil || !final.Completed || final.Score != 400 || final.CorrectCount != 4 || final.Accuracy != 100 {
			t.Fatal("published quiz completion", final, err)
		}
		snapshot := waitSnapshot(t, live, final.Version)
		if snapshot.ParticipantCount != i+1 {
			t.Fatal("published live room did not synchronize")
		}
	}
	request(t, op, a.http.URL, "POST", path, edits(published), 409)
	request(t, op, a.http.URL, "POST", "/api/instructor/logout", map[string]any{}, 200)
	request(t, op, a.http.URL, "GET", path, nil, 401)
}

func TestInstructorEightQuestionsAndSecureCookie(t *testing.T) {
	a, op := studioApp(t)
	d := createTestDraft(t, a, op, 8)
	if len(d.Items) != 8 {
		t.Fatal("eight question draft")
	}
	a.app.config.SecureCookie = true
	body, _ := json.Marshal(map[string]string{"secret": testOperatorSecret})
	req, _ := http.NewRequest("POST", a.http.URL+"/api/instructor/session", bytes.NewReader(body))
	req.Header.Set("Origin", origin)
	req.Header.Set("Content-Type", "application/json")
	r, err := op.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatal("secure login failed", r.StatusCode)
	}
	found := false
	for _, c := range r.Cookies() {
		if c.Name == operatorCookie && !c.Secure {
			t.Fatal("production cookie not secure")
		}
		if c.Name == operatorCookie {
			found = true
		}
	}
	if !found {
		t.Fatal("operator cookie missing")
	}
}

func TestInstructorQuotasRejectBeforeGenerationAndPreserveDraft(t *testing.T) {
	a, op := studioApp(t)
	ctx := context.Background()
	d := createTestDraft(t, a, op, 4)
	for i, item := range d.Items {
		var err error
		d, err = a.app.Learning.ApproveDraftItem(ctx, d.ID, item.ID, mutation(d.Version, 7000+i))
		if err != nil {
			t.Fatal(err)
		}
	}
	before, _ := json.Marshal(d)
	for i := 0; i < 20; i++ {
		if err := a.store.Client.HSet(ctx, a.store.ContentKey(), fmt.Sprintf("TEST-QUOTA-%d", i), "test-quota-placeholder").Err(); err != nil {
			t.Fatal(err)
		}
	}
	_, err := a.app.Learning.PublishDraft(ctx, d.ID, learning.DraftPublish{DraftMutation: mutation(d.Version, 7100), FinalReviewed: true})
	if domain.Code(err) != "AUTHORING_QUOTA" {
		t.Fatal("publication quota", err)
	}
	unchanged, _ := a.app.Learning.GetDraft(ctx, d.ID)
	after, _ := json.Marshal(unchanged)
	if !bytes.Equal(before, after) {
		t.Fatal("quota changed draft")
	}
	for i := 0; i < 19; i++ {
		copy := d
		copy.ID = fmt.Sprintf("%032x", i+1)
		raw, _ := json.Marshal(copy)
		a.store.Client.Set(ctx, a.store.Namespace+":authoring:{studio}:draft:"+copy.ID, raw, time.Hour)
		a.store.Client.ZAdd(ctx, a.store.Namespace+":authoring:{studio}:drafts", redis.Z{Score: float64(time.Now().Add(time.Hour).Unix()), Member: copy.ID})
	}
	// Recorded provider input would reveal a quota check occurring after generation.
	inspected := &inspectedProvider{base: a.app.Learning.Provider.(*testsupport.Provider)}
	a.app.Learning.Provider = inspected
	_, err = a.app.Learning.CreateDraft(ctx, "operator", learning.DraftCreate{ClientActionID: "00000000-0000-4000-8000-000000007101", Brief: d.Brief})
	if domain.Code(err) != "AUTHORING_QUOTA" || len(inspected.inputs) != 0 {
		t.Fatal("draft quota reached provider", err)
	}
}
