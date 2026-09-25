package learning

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vocabulary.live/internal/domain"
)

type localTransport struct {
	base  string
	inner http.RoundTripper
}

func (l localTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	clone := r.Clone(r.Context())
	u := *r.URL
	clone.URL = &u
	u.Scheme = "http"
	u.Host = strings.TrimPrefix(l.base, "http://")
	return l.inner.RoundTrip(clone)
}
func TestResponsesContractAndFailureBoundaries(t *testing.T) {
	var response atomic.Value
	response.Store(`{"status":"completed","output":[{"type":"reasoning"},{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"{\"example\":\"A concise note.\"}"}]}]}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("unexpected request")
		}
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		if b["store"] != false || b["model"] != "gpt-5.4-mini" || b["response_format"] != nil || b["tools"] != nil {
			t.Error("wrong Responses contract")
		}
		f := b["text"].(map[string]any)["format"].(map[string]any)
		if f["strict"] != true || f["type"] != "json_schema" {
			t.Error("missing strict schema")
		}
		_, _ = io.WriteString(w, response.Load().(string))
	}))
	defer server.Close()
	p := newOpenAI(Config{Key: "test-key", TextModel: "gpt-5.4-mini"}, localTransport{server.URL, http.DefaultTransport})
	req := TextRequest{Action: "example", Input: map[string]string{"word": "concise"}, Instructions: "test instructions", Schema: objectSchema(map[string]any{"example": stringSchema(240)}), Tokens: 512}
	if _, err := p.Text(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ raw, code string }{
		{`{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"refusal","refusal":"no"}]}]}`, "AI_REFUSED"},
		{`{"status":"incomplete","output":[]}`, "AI_INCOMPLETE"},
		{`{"status":"completed","output":[]}`, "AI_INVALID_OUTPUT"},
		{`{"status":"completed","error":{"code":"internal"}}`, "AI_UNAVAILABLE"},
		{"{", "AI_INVALID_OUTPUT"}, {strings.Repeat("x", MaxTextBytes+1), "AI_INVALID_OUTPUT"},
	} {
		response.Store(tc.raw)
		if _, err := p.Text(context.Background(), req); domain.Code(err) != tc.code {
			t.Errorf("want %s got %v", tc.code, err)
		}
	}
}
func TestProviderNeverFollowsRedirects(t *testing.T) {
	var leaked atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Add(1) }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer server.Close()
	p := newOpenAI(Config{Key: "secret"}, localTransport{server.URL, http.DefaultTransport})
	if _, _, err := p.post(context.Background(), "responses", map[string]string{}, 128); domain.Code(err) != "AI_UNAVAILABLE" || leaked.Load() != 0 {
		t.Fatal("redirect forwarded")
	}
}

func TestSpeechContractAndBoundedValidation(t *testing.T) {
	clip := []byte{0xff, 0xfb, 0x90, 0, 0, 0, 0, 0}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/speech" {
			t.Error("wrong speech path")
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "gpt-4o-mini-tts" || body["voice"] != "cedar" || body["input"] != "reserve" || body["response_format"] != "mp3" {
			t.Error("wrong speech contract")
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write(clip)
	}))
	defer server.Close()
	p := newOpenAI(Config{Key: "test", SpeechModel: "gpt-4o-mini-tts", Voice: "cedar"}, localTransport{server.URL, http.DefaultTransport})
	if _, err := p.Speech(context.Background(), SpeechRequest{"reserve", "verb", "arrange for future use"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Speech(context.Background(), SpeechRequest{Text: strings.Repeat("x", 81)}); domain.Code(err) != "VALIDATION_ERROR" {
		t.Fatal("unbounded target")
	}
	if ValidMP3([]byte("ID3")) || ValidMP3([]byte("not audio")) || ValidMP3(make([]byte, MaxAudioBytes+1)) {
		t.Fatal("invalid audio accepted")
	}
}
func TestGatewayCoalescingWaiterCancellationAndBounds(t *testing.T) {
	g := NewGateway(nil)
	defer g.Close()
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	fn := func(ctx context.Context) ([]byte, error) {
		calls.Add(1)
		close(started)
		select {
		case <-release:
			return []byte("valid"), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	first, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = g.Call(first, "one", "explanation", "key", time.Second, fn) }()
	<-started
	second := make(chan error, 1)
	go func() {
		defer wg.Done()
		b, e := g.Call(context.Background(), "one", "explanation", "key", time.Second, fn)
		if e == nil && string(b) != "valid" {
			t.Error("wrong result")
		}
		second <- e
	}()
	deadline := time.Now().Add(time.Second)
	for {
		g.mu.Lock()
		n := g.waiting
		g.mu.Unlock()
		if n == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("second waiter missing")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	close(release)
	wg.Wait()
	if e := <-second; e != nil || calls.Load() != 1 {
		t.Fatalf("coalescing failed: %v %d", e, calls.Load())
	}
	if _, e := g.Call(context.Background(), "one", "explanation", "key", time.Second, fn); e != nil || calls.Load() != 1 {
		t.Fatal("cache replay regenerated")
	}
	c := newCache(2, 6)
	c.put("a", []byte("aa"))
	c.put("b", []byte("bb"))
	_, _ = c.get("a")
	c.put("c", []byte("cccc"))
	if len(c.items) > 2 || c.bytes > 6 {
		t.Fatal("cache exceeded bounds")
	}
	if _, ok := c.get("b"); ok {
		t.Fatal("LRU did not evict")
	}
}
func TestGatewayCancelsAbandonedWorkAndLimitsFailedAttempts(t *testing.T) {
	g := NewGateway(nil)
	defer g.Close()
	started := make(chan struct{})
	canceled := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_, _ = g.Call(ctx, "owner", "explanation", "abandoned", time.Second, func(up context.Context) ([]byte, error) {
			close(started)
			<-up.Done()
			close(canceled)
			return nil, up.Err()
		})
		close(done)
	}()
	<-started
	cancel()
	<-done
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("upstream leaked")
	}
	for i := 0; i < 5; i++ {
		_, _ = g.Call(context.Background(), "owner", "explanation", string(rune('a'+i)), time.Second, func(context.Context) ([]byte, error) { return nil, domain.Err("AI_INVALID_OUTPUT") })
	}
	if _, e := g.Call(context.Background(), "owner", "explanation", "limit", time.Second, func(context.Context) ([]byte, error) { t.Error("over budget provider call"); return nil, nil }); domain.Code(e) != "AI_RATE_LIMITED" {
		t.Fatalf("bad limit: %v", e)
	}
}

func TestGatewayGlobalBudgetConcurrencyAndShutdown(t *testing.T) {
	g := NewGateway(nil)
	started := make(chan struct{}, 8)
	var peak atomic.Int32
	var active atomic.Int32
	fn := func(ctx context.Context) ([]byte, error) {
		n := active.Add(1)
		for {
			old := peak.Load()
			if n <= old || peak.CompareAndSwap(old, n) {
				break
			}
		}
		started <- struct{}{}
		<-ctx.Done()
		active.Add(-1)
		return nil, ctx.Err()
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			g.Call(context.Background(), fmt.Sprint(i), "explanation", fmt.Sprint(i), time.Second, fn)
		}(i)
	}
	<-started
	<-started
	end := time.Now().Add(time.Second)
	for {
		g.mu.Lock()
		n := len(g.flights)
		g.mu.Unlock()
		if n == 8 {
			break
		}
		if time.Now().After(end) {
			t.Fatal("bounded flights never started")
		}
		time.Sleep(time.Millisecond)
	}
	if g.Active() != 2 {
		t.Fatal("unexpected active provider count")
	}
	if _, err := g.Call(context.Background(), "overflow", "explanation", "overflow", time.Second, fn); domain.Code(err) != "AI_BUSY" {
		t.Fatal("in-flight key limit", err)
	}
	g.Close()
	wg.Wait()
	if peak.Load() > 2 || active.Load() != 0 || g.Active() != 0 {
		t.Fatal("provider leaked on shutdown")
	}
	h := NewGateway(nil)
	defer h.Close()
	for i := 0; i < 100; i++ {
		_, err := h.Call(context.Background(), fmt.Sprint(i), "explanation", fmt.Sprint(i), time.Second, func(context.Context) ([]byte, error) { return nil, domain.Err("AI_UNAVAILABLE") })
		if domain.Code(err) != "AI_UNAVAILABLE" {
			t.Fatal(i, err)
		}
	}
	if _, err := h.Call(context.Background(), "101", "explanation", "101", time.Second, func(context.Context) ([]byte, error) { t.Error("hourly cap reached provider"); return nil, nil }); domain.Code(err) != "AI_RATE_LIMITED" {
		t.Fatal("hourly budget", err)
	}
	c := newCache(2, 100)
	c.put("expired", []byte("text"))
	entry := c.items["expired"]
	value := entry.Value.(cacheEntry)
	value.expires = time.Now().Add(-time.Second)
	entry.Value = value
	if _, ok := c.get("expired"); ok || len(c.items) != 0 || c.bytes != 0 {
		t.Fatal("expired content retained")
	}
}

func TestVocabularyVisibilityRequiresWholeWord(t *testing.T) {
	for _, v := range []struct {
		text, word string
		want       bool
	}{{"The scar is visible", "car", false}, {"A CAR is outside", "car", true}, {"Our check-in went well.", "check-in", true}, {"I reserved a table.", "reserve", false}, {"A concise note.", "concise", true}} {
		if containsWord(v.text, v.word) != v.want {
			t.Errorf("whole target boundary: %q %q", v.text, v.word)
		}
	}
}
