// Package learning adds optional, private learning actions outside competitive grading.
package learning

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"vocabulary.live/internal/domain"
)

const PromptVersion = "learning-v1"
const SpeechVersion = "speech-v1"
const MaxAudioBytes = 1 << 20
const MaxTextBytes = 128 << 10

type Config struct {
	Key, TextModel, SpeechModel, Voice, AdminSecret  string
	LearningEnabled, SpeechEnabled, AuthoringEnabled bool
}

func FromEnv() Config {
	value := func(k, d string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return d
	}
	return Config{Key: os.Getenv("OPENAI_API_KEY"), TextModel: value("OPENAI_TEXT_MODEL", "gpt-5.4-mini"), SpeechModel: value("OPENAI_TTS_MODEL", "gpt-4o-mini-tts"), Voice: value("OPENAI_TTS_VOICE", "cedar"), AdminSecret: os.Getenv("AUTHORING_ADMIN_SECRET"), LearningEnabled: value("AI_LEARNING_ENABLED", "true") == "true", SpeechEnabled: value("OPENAI_TTS_ENABLED", "true") == "true", AuthoringEnabled: value("AI_AUTHORING_ENABLED", "false") == "true"}
}
func (c Config) TextAvailable() bool { return c.LearningEnabled && c.Key != "" && c.TextModel != "" }
func (c Config) SpeechAvailable() bool {
	return c.SpeechEnabled && c.Key != "" && c.SpeechModel != "" && c.Voice != ""
}
func (c Config) StudioAvailable() bool {
	return c.AuthoringEnabled && len(c.AdminSecret) >= 32 && len(c.AdminSecret) <= 512
}

type TextRequest struct {
	Action       string         `json:"action"`
	Instructions string         `json:"instructions"`
	Input        any            `json:"input"`
	Schema       map[string]any `json:"schema"`
	Tokens       int            `json:"tokens"`
}
type SpeechRequest struct{ Text, PartOfSpeech, Sense string }
type StructuredTextProvider interface {
	Text(context.Context, TextRequest) ([]byte, error)
}
type SpeechProvider interface {
	Speech(context.Context, SpeechRequest) ([]byte, error)
}
type Provider interface {
	StructuredTextProvider
	SpeechProvider
	ValidateCredentials(context.Context) error
}

// OpenAI has no runtime URL override. Tests inject a transport, never a browser URL.
type OpenAI struct {
	config      Config
	client      *http.Client
	credentials credentialCheck
}

func NewOpenAI(c Config) *OpenAI { return newOpenAI(c, http.DefaultTransport) }
func newOpenAI(c Config, transport http.RoundTripper) *OpenAI {
	return &OpenAI{config: c, client: &http.Client{Transport: transport, Timeout: 45 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (p *OpenAI) post(ctx context.Context, path string, body any, cap int) ([]byte, string, error) {
	encoded, err := json.Marshal(body)
	if err != nil || len(encoded) > 64<<10 {
		return nil, "", domain.Err("AI_INVALID_OUTPUT")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/"+path, bytes.NewReader(encoded))
	if err != nil {
		return nil, "", domain.Err("AI_UNAVAILABLE")
	}
	req.Header.Set("Authorization", "Bearer "+p.config.Key)
	req.Header.Set("Content-Type", "application/json")
	response, err := p.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, "", domain.Err("AI_TIMEOUT")
		}
		return nil, "", domain.Err("AI_UNAVAILABLE")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		if response.StatusCode == http.StatusUnauthorized {
			p.rejectCredentials()
			return nil, "", domain.Err("AI_CREDENTIALS_INVALID")
		}
		return nil, "", domain.Err("AI_UNAVAILABLE")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, int64(cap+1)))
	if err != nil || len(raw) == 0 || len(raw) > cap {
		return nil, "", domain.Err("AI_INVALID_OUTPUT")
	}
	return raw, response.Header.Get("Content-Type"), nil
}
func (p *OpenAI) Text(ctx context.Context, input TextRequest) ([]byte, error) {
	rawInput, err := json.Marshal(input.Input)
	if err != nil || len(rawInput) > 32<<10 {
		return nil, domain.Err("VALIDATION_ERROR")
	}
	raw, _, err := p.post(ctx, "responses", map[string]any{
		"model": p.config.TextModel, "store": false, "max_output_tokens": input.Tokens,
		"instructions": input.Instructions,
		"input":        []any{map[string]any{"role": "user", "content": string(rawInput)}},
		"text":         map[string]any{"format": map[string]any{"type": "json_schema", "name": input.Action, "strict": true, "schema": input.Schema}},
	}, MaxTextBytes)
	if err != nil {
		return nil, err
	}
	var result struct {
		Status string          `json:"status"`
		Error  json.RawMessage `json:"error"`
		Output []struct {
			Type, Status, Role string
			Content            []struct{ Type, Text, Refusal string }
		} `json:"output"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return nil, domain.Err("AI_INVALID_OUTPUT")
	}
	if len(result.Error) > 0 && string(result.Error) != "null" {
		return nil, domain.Err("AI_UNAVAILABLE")
	}
	if result.Status != "completed" {
		return nil, domain.Err("AI_INCOMPLETE")
	}
	var text string
	for _, item := range result.Output {
		if item.Type != "message" {
			continue
		}
		if item.Role != "assistant" || (item.Status != "" && item.Status != "completed") {
			return nil, domain.Err("AI_INVALID_OUTPUT")
		}
		for _, part := range item.Content {
			if part.Type == "refusal" || part.Refusal != "" {
				return nil, domain.Err("AI_REFUSED")
			}
			if part.Type == "output_text" {
				if text != "" {
					return nil, domain.Err("AI_INVALID_OUTPUT")
				}
				text = part.Text
			}
		}
	}
	if len(text) == 0 || !json.Valid([]byte(text)) {
		return nil, domain.Err("AI_INVALID_OUTPUT")
	}
	return []byte(text), nil
}
func (p *OpenAI) Speech(ctx context.Context, input SpeechRequest) ([]byte, error) {
	if !ValidTarget(input.Text) {
		return nil, domain.Err("VALIDATION_ERROR")
	}
	instructions := "speech-v1. Say exactly the supplied word or short phrase once, with clear General American pronunciation, natural stress and a slightly slower teaching pace. No definition, introduction, spelling, music or extra commentary. Intended part of speech and sense (context only, do not speak): " + input.PartOfSpeech + "; " + input.Sense
	raw, typ, err := p.post(ctx, "audio/speech", map[string]any{"model": p.config.SpeechModel, "voice": p.config.Voice, "input": input.Text, "instructions": instructions, "response_format": "mp3"}, MaxAudioBytes)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(typ, "audio/mpeg") || !ValidMP3(raw) {
		return nil, domain.Err("AI_INVALID_OUTPUT")
	}
	return raw, nil
}

// Check ID3 bounds and an MPEG audio frame sync. Browser decoding remains a
// separate failure boundary; this does not certify pronunciation or phonetics.
func ValidMP3(b []byte) bool {
	if len(b) < 4 || len(b) > MaxAudioBytes {
		return false
	}
	start := 0
	if bytes.HasPrefix(b, []byte("ID3")) {
		if len(b) < 10 {
			return false
		}
		size := 0
		for _, v := range b[6:10] {
			if v&128 != 0 {
				return false
			}
			size = (size << 7) | int(v)
		}
		start = 10 + size
		if b[5]&16 != 0 {
			start += 10
		}
	}
	if start+4 > len(b) {
		return false
	}
	h := b[start:]
	return h[0] == 255 && h[1]&224 == 224 && h[1]&6 != 0 && h[2]&240 != 0 && h[2]&240 != 240 && h[2]&12 != 12
}
