package httpapi

import (
	"net/http"
	"vocabulary.live/internal/domain"
	"vocabulary.live/internal/learning"
)

func (s *Server) reflection(w http.ResponseWriter, r *http.Request) {
	owner, err := s.identity(r)
	if err != nil {
		s.failure(w, r, err)
		return
	}
	var req learning.LessonRequest
	if err = decode(w, r, &req); err != nil {
		s.failure(w, r, err)
		return
	}
	a, err := s.Learning.Reflection(r.Context(), owner, r.PathValue("quizId"), req)
	if err != nil {
		s.failure(w, r, err)
		return
	}
	if _, err = s.identity(r); err != nil {
		s.failure(w, r, err)
		return
	}
	s.success(w, r, a)
}
func (s *Server) startReview(w http.ResponseWriter, r *http.Request) {
	owner, err := s.identity(r)
	if err != nil {
		s.failure(w, r, err)
		return
	}
	var req learning.ReviewStart
	if err = decode(w, r, &req); err != nil {
		s.failure(w, r, err)
		return
	}
	review, err := s.Learning.StartReview(r.Context(), owner, r.PathValue("quizId"), req)
	if err != nil {
		s.failure(w, r, err)
		return
	}
	if _, err = s.identity(r); err != nil {
		s.failure(w, r, err)
		return
	}
	s.success(w, r, review)
}
func (s *Server) currentReview(w http.ResponseWriter, r *http.Request) {
	owner, err := s.identity(r)
	if err != nil {
		s.failure(w, r, err)
		return
	}
	state, err := s.Store.Snapshot(r.Context(), r.PathValue("quizId"), owner)
	if err != nil {
		s.failure(w, r, err)
		return
	}
	if !state.Completed {
		s.failure(w, r, domain.Err("LEARNING_INELIGIBLE"))
		return
	}
	id := learning.Hash([]string{owner, state.QuizID, state.Epoch, state.ContentVersion, "review-v1"})[:32]
	review, err := s.Learning.GetReview(r.Context(), owner, id)
	if domain.Code(err) == "REVIEW_EXPIRED" {
		s.success(w, r, nil)
		return
	}
	if err != nil {
		s.failure(w, r, err)
		return
	}
	s.success(w, r, review)
}
func (s *Server) getReview(w http.ResponseWriter, r *http.Request) {
	owner, err := s.identity(r)
	if err != nil {
		s.failure(w, r, err)
		return
	}
	review, err := s.Learning.GetReview(r.Context(), owner, r.PathValue("reviewId"))
	if err != nil {
		s.failure(w, r, err)
		return
	}
	s.success(w, r, review)
}
func (s *Server) reviewAction(w http.ResponseWriter, r *http.Request) {
	owner, err := s.identity(r)
	if err != nil {
		s.failure(w, r, err)
		return
	}
	var req learning.ReviewAction
	if err = decode(w, r, &req); err != nil {
		s.failure(w, r, err)
		return
	}
	var review learning.ReviewState
	if r.PathValue("action") == "hint" {
		if req.OptionID != "" || req.Skip || req.ClientActionID != "" {
			err = domain.Err("VALIDATION_ERROR")
		} else {
			review, err = s.Learning.ReviewHint(r.Context(), owner, r.PathValue("reviewId"), r.PathValue("cardId"), req.Epoch)
		}
	} else {
		review, err = s.Learning.ReviewAction(r.Context(), owner, r.PathValue("reviewId"), r.PathValue("cardId"), r.PathValue("action"), req)
	}
	if err != nil {
		s.failure(w, r, err)
		return
	}
	if _, err = s.identity(r); err != nil {
		s.failure(w, r, err)
		return
	}
	s.success(w, r, review)
}
