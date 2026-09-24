package domain

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Option struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}
type Question struct {
	ID                string          `json:"id"`
	Prompt            string          `json:"prompt"`
	Options           []Option        `json:"options"`
	Timing            *QuestionTiming `json:"timing,omitempty"`
	PronunciationText string          `json:"pronunciationText,omitempty"`
}

// Difficulty determines the server-owned window for full credit.
type QuestionTiming struct {
	Difficulty      string `json:"difficulty"`
	DurationSeconds int    `json:"durationSeconds"`
}
type Vocabulary struct {
	Word          string   `json:"word"`
	Pronunciation string   `json:"pronunciation"`
	PartOfSpeech  string   `json:"partOfSpeech"`
	Definition    string   `json:"definition"`
	Sense         string   `json:"sense,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	Example       string   `json:"example,omitempty"`
}
type StudyDetails struct {
	Timing              QuestionTiming
	Vocabulary          Vocabulary
	PublicPronunciation string
	TargetLevel         string
}
type GradedQuestion struct {
	Question
	CorrectOptionID string
	Explanation     string
	// Teaching content is private until acceptance. Fixtures hash timing
	// separately because the full-credit duration now affects grading.
	Study StudyDetails `json:"-"`
}
type Quiz struct {
	ID                   string
	Title                string
	ContentVersion       string
	LegacyContentVersion string
	Questions            []GradedQuestion
	Policies             Policies
}

func (q Quiz) Question(id string) (GradedQuestion, bool) {
	for _, v := range q.Questions {
		if v.ID == id {
			return v, true
		}
	}
	return GradedQuestion{}, false
}

type Answer struct {
	SubmissionID string `json:"submissionId"`
	Epoch        string `json:"epoch"`
	QuestionID   string `json:"questionId"`
	OptionID     string `json:"optionId"`
}
type StartQuestion struct {
	Epoch      string `json:"epoch"`
	QuestionID string `json:"questionId"`
}
type QuestionClock struct {
	Epoch        string `json:"epoch"`
	QuestionID   string `json:"questionId"`
	DeadlineMS   int64  `json:"deadlineMs"`
	ServerTimeMS int64  `json:"serverTimeMs"`
}
type Receipt struct {
	SubmissionID           string      `json:"submissionId"`
	QuizID                 string      `json:"quizId"`
	Epoch                  string      `json:"epoch"`
	QuestionID             string      `json:"questionId"`
	SelectedOptionID       string      `json:"selectedOptionId"`
	Correctness            bool        `json:"correctness"`
	TimedOut               bool        `json:"timedOut"`
	DeadlineMS             int64       `json:"deadlineMs,omitempty"`
	AcceptedAtMS           int64       `json:"acceptedAtMs,omitempty"`
	PointsAwarded          int         `json:"pointsAwarded"`
	TotalScoreAtAcceptance int         `json:"totalScoreAtAcceptance"`
	AcceptedVersion        int64       `json:"acceptedVersion"`
	CorrectOptionID        string      `json:"correctOptionId"`
	Explanation            string      `json:"explanation"`
	Question               Question    `json:"question"`
	QuestionNumber         int         `json:"questionNumber"`
	Vocabulary             *Vocabulary `json:"vocabulary,omitempty"`
}
type AnswerResult struct {
	Outcome string  `json:"outcome"`
	Receipt Receipt `json:"receipt"`
}
type Row struct {
	ParticipantID string `json:"participantId"`
	DisplayName   string `json:"displayName"`
	Score         int    `json:"score"`
	Rank          int    `json:"rank"`
	AnsweredCount int    `json:"answeredCount"`
}
type Snapshot struct {
	QuizID           string `json:"quizId"`
	Epoch            string `json:"epoch"`
	Version          int64  `json:"version"`
	ParticipantCount int    `json:"participantCount"`
	Leaderboard      []Row  `json:"leaderboard"`
}
type State struct {
	Snapshot
	Metadata
	ParticipantID   string    `json:"participantId"`
	AnsweredCount   int       `json:"answeredCount"`
	Score           int       `json:"score"`
	Completed       bool      `json:"completed"`
	CorrectCount    int       `json:"correctCount"`
	Accuracy        float64   `json:"accuracy"`
	CurrentRank     int       `json:"currentRank"`
	QuestionNumber  int       `json:"questionNumber"`
	CurrentQuestion *Question `json:"currentQuestion"`
	Receipts        []Receipt `json:"receipts"`
}
type Event struct {
	Type     string    `json:"type"`
	Snapshot *Snapshot `json:"snapshot,omitempty"`
	Code     string    `json:"code,omitempty"`
}
type Error struct {
	Code    string
	Receipt *Receipt
}

func (e *Error) Error() string { return e.Code }
func Code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return "DEPENDENCY_UNAVAILABLE"
}
func Err(code string) error { return &Error{Code: code} }

var uuid = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func ValidSubmission(id string) bool { return uuid.MatchString(id) }
func ID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func Name(s string) (string, error) {
	s = strings.TrimSpace(s)
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) < 1 || utf8.RuneCountInString(s) > 32 {
		return "", Err("VALIDATION_ERROR")
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return "", Err("VALIDATION_ERROR")
		}
	}
	return s, nil
}
func Rank(rows []Row) {
	rank := 0
	for i := range rows {
		if i == 0 || rows[i].Score != rows[i-1].Score {
			rank = i + 1
		}
		rows[i].Rank = rank
	}
}
