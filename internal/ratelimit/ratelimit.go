// Package ratelimit provides a simple per-key fixed-window counter.
package ratelimit

import (
	"sync"
	"time"
)

// Limiter is a fixed-window rate limiter (lenient, in-memory, single node).
type Limiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	buckets map[string]*bucket
}

type bucket struct {
	count int
	start time.Time
}

// New returns a limiter allowing `limit` events per window per key.
func New(limit int, window time.Duration) *Limiter {
	if limit <= 0 {
		limit = 1
	}
	if window <= 0 {
		window = time.Minute
	}
	return &Limiter{
		limit:   limit,
		window:  window,
		buckets: make(map[string]*bucket),
	}
}

// Allow reports whether key may proceed. On deny, retryAfter is positive.
func (l *Limiter) Allow(key string, now time.Time) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	b, okb := l.buckets[key]
	if !okb || now.Sub(b.start) >= l.window {
		l.buckets[key] = &bucket{count: 1, start: now}
		return true, 0
	}
	if b.count >= l.limit {
		left := l.window - now.Sub(b.start)
		if left < time.Second {
			left = time.Second
		}
		return false, left
	}
	b.count++
	return true, 0
}

// GC drops stale buckets (call occasionally).
func (l *Limiter) GC(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, b := range l.buckets {
		if now.Sub(b.start) >= l.window*2 {
			delete(l.buckets, k)
		}
	}
}
