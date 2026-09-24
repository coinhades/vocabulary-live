package fixtures

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"vocabulary.live/internal/domain"
)

func question(prefix string, n int, prompt string, options [4]string, correct string, explanation string) domain.GradedQuestion {
	q := domain.GradedQuestion{Question: domain.Question{ID: fmt.Sprintf("%s-q%d", prefix, n), Prompt: prompt, Options: make([]domain.Option, 4)}, CorrectOptionID: correct, Explanation: explanation}
	for i, text := range options {
		q.Options[i] = domain.Option{ID: string(rune('a' + i)), Text: text}
	}
	return q
}

// These original educational fixtures are never imported by the frontend.
func All() map[string]domain.Quiz {
	quizzes := []domain.Quiz{
		{ID: "VOCAB-DEMO", Title: "Everyday words, sharper meaning", Questions: []domain.GradedQuestion{
			question("vocab", 1, "A concise message is…", [4]string{"full of repeated ideas", "brief and clearly expressed", "written in secret", "difficult to believe"}, "b", "Concise means expressing what matters in few words, without unnecessary detail."),
			question("vocab", 2, "Which word means to improve something by making small changes?", [4]string{"Refine", "Abandon", "Conceal", "Postpone"}, "a", "To refine is to improve or perfect something through small adjustments."),
			question("vocab", 3, "A reliable colleague is someone you can…", [4]string{"avoid", "surprise", "depend on", "replace immediately"}, "c", "Reliable describes someone or something that can be trusted to do what is expected."),
			question("vocab", 4, "If two events occur simultaneously, they happen…", [4]string{"without a reason", "in different years", "one after another", "at the same time"}, "d", "Simultaneously means at the same time."),
			question("vocab", 5, "Which word is closest in meaning to abundant?", [4]string{"Plentiful", "Fragile", "Hidden", "Scarce"}, "a", "Abundant means existing in large quantities; plentiful is a close synonym."),
			question("vocab", 6, "To clarify an instruction is to make it…", [4]string{"longer by repeating it", "optional for everyone", "easier to understand", "harder to follow"}, "c", "Clarify means to remove confusion and make meaning clear."),
			question("vocab", 7, "A temporary arrangement is intended to…", [4]string{"continue forever", "last for a limited time", "remain a secret", "start a disagreement"}, "b", "Temporary describes something that lasts for a limited period rather than permanently."),
			question("vocab", 8, "Which word describes a solution that is practical and possible?", [4]string{"Obsolete", "Accidental", "Invisible", "Feasible"}, "d", "Feasible means possible to do successfully with the time or resources available."),
		}},
		{ID: "TRAVEL-DEMO", Title: "Words for the next adventure", Questions: []domain.GradedQuestion{
			question("travel", 1, "An itinerary is a…", [4]string{"type of luggage", "plan of a journey", "person checking tickets", "fee for a passport"}, "b", "An itinerary is a planned route or schedule for a journey."),
			question("travel", 2, "Your destination is the place where you…", [4]string{"intend to arrive", "bought your suitcase", "exchanged money", "began packing"}, "a", "A destination is the place to which someone is traveling."),
			question("travel", 3, "At an airport, a departure is a flight…", [4]string{"being cleaned", "arriving from abroad", "leaving the airport", "with no passengers"}, "c", "Departure means the act of leaving a place."),
			question("travel", 4, "A direct route avoids…", [4]string{"all roads", "any daylight", "the destination", "unnecessary detours"}, "d", "A direct route takes you toward a destination without unnecessary changes of direction."),
			question("travel", 5, "To reserve a room means to…", [4]string{"arrange to have it held for you", "clean it before leaving", "share it with a stranger", "cancel an earlier booking"}, "a", "Reserve means to arrange for something to be kept for your future use."),
			question("travel", 6, "A pedestrian is someone traveling…", [4]string{"by train", "by boat", "on foot", "by airplane"}, "c", "A pedestrian is a person walking, especially in an area with roads."),
			question("travel", 7, "If a train is delayed, it will leave…", [4]string{"from every platform", "later than scheduled", "without its driver", "earlier than scheduled"}, "b", "Delayed means happening later than the planned or expected time."),
			question("travel", 8, "A landmark is a place or object that is…", [4]string{"always underwater", "impossible to locate", "open only at night", "easy to recognize and useful for navigation"}, "d", "A landmark is a recognizable feature that helps identify a location."),
		}},
	}
	out := make(map[string]domain.Quiz, len(quizzes))
	study := studyDetails()
	for _, q := range quizzes {
		for i := range q.Questions {
			q.Questions[i].Study = study[q.Questions[i].ID]
		}
		q.Policies = domain.LivePractice()
		// Recognize only the preceding 100-point policy for the explicit,
		// non-destructive upgrade. Questions, permutations and receipts stay intact.
		legacy := q.Policies
		legacy.ScoringPolicy = "FIRST_ANSWER_100"
		b, _ := json.Marshal(struct {
			Schema    int
			Questions []domain.GradedQuestion
			Policies  domain.Policies
		}{2, q.Questions, legacy})
		h := sha256.Sum256(b)
		q.LegacyContentVersion = hex.EncodeToString(h[:])
		timings := map[string]domain.QuestionTiming{}
		for _, question := range q.Questions {
			timings[question.ID] = question.Study.Timing
		}
		b, _ = json.Marshal(struct {
			Schema    int
			Questions []domain.GradedQuestion
			Policies  domain.Policies
			Timings   map[string]domain.QuestionTiming
		}{3, q.Questions, q.Policies, timings})
		h = sha256.Sum256(b)
		q.ContentVersion = hex.EncodeToString(h[:])
		out[q.ID] = q
	}
	return out
}
