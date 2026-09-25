package realtime

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"vocabulary.live/internal/domain"
	"vocabulary.live/internal/observability"
	"vocabulary.live/internal/store"
)

type client struct {
	conn       *websocket.Conn
	pid, epoch string
	queue      chan *broadcast
	done       chan struct{}
	once       sync.Once
}

func (c *client) close() { c.once.Do(func() { close(c.done); _ = c.conn.Close() }) }

type room struct {
	clients    map[*client]bool
	dirty      bool
	refreshing bool
	latest     *broadcast
}

type broadcast struct {
	event   domain.Event
	message *websocket.PreparedMessage
}

func prepare(event domain.Event) (*broadcast, error) {
	data, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	message, err := websocket.NewPreparedMessage(websocket.TextMessage, data)
	return &broadcast{event: event, message: message}, err
}

type Hub struct {
	store               *store.Store
	metrics             *observability.Metrics
	log                 *slog.Logger
	mu                  sync.Mutex
	rooms               map[string]*room
	cancel              context.CancelFunc
	ctx                 context.Context
	wg                  sync.WaitGroup
	sockets             sync.WaitGroup
	closed              bool
	coalesce, reconcile time.Duration
	jobs                chan string
}

func New(s *store.Store, m *observability.Metrics, logger *slog.Logger, coalesce, reconcile time.Duration) *Hub {
	ctx, cancel := context.WithCancel(context.Background())
	h := &Hub{store: s, metrics: m, log: logger, rooms: map[string]*room{}, ctx: ctx, cancel: cancel, coalesce: coalesce, reconcile: reconcile, jobs: make(chan string, 32)}
	for id := range s.Quizzes {
		h.rooms[id] = &room{clients: map[*client]bool{}}
	}
	h.wg.Add(6)
	go h.run()
	go h.subscribe()
	for range 4 {
		go h.worker()
	}
	return h
}
func (h *Hub) Dirty(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r := h.rooms[id]; r != nil {
		r.dirty = true
	}
}
func (h *Hub) dirtyAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.rooms {
		r.dirty = true
	}
}
func (h *Hub) Close() {
	h.mu.Lock()
	h.closed = true
	for _, r := range h.rooms {
		for c := range r.clients {
			c.close()
		}
	}
	h.mu.Unlock()
	h.cancel()
	h.wg.Wait()
	h.sockets.Wait()
}

// Registration precedes the first authoritative read. A queued full snapshot
// replaces its predecessor so slow sockets cannot block the room.
func (h *Hub) Serve(conn *websocket.Conn, id, pid, epoch string, ttl time.Duration) bool {
	c := &client{conn: conn, pid: pid, epoch: epoch, queue: make(chan *broadcast, 1), done: make(chan struct{})}
	h.mu.Lock()
	r := h.rooms[id]
	// HTTP already resolved membership against the immutable repository.
	// Published quizzes may appear after this hub starts; add their bounded
	// local room lazily, then use the same snapshot/PubSub reconciliation.
	if h.closed || (r == nil && len(h.rooms) >= len(h.store.Quizzes)+20) {
		h.mu.Unlock()
		c.close()
		return false
	}
	if r == nil {
		r = &room{clients: map[*client]bool{}}
		h.rooms[id] = r
	}
	count := 0
	for other := range r.clients {
		if other.pid == pid {
			count++
		}
	}
	if h.closed || len(r.clients) >= 800 || count >= 4 {
		h.mu.Unlock()
		c.close()
		return false
	}
	r.clients[c] = true
	r.dirty = true
	h.sockets.Add(1)
	h.mu.Unlock()
	h.metrics.Sockets.Inc()
	defer func() {
		c.close()
		h.mu.Lock()
		delete(r.clients, c)
		h.mu.Unlock()
		h.metrics.Sockets.Dec()
		h.sockets.Done()
	}()
	readerDone := make(chan struct{})
	conn.SetReadLimit(1024)
	_ = conn.SetReadDeadline(time.Now().Add(45 * time.Second))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(45 * time.Second)) })
	go func() {
		defer close(readerDone)
		defer c.close()
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
			return
		}
	}()
	defer func() { c.close(); <-readerDone }()
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	expiry := time.NewTimer(ttl)
	defer expiry.Stop()
	for {
		select {
		case <-h.ctx.Done():
			return true
		case <-c.done:
			return true
		case <-expiry.C:
			_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
			_ = conn.WriteJSON(domain.Event{Type: "status", Code: "SESSION_EXPIRED"})
			return true
		case <-ping.C:
			_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
			if conn.WriteMessage(websocket.PingMessage, nil) != nil {
				return true
			}
		case event := <-c.queue:
			_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
			if conn.WritePreparedMessage(event.message) != nil {
				return true
			}
			if event.event.Code == "RESET_REQUIRED" {
				return true
			}
		}
	}
}
func (h *Hub) run() {
	defer h.wg.Done()
	tick := time.NewTicker(h.coalesce)
	defer tick.Stop()
	reconcile := time.NewTicker(h.reconcile)
	defer reconcile.Stop()
	for {
		select {
		case <-h.ctx.Done():
			return
		case <-reconcile.C:
			h.dirtyAll()
		case <-tick.C:
			h.mu.Lock()
			for id, r := range h.rooms {
				if r.dirty && !r.refreshing && len(r.clients) > 0 {
					select {
					case h.jobs <- id:
						r.dirty = false
						r.refreshing = true
					default:
					}
				}
			}
			h.mu.Unlock()
		}
	}
}

func (h *Hub) worker() {
	defer h.wg.Done()
	for {
		select {
		case <-h.ctx.Done():
			return
		case id := <-h.jobs:
			h.refresh(id)
			h.mu.Lock()
			h.rooms[id].refreshing = false
			h.mu.Unlock()
		}
	}
}

func (h *Hub) refresh(id string) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(h.ctx, 800*time.Millisecond)
	state, err := h.store.Snapshot(ctx, id, "")
	cancel()
	h.metrics.SnapshotLatency.Observe(time.Since(start).Seconds())
	event := domain.Event{Type: "snapshot", Snapshot: &state.Snapshot}
	if err != nil {
		h.metrics.Dependencies.WithLabelValues("snapshot").Inc()
		h.log.Warn("room synchronization unavailable", "operation", "snapshot", "error", err)
		event = domain.Event{Type: "status", Code: "DEPENDENCY_UNAVAILABLE"}
	}
	start = time.Now()
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.rooms[id]
	message := r.latest
	if err != nil || message == nil || message.event.Snapshot.Epoch != state.Epoch || message.event.Snapshot.Version != state.Version {
		var encodeErr error
		message, encodeErr = prepare(event)
		if encodeErr != nil {
			h.log.Error("room event encoding failed", "error", encodeErr)
			return
		}
		if err == nil {
			r.latest = message
		}
	}
	var reset *broadcast
	for c := range r.clients {
		next := message
		if err == nil && c.epoch != state.Epoch {
			if reset == nil {
				reset, _ = prepare(domain.Event{Type: "status", Code: "RESET_REQUIRED"})
			}
			next = reset
		}
		select {
		case c.queue <- next:
		default:
			select {
			case <-c.queue:
				h.metrics.SlowClients.Inc()
			default:
			}
			select {
			case c.queue <- next:
			default:
			}
		}
	}
	h.metrics.BroadcastLatency.Observe(time.Since(start).Seconds())
}
func (h *Hub) subscribe() {
	defer h.wg.Done()
	ps := h.store.Client.Subscribe(h.ctx, h.store.Channel())
	defer ps.Close()
	go func() { <-h.ctx.Done(); _ = ps.Close() }()
	everSubscribed := false
	backoff := 100 * time.Millisecond
	for {
		msg, err := ps.Receive(h.ctx)
		if err != nil {
			if h.ctx.Err() != nil {
				return
			}
			h.metrics.Dependencies.WithLabelValues("pubsub").Inc()
			timer := time.NewTimer(backoff)
			select {
			case <-timer.C:
			case <-h.ctx.Done():
				timer.Stop()
				return
			}
			if backoff < 2*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = 100 * time.Millisecond
		switch v := msg.(type) {
		case *redis.Subscription:
			if everSubscribed {
				h.metrics.PubSubReconnects.Inc()
			}
			everSubscribed = true
			h.dirtyAll()
		case *redis.Message:
			var change store.Change
			if json.Unmarshal([]byte(v.Payload), &change) == nil {
				h.Dirty(change.QuizID)
			}
		}
	}
}
