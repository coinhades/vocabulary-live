package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
	"vocabulary.live/internal/domain"
	"vocabulary.live/internal/learning"
)

const operatorCookie = "vocab_operator"

var operatorLoginLimit = redis.NewScript(`
local global=tonumber(redis.call('GET',KEYS[1]) or '0');if global>=20 then return 0 end
local n=redis.call('INCR',KEYS[1]);if n==1 then redis.call('EXPIRE',KEYS[1],60) end
local ip=tonumber(redis.call('GET',KEYS[2]) or '0');if ip>=5 then return 0 end
n=redis.call('INCR',KEYS[2]);if n==1 then redis.call('EXPIRE',KEYS[2],60) end
return 1`)

func (s *Server) operator(r *http.Request) error {
	if !s.Learning.Config.StudioAvailable() {
		return domain.Err("AUTHORING_DISABLED")
	}
	cookie, err := r.Cookie(operatorCookie)
	if err != nil || len(cookie.Value) != 64 {
		return domain.Err("OPERATOR_UNAUTHENTICATED")
	}
	hash, err := s.Store.Client.Get(r.Context(), s.Store.Namespace+":authoring:operator:"+learning.Hash(cookie.Value)).Result()
	if err == redis.Nil {
		return domain.Err("OPERATOR_UNAUTHENTICATED")
	}
	if err != nil {
		return err
	}
	expected := learning.Hash(s.Learning.Config.AdminSecret)
	if subtle.ConstantTimeCompare([]byte(hash), []byte(expected)) != 1 {
		return domain.Err("OPERATOR_UNAUTHENTICATED")
	}
	return nil
}
func (s *Server) operatorSession(w http.ResponseWriter, r *http.Request) {
	if !s.Learning.Config.StudioAvailable() {
		s.failure(w, r, domain.Err("AUTHORING_DISABLED"))
		return
	}
	if r.Method == "GET" {
		if err := s.operator(r); err != nil {
			s.failure(w, r, err)
			return
		}
		s.success(w, r, map[string]any{"authenticated": true, "textConfigured": s.Learning.Capabilities(r.Context()).TextConfigured})
		return
	}
	prefix := s.Store.Namespace + ":authoring:{login}:"
	allowed, err := operatorLoginLimit.Run(r.Context(), s.Store.Client, []string{prefix + "global", prefix + learning.Hash(s.clientIP(r))}).Int()
	if err != nil {
		s.failure(w, r, err)
		return
	}
	if allowed != 1 {
		s.failure(w, r, domain.Err("AI_RATE_LIMITED"))
		return
	}
	var req struct {
		Secret string `json:"secret"`
	}
	if err = decode(w, r, &req); err != nil {
		s.failure(w, r, err)
		return
	}
	actual := sha256.Sum256([]byte(req.Secret))
	expected := sha256.Sum256([]byte(s.Learning.Config.AdminSecret))
	if len(req.Secret) > 512 || subtle.ConstantTimeCompare(actual[:], expected[:]) != 1 {
		s.failure(w, r, domain.Err("OPERATOR_UNAUTHENTICATED"))
		return
	}
	credential := domain.ID() + domain.ID()
	if err = s.Store.Client.Set(r.Context(), s.Store.Namespace+":authoring:operator:"+learning.Hash(credential), learning.Hash(s.Learning.Config.AdminSecret), time.Hour).Err(); err != nil {
		s.failure(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: operatorCookie, Value: credential, Path: "/api/instructor", HttpOnly: true, Secure: s.config.SecureCookie, SameSite: http.SameSiteStrictMode, MaxAge: 3600})
	s.success(w, r, map[string]any{"authenticated": true, "textConfigured": s.Learning.Capabilities(r.Context()).TextConfigured})
}
func (s *Server) operatorLogout(w http.ResponseWriter, r *http.Request) {
	if err := s.operator(r); err != nil {
		s.failure(w, r, err)
		return
	}
	cookie, _ := r.Cookie(operatorCookie)
	if err := s.Store.Client.Del(r.Context(), s.Store.Namespace+":authoring:operator:"+learning.Hash(cookie.Value)).Err(); err != nil {
		s.failure(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: operatorCookie, Path: "/api/instructor", HttpOnly: true, Secure: s.config.SecureCookie, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	s.success(w, r, map[string]bool{"signedOut": true})
}
func (s *Server) instructorDrafts(w http.ResponseWriter, r *http.Request) {
	if err := s.operator(r); err != nil {
		s.failure(w, r, err)
		return
	}
	if r.Method == "GET" {
		drafts, err := s.Learning.ListDrafts(r.Context())
		if err != nil {
			s.failure(w, r, err)
			return
		}
		summaries := []map[string]any{}
		for _, d := range drafts {
			summaries = append(summaries, map[string]any{"id": d.ID, "title": d.Title, "status": d.Status, "version": d.Version, "publishedQuizId": d.PublishedQuizID, "expiresAt": d.ExpiresAt})
		}
		s.success(w, r, summaries)
		return
	}
	var req learning.DraftCreate
	if err := decode(w, r, &req); err != nil {
		s.failure(w, r, err)
		return
	}
	draft, err := s.Learning.CreateDraft(r.Context(), "operator", req)
	if err != nil {
		s.failure(w, r, err)
		return
	}
	if err = s.operator(r); err != nil {
		s.failure(w, r, err)
		return
	}
	s.success(w, r, draft)
}
func (s *Server) instructorDraft(w http.ResponseWriter, r *http.Request) {
	if err := s.operator(r); err != nil {
		s.failure(w, r, err)
		return
	}
	id := r.PathValue("draftId")
	if r.Method == "GET" {
		d, err := s.Learning.GetDraft(r.Context(), id)
		if err != nil {
			s.failure(w, r, err)
			return
		}
		s.success(w, r, d)
		return
	}
	var req learning.DraftSave
	if err := decodeLimit(w, r, &req, 32768); err != nil {
		s.failure(w, r, err)
		return
	}
	draft, err := s.Learning.SaveDraft(r.Context(), id, req)
	if err != nil {
		s.failure(w, r, err)
		return
	}
	s.success(w, r, draft)
}
func (s *Server) instructorQuestion(w http.ResponseWriter, r *http.Request) {
	if err := s.operator(r); err != nil {
		s.failure(w, r, err)
		return
	}
	var req learning.DraftMutation
	if err := decode(w, r, &req); err != nil {
		s.failure(w, r, err)
		return
	}
	var value any
	var err error
	switch r.PathValue("action") {
	case "approve":
		value, err = s.Learning.ApproveDraftItem(r.Context(), r.PathValue("draftId"), r.PathValue("questionId"), req)
	case "regenerate":
		value, err = s.Learning.RegenerateDraftItem(r.Context(), "operator", r.PathValue("draftId"), r.PathValue("questionId"), req)
	default:
		err = domain.Err("NOT_FOUND")
	}
	if err != nil {
		s.failure(w, r, err)
		return
	}
	if err = s.operator(r); err != nil {
		s.failure(w, r, err)
		return
	}
	s.success(w, r, value)
}
func (s *Server) instructorPublish(w http.ResponseWriter, r *http.Request) {
	if err := s.operator(r); err != nil {
		s.failure(w, r, err)
		return
	}
	var req learning.DraftPublish
	if err := decode(w, r, &req); err != nil {
		s.failure(w, r, err)
		return
	}
	draft, err := s.Learning.PublishDraft(r.Context(), r.PathValue("draftId"), req)
	if err != nil {
		s.failure(w, r, err)
		return
	}
	s.success(w, r, draft)
}
func (s *Server) instructorReports(w http.ResponseWriter, r *http.Request) {
	if err := s.operator(r); err != nil {
		s.failure(w, r, err)
		return
	}
	rows, err := s.Store.Client.LRange(r.Context(), s.Store.Namespace+":learning:{reports}:recent", 0, 19).Result()
	if err != nil {
		s.failure(w, r, err)
		return
	}
	reports := []any{}
	for _, raw := range rows {
		var report learning.Report
		if json.Unmarshal([]byte(raw), &report) != nil {
			continue
		}
		stored, err := s.Store.Client.Get(r.Context(), s.Store.Namespace+":learning:artifact:"+report.ArtifactID).Bytes()
		if err == redis.Nil {
			reports = append(reports, map[string]any{"report": report, "artifact": nil})
			continue
		}
		if err != nil {
			s.failure(w, r, err)
			return
		}
		var record struct {
			Artifact learning.Artifact `json:"artifact"`
		}
		if json.Unmarshal(stored, &record) != nil {
			continue
		}
		reports = append(reports, map[string]any{"report": report, "artifact": record.Artifact})
	}
	s.success(w, r, reports)
}
