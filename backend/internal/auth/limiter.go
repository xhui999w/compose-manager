package auth

import (
	"sync"
	"time"
)

type attempt struct {
	count int
	reset time.Time
}

type Limiter struct {
	mu       sync.Mutex
	attempts map[string]attempt
	limit    int
	window   time.Duration
}

const limiterCleanupThreshold = 4096

func NewLimiter(limit int, window time.Duration) *Limiter {
	return &Limiter{attempts: make(map[string]attempt), limit: limit, window: window}
}

func (l *Limiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	item, exists := l.attempts[key]
	if !exists || now.After(item.reset) {
		delete(l.attempts, key)
		return true, 0
	}
	if item.count < l.limit {
		return true, 0
	}
	return false, time.Until(item.reset)
}

func (l *Limiter) Failure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if len(l.attempts) >= limiterCleanupThreshold {
		for candidate, item := range l.attempts {
			if now.After(item.reset) {
				delete(l.attempts, candidate)
			}
		}
	}
	item, exists := l.attempts[key]
	if !exists || now.After(item.reset) {
		l.attempts[key] = attempt{count: 1, reset: now.Add(l.window)}
		return
	}
	item.count++
	l.attempts[key] = item
}

func (l *Limiter) Success(key string) {
	l.mu.Lock()
	delete(l.attempts, key)
	l.mu.Unlock()
}
