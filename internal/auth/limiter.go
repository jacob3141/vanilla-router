// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"sync"
	"time"
)

// Limiter throttles login attempts per IP: the first three failed attempts are free, after that
// the wait doubles (1 s, 2 s, 4 s …) up to at most 15 minutes.
type Limiter struct {
	mu  sync.Mutex
	m   map[string]*attempts
	now func() time.Time
}

type attempts struct {
	fails int
	until time.Time
	last  time.Time
}

const (
	freeAttempts = 3
	maxDelay     = 15 * time.Minute
	forgetAfter  = time.Hour
)

func NewLimiter() *Limiter { return &Limiter{m: map[string]*attempts{}, now: time.Now} }

// Wait returns how long the IP still has to wait (0 = may try).
func (l *Limiter) Wait(ip string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.m[ip]
	if !ok {
		return 0
	}
	now := l.now()
	if now.Sub(a.last) > forgetAfter {
		delete(l.m, ip)
		return 0
	}
	if d := a.until.Sub(now); d > 0 {
		return d
	}
	return 0
}

// Fail counts a failed attempt.
func (l *Limiter) Fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	a, ok := l.m[ip]
	if !ok || now.Sub(a.last) > forgetAfter {
		a = &attempts{}
		l.m[ip] = a
	}
	a.fails++
	a.last = now
	if a.fails > freeAttempts {
		d := time.Second << min(a.fails-freeAttempts-1, 20)
		a.until = now.Add(min(d, maxDelay))
	}
}

// Success resets the counter after a successful login.
func (l *Limiter) Success(ip string) {
	l.mu.Lock()
	delete(l.m, ip)
	l.mu.Unlock()
}
