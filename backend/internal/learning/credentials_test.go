package learning

import (
	"context"
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

func TestCredentialsValidateWithoutGeneratingAndCacheResults(t *testing.T) {
	for _, tc := range []struct {
		name       string
		key        string
		status     int
		body, code string
	}{
		{"missing", "", 200, `{"object":"list","data":[]}`, "AI_DISABLED"},
		{"valid", "test-key", 200, `{"object":"list","data":[]}`, ""},
		{"rejected", "test-key", 401, `{"error":{"message":"secret provider error"}}`, "AI_CREDENTIALS_INVALID"},
		{"forbidden", "test-key", 403, "", "AI_CREDENTIALS_INVALID"},
		{"rate limited", "test-key", 429, "", "AI_UNAVAILABLE"},
		{"malformed", "test-key", 200, `{"object":"list"}`, "AI_UNAVAILABLE"},
		{"too large", "test-key", 200, strings.Repeat("x", (512<<10)+1), "AI_UNAVAILABLE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				body, _ := io.ReadAll(r.Body)
				if r.Method != http.MethodGet || r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer test-key" || len(body) != 0 {
					t.Error("credential probe sent unexpected data")
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			config := Config{Key: tc.key, LearningEnabled: true, SpeechEnabled: true, TextModel: "text", SpeechModel: "speech", Voice: "voice"}
			p := newOpenAI(config, localTransport{server.URL, http.DefaultTransport})
			for i := 0; i < 3; i++ {
				if err := p.ValidateCredentials(context.Background()); (tc.code == "" && err != nil) || (tc.code != "" && domain.Code(err) != tc.code) {
					t.Fatalf("want %s, got %v", tc.code, err)
				}
			}
			s := New(nil, config, p, nil)
			defer s.Close()
			caps := s.Capabilities(context.Background())
			if caps.TextConfigured != (tc.code == "") || caps.SpeechConfigured != (tc.code == "") {
				t.Fatalf("unexpected capabilities: %+v", caps)
			}
			expected := int32(1)
			if tc.key == "" {
				expected = 0
			}
			if calls.Load() != expected {
				t.Fatalf("cache or missing-key gate failed: %d probes", calls.Load())
			}
		})
	}
}

func TestCredentialChecksCoalesceAndWaitingRequestCanCancel(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		_, _ = io.WriteString(w, `{"object":"list","data":[]}`)
	}))
	defer server.Close()
	p := newOpenAI(Config{Key: "test-key"}, localTransport{server.URL, http.DefaultTransport})
	first := make(chan error, 1)
	go func() { first <- p.ValidateCredentials(context.Background()) }()
	<-started
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := p.ValidateCredentials(canceled); err != context.Canceled {
		t.Errorf("canceled waiter: %v", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := p.ValidateCredentials(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	close(release)
	wg.Wait()
	if err := <-first; err != nil || calls.Load() != 1 {
		t.Fatalf("coalescing: %v, calls=%d", err, calls.Load())
	}
}

func TestProviderRejectionOverridesInFlightCredentialCheck(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var probes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(401)
			return
		}
		if probes.Add(1) == 1 {
			close(started)
			<-release
		}
		_, _ = io.WriteString(w, `{"object":"list","data":[]}`)
	}))
	defer server.Close()
	p := newOpenAI(Config{Key: "test-key"}, localTransport{server.URL, http.DefaultTransport})
	result := make(chan error, 1)
	go func() { result <- p.ValidateCredentials(context.Background()) }()
	<-started
	if _, _, err := p.post(context.Background(), "responses", map[string]string{}, 128); domain.Code(err) != "AI_CREDENTIALS_INVALID" {
		t.Error(err)
	}
	close(release)
	if err := <-result; domain.Code(err) != "AI_CREDENTIALS_INVALID" {
		t.Fatalf("stale success overrode rejection: %v", err)
	}
	if err := p.ValidateCredentials(context.Background()); domain.Code(err) != "AI_CREDENTIALS_INVALID" || probes.Load() != 1 {
		t.Fatal("rejection was not cached", err)
	}
	p.credentials.mu.Lock()
	p.credentials.expires = time.Now().Add(-time.Second)
	p.credentials.mu.Unlock()
	if err := p.ValidateCredentials(context.Background()); err != nil || probes.Load() != 2 {
		t.Fatal("expired rejection did not recover", err)
	}
}

func TestCredentialProbeBoundedAndDoesNotFollowRedirect(t *testing.T) {
	var leaks atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaks.Add(1) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer redirect.Close()
	p := newOpenAI(Config{Key: "test-key"}, localTransport{redirect.URL, http.DefaultTransport})
	if err := p.ValidateCredentials(context.Background()); domain.Code(err) != "AI_UNAVAILABLE" || leaks.Load() != 0 {
		t.Fatal("credential redirect leaked", err)
	}
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer slow.Close()
	p = newOpenAI(Config{Key: "test-key"}, localTransport{slow.URL, http.DefaultTransport})
	start := time.Now()
	if err := p.ValidateCredentials(context.Background()); domain.Code(err) != "AI_UNAVAILABLE" {
		t.Fatal(err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("credential check exceeded bounded timeout")
	}
}
