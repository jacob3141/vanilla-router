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

// Session is a signed-in browser session. Sessions only live in memory,
// so a restart of the service signs everyone out.
type Session struct {
	Created  time.Time
	LastSeen time.Time
	IP       string
}

type Sessions struct {
	mu   sync.Mutex
	m    map[string]*Session // key: SHA-256 of the token, the token itself is not stored
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

// Create creates a session and returns the token for the cookie (256 bits).
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

// Touch checks the token, extends the session and reports whether it is valid.
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

// Valid checks the token without extending the session. For background requests like the
// live stream, so that an open page alone does not protect against being signed out for inactivity.
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

// DeleteAll signs out all sessions, e.g. after a password change.
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

// SessionInfo describes a session for the web interface. The ID is part of the hash,
// never the token itself.
type SessionInfo struct {
	ID       string    `json:"id"`
	IP       string    `json:"ip"`
	Created  time.Time `json:"created"`
	LastSeen time.Time `json:"last_seen"`
	Current  bool      `json:"current"`
}

func shortID(k string) string { return k[:16] }

// List returns all valid sessions; current marks your own.
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

// DeleteID signs out the session with this ID.
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

// Cleanup removes expired sessions.
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
