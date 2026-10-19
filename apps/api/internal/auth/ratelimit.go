package auth

import (
	"sync"
	"time"
)

// failLimiter blocks a key (client IP) after too many failed logins in a window.
type failLimiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string][]time.Time
}

func newFailLimiter(max int, window time.Duration) *failLimiter {
	return &failLimiter{max: max, window: window, hits: map[string][]time.Time{}}
}

func (l *failLimiter) prune(key string, now time.Time) []time.Time {
	h := l.hits[key]
	i := 0
	for i < len(h) && now.Sub(h[i]) > l.window {
		i++
	}
	h = h[i:]
	if len(h) == 0 {
		delete(l.hits, key)
	} else {
		l.hits[key] = h
	}
	return h
}

func (l *failLimiter) Blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.prune(key, time.Now())) >= l.max
}

func (l *failLimiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.hits[key] = append(l.prune(key, now), now)
}

func (l *failLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.hits, key)
}
