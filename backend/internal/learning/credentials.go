package learning

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	"vocabulary.live/internal/domain"
)

type credentialCheck struct {
	mu      sync.Mutex
	wait    chan struct{}
	expires time.Time
	err     error
	version uint64
}

// ValidateCredentials checks authentication without creating model output.
// Concurrent capability requests share one bounded GET. Results stay server-side.
func (p *OpenAI) ValidateCredentials(ctx context.Context) error {
	if p.config.Key == "" {
		return domain.Err("AI_DISABLED")
	}
	for {
		p.credentials.mu.Lock()
		if time.Now().Before(p.credentials.expires) {
			err := p.credentials.err
			p.credentials.mu.Unlock()
			return err
		}
		if wait := p.credentials.wait; wait != nil {
			p.credentials.mu.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		wait := make(chan struct{})
		p.credentials.wait = wait
		version := p.credentials.version
		p.credentials.mu.Unlock()
		err := p.checkCredentials(ctx)
		ttl := 5 * time.Minute
		if err != nil {
			ttl = 15 * time.Second
		}
		if domain.Code(err) == "AI_CREDENTIALS_INVALID" {
			ttl = time.Minute
		}
		p.credentials.mu.Lock()
		if version == p.credentials.version {
			p.credentials.err = err
			p.credentials.expires = time.Now().Add(ttl)
		}
		err = p.credentials.err
		p.credentials.wait = nil
		close(wait)
		p.credentials.mu.Unlock()
		return err
	}
}

func (p *OpenAI) rejectCredentials() {
	p.credentials.mu.Lock()
	defer p.credentials.mu.Unlock()
	p.credentials.version++
	p.credentials.err = domain.Err("AI_CREDENTIALS_INVALID")
	p.credentials.expires = time.Now().Add(time.Minute)
}

func (p *OpenAI) checkCredentials(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.openai.com/v1/models", nil)
	if err != nil {
		return domain.Err("AI_UNAVAILABLE")
	}
	req.Header.Set("Authorization", "Bearer "+p.config.Key)
	response, err := p.client.Do(req)
	if err != nil {
		return domain.Err("AI_UNAVAILABLE")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
			return domain.Err("AI_CREDENTIALS_INVALID")
		}
		return domain.Err("AI_UNAVAILABLE")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (512<<10)+1))
	var result struct {
		Object string            `json:"object"`
		Data   []json.RawMessage `json:"data"`
	}
	if err != nil || len(raw) > 512<<10 || json.Unmarshal(raw, &result) != nil || result.Object != "list" || result.Data == nil {
		return domain.Err("AI_UNAVAILABLE")
	}
	return nil
}
