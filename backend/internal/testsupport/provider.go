// Package testsupport contains deterministic test fixtures, not measured AI output.
// Only the dedicated local learning-testserver and tests import this package.
package testsupport

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"vocabulary.live/internal/domain"
	"vocabulary.live/internal/learning"
)

type Provider struct {
	mu    sync.Mutex
	Mode  string
	Delay time.Duration
	Calls int
	Audio []byte
}

func (p *Provider) Set(mode string, delay time.Duration) {
	p.mu.Lock()
	p.Mode = mode
	p.Delay = delay
	p.mu.Unlock()
}
func (p *Provider) Count() int { p.mu.Lock(); defer p.mu.Unlock(); return p.Calls }
func (p *Provider) ValidateCredentials(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Mode == "invalid-key" {
		return domain.Err("AI_CREDENTIALS_INVALID")
	}
	return nil
}
func (p *Provider) begin(ctx context.Context) error {
	p.mu.Lock()
	p.Calls++
	delay, mode := p.Delay, p.Mode
	p.mu.Unlock()
	if delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return domain.Err("AI_TIMEOUT")
		}
	}
	switch mode {
	case "invalid-key":
		return domain.Err("AI_CREDENTIALS_INVALID")
	case "timeout":
		return domain.Err("AI_TIMEOUT")
	case "unavailable":
		return domain.Err("AI_UNAVAILABLE")
	case "refused":
		return domain.Err("AI_REFUSED")
	case "invalid":
		return domain.Err("AI_INVALID_OUTPUT")
	}
	return nil
}
func (p *Provider) Speech(ctx context.Context, r learning.SpeechRequest) ([]byte, error) {
	if err := p.begin(ctx); err != nil {
		return nil, err
	}
	return append([]byte(nil), p.Audio...), nil
}
func (p *Provider) Text(ctx context.Context, r learning.TextRequest) ([]byte, error) {
	if err := p.begin(ctx); err != nil {
		return nil, err
	}
	input, _ := json.Marshal(r.Input)
	p.mu.Lock()
	mode := p.Mode
	p.mu.Unlock()
	var v map[string]json.RawMessage
	_ = json.Unmarshal(input, &v)
	var evidence learning.Evidence
	var data any
	switch r.Action {
	case "authoring", "regenerate":
		var brief learning.AuthoringBrief
		var current *learning.ItemContent
		var count int
		_ = json.Unmarshal(v["brief"], &brief)
		_ = json.Unmarshal(v["current"], &current)
		_ = json.Unmarshal(v["count"], &count)
		words := []string{"itinerary", "reservation", "destination", "luggage", "departure", "arrival", "journey", "accommodation"}
		if len(brief.TargetWords) > 0 {
			words = brief.TargetWords
		}
		items := []learning.ItemContent{}
		for i := 0; i < count; i++ {
			word := words[i%len(words)]
			if current != nil {
				word = current.Word
			}
			c := learning.ItemContent{Word: word, Sense: "A travel vocabulary sense for instructor review", PartOfSpeech: "noun", Pronunciation: "", Meaning: "A proposed travel meaning for " + word, Prompt: fmt.Sprintf("Which option defines %s in this travel context?", word), Options: []string{"A proposed travel meaning for " + word, "An unrelated weather condition", "A kind of musical instrument", "A unit of measurement"}, CorrectIndex: 0, Explanation: "Test fixture: review the definition and all choices before publication.", Example: "We discussed the " + word + " before leaving.", Difficulty: "medium", Level: brief.Level, Tags: []string{}, SuggestedTags: []string{"travel"}}
			if current != nil {
				c.Prompt = "New preview: " + c.Prompt
			}
			items = append(items, c)
		}
		data = learning.DraftGeneration{Items: items}
	case "explanation":
		_ = json.Unmarshal(input, &evidence)
		contrast := ""
		if !evidence.Correct {
			contrast = "The selected option describes a different idea."
		}
		data = learning.Explanation{Explanation: evidence.Explanation, Contrast: contrast, Example: "We used the word " + evidence.Word + " in our conversation.", CanonicalMeaning: evidence.CanonicalMeaning, Supported: true}
	case "example":
		_ = json.Unmarshal(v["evidence"], &evidence)
		var context string
		_ = json.Unmarshal(v["context"], &context)
		data = learning.Example{Sentence: "We practiced the word " + evidence.Word + " in a " + context + " conversation.", UsageNote: "Test fixture: compare this sentence with the canonical definition.", CanonicalMeaning: evidence.CanonicalMeaning, Supported: true}
		if mode == "long" {
			data = learning.Example{Sentence: "During our careful discussion of the word " + evidence.Word + ", we compared the instructions with a familiar daily situation, checked the intended meaning together, and wrote a clear sentence that everyone in the group could understand without needing additional context.", UsageNote: "Long test fixture for wrapping, not an educational quality claim.", CanonicalMeaning: evidence.CanonicalMeaning, Supported: true}
		}
	case "reflection":
		var all bool
		_ = json.Unmarshal(v["allCorrect"], &all)
		var ids []string
		_ = json.Unmarshal(v["allowedQuestionReferences"], &ids)
		summary := "In this practice, revisit the words listed below using their canonical meanings."
		if all {
			summary = "In this practice, no words were missed. Extra practice is optional."
		}
		steps := []learning.Suggestion{}
		for _, id := range ids {
			steps = append(steps, learning.Suggestion{QuestionID: id, Step: "Read the canonical meaning, then write a short sentence of your own."})
			if len(steps) == 3 {
				break
			}
		}
		data = learning.Reflection{Summary: summary, Suggestions: steps, Supported: true}
	case "review":
		var inputs []struct {
			Evidence learning.Evidence `json:"evidence"`
		}
		_ = json.Unmarshal(input, &inputs)
		cards := []learning.ReviewStem{}
		for _, v := range inputs {
			e := v.Evidence
			cards = append(cards, learning.ReviewStem{QuestionID: e.QuestionID, Word: e.Word, CanonicalMeaning: e.CanonicalMeaning, Prompt: "Practice again: " + e.Prompt})
		}
		data = learning.ReviewPlanOutput{Cards: cards, Supported: true}
	case "hint":
		data = learning.Hint{Hint: "Think about the canonical meaning you saw after this word in the quiz.", Supported: true}
	default:
		return nil, domain.Err("AI_INVALID_OUTPUT")
	}
	return json.Marshal(data)
}
