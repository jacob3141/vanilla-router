// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"sync"
	"time"
)

// Limiter bremst Login-Versuche pro IP: Die ersten drei Fehlversuche sind frei, danach
// verdoppelt sich die Wartezeit (1 s, 2 s, 4 s …) bis höchstens 15 Minuten.
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

// Wait liefert, wie lange die IP noch warten muss (0 = darf versuchen).
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

// Fail zählt einen Fehlversuch.
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

// Success setzt den Zähler nach einem erfolgreichen Login zurück.
func (l *Limiter) Success(ip string) {
	l.mu.Lock()
	delete(l.m, ip)
	l.mu.Unlock()
}
