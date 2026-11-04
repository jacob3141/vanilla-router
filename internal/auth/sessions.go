// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"sync"
	"time"
)

// Session ist eine angemeldete Browser-Sitzung. Sitzungen liegen nur im Speicher,
// ein Neustart des Dienstes meldet also alle ab.
type Session struct {
	Created  time.Time
	LastSeen time.Time
	IP       string
}

type Sessions struct {
	mu   sync.Mutex
	m    map[string]*Session // Schlüssel: SHA-256 des Tokens, das Token selbst wird nicht gespeichert
	Idle time.Duration
	Max  time.Duration
	now  func() time.Time
}

func NewSessions(idle, max time.Duration) *Sessions {
	return &Sessions{m: map[string]*Session{}, Idle: idle, Max: max, now: time.Now}
}

func key(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// Create legt eine Sitzung an und liefert das Token für das Cookie (256 Bit).
func (s *Sessions) Create(ip string) string {
	raw := make([]byte, 32)
	rand.Read(raw)
	token := base64.RawURLEncoding.EncodeToString(raw)
	now := s.now()
	s.mu.Lock()
	s.m[key(token)] = &Session{Created: now, LastSeen: now, IP: ip}
	s.mu.Unlock()
	return token
}

// Touch prüft das Token, verlängert die Sitzung und meldet, ob sie gültig ist.
func (s *Sessions) Touch(token string) bool {
	if token == "" {
		return false
	}
	k := key(token)
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.m[k]
	if !ok {
		return false
	}
	if now.Sub(sess.LastSeen) > s.Idle || now.Sub(sess.Created) > s.Max {
		delete(s.m, k)
		return false
	}
	sess.LastSeen = now
	return true
}

// Valid prüft das Token, ohne die Sitzung zu verlängern. Für Hintergrundabfragen wie den
// Live-Stream, damit eine offene Seite allein nicht vor der Abmeldung wegen Untätigkeit schützt.
func (s *Sessions) Valid(token string) bool {
	if token == "" {
		return false
	}
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.m[key(token)]
	return ok && now.Sub(sess.LastSeen) <= s.Idle && now.Sub(sess.Created) <= s.Max
}

func (s *Sessions) Delete(token string) {
	s.mu.Lock()
	delete(s.m, key(token))
	s.mu.Unlock()
}

// DeleteAll meldet alle Sitzungen ab, z. B. nach einer Passwortänderung.
func (s *Sessions) DeleteAll() {
	s.mu.Lock()
	s.m = map[string]*Session{}
	s.mu.Unlock()
}

func (s *Sessions) Count() int {
	s.Cleanup()
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.m)
}

// SessionInfo beschreibt eine Sitzung für die Oberfläche. Die ID ist ein Teil des Hashes,
// nie das Token selbst.
type SessionInfo struct {
	ID       string    `json:"id"`
	IP       string    `json:"ip"`
	Created  time.Time `json:"created"`
	LastSeen time.Time `json:"last_seen"`
	Current  bool      `json:"current"`
}

func shortID(k string) string { return k[:16] }

// List liefert alle gültigen Sitzungen; current markiert die eigene.
func (s *Sessions) List(currentToken string) []SessionInfo {
	s.Cleanup()
	cur := ""
	if currentToken != "" {
		cur = key(currentToken)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SessionInfo, 0, len(s.m))
	for k, sess := range s.m {
		out = append(out, SessionInfo{ID: shortID(k), IP: sess.IP, Created: sess.Created, LastSeen: sess.LastSeen, Current: k == cur})
	}
	return out
}

// DeleteID meldet die Sitzung mit dieser ID ab.
func (s *Sessions) DeleteID(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k := range s.m {
		if shortID(k) == id {
			delete(s.m, k)
			return true
		}
	}
	return false
}

// Cleanup entfernt abgelaufene Sitzungen.
func (s *Sessions) Cleanup() {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, sess := range s.m {
		if now.Sub(sess.LastSeen) > s.Idle || now.Sub(sess.Created) > s.Max {
			delete(s.m, k)
		}
	}
}
