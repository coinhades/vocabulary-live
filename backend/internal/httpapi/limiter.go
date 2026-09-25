package httpapi

import (
	"sync"
	"time"
)

type bucket struct {
	tokens float64
	at     time.Time
}

type limiter struct {
	mu          sync.Mutex
	buckets     map[string]bucket
	rate, burst float64
	nextSweep   time.Time
}

func newLimiter(rate, burst float64) *limiter {
	return &limiter{buckets: map[string]bucket{}, rate: rate, burst: burst}
}
func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	b, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) >= 4096 {
			if !now.Before(l.nextSweep) {
				for k, v := range l.buckets {
					if now.Sub(v.at) > 2*time.Minute {
						delete(l.buckets, k)
					}
				}
				l.nextSweep = now.Add(time.Minute)
			}
			if len(l.buckets) >= 4096 {
				return false
			}
		}
		b = bucket{tokens: l.burst, at: now}
	}
	b.tokens = min(l.burst, b.tokens+now.Sub(b.at).Seconds()*l.rate)
	b.at = now
	allowed := b.tokens >= 1
	if allowed {
		b.tokens--
	}
	l.buckets[key] = b
	return allowed
}
