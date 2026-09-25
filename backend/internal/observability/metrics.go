package observability

import "github.com/prometheus/client_golang/prometheus"

type Metrics struct {
	Registry         *prometheus.Registry
	Requests         *prometheus.CounterVec
	Answers          *prometheus.CounterVec
	Dependencies     *prometheus.CounterVec
	AnswerLatency    prometheus.Histogram
	SnapshotLatency  prometheus.Histogram
	BroadcastLatency prometheus.Histogram
	Sockets          prometheus.Gauge
	PubSubReconnects prometheus.Counter
	SlowClients      prometheus.Counter
	LearningRequests *prometheus.CounterVec
	LearningCache    *prometheus.CounterVec
	LearningDuration *prometheus.HistogramVec
	LearningBytes    *prometheus.HistogramVec
}

func New() *Metrics {
	m := &Metrics{
		Registry:         prometheus.NewRegistry(),
		Requests:         prometheus.NewCounterVec(prometheus.CounterOpts{Name: "quiz_requests_total", Help: "HTTP API requests by bounded outcome."}, []string{"route", "outcome"}),
		Answers:          prometheus.NewCounterVec(prometheus.CounterOpts{Name: "quiz_answers_total", Help: "Answer commands by bounded outcome."}, []string{"outcome"}),
		Dependencies:     prometheus.NewCounterVec(prometheus.CounterOpts{Name: "quiz_dependency_failures_total", Help: "Dependency failures by operation."}, []string{"operation"}),
		AnswerLatency:    prometheus.NewHistogram(prometheus.HistogramOpts{Name: "quiz_answer_duration_seconds", Help: "Answer command time, including validation and persistence.", Buckets: prometheus.DefBuckets}),
		SnapshotLatency:  prometheus.NewHistogram(prometheus.HistogramOpts{Name: "quiz_snapshot_duration_seconds", Help: "Consistent room snapshot generation time.", Buckets: prometheus.DefBuckets}),
		BroadcastLatency: prometheus.NewHistogram(prometheus.HistogramOpts{Name: "quiz_broadcast_duration_seconds", Help: "Time to enqueue a room snapshot; excludes socket delivery.", Buckets: prometheus.DefBuckets}),
		Sockets:          prometheus.NewGauge(prometheus.GaugeOpts{Name: "quiz_active_sockets", Help: "Current local WebSocket connections."}),
		PubSubReconnects: prometheus.NewCounter(prometheus.CounterOpts{Name: "quiz_pubsub_reconnects_total", Help: "PubSub subscriptions established after the initial subscription."}),
		SlowClients:      prometheus.NewCounter(prometheus.CounterOpts{Name: "quiz_slow_client_replacements_total", Help: "Superseded queued messages for slow consumers."}),
	}
	m.Registry.MustRegister(m.Requests, m.Answers, m.Dependencies, m.AnswerLatency, m.SnapshotLatency, m.BroadcastLatency, m.Sockets, m.PubSubReconnects, m.SlowClients)
	m.LearningRequests = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "learning_requests_total", Help: "Optional generation outcomes."}, []string{"action", "outcome"})
	m.LearningCache = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "learning_cache_hits_total", Help: "Authorized generation cache hits."}, []string{"action"})
	m.LearningDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "learning_duration_seconds", Help: "Optional generation duration including bounded queue."}, []string{"action"})
	m.LearningBytes = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "learning_output_bytes", Help: "Bounded generation output bytes.", Buckets: []float64{256, 1024, 4096, 32768, 131072, 1048576}}, []string{"action"})
	m.Registry.MustRegister(m.LearningRequests, m.LearningCache, m.LearningDuration, m.LearningBytes)
	return m
}
