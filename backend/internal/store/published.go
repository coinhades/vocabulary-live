package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
	"vocabulary.live/internal/domain"
)

// QuizRepository resolves immutable published definitions. Seed fixtures retain
// their existing hashes; dynamic content lives in Redis and is read by every replica.
type QuizRepository interface {
	Get(context.Context, string) (domain.Quiz, error)
}
type RedisQuizRepository struct {
	Client *redis.Client
	Key    string
}
type PublishedDefinition struct {
	Quiz  domain.Quiz                    `json:"quiz"`
	Study map[string]domain.StudyDetails `json:"study"`
}

func (r RedisQuizRepository) Get(ctx context.Context, id string) (domain.Quiz, error) {
	raw, err := r.Client.HGet(ctx, r.Key, id).Bytes()
	if err == redis.Nil {
		return domain.Quiz{}, domain.Err("QUIZ_NOT_FOUND")
	}
	if err != nil {
		return domain.Quiz{}, err
	}
	var d PublishedDefinition
	if json.Unmarshal(raw, &d) != nil || d.Quiz.ID != id || len(d.Quiz.Questions) < 4 || len(d.Quiz.Questions) > 8 || d.Quiz.Policies != domain.LivePractice() {
		return domain.Quiz{}, domain.Err("DEPENDENCY_UNAVAILABLE")
	}
	for i := range d.Quiz.Questions {
		v, ok := d.Study[d.Quiz.Questions[i].ID]
		if !ok {
			return domain.Quiz{}, domain.Err("DEPENDENCY_UNAVAILABLE")
		}
		d.Quiz.Questions[i].Study = v
	}
	return d.Quiz, nil
}
func (s *Store) ContentKey() string { return s.Namespace + ":authoring:{studio}:contents" }
func (s *Store) QuizContext(ctx context.Context, id string) (domain.Quiz, error) {
	if q, ok := s.Quizzes[id]; ok {
		return q, nil
	}
	return s.Repository.Get(ctx, id)
}
func (s *Store) Quiz(id string) (domain.Quiz, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return s.QuizContext(ctx, id)
}
func Manifest(q domain.Quiz) string {
	m := map[string][]string{}
	for _, v := range q.Questions {
		for _, o := range v.Options {
			m[v.ID] = append(m[v.ID], o.ID)
		}
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// All publication writes are a single primary-Redis operation. This demo does
// not claim Redis Cluster support for the separate authoring namespace.
var publicationScript = redis.NewScript(`
local types={'string','hash','hash','hash','zset','hash','hash','hash','hash'}
for i=1,#KEYS do local t=redis.call('TYPE',KEYS[i]).ok;if t~='none' and t~=types[i] then return redis.error_reply('STORAGE_TYPE') end end
local old=redis.call('GET',KEYS[1]);if not old then return 'DRAFT_NOT_FOUND' end
if old~=ARGV[1] then return 'VERSION_CONFLICT' end
if redis.call('HEXISTS',KEYS[2],ARGV[3])==1 then return 'VERSION_CONFLICT' end
if redis.call('HLEN',KEYS[2])>=20 then return 'AUTHORING_QUOTA' end
for i=3,#KEYS do if redis.call('EXISTS',KEYS[i])~=0 then return redis.error_reply('PUBLICATION_COLLISION') end end
local definition=cjson.decode(ARGV[4]);local draft=cjson.decode(ARGV[2]);local manifest=cjson.decode(ARGV[7])
redis.call('HSET',KEYS[2],ARGV[3],ARGV[4])
redis.call('HSET',KEYS[3],'epoch',ARGV[5],'version',0,'content',ARGV[6],'capacity',ARGV[8],'manifest',ARGV[7])
redis.call('SET',KEYS[1],ARGV[2],'KEEPTTL')
return 'ok'`)

func (s *Store) PublishDefinition(ctx context.Context, draftKey, before, after string, definition PublishedDefinition, epoch string) error {
	if _, seed := s.Quizzes[definition.Quiz.ID]; seed {
		return domain.Err("VALIDATION_ERROR")
	}
	raw, _ := json.Marshal(definition)
	keys := append([]string{draftKey, s.ContentKey()}, s.Keys(definition.Quiz.ID)...)
	code, err := publicationScript.Run(ctx, s.Client, keys, before, after, definition.Quiz.ID, string(raw), epoch, definition.Quiz.ContentVersion, Manifest(definition.Quiz), s.Capacity).Text()
	if err != nil {
		return err
	}
	if code != "ok" {
		return domain.Err(code)
	}
	return nil
}
