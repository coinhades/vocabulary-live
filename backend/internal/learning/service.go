package learning

import (
	"context"

	"vocabulary.live/internal/domain"
	"vocabulary.live/internal/store"
)

type Capabilities struct {
	SpeechConfigured bool   `json:"speechConfigured"`
	TextConfigured   bool   `json:"textConfigured"`
	PromptVersion    string `json:"promptVersion"`
}
type Service struct {
	Store    *store.Store
	Config   Config
	Provider Provider
	Gateway  *Gateway
}

func New(s *store.Store, c Config, p Provider, observe func(Observation)) *Service {
	if p == nil {
		p = NewOpenAI(c)
	}
	return &Service{Store: s, Config: c, Provider: p, Gateway: NewGateway(observe)}
}
func (s *Service) Capabilities(ctx context.Context) Capabilities {
	valid := false
	if s.Config.SpeechAvailable() || s.Config.TextAvailable() {
		valid = s.Provider.ValidateCredentials(ctx) == nil
	}
	return Capabilities{valid && s.Config.SpeechAvailable(), valid && s.Config.TextAvailable(), PromptVersion}
}
func (s *Service) checkProvider(ctx context.Context, speech bool) error {
	if (speech && !s.Config.SpeechAvailable()) || (!speech && !s.Config.TextAvailable()) {
		return domain.Err("AI_DISABLED")
	}
	return s.Provider.ValidateCredentials(ctx)
}
func (s *Service) Close() { s.Gateway.Close() }
func (s *Service) Authorize(ctx context.Context, owner, quiz, epoch string) (domain.State, error) {
	state, err := s.Store.Snapshot(ctx, quiz, owner)
	if err != nil {
		return state, err
	}
	if state.Epoch != epoch {
		return state, domain.Err("EPOCH_MISMATCH")
	}
	return state, nil
}
