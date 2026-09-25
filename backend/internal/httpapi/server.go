package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"vocabulary.live/internal/domain"
	"vocabulary.live/internal/learning"
	"vocabulary.live/internal/observability"
	"vocabulary.live/internal/realtime"
	"vocabulary.live/internal/store"
)

const CookieName = "vocab_session"

type Config struct {
	Origins             []string
	TrustedProxies      []netip.Prefix
	SecureCookie        bool
	StaticDir           string
	Rate, Burst         float64
	Coalesce, Reconcile time.Duration
	AI                  learning.Config
	Provider            learning.Provider
}
type Server struct {
	Store    *store.Store
	Hub      *realtime.Hub
	Metrics  *observability.Metrics
	Learning *learning.Service
	config   Config
	log      *slog.Logger
	limit    *limiter
	publish  func(context.Context, store.Change) error
}

// Publish injection is for deterministic integration fault tests, never a route.
func New(s *store.Store, c Config, logger *slog.Logger, publish func(context.Context, store.Change) error) *Server {
	if c.Rate <= 0 {
		c.Rate = 10
	}
	if c.Burst <= 0 {
		c.Burst = 40
	}
	if c.Coalesce <= 0 {
		c.Coalesce = 80 * time.Millisecond
	}
	if c.Reconcile <= 0 {
		c.Reconcile = 2 * time.Second
	}
	m := observability.New()
	if publish == nil {
		publish = s.Publish
	}
	l := learning.New(s, c.AI, c.Provider, func(o learning.Observation) {
		m.LearningRequests.WithLabelValues(o.Action, o.Outcome).Inc()
		if o.CacheHit {
			m.LearningCache.WithLabelValues(o.Action).Inc()
		}
		m.LearningDuration.WithLabelValues(o.Action).Observe(o.Duration.Seconds())
		m.LearningBytes.WithLabelValues(o.Action).Observe(float64(o.Bytes))
		logger.Info("learning action", "action", o.Action, "outcome", o.Outcome, "cacheHit", o.CacheHit, "durationMs", o.Duration.Milliseconds(), "bytes", o.Bytes, "active", o.Active)
	})
	m.Registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "learning_active_generations", Help: "Current local upstream provider work."}, func() float64 { return float64(l.Gateway.Active()) }))
	return &Server{Store: s, Hub: realtime.New(s, m, logger, c.Coalesce, c.Reconcile), Metrics: m, Learning: l, config: c, log: logger, limit: newLimiter(c.Rate, c.Burst), publish: publish}
}
func (s *Server) Close() { s.Learning.Close(); s.Hub.Close() }
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 500*time.Millisecond)
		defer cancel()
		if err := s.Store.Client.Ping(ctx).Err(); err != nil {
			s.failure(w, r, err)
			return
		}
		writeJSON(w, 200, map[string]string{"status": "ready"})
	})
	mux.Handle("GET /metrics", promhttp.HandlerFor(s.Metrics.Registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("POST /api/session", s.session)
	mux.HandleFunc("GET /api/learning-capabilities", func(w http.ResponseWriter, r *http.Request) { s.success(w, r, s.Learning.Capabilities(r.Context())) })
	mux.HandleFunc("GET /api/quizzes/{quizId}", s.preview)
	mux.HandleFunc("POST /api/quizzes/{quizId}/join", s.join)
	mux.HandleFunc("GET /api/quizzes/{quizId}/state", s.state)
	mux.HandleFunc("POST /api/quizzes/{quizId}/answers", s.answer)
	mux.HandleFunc("POST /api/quizzes/{quizId}/start", s.startQuestion)
	mux.HandleFunc("POST /api/quizzes/{quizId}/questions/{questionId}/{action}", s.learningQuestion)
	mux.HandleFunc("POST /api/learning-artifacts/{artifactId}/reports", s.reportLearning)
	mux.HandleFunc("POST /api/quizzes/{quizId}/learning-reflection", s.reflection)
	mux.HandleFunc("POST /api/quizzes/{quizId}/reviews", s.startReview)
	mux.HandleFunc("GET /api/quizzes/{quizId}/review", s.currentReview)
	mux.HandleFunc("GET /api/reviews/{reviewId}", s.getReview)
	mux.HandleFunc("POST /api/reviews/{reviewId}/cards/{cardId}/{action}", s.reviewAction)
	mux.HandleFunc("GET /api/instructor/session", s.operatorSession)
	mux.HandleFunc("POST /api/instructor/session", s.operatorSession)
	mux.HandleFunc("POST /api/instructor/logout", s.operatorLogout)
	mux.HandleFunc("GET /api/instructor/drafts", s.instructorDrafts)
	mux.HandleFunc("POST /api/instructor/drafts", s.instructorDrafts)
	mux.HandleFunc("GET /api/instructor/drafts/{draftId}", s.instructorDraft)
	mux.HandleFunc("POST /api/instructor/drafts/{draftId}", s.instructorDraft)
	mux.HandleFunc("POST /api/instructor/drafts/{draftId}/questions/{questionId}/{action}", s.instructorQuestion)
	mux.HandleFunc("POST /api/instructor/drafts/{draftId}/publish", s.instructorPublish)
	mux.HandleFunc("GET /api/instructor/reports", s.instructorReports)
	mux.HandleFunc("GET /api/quizzes/{quizId}/live", s.live)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { s.failure(w, r, domain.Err("NOT_FOUND")) })
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		s.static(w, r)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := domain.ID()
		w.Header().Set("X-Request-Id", requestID)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if s.config.SecureCookie {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		w.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; style-src 'self'; script-src 'self'; img-src 'self' data:; media-src 'self' blob:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
			if !s.limit.allow(s.clientIP(r)) {
				w.Header().Set("Retry-After", "1")
				s.failure(w, r, domain.Err("RATE_LIMITED"))
				return
			}
			if r.Method != "GET" || strings.HasSuffix(r.URL.Path, "/live") {
				if !s.origin(r) {
					s.failure(w, r, domain.Err("BAD_ORIGIN"))
					return
				}
			}
			if !strings.HasSuffix(r.URL.Path, "/live") {
				deadline := 3 * time.Second
				if strings.Contains(r.URL.Path, "/questions/") || strings.HasSuffix(r.URL.Path, "/hint") || strings.HasSuffix(r.URL.Path, "/learning-reflection") || strings.HasSuffix(r.URL.Path, "/reviews") {
					deadline = 24 * time.Second
				}
				if strings.HasPrefix(r.URL.Path, "/api/instructor/") {
					deadline = 48 * time.Second
				}
				ctx, cancel := context.WithTimeout(r.Context(), deadline)
				defer cancel()
				r = r.WithContext(ctx)
			}
		}
		mux.ServeHTTP(w, r)
	})
}
func (s *Server) origin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	for _, v := range s.config.Origins {
		if v == origin && v != "" {
			return true
		}
	}
	return false
}
func (s *Server) identity(r *http.Request) (string, error) {
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return "", domain.Err("UNAUTHENTICATED")
	}
	return s.Store.Session(r.Context(), cookie.Value)
}
func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Restart    bool `json:"restart"`
		ResumeOnly bool `json:"resumeOnly"`
	}
	if err := decode(w, r, &body); err != nil {
		s.failure(w, r, err)
		return
	}
	if cookie, err := r.Cookie(CookieName); err == nil && !body.Restart {
		pid, err := s.Store.Session(r.Context(), cookie.Value)
		if err != nil {
			s.failure(w, r, err)
			return
		}
		s.success(w, r, map[string]string{"participantId": pid})
		return
	}
	if body.ResumeOnly && !body.Restart {
		s.failure(w, r, domain.Err("UNAUTHENTICATED"))
		return
	}
	credential, pid, err := s.Store.CreateSession(r.Context())
	if err != nil {
		s.failure(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: credential, Path: "/", HttpOnly: true, Secure: s.config.SecureCookie, SameSite: http.SameSiteLaxMode, MaxAge: 7 * 24 * 3600})
	s.success(w, r, map[string]string{"participantId": pid})
}
func (s *Server) success(w http.ResponseWriter, r *http.Request, value any) {
	s.Metrics.Requests.WithLabelValues(route(r), "ok").Inc()
	s.log.Info("api request", "requestId", w.Header().Get("X-Request-Id"), "route", route(r), "outcome", "ok", "status", 200)
	writeJSON(w, 200, value)
}
func (s *Server) notify(change store.Change) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if err := s.publish(ctx, change); err != nil {
		s.Metrics.Dependencies.WithLabelValues("publish").Inc()
		s.log.Warn("committed change notification failed", "operation", "publish", "error", err)
	}
}
func (s *Server) join(w http.ResponseWriter, r *http.Request) {
	pid, err := s.identity(r)
	if err != nil {
		s.failure(w, r, err)
		return
	}
	var body struct {
		DisplayName string `json:"displayName"`
	}
	if err := decode(w, r, &body); err != nil {
		s.failure(w, r, err)
		return
	}
	change, changed, err := s.Store.Join(r.Context(), r.PathValue("quizId"), pid, body.DisplayName)
	if err != nil {
		s.failure(w, r, err)
		return
	}
	if changed {
		s.notify(change)
	}
	state, err := s.Store.Snapshot(r.Context(), change.QuizID, pid)
	if err != nil {
		s.failure(w, r, err)
		return
	}
	s.success(w, r, state)
}
func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	pid, err := s.identity(r)
	if err != nil {
		s.failure(w, r, err)
		return
	}
	state, err := s.Store.Snapshot(r.Context(), r.PathValue("quizId"), pid)
	if err != nil {
		s.failure(w, r, err)
		return
	}
	s.success(w, r, state)
}

func (s *Server) preview(w http.ResponseWriter, r *http.Request) {
	metadata, err := s.Store.Preview(r.Context(), r.PathValue("quizId"))
	if err != nil {
		s.failure(w, r, err)
		return
	}
	s.success(w, r, metadata)
}
func (s *Server) startQuestion(w http.ResponseWriter, r *http.Request) {
	pid, err := s.identity(r)
	if err != nil {
		s.failure(w, r, err)
		return
	}
	var a domain.StartQuestion
	if err := decode(w, r, &a); err != nil {
		s.failure(w, r, err)
		return
	}
	clock, err := s.Store.StartQuestion(r.Context(), r.PathValue("quizId"), pid, a)
	if err != nil {
		s.failure(w, r, err)
		return
	}
	s.success(w, r, clock)
}
func (s *Server) answer(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	defer func() { s.Metrics.AnswerLatency.Observe(time.Since(start).Seconds()) }()
	pid, err := s.identity(r)
	if err != nil {
		s.failure(w, r, err)
		return
	}
	var a domain.Answer
	if err := decode(w, r, &a); err != nil {
		s.failure(w, r, err)
		return
	}
	result, err := s.Store.Answer(r.Context(), r.PathValue("quizId"), pid, a)
	if err != nil {
		s.failure(w, r, err)
		return
	}
	s.Metrics.Answers.WithLabelValues(result.Outcome).Inc()
	if result.Outcome == "accepted" {
		s.notify(store.Change{QuizID: result.Receipt.QuizID, Epoch: result.Receipt.Epoch, Version: result.Receipt.AcceptedVersion})
	}
	s.success(w, r, result)
}
func (s *Server) live(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	pid, err := s.identity(r)
	if err != nil {
		s.failure(w, r, err)
		return
	}
	id := r.PathValue("quizId")
	state, err := s.Store.Snapshot(ctx, id, pid)
	if err != nil {
		s.failure(w, r, err)
		return
	}
	cookie, _ := r.Cookie(CookieName)
	ttl, err := s.Store.SessionTTL(ctx, cookie.Value)
	if err != nil {
		s.failure(w, r, err)
		return
	}
	upgrader := websocket.Upgrader{CheckOrigin: s.origin, HandshakeTimeout: 3 * time.Second, ReadBufferSize: 1024, WriteBufferSize: 4096, EnableCompression: false}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	s.Hub.Serve(conn, id, pid, state.Epoch, ttl)
}
