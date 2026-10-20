package auth

import (
	"sort"
	"sync"
	"time"
)

// failLimiter counts failed logins per key (client IP or account) in a
// window; reaching max locks the key for lockFor. max 0 never locks.
type failLimiter struct {
	mu      sync.Mutex
	max     int
	window  time.Duration
	lockFor time.Duration
	hits    map[string][]time.Time
	locked  map[string]time.Time
	// onLock runs (in its own goroutine) when a key gets locked.
	onLock func(key string, d time.Duration)
}

func newFailLimiter(max int, window time.Duration) *failLimiter {
	return &failLimiter{max: max, window: window, lockFor: window, hits: map[string][]time.Time{}, locked: map[string]time.Time{}}
}

func (l *failLimiter) set(max int, lockFor time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.max, l.lockFor = max, lockFor
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
	until, ok := l.locked[key]
	if ok && time.Now().After(until) {
		delete(l.locked, key)
		return false
	}
	return ok
}

// Fail records a failure and reports whether it locked the key.
func (l *failLimiter) Fail(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	h := append(l.prune(key, now), now)
	l.hits[key] = h
	if l.max <= 0 || len(h) < l.max {
		return false
	}
	delete(l.hits, key)
	l.locked[key] = now.Add(l.lockFor)
	if l.onLock != nil {
		go l.onLock(key, l.lockFor)
	}
	return true
}

func (l *failLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.hits, key)
}

func (l *failLimiter) Unlock(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.locked[key]
	delete(l.locked, key)
	delete(l.hits, key)
	return ok
}

// Lock is one locked key.
type Lock struct {
	Key   string    `json:"key"`
	Until time.Time `json:"until"`
}

func (l *failLimiter) Locks() []Lock {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	out := []Lock{}
	for k, u := range l.locked {
		if now.After(u) {
			delete(l.locked, k)
			continue
		}
		out = append(out, Lock{Key: k, Until: u})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Until.After(out[j].Until) })
	return out
}
