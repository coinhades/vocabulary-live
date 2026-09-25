package learning

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/redis/go-redis/v9"
	"vocabulary.live/internal/domain"
)

type ReviewStart struct {
	Epoch          string `json:"epoch"`
	ClientActionID string `json:"clientActionId"`
	ExtraPractice  bool   `json:"extraPractice"`
	UseAI          bool   `json:"useAI"`
}
type ReviewAction struct {
	Epoch          string `json:"epoch"`
	ClientActionID string `json:"clientActionId"`
	OptionID       string `json:"optionId,omitempty"`
	Skip           bool   `json:"skip,omitempty"`
}
type ReviewCard struct {
	ID            string          `json:"id"`
	QuestionID    string          `json:"questionId"`
	Prompt        string          `json:"prompt"`
	Options       []domain.Option `json:"options"`
	Word          string          `json:"word"`
	Source        string          `json:"source"`
	ArtifactID    string          `json:"artifactId"`
	Reinforcement bool            `json:"reinforcement"`
}
type ReviewFeedback struct {
	CardID           string `json:"cardId"`
	Status           string `json:"status"`
	SelectedOptionID string `json:"selectedOptionId"`
	Correct          bool   `json:"correct"`
	CorrectOptionID  string `json:"correctOptionId"`
	Definition       string `json:"definition"`
	Explanation      string `json:"explanation"`
	HintUsed         bool   `json:"hintUsed"`
	NeedsAnotherLook bool   `json:"needsAnotherLook"`
}
type storedCard struct {
	Card            ReviewCard        `json:"card"`
	CorrectOptionID string            `json:"correctOptionId"`
	Vocabulary      domain.Vocabulary `json:"vocabulary"`
	Explanation     string            `json:"explanation"`
	ReceiptID       string            `json:"receiptId"`
}
type reviewDocument struct {
	ID             string                    `json:"id"`
	Owner          string                    `json:"owner"`
	QuizID         string                    `json:"quizId"`
	Epoch          string                    `json:"epoch"`
	ContentVersion string                    `json:"contentVersion"`
	Version        int                       `json:"version"`
	ExpiresAt      string                    `json:"expiresAt"`
	Cursor         int                       `json:"cursor"`
	Cards          []storedCard              `json:"cards"`
	Responses      map[string]ReviewFeedback `json:"responses"`
	Hints          map[string]Artifact       `json:"hints"`
	Intents        map[string]string         `json:"intents"`
	Fallback       string                    `json:"fallback"`
}
type ReviewState struct {
	ReviewSessionID  string          `json:"reviewSessionId"`
	QuizID           string          `json:"quizId"`
	Epoch            string          `json:"epoch"`
	ContentVersion   string          `json:"contentVersion"`
	Version          int             `json:"version"`
	ExpiresAt        string          `json:"expiresAt"`
	Position         int             `json:"position"`
	TotalCards       int             `json:"totalCards"`
	Completed        bool            `json:"completed"`
	CurrentCard      *ReviewCard     `json:"currentCard"`
	Feedback         *ReviewFeedback `json:"feedback"`
	Hint             *Artifact       `json:"hint"`
	Checked          int             `json:"checked"`
	Skipped          int             `json:"skipped"`
	HintUsed         int             `json:"hintUsed"`
	NeedsAnotherLook int             `json:"needsAnotherLook"`
	Fallback         string          `json:"fallback"`
}

func (d reviewDocument) public() ReviewState {
	out := ReviewState{ReviewSessionID: d.ID, QuizID: d.QuizID, Epoch: d.Epoch, ContentVersion: d.ContentVersion, Version: d.Version, ExpiresAt: d.ExpiresAt, Position: d.Cursor + 1, TotalCards: len(d.Cards), Completed: d.Cursor >= len(d.Cards), Fallback: d.Fallback}
	for _, r := range d.Responses {
		if r.Status == "skipped" {
			out.Skipped++
		} else {
			out.Checked++
		}
		if r.NeedsAnotherLook {
			out.NeedsAnotherLook++
		}
	}
	out.HintUsed = len(d.Hints)
	if !out.Completed {
		card := d.Cards[d.Cursor].Card
		out.CurrentCard = &card
		if response, ok := d.Responses[card.ID]; ok {
			out.Feedback = &response
		}
		if hint, ok := d.Hints[card.ID]; ok {
			out.Hint = &hint
		}
	}
	return out
}
func (s *Service) reviewKey(id string) string { return s.Store.Namespace + ":learning:review:" + id }
func (s *Service) readReview(ctx context.Context, owner, id string) (reviewDocument, string, error) {
	raw, err := s.Store.Client.Get(ctx, s.reviewKey(id)).Result()
	if err == redis.Nil {
		return reviewDocument{}, "", domain.Err("REVIEW_EXPIRED")
	}
	if err != nil {
		return reviewDocument{}, "", err
	}
	var d reviewDocument
	if json.Unmarshal([]byte(raw), &d) != nil {
		return d, "", domain.Err("DEPENDENCY_UNAVAILABLE")
	}
	if d.Owner != owner {
		return d, "", domain.Err("NOT_FOUND")
	}
	if _, err = s.Authorize(ctx, owner, d.QuizID, d.Epoch); err != nil {
		return d, "", err
	}
	return d, raw, nil
}

var reviewCAS = redis.NewScript(`
if redis.call('HGET',KEYS[2],'epoch')~=ARGV[3] then return 'EPOCH_MISMATCH' end
local old=redis.call('GET',KEYS[1])
if (old or '')~=ARGV[1] then return 'VERSION_CONFLICT' end
if old then redis.call('SET',KEYS[1],ARGV[2],'KEEPTTL') else redis.call('SET',KEYS[1],ARGV[2],'EX',86400) end
return 'ok'`)

func (s *Service) saveReview(ctx context.Context, before string, d reviewDocument) error {
	raw, _ := json.Marshal(d)
	code, err := reviewCAS.Run(ctx, s.Store.Client, []string{s.reviewKey(d.ID), s.Store.Keys(d.QuizID)[0]}, before, string(raw), d.Epoch).Text()
	if err != nil {
		return err
	}
	if code != "ok" {
		return domain.Err(code)
	}
	return nil
}
func (s *Service) GetReview(ctx context.Context, owner, id string) (ReviewState, error) {
	d, _, err := s.readReview(ctx, owner, id)
	return d.public(), err
}

type ReviewStem struct {
	QuestionID       string `json:"questionId"`
	Word             string `json:"word"`
	CanonicalMeaning string `json:"canonicalMeaning"`
	Prompt           string `json:"prompt"`
}
type ReviewPlanOutput struct {
	Cards     []ReviewStem `json:"cards"`
	Supported bool         `json:"supported"`
}

func (s *Service) StartReview(ctx context.Context, owner, quiz string, req ReviewStart) (ReviewState, error) {
	state, err := s.Authorize(ctx, owner, quiz, req.Epoch)
	if err != nil {
		return ReviewState{}, err
	}
	if !state.Completed {
		return ReviewState{}, domain.Err("LEARNING_INELIGIBLE")
	}
	if state.CorrectCount == state.TotalQuestions && !req.ExtraPractice {
		return ReviewState{}, domain.Err("LEARNING_INELIGIBLE")
	}
	if err = s.intent(ctx, owner, req.Epoch, req.ClientActionID, []any{"review", quiz, req}); err != nil {
		return ReviewState{}, err
	}
	id := Hash([]string{owner, quiz, req.Epoch, state.ContentVersion, "review-v1"})[:32]
	if d, _, e := s.readReview(ctx, owner, id); e == nil {
		return d.public(), nil
	} else if domain.Code(e) != "REVIEW_EXPIRED" {
		return ReviewState{}, e
	}
	selected := []domain.Receipt{}
	ordered := append([]domain.Receipt(nil), state.Receipts...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].QuestionNumber < ordered[j].QuestionNumber })
	for _, r := range ordered {
		if !r.Correctness || state.CorrectCount == state.TotalQuestions {
			selected = append(selected, r)
			if len(selected) == 3 {
				break
			}
		}
	}
	d := reviewDocument{ID: id, Owner: owner, QuizID: quiz, Epoch: req.Epoch, ContentVersion: state.ContentVersion, Version: 1, ExpiresAt: time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339), Cards: []storedCard{}, Responses: map[string]ReviewFeedback{}, Hints: map[string]Artifact{}, Intents: map[string]string{}}
	stems := map[string]string{}
	artifactID := ""
	if req.UseAI && s.Config.TextAvailable() {
		inputs := []any{}
		ids := []string{}
		for _, r := range selected {
			inputs = append(inputs, struct {
				Evidence Evidence        `json:"evidence"`
				Options  []domain.Option `json:"options"`
			}{evidence(r), r.Question.Options})
			ids = append(ids, r.QuestionID)
		}
		spec := TextRequest{Action: "review", Input: inputs, Instructions: groundedInstructions + "Create one short alternate vocabulary question stem per supplied source. Preserve the supplied correct option and all choices exactly; return only new stems, never new answer keys or options. Include the exact target word and canonical meaning fields for validation. Avoid double negatives, ambiguous contexts and revealing which option to select.", Schema: objectSchema(map[string]any{"supported": map[string]any{"type": "boolean"}, "cards": map[string]any{"type": "array", "minItems": len(selected), "maxItems": len(selected), "items": objectSchema(map[string]any{"questionId": map[string]any{"type": "string", "enum": ids}, "word": stringSchema(80), "canonicalMeaning": stringSchema(600), "prompt": stringSchema(320)})}}), Tokens: 1600}
		a, e := s.generated(ctx, owner, state, "", "", "review-plan", spec, func(raw []byte) bool {
			var v ReviewPlanOutput
			if !Decode(raw, &v) || !v.Supported || len(v.Cards) != len(selected) {
				return false
			}
			seen := map[string]bool{}
			for _, c := range v.Cards {
				found := false
				for _, r := range selected {
					if c.QuestionID == r.QuestionID && c.Word == r.Vocabulary.Word && c.CanonicalMeaning == r.Vocabulary.Definition {
						found = true
					}
				}
				if !found || seen[c.QuestionID] || !Plain(c.Prompt, 320, false) {
					return false
				}
				seen[c.QuestionID] = true
			}
			return true
		})
		if e == nil {
			var output ReviewPlanOutput
			_ = json.Unmarshal(a.Data, &output)
			for _, v := range output.Cards {
				stems[v.QuestionID] = v.Prompt
			}
			artifactID = a.ArtifactID
		} else {
			d.Fallback = "AI review was unavailable or could not be validated. These cards use the canonical questions."
		}
	} else if req.UseAI {
		d.Fallback = "AI is not configured. These cards use the canonical questions."
	}
	for _, r := range selected {
		card := ReviewCard{ID: Hash([]string{id, r.QuestionID})[:32], QuestionID: r.QuestionID, Prompt: r.Question.Prompt, Options: append([]domain.Option(nil), r.Question.Options...), Word: r.Vocabulary.Word, Source: "canonical", ArtifactID: artifactID}
		if stem, ok := stems[r.QuestionID]; ok {
			card.Prompt = stem
			card.Source = "ai"
		}
		if card.ArtifactID == "" {
			a, e := s.saveArtifact(ctx, owner, Artifact{ArtifactID: Hash([]string{id, r.QuestionID, "canonical"})[:32], QuizID: quiz, QuestionID: r.QuestionID, Epoch: req.Epoch, ContentVersion: state.ContentVersion, AcceptedReceiptID: r.SubmissionID, Source: "canonical", GeneratedAt: time.Now().UTC().Format(time.RFC3339), PromptVersion: PromptVersion, Action: "review", Data: rawData(card)})
			if e != nil {
				return ReviewState{}, e
			}
			card.ArtifactID = a.ArtifactID
		}
		d.Cards = append(d.Cards, storedCard{card, r.CorrectOptionID, *r.Vocabulary, r.Explanation, r.SubmissionID})
	}
	if err = s.saveReview(ctx, "", d); domain.Code(err) == "VERSION_CONFLICT" {
		return s.GetReview(ctx, owner, id)
	} else if err != nil {
		return ReviewState{}, err
	}
	return d.public(), nil
}
func (s *Service) ReviewAction(ctx context.Context, owner, id, cardID, operation string, req ReviewAction) (ReviewState, error) {
	if !domain.ValidSubmission(req.ClientActionID) {
		return ReviewState{}, domain.Err("VALIDATION_ERROR")
	}
	fingerprint := Hash([]any{cardID, operation, req})
	for tries := 0; tries < 6; tries++ {
		d, before, err := s.readReview(ctx, owner, id)
		if err != nil {
			return ReviewState{}, err
		}
		if d.Epoch != req.Epoch {
			return ReviewState{}, domain.Err("EPOCH_MISMATCH")
		}
		if old, ok := d.Intents[req.ClientActionID]; ok {
			if old != fingerprint {
				return ReviewState{}, domain.Err("IDEMPOTENCY_CONFLICT")
			}
			return d.public(), nil
		}
		if len(d.Intents) >= 24 {
			return ReviewState{}, domain.Err("AI_LIMIT_REACHED")
		}
		if d.Cursor >= len(d.Cards) || d.Cards[d.Cursor].Card.ID != cardID {
			if _, ok := d.Responses[cardID]; ok {
				return d.public(), nil
			}
			return ReviewState{}, domain.Err("LEARNING_INELIGIBLE")
		}
		card := d.Cards[d.Cursor]
		response, answered := d.Responses[cardID]
		switch operation {
		case "answers":
			if answered {
				return d.public(), nil
			}
			if req.Skip && req.OptionID != "" {
				return ReviewState{}, domain.Err("VALIDATION_ERROR")
			}
			if !req.Skip {
				valid := false
				for _, o := range card.Card.Options {
					if o.ID == req.OptionID {
						valid = true
					}
				}
				if !valid {
					return ReviewState{}, domain.Err("INVALID_OPTION")
				}
			}
			_, hinted := d.Hints[cardID]
			correct := !req.Skip && req.OptionID == card.CorrectOptionID
			status := "checked"
			if req.Skip {
				status = "skipped"
			}
			response = ReviewFeedback{CardID: cardID, Status: status, SelectedOptionID: req.OptionID, Correct: correct, CorrectOptionID: card.CorrectOptionID, Definition: card.Vocabulary.Definition, Explanation: card.Explanation, HintUsed: hinted, NeedsAnotherLook: !correct}
			d.Responses[cardID] = response
			if !req.Skip && !correct && !card.Card.Reinforcement && len(d.Cards) < 6 {
				card.Card.ID = Hash([]string{id, cardID, "reinforce"})[:32]
				card.Card.Reinforcement = true
				d.Cards = append(d.Cards, card)
			}
		case "advance":
			if !answered || req.OptionID != "" || req.Skip {
				return ReviewState{}, domain.Err("LEARNING_INELIGIBLE")
			}
			d.Cursor++
		default:
			return ReviewState{}, domain.Err("NOT_FOUND")
		}
		d.Intents[req.ClientActionID] = fingerprint
		d.Version++
		if err = s.saveReview(ctx, before, d); domain.Code(err) == "VERSION_CONFLICT" {
			continue
		} else if err != nil {
			return ReviewState{}, err
		}
		return d.public(), nil
	}
	return ReviewState{}, domain.Err("VERSION_CONFLICT")
}

type Hint struct {
	Hint      string `json:"hint"`
	Supported bool   `json:"supported"`
}

func (s *Service) ReviewHint(ctx context.Context, owner, id, cardID, epoch string) (ReviewState, error) {
	d, _, err := s.readReview(ctx, owner, id)
	if err != nil {
		return ReviewState{}, err
	}
	if d.Epoch != epoch {
		return ReviewState{}, domain.Err("EPOCH_MISMATCH")
	}
	if d.Cursor >= len(d.Cards) || d.Cards[d.Cursor].Card.ID != cardID {
		return ReviewState{}, domain.Err("LEARNING_INELIGIBLE")
	}
	if _, ok := d.Responses[cardID]; ok {
		return ReviewState{}, domain.Err("LEARNING_INELIGIBLE")
	}
	if _, ok := d.Hints[cardID]; ok {
		return d.public(), nil
	}
	state, err := s.Authorize(ctx, owner, d.QuizID, d.Epoch)
	if err != nil {
		return ReviewState{}, err
	}
	card := d.Cards[d.Cursor]
	spec := TextRequest{Action: "hint", Input: struct {
		Word, Meaning, Prompt string
		Options               []domain.Option
	}{card.Card.Word, card.Vocabulary.Definition, card.Card.Prompt, card.Card.Options}, Instructions: groundedInstructions + "Give one short vocabulary clue for this unscored review card using its canonical sense. No chain of thought, option letters, answer key or extra explanation.", Schema: objectSchema(map[string]any{"hint": stringSchema(200), "supported": map[string]any{"type": "boolean"}}), Tokens: 500}
	a, err := s.generated(ctx, owner, state, card.Card.QuestionID, card.ReceiptID, "hint-"+cardID, spec, func(raw []byte) bool { var v Hint; return Decode(raw, &v) && v.Supported && Plain(v.Hint, 200, false) })
	if err != nil {
		return ReviewState{}, err
	}
	for tries := 0; tries < 6; tries++ {
		d, before, e := s.readReview(ctx, owner, id)
		if e != nil {
			return ReviewState{}, e
		}
		if d.Cursor >= len(d.Cards) || d.Cards[d.Cursor].Card.ID != cardID {
			return d.public(), nil
		}
		if _, ok := d.Responses[cardID]; ok {
			return d.public(), nil
		}
		if _, ok := d.Hints[cardID]; ok {
			return d.public(), nil
		}
		d.Hints[cardID] = a
		d.Version++
		if e = s.saveReview(ctx, before, d); domain.Code(e) == "VERSION_CONFLICT" {
			continue
		} else if e != nil {
			return ReviewState{}, e
		}
		return d.public(), nil
	}
	return ReviewState{}, domain.Err("VERSION_CONFLICT")
}
