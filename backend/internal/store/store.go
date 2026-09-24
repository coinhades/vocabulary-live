package store

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"vocabulary.live/internal/domain"
)

//go:embed transition.lua
var transition string
var script = redis.NewScript(transition)
var namespacePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

type Store struct {
	Client     *redis.Client
	Namespace  string
	Quizzes    map[string]domain.Quiz
	Repository QuizRepository
	Capacity   int
	random     domain.RandomInt
	randomMu   sync.Mutex
}

func New(client *redis.Client, namespace string, quizzes map[string]domain.Quiz, capacity int) (*Store, error) {
	return NewWithRandom(client, namespace, quizzes, capacity, domain.SecureRandom)
}

// Randomness is injectable at construction, never through an HTTP/demo switch.
// Serialize draws so a deterministic test source need not be concurrency-safe.
func NewWithRandom(client *redis.Client, namespace string, quizzes map[string]domain.Quiz, capacity int, random domain.RandomInt) (*Store, error) {
	if !namespacePattern.MatchString(namespace) || capacity < 1 || capacity > 200 {
		return nil, fmt.Errorf("invalid namespace or capacity")
	}
	if random == nil {
		return nil, fmt.Errorf("missing random source")
	}
	for _, q := range quizzes {
		if q.Policies != domain.LivePractice() || len(q.Questions) != 8 {
			return nil, fmt.Errorf("unsupported quiz policies or size")
		}
	}
	return &Store{Client: client, Namespace: namespace, Quizzes: quizzes, Repository: RedisQuizRepository{client, namespace + ":authoring:{studio}:contents"}, Capacity: capacity, random: random}, nil
}
func (s *Store) Keys(id string) []string {
	p := s.Namespace + ":quiz:{" + id + "}:"
	return []string{p + "meta", p + "participants", p + "scores", p + "answers", p + "requests", p + "attempts", p + "timers"}
}
func (s *Store) Channel() string { return s.Namespace + ":changes" }
func (s *Store) run(ctx context.Context, id, op string, args ...any) ([]any, error) {
	q, err := s.QuizContext(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.runQuiz(ctx, q, op, args...)
}

func (s *Store) runQuiz(ctx context.Context, q domain.Quiz, op string, args ...any) ([]any, error) {
	all := append([]any{op, q.ContentVersion}, args...)
	result, err := script.Run(ctx, s.Client, s.Keys(q.ID), all...).Slice()
	if err != nil {
		return nil, fmt.Errorf("redis transition %s: %w", op, err)
	}
	if len(result) < 1 {
		return nil, fmt.Errorf("empty redis result")
	}
	code, _ := result[0].(string)
	if code != "accepted" && code != "replayed" && code != "ok" {
		e := &domain.Error{Code: code}
		if code == "ALREADY_ANSWERED" && len(result) > 1 {
			var receipt domain.Receipt
			if err := json.Unmarshal([]byte(result[1].(string)), &receipt); err != nil {
				return nil, err
			}
			e.Receipt = &receipt
			q.Teach(e.Receipt)
		}
		return nil, e
	}
	return result, nil
}
func (s *Store) Seed(ctx context.Context) error {
	for id := range s.Quizzes {
		if _, err := s.run(ctx, id, "seed", domain.ID(), s.Capacity, s.manifest(id), s.Quizzes[id].LegacyContentVersion); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) Reset(ctx context.Context) error {
	for id := range s.Quizzes {
		if _, err := s.run(ctx, id, "reset", domain.ID(), s.Capacity, s.manifest(id)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) manifest(id string) string {
	return Manifest(s.Quizzes[id])
}

func (s *Store) Preview(ctx context.Context, id string) (domain.Metadata, error) {
	q, err := s.QuizContext(ctx, id)
	if err != nil {
		return domain.Metadata{}, err
	}
	if _, err := s.runQuiz(ctx, q, "preview"); err != nil {
		return domain.Metadata{}, err
	}
	return q.Metadata(), nil
}

type Change struct {
	QuizID  string `json:"quizId"`
	Epoch   string `json:"epoch"`
	Version int64  `json:"version"`
}

func (s *Store) Join(ctx context.Context, id, pid, name string) (Change, bool, error) {
	name, err := domain.Name(name)
	if err != nil {
		return Change{}, false, err
	}
	r, err := s.run(ctx, id, "join", pid, name, "")
	if domain.Code(err) == "PLAN_REQUIRED" {
		q, quizErr := s.QuizContext(ctx, id)
		if quizErr != nil {
			return Change{}, false, quizErr
		}
		s.randomMu.Lock()
		plan, planErr := domain.NewAttemptPlan(q, s.random)
		s.randomMu.Unlock()
		if planErr != nil {
			return Change{}, false, planErr
		}
		encoded, encodeErr := json.Marshal(plan)
		if encodeErr != nil {
			return Change{}, false, encodeErr
		}
		// A racing first join may have already won; Lua returns that membership
		// without replacing its plan, score, cursor or original name.
		r, err = s.run(ctx, id, "join", pid, name, string(encoded))
		if err != nil {
			return Change{}, false, err
		}
	}
	if err != nil {
		return Change{}, false, err
	}
	version, err := strconv.ParseInt(r[2].(string), 10, 64)
	return Change{id, r[1].(string), version}, r[0] == "accepted", err
}
func (s *Store) Answer(ctx context.Context, id, pid string, a domain.Answer) (domain.AnswerResult, error) {
	q, err := s.QuizContext(ctx, id)
	if err != nil {
		return domain.AnswerResult{}, err
	}
	if !domain.ValidSubmission(a.SubmissionID) || len(a.Epoch) != 32 {
		return domain.AnswerResult{}, domain.Err("VALIDATION_ERROR")
	}
	question, ok := q.Question(a.QuestionID)
	if !ok {
		return domain.AnswerResult{}, domain.Err("QUESTION_NOT_FOUND")
	}
	valid := false
	for _, o := range question.Options {
		if o.ID == a.OptionID {
			valid = true
		}
	}
	if !valid {
		return domain.AnswerResult{}, domain.Err("INVALID_OPTION")
	}
	points := 0
	if question.CorrectOptionID == a.OptionID {
		points = 100
	}
	public, _ := json.Marshal(question.Question)
	r, err := s.runQuiz(ctx, q, "answer", pid, a.Epoch, a.SubmissionID, a.QuestionID, a.OptionID, points, id, question.CorrectOptionID, question.Explanation, string(public))
	if err != nil {
		return domain.AnswerResult{}, err
	}
	result := domain.AnswerResult{Outcome: r[0].(string)}
	err = json.Unmarshal([]byte(r[1].(string)), &result.Receipt)
	if err == nil {
		q.Teach(&result.Receipt)
	}
	return result, err
}
func (s *Store) StartQuestion(ctx context.Context, id, pid string, a domain.StartQuestion) (domain.QuestionClock, error) {
	q, err := s.QuizContext(ctx, id)
	if err != nil {
		return domain.QuestionClock{}, err
	}
	question, ok := q.Question(a.QuestionID)
	if !ok {
		return domain.QuestionClock{}, domain.Err("QUESTION_NOT_FOUND")
	}
	if len(a.Epoch) != 32 {
		return domain.QuestionClock{}, domain.Err("VALIDATION_ERROR")
	}
	duration := question.Study.Timing.DurationSeconds
	if duration < 1 || duration > 120 {
		return domain.QuestionClock{}, fmt.Errorf("invalid question duration")
	}
	r, err := s.runQuiz(ctx, q, "start", pid, a.Epoch, a.QuestionID, duration*1000)
	if err != nil {
		return domain.QuestionClock{}, err
	}
	deadline, err := strconv.ParseInt(r[3].(string), 10, 64)
	if err != nil {
		return domain.QuestionClock{}, err
	}
	now, err := strconv.ParseInt(r[4].(string), 10, 64)
	return domain.QuestionClock{Epoch: r[1].(string), QuestionID: r[2].(string), DeadlineMS: deadline, ServerTimeMS: now}, err
}
func (s *Store) Snapshot(ctx context.Context, id, pid string) (domain.State, error) {
	q, err := s.QuizContext(ctx, id)
	if err != nil {
		return domain.State{}, err
	}
	r, err := s.runQuiz(ctx, q, "snapshot", pid)
	if err != nil {
		return domain.State{}, err
	}
	version, err := strconv.ParseInt(r[2].(string), 10, 64)
	if err != nil {
		return domain.State{}, err
	}
	state := domain.State{Snapshot: domain.Snapshot{QuizID: id, Epoch: r[1].(string), Version: version, Leaderboard: []domain.Row{}}, Metadata: q.Metadata(), ParticipantID: pid, Receipts: []domain.Receipt{}}
	type member struct {
		Name   string `json:"name"`
		Count  int    `json:"count"`
		Cursor int    `json:"cursor"`
	}
	members := map[string]member{}
	metadata := r[4].([]any)
	for i := 0; i < len(metadata); i += 2 {
		var m member
		if err := json.Unmarshal([]byte(metadata[i+1].(string)), &m); err != nil {
			return domain.State{}, err
		}
		members[metadata[i].(string)] = m
	}
	rows := r[3].([]any)
	for i := 0; i < len(rows); i += 2 {
		id := rows[i].(string)
		score, err := strconv.Atoi(rows[i+1].(string))
		m, ok := members[id]
		if err != nil || !ok || score < 0 || score > len(q.Questions)*100 || score%50 != 0 || m.Count < 0 || m.Count > len(q.Questions) || score > 100*m.Count || m.Name == "" {
			return domain.State{}, fmt.Errorf("invalid stored row")
		}
		state.Leaderboard = append(state.Leaderboard, domain.Row{ParticipantID: id, DisplayName: m.Name, Score: score, AnsweredCount: m.Count})
	}
	domain.Rank(state.Leaderboard)
	state.ParticipantCount = len(state.Leaderboard)
	for _, raw := range r[5].([]any) {
		var receipt domain.Receipt
		if err := json.Unmarshal([]byte(raw.(string)), &receipt); err != nil {
			return domain.State{}, err
		}
		state.Receipts = append(state.Receipts, receipt)
		q.Teach(&state.Receipts[len(state.Receipts)-1])
	}
	sort.Slice(state.Receipts, func(i, j int) bool { return state.Receipts[i].AcceptedVersion < state.Receipts[j].AcceptedVersion })
	for _, receipt := range state.Receipts {
		if receipt.Correctness {
			state.CorrectCount++
		}
	}
	state.Accuracy = float64((state.CorrectCount*1000+len(q.Questions)/2)/len(q.Questions)) / 10
	if pid != "" {
		var plan domain.AttemptPlan
		if err := json.Unmarshal([]byte(r[6].(string)), &plan); err != nil {
			return domain.State{}, err
		}
		if err := plan.Validate(q); err != nil {
			return domain.State{}, err
		}
		for _, row := range state.Leaderboard {
			if row.ParticipantID == pid {
				state.Score, state.AnsweredCount, state.CurrentRank = row.Score, row.AnsweredCount, row.Rank
				break
			}
		}
		state.Completed = state.AnsweredCount == len(plan.QuestionOrder)
		cursor := members[pid].Cursor
		if !state.Completed {
			if cursor < 0 || cursor >= len(plan.QuestionOrder) {
				return domain.State{}, fmt.Errorf("invalid cursor")
			}
			question := plan.Present(q, plan.QuestionOrder[cursor])
			state.CurrentQuestion = &question
			state.QuestionNumber = cursor + 1
		}
	}
	return state, nil
}
func (s *Store) Publish(ctx context.Context, c Change) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return s.Client.Publish(ctx, s.Channel(), b).Err()
}
func digest(credential string) string {
	h := sha256.Sum256([]byte(credential))
	return hex.EncodeToString(h[:])
}
func (s *Store) Session(ctx context.Context, credential string) (string, error) {
	if len(credential) != 64 {
		return "", domain.Err("UNAUTHENTICATED")
	}
	id, err := s.Client.Get(ctx, s.Namespace+":session:"+digest(credential)).Result()
	if err == redis.Nil {
		return "", domain.Err("UNAUTHENTICATED")
	}
	return id, err
}
func (s *Store) CreateSession(ctx context.Context) (string, string, error) {
	credential := domain.ID() + domain.ID()
	pid := domain.ID()
	err := s.Client.Set(ctx, s.Namespace+":session:"+digest(credential), pid, 7*24*time.Hour).Err()
	return credential, pid, err
}
func (s *Store) SessionTTL(ctx context.Context, credential string) (time.Duration, error) {
	ttl, err := s.Client.PTTL(ctx, s.Namespace+":session:"+digest(credential)).Result()
	if err != nil {
		return 0, err
	}
	if ttl <= 0 {
		return 0, domain.Err("UNAUTHENTICATED")
	}
	return ttl, nil
}
