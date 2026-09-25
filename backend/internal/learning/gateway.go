package learning

import (
	"container/list"
	"context"
	"sync"
	"time"

	"vocabulary.live/internal/domain"
)

type cacheEntry struct {
	key     string
	data    []byte
	expires time.Time
}
type cache struct {
	items                       map[string]*list.Element
	lru                         *list.List
	bytes, maxBytes, maxEntries int
}

func newCache(n, bytes int) *cache {
	return &cache{items: map[string]*list.Element{}, lru: list.New(), maxBytes: bytes, maxEntries: n}
}
func (c *cache) remove(e *list.Element) {
	v := e.Value.(cacheEntry)
	delete(c.items, v.key)
	c.bytes -= len(v.data)
	c.lru.Remove(e)
}
func (c *cache) get(key string) ([]byte, bool) {
	e, ok := c.items[key]
	if !ok {
		return nil, false
	}
	v := e.Value.(cacheEntry)
	if time.Now().After(v.expires) {
		c.remove(e)
		return nil, false
	}
	c.lru.MoveToFront(e)
	return append([]byte(nil), v.data...), true
}
func (c *cache) put(key string, data []byte) {
	if len(data) > c.maxBytes {
		return
	}
	if e, ok := c.items[key]; ok {
		c.remove(e)
	}
	for c.lru.Len() >= c.maxEntries || c.bytes+len(data) > c.maxBytes {
		c.remove(c.lru.Back())
	}
	c.items[key] = c.lru.PushFront(cacheEntry{key, append([]byte(nil), data...), time.Now().Add(24 * time.Hour)})
	c.bytes += len(data)
}

type flight struct {
	done    chan struct{}
	cancel  context.CancelFunc
	waiters int
	data    []byte
	err     error
}
type window struct {
	start time.Time
	count int
}
type Observation struct {
	Action, Outcome string
	CacheHit        bool
	Duration        time.Duration
	Bytes, Active   int
}
type Gateway struct {
	mu              sync.Mutex
	root            context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	audio, text     *cache
	flights         map[string]*flight
	windows         map[string]window
	hour            window
	slots           chan struct{}
	waiting, active int
	observe         func(Observation)
}

func NewGateway(observe func(Observation)) *Gateway {
	ctx, cancel := context.WithCancel(context.Background())
	if observe == nil {
		observe = func(Observation) {}
	}
	return &Gateway{root: ctx, cancel: cancel, audio: newCache(32, 16<<20), text: newCache(128, 4<<20), flights: map[string]*flight{}, windows: map[string]window{}, slots: make(chan struct{}, 2), observe: observe}
}
func (g *Gateway) Close() {
	g.mu.Lock()
	g.cancel()
	for _, f := range g.flights {
		f.cancel()
	}
	g.mu.Unlock()
	g.wg.Wait()
	g.mu.Lock()
	g.audio = newCache(32, 16<<20)
	g.text = newCache(128, 4<<20)
	g.windows = map[string]window{}
	g.mu.Unlock()
}
func (g *Gateway) Active() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.active
}
func (g *Gateway) allow(owner, action string) bool {
	now := time.Now()
	kind, cap := "text", 6
	if action == "pronunciation" {
		kind, cap = "speech", 12
	}
	if action == "authoring" || action == "regenerate" {
		kind, cap = "authoring", 3
	}
	for k, w := range g.windows {
		if now.Sub(w.start) >= time.Minute {
			delete(g.windows, k)
		}
	}
	key := Hash([]string{owner, kind})
	w := g.windows[key]
	if w.start.IsZero() {
		if len(g.windows) >= 2048 {
			return false
		}
		w.start = now
	}
	if now.Sub(g.hour.start) >= time.Hour {
		g.hour = window{start: now}
	}
	if w.count >= cap || g.hour.count >= 100 {
		return false
	}
	w.count++
	g.windows[key] = w
	g.hour.count++
	return true
}

// Call coalesces identical work. A request's cancellation removes only that
// waiter; the upstream context ends when its final waiter leaves. No retries.
func (g *Gateway) Call(ctx context.Context, owner, action, key string, deadline time.Duration, fn func(context.Context) ([]byte, error)) ([]byte, error) {
	g.mu.Lock()
	if g.root.Err() != nil {
		g.mu.Unlock()
		return nil, domain.Err("AI_UNAVAILABLE")
	}
	selected := g.text
	if action == "pronunciation" {
		selected = g.audio
	}
	if b, ok := selected.get(key); ok {
		g.mu.Unlock()
		g.observe(Observation{Action: action, Outcome: "ok", CacheHit: true, Bytes: len(b)})
		return b, nil
	}
	if g.waiting >= 32 {
		g.mu.Unlock()
		return nil, domain.Err("AI_BUSY")
	}
	f, exists := g.flights[key]
	if exists && f.waiters >= 16 {
		g.mu.Unlock()
		return nil, domain.Err("AI_BUSY")
	}
	if !exists {
		if len(g.flights) >= 8 {
			g.mu.Unlock()
			return nil, domain.Err("AI_BUSY")
		}
		if !g.allow(owner, action) {
			g.mu.Unlock()
			g.observe(Observation{Action: action, Outcome: "RATE_LIMITED"})
			return nil, domain.Err("AI_RATE_LIMITED")
		}
		upstream, cancel := context.WithTimeout(g.root, deadline)
		f = &flight{done: make(chan struct{}), cancel: cancel}
		g.flights[key] = f
		g.wg.Add(1)
		go func() {
			defer g.wg.Done()
			defer cancel()
			start := time.Now()
			var b []byte
			var err error
			select {
			case g.slots <- struct{}{}:
				g.mu.Lock()
				g.active++
				g.mu.Unlock()
				b, err = fn(upstream)
				g.mu.Lock()
				g.active--
				g.mu.Unlock()
				<-g.slots
			case <-upstream.Done():
				err = domain.Err("AI_TIMEOUT")
			}
			if upstream.Err() != nil {
				err = domain.Err("AI_TIMEOUT")
			}
			g.mu.Lock()
			f.data, f.err = b, err
			if err == nil && f.waiters > 0 {
				selected.put(key, b)
			}
			if g.flights[key] == f {
				delete(g.flights, key)
			}
			active := g.active
			close(f.done)
			g.mu.Unlock()
			outcome := "ok"
			if err != nil {
				outcome = domain.Code(err)
			}
			g.observe(Observation{Action: action, Outcome: outcome, Duration: time.Since(start), Bytes: len(b), Active: active})
		}()
	}
	f.waiters++
	g.waiting++
	g.mu.Unlock()
	defer func() {
		g.mu.Lock()
		g.waiting--
		f.waiters--
		if f.waiters == 0 {
			f.cancel()
		}
		g.mu.Unlock()
	}()
	select {
	case <-ctx.Done():
		return nil, domain.Err("AI_TIMEOUT")
	case <-f.done:
		return append([]byte(nil), f.data...), f.err
	}
}
