package fixtures

import (
	"encoding/json"
	"strings"
	"testing"
	"vocabulary.live/internal/domain"
)

func TestEveryWordHasPrivateTeachingAndPublicDifficulty(t *testing.T) {
	for _, quiz := range All() {
		plan, err := domain.NewAttemptPlan(quiz, func(n int) (int, error) { return n - 1, nil })
		if err != nil {
			t.Fatal(err)
		}
		for _, q := range quiz.Questions {
			study := q.Study
			if study.Vocabulary.Word == "" || study.Vocabulary.Definition == "" {
				t.Fatalf("missing teaching for %s", q.ID)
			}
			if p := study.Vocabulary.Pronunciation; len(p) < 3 || !strings.HasPrefix(p, "/") || !strings.HasSuffix(p, "/") {
				t.Fatalf("missing slash-delimited pronunciation for %s", q.ID)
			}
			switch study.Vocabulary.PartOfSpeech {
			case "noun", "verb", "adjective", "adverb":
			default:
				t.Fatalf("missing part of speech for %s", q.ID)
			}
			if want := map[string]int{"easy": 20, "medium": 30, "hard": 40}[study.Timing.Difficulty]; want == 0 || want != study.Timing.DurationSeconds {
				t.Fatalf("invalid timing for %s", q.ID)
			}
			public, _ := json.Marshal(plan.Present(quiz, q.ID))
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(public, &fields); err != nil {
				t.Fatal(err)
			}
			for _, private := range []string{"vocabulary", "word", "pronunciation", "partOfSpeech", "definition", "Study", "correctOptionId", "explanation"} {
				if _, found := fields[private]; found {
					t.Fatalf("public question leaked %s", private)
				}
			}
			if _, found := fields["timing"]; !found {
				t.Fatal("missing countdown guidance")
			}
		}
	}
}
