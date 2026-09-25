//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"vocabulary.live/internal/domain"
)

func TestHTTPClockAuthorityAndLateScoreAcrossInstances(t *testing.T) {
	a := newTestApp(t, "", "", nil, nil)
	b := newTestApp(t, a.store.Namespace, "", nil, nil)
	p := newPlayer(t, a)
	state := joinPlayer(t, p, a, "VOCAB-DEMO")
	start := domain.StartQuestion{Epoch: state.Epoch, QuestionID: "vocab-q1"}
	path := "/api/quizzes/VOCAB-DEMO/start"
	var clock, again domain.QuestionClock
	if err := json.Unmarshal(request(t, p, a.http.URL, "POST", path, start, 200), &clock); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(request(t, p, b.http.URL, "POST", path, start, 200), &again); err != nil {
		t.Fatal(err)
	}
	if clock.DeadlineMS != again.DeadlineMS || clock.Epoch != state.Epoch {
		t.Fatal(clock, again)
	}
	for _, field := range []string{"participantId", "deadlineMs", "serverTimeMs", "durationSeconds", "pointsAwarded"} {
		request(t, p, a.http.URL, "POST", path, map[string]any{"epoch": state.Epoch, "questionId": start.QuestionID, field: 9999999999999}, 400)
	}
	outsider := newPlayer(t, a)
	request(t, outsider, a.http.URL, "POST", path, start, 403)
	request(t, outsider, a.http.URL, "POST", path, domain.StartQuestion{Epoch: "00000000000000000000000000000000", QuestionID: start.QuestionID}, 409)
	watcher := newPlayer(t, b)
	joinPlayer(t, watcher, b, "VOCAB-DEMO")
	ws := connect(t, watcher, b, "VOCAB-DEMO")
	waitSnapshot(t, ws, 2)
	ctx := context.Background()
	now, err := a.store.Client.Time(ctx).Result()
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.Client.HSet(ctx, a.store.Keys(state.QuizID)[6], p.pid+":"+start.QuestionID, now.UnixMilli()).Err(); err != nil {
		t.Fatal(err)
	}
	var result, replay domain.AnswerResult
	answer := command(state.Epoch, 1, "b")
	if err := json.Unmarshal(request(t, p, a.http.URL, "POST", "/api/quizzes/VOCAB-DEMO/answers", answer, 200), &result); err != nil {
		t.Fatal(err)
	}
	if result.Receipt.PointsAwarded != 50 || !result.Receipt.Correctness || !result.Receipt.TimedOut {
		t.Fatal(result)
	}
	if err := json.Unmarshal(request(t, p, b.http.URL, "POST", "/api/quizzes/VOCAB-DEMO/answers", answer, 200), &replay); err != nil {
		t.Fatal(err)
	}
	if replay.Outcome != "replayed" || !reflect.DeepEqual(result.Receipt, replay.Receipt) {
		t.Fatal(replay)
	}
	public := waitSnapshot(t, ws, result.Receipt.AcceptedVersion)
	if public.Leaderboard[0].ParticipantID != p.pid || public.Leaderboard[0].Score != 50 {
		t.Fatal(public)
	}
	var restored domain.State
	if err := json.Unmarshal(request(t, p, b.http.URL, "GET", "/api/quizzes/VOCAB-DEMO/state", nil, 200), &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Score != 50 || len(restored.Receipts) != 1 || !reflect.DeepEqual(restored.Receipts[0], result.Receipt) {
		t.Fatal(restored)
	}
}
