//go:build integration

package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"vocabulary.live/internal/domain"
	"vocabulary.live/internal/fixtures"
	"vocabulary.live/internal/store"
)

const origin = "http://quiz.test"

type testApp struct {
	app   *Server
	http  *httptest.Server
	store *store.Store
}

func newTestApp(t *testing.T, namespace, redisURL string, publish func(context.Context, store.Change) error, wrap func(http.Handler) http.Handler, random ...domain.RandomInt) *testApp {
	t.Helper()
	if namespace == "" {
		namespace = "http-test-" + domain.ID()
	}
	if redisURL == "" {
		redisURL = os.Getenv("REDIS_URL")
	}
	if redisURL == "" {
		redisURL = "redis://127.0.0.1:6379/0"
	}
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	options.MaxRetries = 0
	options.DialTimeout = 150 * time.Millisecond
	options.ReadTimeout = 150 * time.Millisecond
	options.WriteTimeout = 150 * time.Millisecond
	client := redis.NewClient(options)
	draw := domain.SecureRandom
	if len(random) > 0 {
		draw = random[0]
	}
	s, err := store.NewWithRandom(client, namespace, fixtures.All(), 200, draw)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Seed(context.Background()); err != nil {
		t.Fatalf("REQUIRED real Redis unavailable (not skipped): %v", err)
	}
	app := New(s, Config{Origins: []string{origin}, Rate: 10000, Burst: 20000, Coalesce: 15 * time.Millisecond, Reconcile: 150 * time.Millisecond}, slog.New(slog.NewTextHandler(io.Discard, nil)), publish)
	handler := app.Handler()
	if wrap != nil {
		handler = wrap(handler)
	}
	ts := httptest.NewServer(handler)
	t.Cleanup(func() {
		app.Close()
		ts.Close()
		ctx := context.Background()
		for id := range s.Quizzes {
			_ = client.Del(ctx, s.Keys(id)...).Err()
		}
		var cursor uint64
		for {
			// This helper owns a unique http-test namespace, including published
			// test quizzes and learning artifacts. Never scan a demo namespace.
			keys, next, err := client.Scan(ctx, cursor, namespace+":*", 100).Result()
			if err != nil {
				break
			}
			if len(keys) > 0 {
				_ = client.Del(ctx, keys...).Err()
			}
			cursor = next
			if cursor == 0 {
				break
			}
		}
		_ = client.Close()
	})
	return &testApp{app, ts, s}
}

type player struct {
	client *http.Client
	pid    string
}

func request(t *testing.T, p *player, base, method, path string, body any, want int) []byte {
	t.Helper()
	var reader io.Reader
	if body != nil {
		switch v := body.(type) {
		case string:
			reader = strings.NewReader(v)
		default:
			b, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			reader = bytes.NewReader(b)
		}
	}
	req, err := http.NewRequest(method, base+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", origin)
	req.Header.Set("Content-Type", "application/json")
	response, err := p.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != want {
		t.Fatalf("%s %s status %d want %d: %s", method, path, response.StatusCode, want, data)
	}
	if strings.HasPrefix(path, "/api/") && response.Header.Get("Cache-Control") != "no-store" {
		t.Error("private response cacheable")
	}
	return data
}
func newPlayer(t *testing.T, a *testApp) *player {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	p := &player{client: &http.Client{Jar: jar, Timeout: 3 * time.Second}}
	b := request(t, p, a.http.URL, "POST", "/api/session", map[string]any{}, 200)
	var result struct {
		ParticipantID string `json:"participantId"`
	}
	if err := json.Unmarshal(b, &result); err != nil {
		t.Fatal(err)
	}
	p.pid = result.ParticipantID
	return p
}
func joinPlayer(t *testing.T, p *player, a *testApp, quiz string) domain.State {
	t.Helper()
	b := request(t, p, a.http.URL, "POST", "/api/quizzes/"+quiz+"/join", map[string]string{"displayName": "<img src=x onerror=alert(1)>"}, 200)
	var v domain.State
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	for _, q := range a.store.Quizzes[quiz].Questions {
		request(t, p, a.http.URL, "POST", "/api/quizzes/"+quiz+"/start", domain.StartQuestion{Epoch: v.Epoch, QuestionID: q.ID}, 200)
	}
	return v
}
func connect(t *testing.T, p *player, a *testApp, quiz string) *websocket.Conn {
	t.Helper()
	u, _ := url.Parse(a.http.URL)
	headers := http.Header{"Origin": []string{origin}}
	for _, cookie := range p.client.Jar.Cookies(u) {
		headers.Add("Cookie", cookie.String())
	}
	c, r, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(a.http.URL, "http")+"/api/quizzes/"+quiz+"/live", headers)
	if err != nil {
		if r != nil {
			t.Fatalf("socket status %d: %v", r.StatusCode, err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}
func waitSnapshot(t *testing.T, c *websocket.Conn, version int64) domain.Snapshot {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		var e domain.Event
		if err := c.ReadJSON(&e); err != nil {
			t.Fatal(err)
		}
		if e.Type == "snapshot" && e.Snapshot.Version >= version {
			return *e.Snapshot
		}
	}
}
func command(epoch string, n int, option string) domain.Answer {
	return domain.Answer{SubmissionID: fmt.Sprintf("00000000-0000-4000-8000-%012d", n), Epoch: epoch, QuestionID: fmt.Sprintf("vocab-q%d", n), OptionID: option}
}

func TestHTTPIdentityAuthorizationAndValidation(t *testing.T) {
	a := newTestApp(t, "", "", nil, nil)
	jar, _ := cookiejar.New(nil)
	missing := &player{client: &http.Client{Jar: jar, Timeout: 3 * time.Second}}
	request(t, missing, a.http.URL, "POST", "/api/session", map[string]bool{"resumeOnly": true}, 401)
	request(t, missing, a.http.URL, "POST", "/api/session", "null", 400)
	p := newPlayer(t, a)
	v := joinPlayer(t, p, a, "VOCAB-DEMO")
	data := request(t, p, a.http.URL, "GET", "/api/quizzes/VOCAB-DEMO/state", nil, 200)
	for _, secret := range []string{"correctOptionId", "correctness", "explanation", "CorrectOptionID"} {
		if bytes.Contains(data, []byte(secret)) {
			t.Fatalf("pre-answer grading leak %s", secret)
		}
	}
	if v.Leaderboard[0].DisplayName != "<img src=x onerror=alert(1)>" {
		t.Fatal("name not preserved as text")
	}
	outsider := newPlayer(t, a)
	request(t, outsider, a.http.URL, "GET", "/api/quizzes/VOCAB-DEMO/state", nil, 403)
	request(t, outsider, a.http.URL, "POST", "/api/quizzes/VOCAB-DEMO/answers", command(v.Epoch, 1, "b"), 403)
	request(t, p, a.http.URL, "GET", "/api/quizzes/TRAVEL-DEMO/state", nil, 403)
	request(t, p, a.http.URL, "POST", "/api/quizzes/UNKNOWN/join", map[string]string{"displayName": "A"}, 404)
	forged := map[string]any{"submissionId": "00000000-0000-4000-8000-000000000001", "epoch": v.Epoch, "questionId": "vocab-q1", "optionId": "b", "score": 800, "participantId": p.pid}
	request(t, p, a.http.URL, "POST", "/api/quizzes/VOCAB-DEMO/answers", forged, 400)
	for _, body := range []string{`{"displayName":55}`, `{"displayName":`, `{"displayName":""}`, `{"displayName":"valid"} {}`} {
		request(t, p, a.http.URL, "POST", "/api/quizzes/VOCAB-DEMO/join", body, 400)
	}
	request(t, p, a.http.URL, "POST", "/api/quizzes/VOCAB-DEMO/join", `{"displayName":"`+strings.Repeat("x", 5000)+`"}`, 413)
	req, _ := http.NewRequest("POST", a.http.URL+"/api/session", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://evil.test")
	r, err := p.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 403 {
		t.Fatal("bad origin accepted")
	}
	u, _ := url.Parse(a.http.URL)
	headers := http.Header{"Origin": []string{"http://evil.test"}}
	for _, cookie := range p.client.Jar.Cookies(u) {
		headers.Add("Cookie", cookie.String())
	}
	_, r, err = websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(a.http.URL, "http")+"/api/quizzes/VOCAB-DEMO/live", headers)
	if err == nil || r.StatusCode != 403 {
		t.Fatal("bad WS origin accepted")
	}
	r.Body.Close()
	headers.Set("Origin", origin)
	headers.Del("Cookie")
	_, r, err = websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(a.http.URL, "http")+"/api/quizzes/VOCAB-DEMO/live", headers)
	if err == nil || r.StatusCode != 401 {
		t.Fatal("anonymous WS accepted")
	}
	r.Body.Close()
	for _, cookie := range outsider.client.Jar.Cookies(u) {
		headers.Add("Cookie", cookie.String())
	}
	_, r, err = websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(a.http.URL, "http")+"/api/quizzes/VOCAB-DEMO/live", headers)
	if err == nil || r.StatusCode != 403 {
		t.Fatal("nonmember WS accepted")
	}
	r.Body.Close()
}

func TestTwoInstancesFanoutDuplicateGuardAndQuizIsolation(t *testing.T) {
	a := newTestApp(t, "", "", nil, nil)
	b := newTestApp(t, a.store.Namespace, "", nil, nil)
	p1 := newPlayer(t, a)
	v := joinPlayer(t, p1, a, "VOCAB-DEMO")
	p2 := newPlayer(t, b)
	joinPlayer(t, p2, b, "VOCAB-DEMO")
	p3 := newPlayer(t, b)
	travel := joinPlayer(t, p3, b, "TRAVEL-DEMO")
	c1 := connect(t, p1, a, "VOCAB-DEMO")
	c2 := connect(t, p2, b, "VOCAB-DEMO")
	ct := connect(t, p3, b, "TRAVEL-DEMO")
	waitSnapshot(t, c1, 2)
	waitSnapshot(t, c2, 2)
	answer := command(v.Epoch, 1, "b")
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			target := a.http.URL
			if i%2 == 1 {
				target = b.http.URL
			}
			data := request(t, p1, target, "POST", "/api/quizzes/VOCAB-DEMO/answers", answer, 200)
			var r domain.AnswerResult
			if err := json.Unmarshal(data, &r); err != nil {
				t.Error(err)
			}
			if r.Outcome == "accepted" {
				accepted.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatal("cross-instance multiple acceptance", accepted.Load())
	}
	for _, c := range []*websocket.Conn{c1, c2} {
		snap := waitSnapshot(t, c, 3)
		if snap.ParticipantCount != 2 || snap.Leaderboard[0].Score != 100 || snap.Leaderboard[1].Score != 0 {
			t.Fatal(snap)
		}
	}
	ts := waitSnapshot(t, ct, travel.Version)
	if ts.QuizID != "TRAVEL-DEMO" || ts.ParticipantCount != 1 || ts.Leaderboard[0].Score != 0 {
		t.Fatal("live quiz leak", ts)
	}
	joined := joinPlayer(t, newPlayer(t, b), b, "VOCAB-DEMO")
	if joined.Version != 4 || joined.Leaderboard[0].Score != 100 {
		t.Fatal(joined)
	}
	reconnected := connect(t, p1, b, "VOCAB-DEMO")
	snap := waitSnapshot(t, reconnected, 4)
	if snap.Leaderboard[0].ParticipantID != p1.pid {
		t.Fatal("identity changed")
	}
}

func TestSuppressedPublishReconcilesWithoutAnotherAnswer(t *testing.T) {
	a := newTestApp(t, "", "", func(context.Context, store.Change) error { return fmt.Errorf("intentional suppressed publish") }, nil)
	b := newTestApp(t, a.store.Namespace, "", nil, nil)
	p := newPlayer(t, a)
	v := joinPlayer(t, p, a, "VOCAB-DEMO")
	observer := newPlayer(t, b)
	joinPlayer(t, observer, b, "VOCAB-DEMO")
	c := connect(t, observer, b, "VOCAB-DEMO")
	waitSnapshot(t, c, 2)
	request(t, p, a.http.URL, "POST", "/api/quizzes/VOCAB-DEMO/answers", command(v.Epoch, 1, "b"), 200)
	snap := waitSnapshot(t, c, 3)
	if snap.Leaderboard[0].Score != 100 {
		t.Fatal(snap)
	}
}

type lostWriter struct{ http.ResponseWriter }

func (w lostWriter) WriteHeader(int) {}
func (w lostWriter) Write(data []byte) (int, error) {
	c, _, err := w.ResponseWriter.(http.Hijacker).Hijack()
	if err == nil {
		_ = c.Close()
	}
	return len(data), err
}
func TestCommittedAnswerLostResponseRecovery(t *testing.T) {
	var lose atomic.Bool
	lose.Store(true)
	a := newTestApp(t, "", "", nil, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/answers") && lose.CompareAndSwap(true, false) {
				next.ServeHTTP(lostWriter{w}, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	p := newPlayer(t, a)
	v := joinPlayer(t, p, a, "VOCAB-DEMO")
	answer := command(v.Epoch, 1, "b")
	body, _ := json.Marshal(answer)
	req, _ := http.NewRequest("POST", a.http.URL+"/api/quizzes/VOCAB-DEMO/answers", bytes.NewReader(body))
	req.Header.Set("Origin", origin)
	req.Header.Set("Content-Type", "application/json")
	r, err := p.client.Do(req)
	if err == nil {
		r.Body.Close()
		t.Fatal("intentional response loss did not occur")
	}
	data := request(t, p, a.http.URL, "GET", "/api/quizzes/VOCAB-DEMO/state", nil, 200)
	var state domain.State
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	if state.Version != 2 || len(state.Receipts) != 1 || state.Leaderboard[0].Score != 100 {
		t.Fatal(state)
	}
	data = request(t, p, a.http.URL, "POST", "/api/quizzes/VOCAB-DEMO/answers", answer, 200)
	var replay domain.AnswerResult
	if err := json.Unmarshal(data, &replay); err != nil {
		t.Fatal(err)
	}
	if replay.Outcome != "replayed" || replay.Receipt.AcceptedVersion != 2 {
		t.Fatal(replay)
	}
}

// A test-owned TCP proxy interrupts only this app's Redis dependency. It never
// stops the shared Redis process or adds a production chaos endpoint.
type dependencyProxy struct {
	listener    net.Listener
	target      string
	up          atomic.Bool
	mu          sync.Mutex
	connections map[net.Conn]bool
	wg          sync.WaitGroup
}

func proxy(t *testing.T) *dependencyProxy {
	t.Helper()
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://127.0.0.1:6379/0"
	}
	u, err := url.Parse(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &dependencyProxy{listener: l, target: u.Host, connections: map[net.Conn]bool{}}
	p.up.Store(true)
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			if !p.up.Load() {
				c.Close()
				continue
			}
			p.mu.Lock()
			p.connections[c] = true
			p.mu.Unlock()
			p.wg.Add(1)
			go func() {
				defer p.wg.Done()
				defer c.Close()
				defer func() { p.mu.Lock(); delete(p.connections, c); p.mu.Unlock() }()
				backend, err := net.DialTimeout("tcp", p.target, time.Second)
				if err != nil {
					return
				}
				defer backend.Close()
				finished := make(chan struct{})
				go func() { _, _ = io.Copy(backend, c); backend.Close(); close(finished) }()
				_, _ = io.Copy(c, backend)
				c.Close()
				<-finished
			}()
		}
	}()
	t.Cleanup(func() { l.Close(); p.set(false); p.wg.Wait() })
	return p
}
func (p *dependencyProxy) set(up bool) {
	p.up.Store(up)
	if !up {
		p.mu.Lock()
		for c := range p.connections {
			c.Close()
		}
		p.mu.Unlock()
	}
}
func TestRedisOutageIsNotAuthenticationFailureAndRecovers(t *testing.T) {
	proxy := proxy(t)
	a := newTestApp(t, "", "redis://"+proxy.listener.Addr().String()+"/0", nil, nil)
	p := newPlayer(t, a)
	v := joinPlayer(t, p, a, "VOCAB-DEMO")
	c := connect(t, p, a, "VOCAB-DEMO")
	waitSnapshot(t, c, 1)
	proxy.set(false)
	defer proxy.set(true)
	request(t, p, a.http.URL, "GET", "/readyz", nil, 503)
	request(t, p, a.http.URL, "GET", "/healthz", nil, 200)
	request(t, p, a.http.URL, "GET", "/api/quizzes/VOCAB-DEMO", nil, 503)
	data := request(t, p, a.http.URL, "POST", "/api/quizzes/VOCAB-DEMO/answers", command(v.Epoch, 1, "b"), 503)
	if !bytes.Contains(data, []byte("DEPENDENCY_UNAVAILABLE")) {
		t.Fatal(string(data))
	}
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		var event domain.Event
		if err := c.ReadJSON(&event); err != nil {
			t.Fatal(err)
		}
		if event.Type == "status" {
			if event.Code != "DEPENDENCY_UNAVAILABLE" {
				t.Fatal(event)
			}
			break
		}
	}
	proxy.set(true)
	snap := waitSnapshot(t, c, 1)
	if snap.Version != 1 || snap.Leaderboard[0].Score != 0 {
		t.Fatal("outage changed authoritative state", snap)
	}
	request(t, p, a.http.URL, "GET", "/readyz", nil, 200)
	request(t, p, a.http.URL, "POST", "/api/quizzes/VOCAB-DEMO/answers", command(v.Epoch, 1, "b"), 200)
	waitSnapshot(t, c, 2)
}

func TestQuizPreviewAndPersistedPresentationAcrossHTTPInstances(t *testing.T) {
	a := newTestApp(t, "", "", nil, nil, func(n int) (int, error) { return n - 1, nil })
	anonymous := &player{client: &http.Client{Timeout: 3 * time.Second}}
	preview := request(t, anonymous, a.http.URL, "GET", "/api/quizzes/VOCAB-DEMO", nil, 200)
	var metadata domain.Metadata
	if err := json.Unmarshal(preview, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.TotalQuestions != 8 || metadata.Policies != domain.LivePractice() {
		t.Fatal(metadata)
	}
	for _, secret := range []string{"questionOrder", "optionOrders", "correctOptionId", "explanation", "questions"} {
		if bytes.Contains(preview, []byte("\""+secret+"\"")) {
			t.Fatal("preview leaked content", string(preview))
		}
	}
	request(t, anonymous, a.http.URL, "GET", "/api/quizzes/UNKNOWN", nil, 404)
	if a.store.Client.HLen(context.Background(), a.store.Keys("VOCAB-DEMO")[1]).Val() != 0 {
		t.Fatal("preview joined a participant")
	}
	p := newPlayer(t, a)
	original := joinPlayer(t, p, a, "VOCAB-DEMO")
	// Construct another complete app after the original attempt was persisted.
	b := newTestApp(t, a.store.Namespace, "", nil, nil, func(int) (int, error) { return 0, nil })
	other := joinPlayer(t, newPlayer(t, b), b, "VOCAB-DEMO")
	if original.CurrentQuestion.ID == other.CurrentQuestion.ID {
		t.Fatal("deterministic plans should differ")
	}
	if reflect.DeepEqual(original.CurrentQuestion.Options, other.CurrentQuestion.Options) {
		t.Fatal("deterministic options should differ")
	}
	before := connect(t, p, a, "VOCAB-DEMO")
	waitSnapshot(t, before, 2)
	before.Close()
	after := connect(t, p, b, "VOCAB-DEMO")
	waitSnapshot(t, after, 2)
	var resumed domain.State
	data := request(t, p, b.http.URL, "GET", "/api/quizzes/VOCAB-DEMO/state", nil, 200)
	if err := json.Unmarshal(data, &resumed); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resumed.CurrentQuestion, original.CurrentQuestion) || resumed.QuestionNumber != 1 {
		t.Fatal("reconnect changed presentation", resumed)
	}
	joined := joinPlayer(t, p, b, "VOCAB-DEMO")
	if !reflect.DeepEqual(joined.CurrentQuestion, original.CurrentQuestion) {
		t.Fatal("retry shuffled presentation")
	}
	q, _ := a.store.Quizzes[original.QuizID].Question(original.CurrentQuestion.ID)
	intent := domain.Answer{SubmissionID: "00000000-0000-4000-8000-000000000001", Epoch: original.Epoch, QuestionID: q.ID, OptionID: q.CorrectOptionID}
	var result domain.AnswerResult
	data = request(t, p, b.http.URL, "POST", "/api/quizzes/VOCAB-DEMO/answers", intent, 200)
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.Receipt.PointsAwarded != 100 || !reflect.DeepEqual(result.Receipt.Question, *original.CurrentQuestion) {
		t.Fatal(result)
	}
	data = request(t, p, a.http.URL, "GET", "/api/quizzes/VOCAB-DEMO/state", nil, 200)
	if err := json.Unmarshal(data, &resumed); err != nil {
		t.Fatal(err)
	}
	if resumed.AnsweredCount != 1 || resumed.QuestionNumber != 2 || resumed.CurrentQuestion.ID != "vocab-q2" {
		t.Fatal("server did not advance its saved cursor", resumed)
	}
}

func TestExpiredSessionCookieRecoveryRateLimitAndReset(t *testing.T) {
	a := newTestApp(t, "", "", nil, nil)
	p := newPlayer(t, a)
	v := joinPlayer(t, p, a, "VOCAB-DEMO")
	c := connect(t, p, a, "VOCAB-DEMO")
	waitSnapshot(t, c, 1)
	if err := a.store.Reset(context.Background()); err != nil {
		t.Fatal(err)
	}
	request(t, p, a.http.URL, "POST", "/api/quizzes/VOCAB-DEMO/answers", command(v.Epoch, 1, "b"), 409)
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		var event domain.Event
		if err := c.ReadJSON(&event); err != nil {
			t.Fatal(err)
		}
		if event.Code == "RESET_REQUIRED" {
			break
		}
	}
	keys, err := a.store.Client.Keys(context.Background(), a.store.Namespace+":session:*").Result()
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.Client.Del(context.Background(), keys...).Err(); err != nil {
		t.Fatal(err)
	}
	request(t, p, a.http.URL, "POST", "/api/session", map[string]any{}, 401)
	request(t, p, a.http.URL, "POST", "/api/session", map[string]bool{"restart": true}, 200)
	a.app.limit = newLimiter(1, 1)
	request(t, p, a.http.URL, "GET", "/api/quizzes/VOCAB-DEMO/state", nil, 403)
	request(t, p, a.http.URL, "GET", "/api/quizzes/VOCAB-DEMO/state", nil, 429)
}

func TestCookieFlagsMetricsAndSocketShutdown(t *testing.T) {
	a := newTestApp(t, "", "", nil, nil)
	req, _ := http.NewRequest("POST", a.http.URL+"/api/session", strings.NewReader("{}"))
	req.Header.Set("Origin", origin)
	req.Header.Set("Content-Type", "application/json")
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	cookies := r.Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].MaxAge != 604800 {
		t.Fatal("cookie policy")
	}
	p := newPlayer(t, a)
	joinPlayer(t, p, a, "VOCAB-DEMO")
	c := connect(t, p, a, "VOCAB-DEMO")
	waitSnapshot(t, c, 1)
	request(t, p, a.http.URL, "GET", "/metrics", nil, 200)
	done := make(chan struct{})
	go func() { a.app.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown leaked socket or subscription loop")
	}
	_ = c.SetReadDeadline(time.Now().Add(time.Second))
	for {
		if _, _, err := c.ReadMessage(); err != nil {
			break
		}
	}
}
