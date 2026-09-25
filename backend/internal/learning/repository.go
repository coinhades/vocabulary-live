package learning

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
	"vocabulary.live/internal/domain"
)

type Artifact struct {
	ArtifactID        string          `json:"artifactId"`
	QuizID            string          `json:"quizId"`
	QuestionID        string          `json:"questionId"`
	Epoch             string          `json:"epoch"`
	ContentVersion    string          `json:"contentVersion"`
	AcceptedReceiptID string          `json:"acceptedReceiptId"`
	Source            string          `json:"source"`
	GeneratedAt       string          `json:"generatedAt"`
	PromptVersion     string          `json:"promptVersion"`
	Action            string          `json:"action"`
	Data              json.RawMessage `json:"data"`
}
type savedArtifact struct {
	Owner    string   `json:"owner"`
	Artifact Artifact `json:"artifact"`
}

func (s *Service) artifactKey(id string) string {
	return s.Store.Namespace + ":learning:artifact:" + id
}
func (s *Service) artifact(ctx context.Context, owner, id string) (Artifact, error) {
	raw, err := s.Store.Client.Get(ctx, s.artifactKey(id)).Bytes()
	if err == redis.Nil {
		return Artifact{}, domain.Err("ARTIFACT_EXPIRED")
	}
	if err != nil {
		return Artifact{}, err
	}
	var a savedArtifact
	if json.Unmarshal(raw, &a) != nil {
		return Artifact{}, domain.Err("DEPENDENCY_UNAVAILABLE")
	}
	if a.Owner != owner {
		return Artifact{}, domain.Err("NOT_FOUND")
	}
	return a.Artifact, nil
}
func (s *Service) saveArtifact(ctx context.Context, owner string, a Artifact) (Artifact, error) {
	raw, _ := json.Marshal(savedArtifact{owner, a})
	if err := s.Store.Client.SetNX(ctx, s.artifactKey(a.ArtifactID), raw, 24*time.Hour).Err(); err != nil {
		return Artifact{}, err
	}
	return s.artifact(ctx, owner, a.ArtifactID)
}

// Bind a bounded number of client intents to their exact payload. Successful
// variants have deterministic slots too, so concurrent replicas converge.
var intentScript = redis.NewScript(`
local old=redis.call('HGET',KEYS[1],ARGV[1])
if old then if old~=ARGV[2] then return 'IDEMPOTENCY_CONFLICT' end return 'ok' end
if redis.call('HLEN',KEYS[1])>=160 then return 'AI_LIMIT_REACHED' end
redis.call('HSET',KEYS[1],ARGV[1],ARGV[2])
if redis.call('TTL',KEYS[1])<0 then redis.call('EXPIRE',KEYS[1],86400) end
return 'ok'`)

func (s *Service) intent(ctx context.Context, owner, epoch, id string, payload any) error {
	if !domain.ValidSubmission(id) {
		return domain.Err("VALIDATION_ERROR")
	}
	code, err := intentScript.Run(ctx, s.Store.Client, []string{s.Store.Namespace + ":learning:intents:" + Hash([]string{owner, epoch})}, id, Hash(payload)).Text()
	if err != nil {
		return err
	}
	if code != "ok" {
		return domain.Err(code)
	}
	return nil
}

type Report struct {
	ArtifactID string `json:"artifactId"`
	Reason     string `json:"reason"`
	ReportedAt string `json:"reportedAt"`
}

var reportScript = redis.NewScript(`
if redis.call('HEXISTS',KEYS[1],ARGV[1])==1 then return 'ok' end
if redis.call('HLEN',KEYS[1])>=20 then return 'AI_LIMIT_REACHED' end
redis.call('HSET',KEYS[1],ARGV[1],ARGV[2]);redis.call('EXPIRE',KEYS[1],86400)
redis.call('LPUSH',KEYS[2],ARGV[2]);redis.call('LTRIM',KEYS[2],0,199);redis.call('EXPIRE',KEYS[2],604800)
return 'ok'`)

func (s *Service) Report(ctx context.Context, owner, id, reason string) error {
	if reason != "confusing" && reason != "wrong-sense" && reason != "inappropriate" {
		return domain.Err("VALIDATION_ERROR")
	}
	a, err := s.artifact(ctx, owner, id)
	if err != nil {
		return err
	}
	if _, err = s.Authorize(ctx, owner, a.QuizID, a.Epoch); err != nil {
		return err
	}
	raw, _ := json.Marshal(Report{id, reason, time.Now().UTC().Format(time.RFC3339)})
	code, err := reportScript.Run(ctx, s.Store.Client, []string{s.Store.Namespace + ":learning:{reports}:" + Hash(owner), s.Store.Namespace + ":learning:{reports}:recent"}, id, string(raw)).Text()
	if err != nil {
		return err
	}
	if code != "ok" {
		return domain.Err(code)
	}
	return nil
}
