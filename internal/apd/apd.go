// SPDX-License-Identifier: AGPL-3.0-or-later

// Package apd is the router's side of the connection to its access points (docs/sdn-concept.md 4.2, 4.5). It speaks
// HTTP over TLS 1.3 on the home network only. Both sides present self-signed certificates that merely carry their
// Ed25519 keys: an AP is known by its key in config.json, the router by the key the AP pinned while pairing.
//
// The process holds no state worth keeping and no privileges. Everything else comes from the helper through the
// fixed actions ap.*, passwords only pass through its memory on their way to an AP.
package apd

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"sync"
	"time"

	"vanilla-router/internal/gen"
	"vanilla-router/internal/pairing"
)

// Port is the TCP port on the home network.
const Port = 7443

const (
	maxBody     = 1 << 18
	maxSessions = 8
	sessionAge  = 30 * time.Minute
	// A rollout goes to one AP at a time. An AP that stays silent for this long no longer blocks the others.
	rolloutSlot = 3 * time.Minute
)

// Hello tells a computer on the cable that a vanilla-router answers here (concept 4.1).
type Hello struct {
	Role    string `json:"role"` // always "vanilla-router"
	Version string `json:"version"`
}

// PairCommit is the AP's first message. Its key is the one of the TLS connection.
type PairCommit struct {
	Commitment string          `json:"commitment"`
	MAC        string          `json:"mac"`
	Hostname   string          `json:"hostname"`
	Model      string          `json:"model"`
	Radios     []gen.RadioCaps `json:"radios"`
}

// PairCommitReply carries the router's random value.
type PairCommitReply struct {
	RouterNonce string `json:"router_nonce"`
}

// PairReveal is the AP's random value.
type PairReveal struct {
	APNonce string `json:"ap_nonce"`
}

// Pairing states.
const (
	PairWaiting = "waiting"
	PairPaired  = "paired"
	PairUnknown = "unknown" // rejected or expired
)

// PairStatus answers the AP while the owner compares the code.
type PairStatus struct {
	State string `json:"state"`
}

// Bundle is everything an AP needs.
type Bundle struct {
	Generation string     `json:"generation"`
	Files      []gen.File `json:"files"`
}

// Report is what an AP sends after applying and regularly afterwards.
type Report struct {
	Applied string          `json:"applied"`
	Version string          `json:"version"`
	Clients int             `json:"clients"`
	Error   string          `json:"error,omitempty"`
	Radios  []gen.RadioCaps `json:"radios,omitempty"`
}

// Revoked is the body of the 403 answer: this AP is no longer part of the network.
type Revoked struct {
	Revoked bool `json:"revoked"`
}

// Backend is the helper (helper.Client).
type Backend interface {
	Call(ctx context.Context, action string, params, out any) error
}

// Helper parameters and results (internal/helper/wireless_ap.go).
type (
	// AddPairing is the parameter of ap.pairing.add.
	AddPairing struct {
		PublicKey   string          `json:"public_key"`
		MAC         string          `json:"mac"`
		Hostname    string          `json:"hostname"`
		Model       string          `json:"model"`
		Radios      []gen.RadioCaps `json:"radios"`
		RouterNonce string          `json:"router_nonce"`
		APNonce     string          `json:"ap_nonce"`
		Commitment  string          `json:"commitment"`
	}
	// KeyParam names an AP by its key.
	KeyParam struct {
		PublicKey string `json:"public_key"`
	}
	// Authorized is the result of ap.authorize.
	Authorized struct {
		ID string `json:"id"`
	}
	// ReportParam is the parameter of ap.report.
	ReportParam struct {
		ID     string `json:"id"`
		Report Report `json:"report"`
	}
)

// Server serves the access points.
type Server struct {
	Backend Backend
	Key     ed25519.PrivateKey
	Version string
	Log     *slog.Logger
	Poll    time.Duration // how often a waiting AP's bundle is compared
	Wait    time.Duration // how long a long poll lasts

	mu       sync.Mutex
	sessions map[string]*session
	offered  map[string]offer
}

type session struct {
	commit PairCommit
	nonce  string
	made   time.Time
}

type offer struct {
	generation string
	at         time.Time
}

// Certificate wraps a key into a self-signed certificate. Nothing checks names or dates, only the key counts.
func Certificate(key ed25519.PrivateKey) (tls.Certificate, error) {
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "vanilla-router"},
		NotBefore: time.Unix(0, 0), NotAfter: time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}

// PeerKey reads the Ed25519 key from the other side's certificate.
func PeerKey(cs tls.ConnectionState) (ed25519.PublicKey, error) {
	if len(cs.PeerCertificates) == 0 {
		return nil, errors.New("no certificate")
	}
	k, ok := cs.PeerCertificates[0].PublicKey.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("not an ed25519 key")
	}
	return k, nil
}

// TLSConfig is the server side: every client must show a key, which one is decided per request.
func (s *Server) TLSConfig() (*tls.Config, error) {
	cert, err := Certificate(s.Key)
	if err != nil {
		return nil, err
	}
	return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAnyClientCert}, nil
}

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	if s.Poll == 0 {
		s.Poll = 3 * time.Second
	}
	if s.Wait == 0 {
		s.Wait = 50 * time.Second
	}
	if s.Log == nil {
		s.Log = slog.New(slog.DiscardHandler)
	}
	s.sessions, s.offered = map[string]*session{}, map[string]offer{}
	m := http.NewServeMux()
	m.HandleFunc("GET /v1/hello", func(w http.ResponseWriter, r *http.Request) {
		reply(w, Hello{Role: "vanilla-router", Version: s.Version})
	})
	m.HandleFunc("POST /v1/pair/commit", s.withKey(s.pairCommit))
	m.HandleFunc("POST /v1/pair/reveal", s.withKey(s.pairReveal))
	m.HandleFunc("GET /v1/pair/status", s.withKey(s.pairStatus))
	m.HandleFunc("GET /v1/bundle", s.withAP(s.bundle))
	m.HandleFunc("POST /v1/report", s.withAP(s.report))
	return m
}

// Serve listens on addr until ctx ends.
func (s *Server) Serve(ctx context.Context, addr string) error {
	cfg, err := s.TLSConfig()
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: s.Handler(), TLSConfig: cfg, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	go func() { <-ctx.Done(); srv.Close() }()
	err = srv.ServeTLS(ln, "", "")
	if ctx.Err() != nil {
		return nil
	}
	return err
}

func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, msg string) { http.Error(w, msg, code) }

func read(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		fail(w, http.StatusBadRequest, "unreadable request")
		return false
	}
	return true
}

func (s *Server) withKey(h func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil {
			fail(w, http.StatusForbidden, "TLS required")
			return
		}
		k, err := PeerKey(*r.TLS)
		if err != nil {
			fail(w, http.StatusForbidden, "no valid key")
			return
		}
		h(w, r, pairing.FormatKey(k))
	}
}

// withAP lets only paired APs through. Any other key is told that it is not part of the network.
func (s *Server) withAP(h func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return s.withKey(func(w http.ResponseWriter, r *http.Request, key string) {
		var a Authorized
		if err := s.Backend.Call(r.Context(), "ap.authorize", KeyParam{key}, &a); err != nil {
			s.Log.Warn("authorize", "err", err)
			fail(w, http.StatusServiceUnavailable, "try again later")
			return
		}
		if a.ID == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(Revoked{Revoked: true})
			return
		}
		h(w, r, a.ID)
	})
}

func (s *Server) pairCommit(w http.ResponseWriter, r *http.Request, key string) {
	var p PairCommit
	if !read(w, r, &p) {
		return
	}
	if b, err := hex.DecodeString(p.Commitment); err != nil || len(b) != 32 {
		fail(w, http.StatusBadRequest, "invalid commitment")
		return
	}
	n, err := pairing.Nonce()
	if err != nil {
		fail(w, http.StatusInternalServerError, "no random values")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, se := range s.sessions {
		if time.Since(se.made) > sessionAge {
			delete(s.sessions, k)
		}
	}
	if _, again := s.sessions[key]; !again && len(s.sessions) >= maxSessions {
		fail(w, http.StatusTooManyRequests, "too many waiting access points")
		return
	}
	se := &session{commit: p, nonce: hex.EncodeToString(n), made: time.Now()}
	s.sessions[key] = se
	reply(w, PairCommitReply{RouterNonce: se.nonce})
}

func (s *Server) pairReveal(w http.ResponseWriter, r *http.Request, key string) {
	var p PairReveal
	if !read(w, r, &p) {
		return
	}
	s.mu.Lock()
	se := s.sessions[key]
	delete(s.sessions, key) // one try per commitment
	s.mu.Unlock()
	if se == nil {
		fail(w, http.StatusConflict, "no pairing in progress")
		return
	}
	req := AddPairing{PublicKey: key, MAC: se.commit.MAC, Hostname: se.commit.Hostname, Model: se.commit.Model, Radios: se.commit.Radios,
		RouterNonce: se.nonce, APNonce: p.APNonce, Commitment: se.commit.Commitment}
	if err := s.Backend.Call(r.Context(), "ap.pairing.add", req, nil); err != nil {
		s.Log.Warn("pairing refused", "err", err)
		fail(w, http.StatusBadRequest, "pairing refused")
		return
	}
	reply(w, PairStatus{State: PairWaiting})
}

func (s *Server) pairStatus(w http.ResponseWriter, r *http.Request, key string) {
	var st PairStatus
	if err := s.Backend.Call(r.Context(), "ap.pairing.status", KeyParam{key}, &st); err != nil {
		fail(w, http.StatusServiceUnavailable, "try again later")
		return
	}
	reply(w, st)
}

// mayOffer implements "one AP at a time": a new generation goes out only if no other AP is still busy with one.
func (s *Server) mayOffer(id, generation string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for other, o := range s.offered {
		if other != id && time.Since(o.at) < rolloutSlot {
			return false
		}
	}
	if o, ok := s.offered[id]; !ok || o.generation != generation {
		s.offered[id] = offer{generation, time.Now()}
	}
	return true
}

func (s *Server) bundle(w http.ResponseWriter, r *http.Request, id string) {
	have := r.URL.Query().Get("have")
	ctx, cancel := context.WithTimeout(r.Context(), s.Wait)
	defer cancel()
	for {
		var b Bundle
		if err := s.Backend.Call(r.Context(), "ap.bundle", map[string]string{"id": id}, &b); err != nil {
			s.Log.Warn("bundle", "ap", id, "err", err)
			fail(w, http.StatusServiceUnavailable, "try again later")
			return
		}
		if b.Generation != "" && b.Generation != have && s.mayOffer(id, b.Generation) {
			reply(w, b)
			return
		}
		select {
		case <-ctx.Done():
			w.WriteHeader(http.StatusNoContent)
			return
		case <-time.After(s.Poll):
		}
	}
}

func (s *Server) report(w http.ResponseWriter, r *http.Request, id string) {
	var p Report
	if !read(w, r, &p) {
		return
	}
	// The AP finished with what it was offered, the next one may go
	s.mu.Lock()
	if o, ok := s.offered[id]; ok && (o.generation == p.Applied || p.Error != "") {
		delete(s.offered, id)
	}
	s.mu.Unlock()
	if err := s.Backend.Call(r.Context(), "ap.report", ReportParam{ID: id, Report: p}, nil); err != nil {
		s.Log.Warn("report", "ap", id, "err", err)
		fail(w, http.StatusServiceUnavailable, "try again later")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
