// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"vanilla-router/internal/auth"
	"vanilla-router/internal/helper"
	"vanilla-router/internal/sys"
)

const origin = "https://router.home.arpa"

func newTestServer(t *testing.T) (*Server, *auth.Store) {
	t.Helper()
	store := auth.NewStore(t.TempDir())
	if _, err := store.SetupCode(); err != nil {
		t.Fatal(err)
	}
	s := New(Config{
		Origins:  []string{origin},
		Static:   fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}},
		Store:    store,
		Sessions: auth.NewSessions(30*time.Minute, 12*time.Hour),
		Limiter:  auth.NewLimiter(),
		Helper:   &helper.Client{Socket: "/nonexistent"},
		Sampler:  sys.NewSampler(),
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	return s, store
}

func do(s *Server, method, path, body string, hdr map[string]string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.RemoteAddr = "192.168.42.127:50000"
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

var okHdr = map[string]string{"Origin": origin, "X-Routerui": "1", "Content-Type": "application/json"}

func TestCSRFRejected(t *testing.T) {
	s, _ := newTestServer(t)
	for name, hdr := range map[string]map[string]string{
		"ohne Header":    {"Origin": origin},
		"fremder Origin": {"Origin": "https://evil.example", "X-Routerui": "1"},
		"ohne Origin":    {"X-Routerui": "1"},
	} {
		if w := do(s, "POST", "/api/login", `{"password":"x"}`, hdr); w.Code != http.StatusForbidden {
			t.Errorf("%s: Status %d, erwartet 403", name, w.Code)
		}
	}
}

func TestProtectedNeedsSession(t *testing.T) {
	s, _ := newTestServer(t)
	if w := do(s, "GET", "/api/dashboard", "", nil); w.Code != http.StatusUnauthorized {
		t.Errorf("Übersicht ohne Anmeldung: Status %d, erwartet 401", w.Code)
	}
	if w := do(s, "GET", "/api/dashboard", "", nil, &http.Cookie{Name: cookieName, Value: "erfunden"}); w.Code != http.StatusUnauthorized {
		t.Errorf("Übersicht mit falschem Cookie: Status %d, erwartet 401", w.Code)
	}
}

func TestSetupThenLogin(t *testing.T) {
	s, store := newTestServer(t)
	code, _ := store.SetupCode()

	if w := do(s, "POST", "/api/setup", `{"code":"FALS-CHER","password":"ein-sicheres-passwort"}`, okHdr); w.Code != http.StatusForbidden {
		t.Fatalf("falscher Code: Status %d", w.Code)
	}
	w := do(s, "POST", "/api/setup", `{"code":"`+code+`","password":"ein-sicheres-passwort"}`, okHdr)
	if w.Code != http.StatusOK {
		t.Fatalf("Einrichtung: Status %d %s", w.Code, w.Body)
	}
	c := w.Result().Cookies()
	if len(c) != 1 || !c[0].HttpOnly || !c[0].Secure || c[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("Sitzungs-Cookie unsicher gesetzt: %+v", c)
	}
	if w := do(s, "GET", "/api/state", "", nil, c[0]); !strings.Contains(w.Body.String(), `"authenticated":true`) {
		t.Errorf("nach Einrichtung nicht angemeldet: %s", w.Body)
	}
	if w := do(s, "POST", "/api/setup", `{"code":"`+code+`","password":"noch-ein-passwort"}`, okHdr); w.Code != http.StatusConflict {
		t.Errorf("zweite Einrichtung: Status %d, erwartet 409", w.Code)
	}

	if w := do(s, "POST", "/api/login", `{"password":"falsch-falsch-falsch"}`, okHdr); w.Code != http.StatusUnauthorized {
		t.Errorf("falsches Passwort: Status %d", w.Code)
	}
	if w := do(s, "POST", "/api/login", `{"password":"ein-sicheres-passwort"}`, okHdr); w.Code != http.StatusOK {
		t.Errorf("richtiges Passwort: Status %d", w.Code)
	}
}

func TestLoginThrottled(t *testing.T) {
	s, store := newTestServer(t)
	store.SetPassword("ein-sicheres-passwort")
	var last int
	for i := 0; i < 5; i++ {
		last = do(s, "POST", "/api/login", `{"password":"falsch-falsch-falsch"}`, okHdr).Code
	}
	if last != http.StatusTooManyRequests {
		t.Errorf("nach 5 Fehlversuchen Status %d, erwartet 429", last)
	}
	// auch das richtige Passwort wird während der Sperre abgewiesen
	if w := do(s, "POST", "/api/login", `{"password":"ein-sicheres-passwort"}`, okHdr); w.Code != http.StatusTooManyRequests {
		t.Errorf("richtiges Passwort während der Sperre: Status %d", w.Code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	s, _ := newTestServer(t)
	w := do(s, "GET", "/", "", nil)
	for _, h := range []string{"Content-Security-Policy", "X-Frame-Options", "X-Content-Type-Options", "Referrer-Policy"} {
		if w.Header().Get(h) == "" {
			t.Errorf("Header %s fehlt", h)
		}
	}
}
