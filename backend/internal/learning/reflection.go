package learning

import (
	"context"
	"strings"

	"vocabulary.live/internal/domain"
)

type Suggestion struct {
	QuestionID string `json:"questionId"`
	Step       string `json:"step"`
}
type Reflection struct {
	Summary     string       `json:"summary"`
	Suggestions []Suggestion `json:"suggestions"`
	Supported   bool         `json:"supported"`
}

func (s *Service) Reflection(ctx context.Context, owner, quiz string, request LessonRequest) (Artifact, error) {
	state, err := s.Authorize(ctx, owner, quiz, request.Epoch)
	if err != nil {
		return Artifact{}, err
	}
	if !state.Completed {
		return Artifact{}, domain.Err("LEARNING_INELIGIBLE")
	}
	if request.Context != "" || request.Variant != 0 {
		return Artifact{}, domain.Err("VALIDATION_ERROR")
	}
	if err = s.intent(ctx, owner, state.Epoch, request.ClientActionID, []any{"reflection", quiz, request}); err != nil {
		return Artifact{}, err
	}
	evidenceList := []Evidence{}
	ids := []string{}
	allowed := map[string]bool{}
	for _, r := range state.Receipts {
		evidenceList = append(evidenceList, evidence(r))
		if state.CorrectCount == state.TotalQuestions || !r.Correctness {
			ids = append(ids, r.QuestionID)
			allowed[r.QuestionID] = true
		}
	}
	req := TextRequest{Action: "reflection", Input: struct {
		Evidence       []Evidence `json:"evidence"`
		SuggestedWords []string   `json:"allowedQuestionReferences"`
		AllCorrect     bool       `json:"allCorrect"`
	}{evidenceList, ids, state.CorrectCount == state.TotalQuestions}, Instructions: groundedInstructions + "Scope the reflection to 'In this practice'. Give at most three practical suggestions with exact source question references from the allowlist. No numbers, score, rank, strengths section, diagnosis, proficiency/CEFR level or learner traits. Do not invent tested themes. If all answers were correct, explicitly say no words were missed; offer only optional extra practice. If words were missed, give supportive concrete review steps without shaming.", Schema: objectSchema(map[string]any{"summary": stringSchema(400), "supported": map[string]any{"type": "boolean"}, "suggestions": map[string]any{"type": "array", "maxItems": 3, "items": objectSchema(map[string]any{"questionId": map[string]any{"type": "string", "enum": ids}, "step": stringSchema(240)})}}), Tokens: 1300}
	return s.generated(ctx, owner, state, "", "", "reflection", req, func(raw []byte) bool {
		var v Reflection
		if !Decode(raw, &v) || !v.Supported || !Plain(v.Summary, 400, false) || len(v.Suggestions) > 3 || !strings.HasPrefix(v.Summary, "In this practice") {
			return false
		}
		combined := v.Summary
		seen := map[string]bool{}
		for _, item := range v.Suggestions {
			if !allowed[item.QuestionID] || seen[item.QuestionID] || !Plain(item.Step, 240, false) {
				return false
			}
			seen[item.QuestionID] = true
			combined += " " + item.Step
		}
		lower := strings.ToLower(combined)
		for _, term := range []string{"cefr", "intelligence", "personality", "disability", "dyslexia", "english level", "proficiency", "mastery", "confidence", "profession", "0123456789"} {
			if term == "0123456789" {
				if strings.ContainsAny(combined, term) {
					return false
				}
			} else if strings.Contains(lower, term) {
				return false
			}
		}
		if state.CorrectCount == state.TotalQuestions && !strings.Contains(strings.ToLower(v.Summary), "no words were missed") {
			return false
		}
		return true
	})
}
