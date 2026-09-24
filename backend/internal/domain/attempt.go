package domain

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

// Only this policy combination is implemented. Future modes must implement
// their server-side disclosure/scoring rules before advertising other policies.
type Policies struct {
	Mode                string `json:"mode"`
	FeedbackPolicy      string `json:"feedbackPolicy"`
	QuestionOrderPolicy string `json:"questionOrderPolicy"`
	AnswerOrderPolicy   string `json:"answerOrderPolicy"`
	LeaderboardPolicy   string `json:"leaderboardPolicy"`
	ScoringPolicy       string `json:"scoringPolicy"`
}

func LivePractice() Policies {
	return Policies{"LIVE_PRACTICE", "IMMEDIATE", "PER_PARTICIPANT_SHUFFLED", "PER_PARTICIPANT_SHUFFLED", "LIVE_ALL", "FIRST_ANSWER_TIMED_100_50"}
}

type Metadata struct {
	Title                  string   `json:"title"`
	ContentVersion         string   `json:"contentVersion"`
	TotalQuestions         int      `json:"totalQuestions"`
	PointsPerCorrectAnswer int      `json:"pointsPerCorrectAnswer"`
	PointsAfterTimeout     int      `json:"pointsAfterTimeout"`
	Policies               Policies `json:"policies"`
}

func (q Quiz) Metadata() Metadata {
	return Metadata{q.Title, q.ContentVersion, len(q.Questions), 100, 50, q.Policies}
}

// AttemptPlan is the immutable part of a ParticipantAttempt. Mutable cursor,
// score, answered questions and completion are normalized in the participant,
// score and receipt records and read with this plan in one Redis operation.
type AttemptPlan struct {
	ContentVersion string              `json:"contentVersion"`
	QuestionOrder  []string            `json:"questionOrder"`
	OptionOrders   map[string][]string `json:"optionOrders"`
}

type RandomInt func(n int) (int, error)

func SecureRandom(n int) (int, error) {
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0, err
	}
	return int(v.Int64()), nil
}

func shuffle(ids []string, random RandomInt) error {
	for i := len(ids) - 1; i > 0; i-- {
		j, err := random(i + 1)
		if err != nil {
			return err
		}
		if j < 0 || j > i {
			return fmt.Errorf("random source returned invalid index")
		}
		ids[i], ids[j] = ids[j], ids[i]
	}
	return nil
}

func NewAttemptPlan(q Quiz, random RandomInt) (AttemptPlan, error) {
	p := AttemptPlan{ContentVersion: q.ContentVersion, QuestionOrder: make([]string, 0, len(q.Questions)), OptionOrders: map[string][]string{}}
	for _, question := range q.Questions {
		p.QuestionOrder = append(p.QuestionOrder, question.ID)
		ids := make([]string, 0, len(question.Options))
		for _, option := range question.Options {
			ids = append(ids, option.ID)
		}
		if err := shuffle(ids, random); err != nil {
			return AttemptPlan{}, err
		}
		p.OptionOrders[question.ID] = ids
	}
	if err := shuffle(p.QuestionOrder, random); err != nil {
		return AttemptPlan{}, err
	}
	return p, p.Validate(q)
}

func (p AttemptPlan) Validate(q Quiz) error {
	if p.ContentVersion != q.ContentVersion || len(p.QuestionOrder) != len(q.Questions) || len(p.OptionOrders) != len(q.Questions) {
		return fmt.Errorf("invalid attempt content")
	}
	seen := map[string]bool{}
	for _, id := range p.QuestionOrder {
		question, ok := q.Question(id)
		if !ok || seen[id] || len(p.OptionOrders[id]) != len(question.Options) {
			return fmt.Errorf("invalid attempt question")
		}
		seen[id] = true
		options := map[string]bool{}
		for _, option := range question.Options {
			options[option.ID] = true
		}
		for _, oid := range p.OptionOrders[id] {
			if !options[oid] {
				return fmt.Errorf("invalid attempt option")
			}
			delete(options, oid)
		}
	}
	return nil
}

func (p AttemptPlan) Present(q Quiz, id string) Question {
	question, _ := q.Question(id)
	options := make([]Option, 0, len(question.Options))
	for _, oid := range p.OptionOrders[id] {
		for _, option := range question.Options {
			if option.ID == oid {
				options = append(options, option)
				break
			}
		}
	}
	return Question{ID: id, Prompt: question.Prompt, Options: options, Timing: &question.Study.Timing, PronunciationText: question.Study.PublicPronunciation}
}

// Add study information only after acceptance. In particular, the focus word
// can itself be the correct choice, so it must never enter a public question.
// Stored grading receipts remain immutable, including across this UI upgrade.
func (q Quiz) Teach(receipt *Receipt) {
	question, ok := q.Question(receipt.QuestionID)
	if !ok {
		return
	}
	receipt.Vocabulary = &question.Study.Vocabulary
	receipt.Question.Timing = &question.Study.Timing
	receipt.Question.PronunciationText = question.Study.PublicPronunciation
}
