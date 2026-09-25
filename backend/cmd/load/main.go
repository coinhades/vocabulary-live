// load is an offline benchmark driver. It creates its own real application and
// native clients, and touches only a dedicated benchmark namespace in Redis.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"vocabulary.live/internal/config"
	"vocabulary.live/internal/domain"
	"vocabulary.live/internal/fixtures"
	"vocabulary.live/internal/httpapi"
	"vocabulary.live/internal/store"
)

type player struct {
	client          *http.Client
	id, quiz, epoch string
	socket          *websocket.Conn
}
type observation struct {
	observer, quiz string
	version        int64
	at             time.Time
}
type measurement struct {
	participant, quiz string
	version           int64
	start             time.Time
	responseMS        float64
	accepted          bool
	outcome           string
}
type distribution struct {
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	P99 float64 `json:"p99"`
}
type report struct {
	Scenario     string            `json:"scenario"`
	Go           string            `json:"go"`
	OS           string            `json:"os"`
	CPUs         int               `json:"logicalCPUs"`
	Redis        string            `json:"redis"`
	Namespace    string            `json:"namespace"`
	Participants int               `json:"participants"`
	Rooms        int               `json:"rooms"`
	Concurrency  int               `json:"concurrency"`
	AnswerRate   float64           `json:"offeredAnswersPerSecond"`
	Offered      int               `json:"offered"`
	Accepted     int               `json:"accepted"`
	Replays      int               `json:"replays"`
	Rejected     int               `json:"rejected"`
	Errors       int               `json:"errors"`
	Unobserved   int               `json:"unobservedAcceptedAnswers"`
	Duration     float64           `json:"durationSeconds"`
	Throughput   float64           `json:"acceptedPerSecond"`
	Response     distribution      `json:"answerResponseMilliseconds"`
	Propagation  distribution      `json:"answerToOtherSocketMilliseconds"`
	Samples      []measurementJSON `json:"samples"`
}
type measurementJSON struct {
	Outcome     string   `json:"outcome"`
	Response    float64  `json:"responseMs"`
	Propagation *float64 `json:"propagationMs"`
}

func quantiles(values []float64) distribution {
	if len(values) == 0 {
		return distribution{}
	}
	sort.Float64s(values)
	at := func(q float64) float64 { return values[int(float64(len(values)-1)*q)] }
	return distribution{at(.5), at(.95), at(.99)}
}
func call(p *player, base, path string, body any, target any) (int, error) {
	method := http.MethodGet
	var payload io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		method = http.MethodPost
		payload = bytes.NewReader(b)
	}
	r, err := http.NewRequest(method, base+path, payload)
	if err != nil {
		return 0, err
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://load.local")
	response, err := p.client.Do(r)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		_, _ = io.Copy(io.Discard, response.Body)
		return response.StatusCode, nil
	}
	return response.StatusCode, json.NewDecoder(response.Body).Decode(target)
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	participants := flag.Int("participants", 100, "native HTTP/WebSocket participants (2..400)")
	rooms := flag.Int("rooms", 2, "room count (1 or 2)")
	concurrency := flag.Int("concurrency", 25, "simultaneous HTTP answer requests")
	rate := flag.Float64("rate", 100, "offered new answers/second; 0 is a burst")
	output := flag.String("output", "../docs/evidence/load-two-rooms.json", "raw measured JSON report")
	namespace := flag.String("namespace", "vocab-bench", "must start with vocab-bench; only this namespace is reset")
	flag.Parse()
	if *participants < 2 || *participants > 400 || *rooms < 1 || *rooms > 2 || (*participants+*rooms-1) / *rooms > 200 || *concurrency < 1 || *concurrency > 400 || *rate < 0 || *rate > 10000 || !strings.HasPrefix(*namespace, "vocab-bench") {
		return fmt.Errorf("invalid or unsafe benchmark settings")
	}
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	rc := redis.NewClient(cfg.Redis)
	defer rc.Close()
	s, err := store.New(rc, *namespace, fixtures.All(), 200)
	if err != nil {
		return err
	}
	ctx := context.Background()
	if err := s.Reset(ctx); err != nil {
		return fmt.Errorf("required real Redis: %w", err)
	}
	// Explicit benchmark-only limit configuration, included in the report docs.
	app := httpapi.New(s, httpapi.Config{Origins: []string{"http://load.local"}, Rate: 10000, Burst: 20000}, slog.New(slog.NewJSONHandler(io.Discard, nil)), nil)
	defer app.Close()
	server := httptest.NewServer(app.Handler())
	defer server.Close()
	players := make([]*player, *participants)
	defer func() {
		for _, p := range players {
			if p != nil && p.socket != nil {
				p.socket.Close()
			}
		}
	}()
	ids := []string{"VOCAB-DEMO", "TRAVEL-DEMO"}
	for i := range players {
		jar, _ := cookiejar.New(nil)
		p := &player{client: &http.Client{Jar: jar, Timeout: 5 * time.Second}, quiz: ids[i%*rooms]}
		players[i] = p
		var session struct {
			ParticipantID string `json:"participantId"`
		}
		status, err := call(p, server.URL, "/api/session", struct{}{}, &session)
		if err != nil || status != 200 {
			return fmt.Errorf("session setup %d: %v", status, err)
		}
		p.id = session.ParticipantID
		var state domain.State
		status, err = call(p, server.URL, "/api/quizzes/"+p.quiz+"/join", map[string]string{"displayName": fmt.Sprintf("Load %03d", i+1)}, &state)
		if err != nil || status != 200 {
			return fmt.Errorf("join setup %d: %v", status, err)
		}
		p.epoch = state.Epoch
	}
	events := make(chan observation, 8192)
	var readers sync.WaitGroup
	var dropped atomic.Int64
	var collecting atomic.Bool
	for _, p := range players {
		u, _ := url.Parse(server.URL)
		headers := http.Header{"Origin": []string{"http://load.local"}}
		for _, c := range p.client.Jar.Cookies(u) {
			headers.Add("Cookie", c.String())
		}
		c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/api/quizzes/"+p.quiz+"/live", headers)
		if err != nil {
			return err
		}
		p.socket = c
		c.SetReadDeadline(time.Now().Add(5 * time.Second))
		var initial domain.Event
		if err := c.ReadJSON(&initial); err != nil {
			return err
		}
		if initial.Snapshot == nil {
			return fmt.Errorf("initial socket not synchronized")
		}
		c.SetReadDeadline(time.Time{})
		readers.Add(1)
		go func(p *player) {
			defer readers.Done()
			for {
				var event domain.Event
				if err := p.socket.ReadJSON(&event); err != nil {
					return
				}
				if event.Snapshot != nil && collecting.Load() {
					select {
					case events <- observation{p.id, event.Snapshot.QuizID, event.Snapshot.Version, time.Now()}:
					default:
						dropped.Add(1)
					}
				}
			}
		}(p)
	}
	var observations []observation
	var observationsMu sync.Mutex
	collectDone := make(chan struct{})
	go func() {
		defer close(collectDone)
		for event := range events {
			observationsMu.Lock()
			observations = append(observations, event)
			observationsMu.Unlock()
		}
	}()
	collecting.Store(true)
	started := time.Now()
	measurements := make([]measurement, 0, *participants*8)
	var mu sync.Mutex
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, *concurrency)
	var limiter *time.Ticker
	if *rate > 0 {
		limiter = time.NewTicker(time.Duration(float64(time.Second) / *rate))
		defer limiter.Stop()
	}
	for question := 0; question < 8; question++ {
		for _, p := range players {
			if limiter != nil {
				<-limiter.C
			}
			semaphore <- struct{}{}
			wg.Add(1)
			go func(p *player, n int) {
				defer wg.Done()
				defer func() { <-semaphore }()
				var state domain.State
				stateStatus, stateErr := call(p, server.URL, "/api/quizzes/"+p.quiz+"/state", nil, &state)
				if stateErr != nil || stateStatus != 200 || state.CurrentQuestion == nil || state.AnsweredCount != n {
					mu.Lock()
					measurements = append(measurements, measurement{participant: p.id, quiz: p.quiz, outcome: "error"})
					mu.Unlock()
					return
				}
				var correctOption string
				for _, q := range s.Quizzes[p.quiz].Questions {
					if q.ID == state.CurrentQuestion.ID {
						correctOption = q.CorrectOptionID
						break
					}
				}
				raw := domain.ID()
				submission := raw[:8] + "-" + raw[8:12] + "-4" + raw[13:16] + "-8" + raw[17:20] + "-" + raw[20:]
				a := domain.Answer{SubmissionID: submission, Epoch: p.epoch, QuestionID: state.CurrentQuestion.ID, OptionID: correctOption}
				var clock domain.QuestionClock
				clockStatus, clockErr := call(p, server.URL, "/api/quizzes/"+p.quiz+"/start", domain.StartQuestion{Epoch: p.epoch, QuestionID: a.QuestionID}, &clock)
				if clockErr != nil || clockStatus != 200 {
					mu.Lock()
					measurements = append(measurements, measurement{participant: p.id, quiz: p.quiz, outcome: "error"})
					mu.Unlock()
					return
				}
				start := time.Now()
				var result domain.AnswerResult
				status, err := call(p, server.URL, "/api/quizzes/"+p.quiz+"/answers", a, &result)
				m := measurement{participant: p.id, quiz: p.quiz, start: start, responseMS: float64(time.Since(start)) / float64(time.Millisecond), version: result.Receipt.AcceptedVersion, accepted: err == nil && status == 200 && result.Outcome == "accepted", outcome: result.Outcome}
				if err != nil {
					m.outcome = "error"
				} else if status != 200 {
					m.outcome = "rejected"
				}
				mu.Lock()
				measurements = append(measurements, m)
				mu.Unlock()
			}(p, question)
		}
		wg.Wait()
	}
	duration := time.Since(started)
	// Wait for an actual final version in each room, not a fixed sleep.
	final := map[string]int64{}
	for _, id := range ids[:*rooms] {
		snapshot, err := s.Snapshot(ctx, id, "")
		if err != nil {
			return err
		}
		final[id] = snapshot.Version
	}
	deadline := time.NewTimer(8 * time.Second)
	poll := time.NewTicker(10 * time.Millisecond)
wait:
	for {
		observationsMu.Lock()
		seen := map[string]bool{}
		for _, event := range observations {
			if event.version >= final[event.quiz] {
				seen[event.quiz] = true
			}
		}
		observationsMu.Unlock()
		if len(seen) == len(final) {
			break
		}
		select {
		case <-deadline.C:
			break wait
		case <-poll.C:
		}
	}
	deadline.Stop()
	poll.Stop()
	for _, p := range players {
		p.socket.Close()
	}
	readers.Wait()
	close(events)
	<-collectDone
	if dropped.Load() != 0 {
		return fmt.Errorf("driver dropped %d observations; measurement invalid", dropped.Load())
	}
	info, err := rc.Info(ctx, "server").Result()
	if err != nil {
		return err
	}
	redisVersion := "unknown"
	for _, line := range strings.Split(info, "\n") {
		if strings.HasPrefix(line, "redis_version:") {
			redisVersion = strings.TrimSpace(strings.TrimPrefix(line, "redis_version:"))
		}
	}
	r := report{Scenario: "split-room baseline", Go: runtime.Version(), OS: runtime.GOOS + "/" + runtime.GOARCH, CPUs: runtime.NumCPU(), Redis: redisVersion, Namespace: *namespace, Participants: *participants, Rooms: *rooms, Concurrency: *concurrency, AnswerRate: *rate, Offered: len(measurements), Duration: duration.Seconds()}
	if *rooms == 1 {
		r.Scenario = "single-room burst"
	}
	responseTimes := []float64{}
	propagationTimes := []float64{}
	for _, m := range measurements {
		sample := measurementJSON{Outcome: m.outcome, Response: m.responseMS}
		responseTimes = append(responseTimes, m.responseMS)
		switch m.outcome {
		case "accepted":
			r.Accepted++
		case "replayed":
			r.Replays++
		case "rejected":
			r.Rejected++
		default:
			r.Errors++
		}
		if m.accepted {
			var first time.Time
			for _, event := range observations {
				if event.observer == m.participant || event.quiz != m.quiz || event.version < m.version || event.at.Before(m.start) {
					continue
				}
				if first.IsZero() || event.at.Before(first) {
					first = event.at
				}
			}
			if first.IsZero() {
				r.Unobserved++
			} else {
				ms := float64(first.Sub(m.start)) / float64(time.Millisecond)
				sample.Propagation = &ms
				propagationTimes = append(propagationTimes, ms)
			}
		}
		r.Samples = append(r.Samples, sample)
	}
	r.Throughput = float64(r.Accepted) / r.Duration
	r.Response = quantiles(responseTimes)
	r.Propagation = quantiles(propagationTimes)
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(*output, append(b, '\n'), 0644); err != nil {
		return err
	}
	fmt.Printf("%s: offered=%d accepted=%d replayed=%d rejected=%d errors=%d unobserved=%d duration=%.3fs accepted/s=%.2f response p95=%.2fms propagation p95=%.2fms\n", r.Scenario, r.Offered, r.Accepted, r.Replays, r.Rejected, r.Errors, r.Unobserved, r.Duration, r.Throughput, r.Response.P95, r.Propagation.P95)
	if r.Accepted != r.Offered || r.Unobserved != 0 {
		return fmt.Errorf("benchmark correctness failed; raw report saved")
	}
	return nil
}
