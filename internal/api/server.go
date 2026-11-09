// SPDX-License-Identifier: AGPL-3.0-or-later

// Package api contains the HTTP part of the web interface: middleware, login and the endpoints.
package api

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"vanilla-router/internal/i18n"

	"vanilla-router/internal/auth"
	"vanilla-router/internal/helper"
	"vanilla-router/internal/sys"
)

const cookieName = "vr_session"

type Config struct {
	Dev      bool
	Origins  []string // allowed Origin values for changing requests
	Static   fs.FS
	Store    *auth.Store
	Sessions *auth.Sessions
	Limiter  *auth.Limiter
	Helper   *helper.Client
	Files    string // socket of the file service, empty = off
	Home     string // socket of the Smart Home service, empty = off
	Sampler  *sys.Sampler
	History  *sys.History
	Monitor  *sys.Monitor
	DataDir  string
	Log      *slog.Logger
	// Version and SourceURL for the "About" page (AGPL §13, F-UI-07), RouterIP for network functions
	Version   string
	SourceURL string
	// Locale is the system locale, it decides the language of the web interface (Q-07)
	Locale   string
	RouterIP string
	Name     string // full name of the router, e.g. router.home.arpa
	WANIf    string // uplink interface: wan0, with PPPoE ppp0
	// FakeWiFi shows the Wi-Fi page without hardware (test instance only, F-WIFI-04)
	FakeWiFi bool
}

type Server struct {
	cfg     Config
	mux     *http.ServeMux
	devices *DeviceStore
}

func New(cfg Config) *Server {
	s := &Server{cfg: cfg, mux: http.NewServeMux(), devices: NewDeviceStore(cfg.DataDir)}

	s.mux.Handle("GET /", http.FileServerFS(cfg.Static))

	// without login
	s.mux.HandleFunc("GET /api/state", s.handleState)
	s.mux.HandleFunc("POST /api/setup", s.handleSetup)
	s.mux.HandleFunc("POST /api/login", s.handleLogin)
	s.mux.HandleFunc("POST /api/logout", s.handleLogout)
	// Source note also without login: the AGPL requires that every user over the network
	// can get the source (§13), also on the login page.
	s.mux.HandleFunc("GET /api/about", s.handleAbout)
	s.mux.HandleFunc("GET /api/i18n", s.handleI18n)

	// with login
	s.route("GET /api/dashboard", s.handleDashboard)
	s.route("GET /api/events", s.handleEvents)
	s.registerNetwork()
	s.registerFirewall()
	s.registerSystem()
	s.registerFiles()
	s.registerHome()
	s.registerMonitor()
	s.registerDeviceExtras()
	s.registerNetstat()
	s.registerAdmin()
	s.registerSettings()
	s.registerWiFi()
	s.registerWireless()
	return s
}

// Start starts background tasks (device detection). Not in tests.
func (s *Server) Start(stop <-chan struct{}) {
	go s.track(stop)
}

// Handler returns the server, so main can pass it to http.Server.
func (s *Server) Handler() http.Handler { return s }

// route registers an endpoint that requires a valid session.
func (s *Server) route(pattern string, h http.HandlerFunc) {
	s.mux.Handle(pattern, s.requireSession(h))
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	h.Set("X-Frame-Options", "DENY")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	h.Set("Cross-Origin-Opener-Policy", "same-origin")
	if strings.HasPrefix(r.URL.Path, "/api/") {
		h.Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !s.sameOrigin(r) {
			s.cfg.Log.Warn("csrf: request rejected", "ip", clientIP(r), "origin", r.Header.Get("Origin"), "path", r.URL.Path)
			writeError(w, http.StatusForbidden, "Request from a foreign site rejected.")
			return
		}
	}
	s.mux.ServeHTTP(w, r)
}

// sameOrigin requires a known Origin for changing requests and the custom header,
// which only the frontend sets (plain forms on foreign sites cannot send it).
func (s *Server) sameOrigin(r *http.Request) bool {
	if r.Header.Get("X-Routerui") != "1" {
		return false
	}
	return slices.Contains(s.cfg.Origins, r.Header.Get("Origin"))
}

func (s *Server) requireSession(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		if err != nil || !s.cfg.Sessions.Touch(c.Value) {
			writeError(w, http.StatusUnauthorized, "Please sign in.")
			return
		}
		next(w, r)
	})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) authenticated(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	return err == nil && s.cfg.Sessions.Touch(c.Value)
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"configured":    s.cfg.Store.Configured(),
		"authenticated": s.authenticated(r),
		"dev":           s.cfg.Dev,
		"name":          s.cfg.Name,
		"features":      s.features(),
		"version":       s.cfg.Version,
		"source":        s.cfg.SourceURL,
		"lang":          i18n.Lang(s.cfg.Locale),
	})
}

// handleI18n returns the catalog for a language (no secrets, also needed on the login page).
func (s *Server) handleI18n(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if c := i18n.Catalog(r.URL.Query().Get("lang")); c != nil {
		w.Write(c)
		return
	}
	io.WriteString(w, "{}")
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: token, Path: "/",
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
		MaxAge: int(s.cfg.Sessions.Max.Seconds()),
	})
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if s.cfg.Store.Configured() {
		writeError(w, http.StatusConflict, "The web interface is already set up.")
		return
	}
	if wait := s.cfg.Limiter.Wait(ip); wait > 0 {
		tooMany(w, wait)
		return
	}
	var req struct {
		Code     string `json:"code"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if !s.cfg.Store.CheckSetupCode(req.Code) {
		s.cfg.Limiter.Fail(ip)
		s.cfg.Log.Warn("auth: failed setup code from " + ip)
		writeError(w, http.StatusForbidden, "The setup code is not correct.")
		return
	}
	if err := s.cfg.Store.SetPassword(req.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.cfg.Limiter.Success(ip)
	s.cfg.Log.Info("auth: password set via setup page", "ip", ip)
	if err := s.call(r, "console.setup_done", nil, nil); err != nil {
		s.cfg.Log.Warn("console: hint not updated", "err", err)
	}
	s.setSessionCookie(w, s.cfg.Sessions.Create(ip))
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if wait := s.cfg.Limiter.Wait(ip); wait > 0 {
		tooMany(w, wait)
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if !s.cfg.Store.Verify(req.Password) {
		s.cfg.Limiter.Fail(ip)
		// Fixed form for the fail2ban jail "routerui"
		s.cfg.Log.Warn("auth: failed login from " + ip)
		writeError(w, http.StatusUnauthorized, "The password is wrong.")
		return
	}
	s.cfg.Limiter.Success(ip)
	s.cfg.Log.Info("auth: login", "ip", ip)
	s.setSessionCookie(w, s.cfg.Sessions.Create(ip))
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		s.cfg.Sessions.Delete(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
	writeJSON(w, map[string]any{"ok": true})
}

func tooMany(w http.ResponseWriter, wait time.Duration) {
	secs := int(wait.Seconds() + 0.999)
	w.Header().Set("Retry-After", itoa(secs))
	writeError(w, http.StatusTooManyRequests, "Too many failed attempts. Please wait "+itoa(secs)+" seconds.")
}

// --- Helper functions ----------------------------------------------------------------

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// readJSON reads at most 64 KiB and rejects unknown fields.
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request.")
		return false
	}
	return true
}

// helperError translates helper errors into a response.
func (s *Server) helperError(w http.ResponseWriter, err error) {
	var ue *helper.UserError
	if errors.As(err, &ue) {
		writeError(w, http.StatusBadRequest, ue.Msg)
		return
	}
	s.cfg.Log.Error("helper", "err", err)
	writeError(w, http.StatusBadGateway, "The helper is not reachable.")
}

func (s *Server) call(r *http.Request, action string, params, out any) error {
	return s.cfg.Helper.Call(r.Context(), action, params, out)
}
func itoa(n int) string { return strconv.Itoa(n) }
