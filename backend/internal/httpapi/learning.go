package httpapi

import (
	"net/http"
	"vocabulary.live/internal/domain"
	"vocabulary.live/internal/learning"
)

func learningRoute(r *http.Request) bool {
	switch r.PathValue("action") {
	case "pronunciation", "explanation", "examples":
		return true
	}
	return false
}
func (s *Server) learningQuestion(w http.ResponseWriter, r *http.Request) {
	owner, err := s.identity(r)
	if err != nil {
		s.failure(w, r, err)
		return
	}
	action := r.PathValue("action")
	if !learningRoute(r) {
		s.failure(w, r, domain.Err("NOT_FOUND"))
		return
	}
	var req learning.LessonRequest
	if err = decode(w, r, &req); err != nil {
		s.failure(w, r, err)
		return
	}
	quiz, qid := r.PathValue("quizId"), r.PathValue("questionId")
	var a learning.Artifact
	var audio []byte
	switch action {
	case "pronunciation":
		if req.Context != "" || req.Variant != 0 || req.ClientActionID != "" {
			err = domain.Err("VALIDATION_ERROR")
		} else {
			audio, err = s.Learning.Pronunciation(r.Context(), owner, quiz, qid, req.Epoch)
		}
	case "explanation":
		a, err = s.Learning.Explanation(r.Context(), owner, quiz, qid, req)
	case "examples":
		a, err = s.Learning.Example(r.Context(), owner, quiz, qid, req)
	}
	if err != nil {
		s.failure(w, r, err)
		return
	}
	// Recheck the authenticated credential after slow generation as well as epoch.
	if _, err = s.identity(r); err != nil {
		s.failure(w, r, err)
		return
	}
	if action == "pronunciation" {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.WriteHeader(200)
		_, _ = w.Write(audio)
		return
	}
	s.success(w, r, a)
}
func (s *Server) reportLearning(w http.ResponseWriter, r *http.Request) {
	owner, err := s.identity(r)
	if err != nil {
		s.failure(w, r, err)
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if err = decode(w, r, &req); err != nil {
		s.failure(w, r, err)
		return
	}
	if err = s.Learning.Report(r.Context(), owner, r.PathValue("artifactId"), req.Reason); err != nil {
		s.failure(w, r, err)
		return
	}
	s.success(w, r, map[string]bool{"reported": true})
}
