package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"vocabulary.live/internal/domain"
)

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

type errorBody struct {
	Code      string          `json:"code"`
	Message   string          `json:"message"`
	RequestID string          `json:"requestId"`
	Retryable bool            `json:"retryable"`
	Receipt   *domain.Receipt `json:"receipt,omitempty"`
}

var errorsByCode = map[string]struct {
	status  int
	message string
}{
	"AUTHORING_DISABLED":        {404, "The instructor studio is disabled. An operator must configure it locally."},
	"OPERATOR_UNAUTHENTICATED":  {401, "Unlock the instructor studio with the configured operator secret."},
	"AUTHORING_QUOTA":           {409, "This demo allows at most 20 saved drafts and 20 published sessions."},
	"DRAFT_NOT_FOUND":           {404, "This draft was not found or has expired."},
	"DRAFT_PUBLISHED":           {409, "This draft is already published. Published content cannot be edited."},
	"APPROVAL_REQUIRED":         {409, "Review and approve every saved question, then confirm the final review."},
	"REVIEW_EXPIRED":            {410, "This review expired after 24 hours. Return to your results and explicitly start a new review."},
	"VERSION_CONFLICT":          {409, "This item changed in another tab. Reload its saved version before continuing."},
	"AI_DISABLED":               {503, "Optional AI is not configured. Your quiz and canonical feedback remain available."},
	"AI_UNAVAILABLE":            {503, "The AI provider is unavailable. You can continue and try again later."},
	"AI_CREDENTIALS_INVALID":    {503, "AI learning is unavailable with the current server credentials."},
	"AI_TIMEOUT":                {504, "This learning request took too long. You can continue or retry."},
	"AI_REFUSED":                {422, "No teaching result was returned. Use the canonical feedback."},
	"AI_INCOMPLETE":             {422, "The teaching result was incomplete. Use the canonical feedback or retry."},
	"AI_INVALID_OUTPUT":         {422, "This generated content could not be validated. Use the canonical content."},
	"AI_BUSY":                   {429, "Learning requests are busy. Continue and try again shortly."},
	"AI_RATE_LIMITED":           {429, "The learning request limit was reached. Please try again later."},
	"AI_LIMIT_REACHED":          {409, "This practice has reached its learning limit."},
	"LEARNING_INELIGIBLE":       {403, "This learning action is not available at this point in your practice."},
	"ARTIFACT_EXPIRED":          {410, "This learning result has expired. Return to the practice to request a new result."},
	"QUESTION_ALREADY_ANSWERED": {409, "This question has already been answered. Synchronize to continue."},
	"VALIDATION_ERROR":          {400, "Check the request fields and try again."},
	"INVALID_OPTION":            {400, "Choose one of the available options."},
	"UNAUTHENTICATED":           {401, "Your anonymous session has expired. Start a new session to continue."},
	"NOT_JOINED":                {403, "Join this quiz before continuing."},
	"BAD_ORIGIN":                {403, "This request origin is not allowed."},
	"QUIZ_NOT_FOUND":            {404, "That quiz code was not found."},
	"QUESTION_NOT_FOUND":        {404, "That question does not belong to this quiz."},
	"NOT_FOUND":                 {404, "Route not found."},
	"QUIZ_FULL":                 {409, "This demo quiz has reached its 200-person capacity."},
	"IDEMPOTENCY_CONFLICT":      {409, "This submission ID was already used for a different answer."},
	"ALREADY_ANSWERED":          {409, "Your first answer has already been recorded."},
	"EPOCH_MISMATCH":            {409, "This quiz has been reset. Rejoin before answering."},
	"RATE_LIMITED":              {429, "Too many requests. Please wait a moment and retry."},
	"PAYLOAD_TOO_LARGE":         {413, "The request is too large."},
	"DEPENDENCY_UNAVAILABLE":    {503, "The quiz service is temporarily unavailable. Your last known standings may be stale."},
}

func (s *Server) failure(w http.ResponseWriter, r *http.Request, err error) {
	code := domain.Code(err)
	entry, ok := errorsByCode[code]
	if !ok {
		code = "DEPENDENCY_UNAVAILABLE"
		entry = errorsByCode[code]
	}
	body := errorBody{Code: code, Message: entry.message, RequestID: w.Header().Get("X-Request-Id"), Retryable: entry.status == 503 || entry.status == 504 || entry.status == 429}
	if entry.status == 429 && w.Header().Get("Retry-After") == "" {
		w.Header().Set("Retry-After", "60")
	}
	var d *domain.Error
	if errors.As(err, &d) {
		body.Receipt = d.Receipt
	}
	if entry.status == 503 {
		s.Metrics.Dependencies.WithLabelValues("http").Inc()
		s.log.Error("request dependency failure", "requestId", body.RequestID, "error", err)
	}
	s.Metrics.Requests.WithLabelValues(route(r), code).Inc()
	if route(r) == "answers" {
		s.Metrics.Answers.WithLabelValues(code).Inc()
	}
	s.log.Info("api request", "requestId", body.RequestID, "route", route(r), "outcome", code, "status", entry.status)
	writeJSON(w, entry.status, map[string]any{"error": body})
}
func route(r *http.Request) string {
	for _, v := range []string{"session", "join", "state", "answers", "start", "live"} {
		if strings.HasSuffix(r.URL.Path, "/"+v) {
			return v
		}
	}
	return "other"
}
func decode(w http.ResponseWriter, r *http.Request, target any) error {
	return decodeLimit(w, r, target, 4096)
}
func decodeLimit(w http.ResponseWriter, r *http.Request, target any, limit int64) error {
	typ, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || typ != "application/json" {
		return domain.Err("VALIDATION_ERROR")
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	raw, readErr := io.ReadAll(r.Body)
	if readErr != nil {
		var large *http.MaxBytesError
		if errors.As(readErr, &large) {
			return domain.Err("PAYLOAD_TOO_LARGE")
		}
		return domain.Err("VALIDATION_ERROR")
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return domain.Err("VALIDATION_ERROR")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			return domain.Err("PAYLOAD_TOO_LARGE")
		}
		return domain.Err("VALIDATION_ERROR")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			return domain.Err("PAYLOAD_TOO_LARGE")
		}
		return domain.Err("VALIDATION_ERROR")
	}
	return nil
}
