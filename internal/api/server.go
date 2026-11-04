// SPDX-License-Identifier: AGPL-3.0-or-later

// Package api enthält den HTTP-Teil der Oberfläche: Middleware, Login und die Endpunkte.
package api

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"vanilla-router/internal/auth"
	"vanilla-router/internal/helper"
	"vanilla-router/internal/sys"
)

const cookieName = "vr_session"

type Config struct {
	Dev      bool
	Origins  []string // erlaubte Origin-Werte für ändernde Anfragen
	Static   fs.FS
	Store    *auth.Store
	Sessions *auth.Sessions
	Limiter  *auth.Limiter
	Helper   *helper.Client
	Files    string // Socket des Dateidienstes, leer = aus
	Sampler  *sys.Sampler
	History  *sys.History
	Monitor  *sys.Monitor
	DataDir  string
	Log      *slog.Logger
	// Version und SourceURL für die Seite „Über“ (AGPL §13, F-UI-07), RouterIP für Netzfunktionen
	Version   string
	SourceURL string
	RouterIP  string
	Name      string // voller Name des Routers, z. B. router.home.arpa
	WANIf     string // Uplink-Interface: wan0, bei PPPoE ppp0
}

type Server struct {
	cfg     Config
	mux     *http.ServeMux
	devices *DeviceStore
}

func New(cfg Config) *Server {
	s := &Server{cfg: cfg, mux: http.NewServeMux(), devices: NewDeviceStore(cfg.DataDir)}

	s.mux.Handle("GET /", http.FileServerFS(cfg.Static))

	// ohne Anmeldung
	s.mux.HandleFunc("GET /api/state", s.handleState)
	s.mux.HandleFunc("POST /api/setup", s.handleSetup)
	s.mux.HandleFunc("POST /api/login", s.handleLogin)
	s.mux.HandleFunc("POST /api/logout", s.handleLogout)
	// Quelltext-Hinweis auch ohne Anmeldung: Die AGPL verlangt, dass jeder Nutzer über das Netz
	// an den Quelltext kommt (§13), auch auf der Anmeldeseite.
	s.mux.HandleFunc("GET /api/about", s.handleAbout)

	// mit Anmeldung
	s.route("GET /api/dashboard", s.handleDashboard)
	s.route("GET /api/events", s.handleEvents)
	s.registerNetwork()
	s.registerFirewall()
	s.registerSystem()
	s.registerFiles()
	s.registerMonitor()
	s.registerDeviceExtras()
	s.registerNetstat()
	s.registerAdmin()
	return s
}

// Start startet Hintergrundaufgaben (Geräteerkennung). Nicht in Tests.
func (s *Server) Start(stop <-chan struct{}) {
	go s.track(stop)
}

// Handler liefert den Server, damit main ihn an http.Server übergeben kann.
func (s *Server) Handler() http.Handler { return s }

// route registriert einen Endpunkt, der eine gültige Sitzung verlangt.
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
			s.cfg.Log.Warn("csrf: anfrage abgelehnt", "ip", clientIP(r), "origin", r.Header.Get("Origin"), "path", r.URL.Path)
			writeError(w, http.StatusForbidden, "Anfrage von fremder Seite abgelehnt.")
			return
		}
	}
	s.mux.ServeHTTP(w, r)
}

// sameOrigin verlangt für ändernde Anfragen einen bekannten Origin und den eigenen Header,
// den nur das Frontend setzt (einfache Formulare fremder Seiten können ihn nicht senden).
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
			writeError(w, http.StatusUnauthorized, "Bitte anmelden.")
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
		"version":       s.cfg.Version,
		"source":        s.cfg.SourceURL,
	})
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
		writeError(w, http.StatusConflict, "Die Oberfläche ist bereits eingerichtet.")
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
		writeError(w, http.StatusForbidden, "Der Einrichtungscode stimmt nicht.")
		return
	}
	if err := s.cfg.Store.SetPassword(req.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.cfg.Limiter.Success(ip)
	s.cfg.Log.Info("auth: password set via setup page", "ip", ip)
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
		// Feste Form für die fail2ban-Jail "routerui"
		s.cfg.Log.Warn("auth: failed login from " + ip)
		writeError(w, http.StatusUnauthorized, "Das Passwort ist falsch.")
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
	writeError(w, http.StatusTooManyRequests, "Zu viele Fehlversuche. Bitte "+itoa(secs)+" Sekunden warten.")
}

// --- Hilfsfunktionen ----------------------------------------------------------------

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// readJSON liest höchstens 64 KiB und lehnt unbekannte Felder ab.
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige Anfrage.")
		return false
	}
	return true
}

// helperError übersetzt Fehler des Helpers in eine Antwort.
func (s *Server) helperError(w http.ResponseWriter, err error) {
	var ue *helper.UserError
	if errors.As(err, &ue) {
		writeError(w, http.StatusBadRequest, ue.Msg)
		return
	}
	s.cfg.Log.Error("helper", "err", err)
	writeError(w, http.StatusBadGateway, "Der Helper ist nicht erreichbar.")
}

func (s *Server) call(r *http.Request, action string, params, out any) error {
	return s.cfg.Helper.Call(r.Context(), action, params, out)
}
func itoa(n int) string { return strconv.Itoa(n) }
