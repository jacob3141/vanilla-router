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
		"without header": {"Origin": origin},
		"foreign origin": {"Origin": "https://evil.example", "X-Routerui": "1"},
		"without origin": {"X-Routerui": "1"},
	} {
		if w := do(s, "POST", "/api/login", `{"password":"x"}`, hdr); w.Code != http.StatusForbidden {
			t.Errorf("%s: status %d, expected 403", name, w.Code)
		}
	}
}

func TestProtectedNeedsSession(t *testing.T) {
	s, _ := newTestServer(t)
	if w := do(s, "GET", "/api/dashboard", "", nil); w.Code != http.StatusUnauthorized {
		t.Errorf("overview without login: status %d, expected 401", w.Code)
	}
	if w := do(s, "GET", "/api/dashboard", "", nil, &http.Cookie{Name: cookieName, Value: "made-up"}); w.Code != http.StatusUnauthorized {
		t.Errorf("overview with a wrong cookie: status %d, expected 401", w.Code)
	}
}

func TestSetupThenLogin(t *testing.T) {
	s, store := newTestServer(t)
	code, _ := store.SetupCode()

	if w := do(s, "POST", "/api/setup", `{"code":"FALS-CHER","password":"a-secure-password"}`, okHdr); w.Code != http.StatusForbidden {
		t.Fatalf("wrong code: status %d", w.Code)
	}
	w := do(s, "POST", "/api/setup", `{"code":"`+code+`","password":"a-secure-password"}`, okHdr)
	if w.Code != http.StatusOK {
		t.Fatalf("setup: status %d %s", w.Code, w.Body)
	}
	c := w.Result().Cookies()
	if len(c) != 1 || !c[0].HttpOnly || !c[0].Secure || c[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("session cookie set insecurely: %+v", c)
	}
	if w := do(s, "GET", "/api/state", "", nil, c[0]); !strings.Contains(w.Body.String(), `"authenticated":true`) {
		t.Errorf("not logged in after setup: %s", w.Body)
	}
	if w := do(s, "POST", "/api/setup", `{"code":"`+code+`","password":"yet-another-password"}`, okHdr); w.Code != http.StatusConflict {
		t.Errorf("second setup: status %d, expected 409", w.Code)
	}

	if w := do(s, "POST", "/api/login", `{"password":"wrong-wrong-wrong"}`, okHdr); w.Code != http.StatusUnauthorized {
		t.Errorf("wrong password: status %d", w.Code)
	}
	if w := do(s, "POST", "/api/login", `{"password":"a-secure-password"}`, okHdr); w.Code != http.StatusOK {
		t.Errorf("correct password: status %d", w.Code)
	}
}

func TestLoginThrottled(t *testing.T) {
	s, store := newTestServer(t)
	store.SetPassword("a-secure-password")
	var last int
	for i := 0; i < 5; i++ {
		last = do(s, "POST", "/api/login", `{"password":"wrong-wrong-wrong"}`, okHdr).Code
	}
	if last != http.StatusTooManyRequests {
		t.Errorf("after 5 failed attempts status %d, expected 429", last)
	}
	// even the correct password is rejected during the lockout
	if w := do(s, "POST", "/api/login", `{"password":"a-secure-password"}`, okHdr); w.Code != http.StatusTooManyRequests {
		t.Errorf("correct password during the lockout: status %d", w.Code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	s, _ := newTestServer(t)
	w := do(s, "GET", "/", "", nil)
	for _, h := range []string{"Content-Security-Policy", "X-Frame-Options", "X-Content-Type-Options", "Referrer-Policy"} {
		if w.Header().Get(h) == "" {
			t.Errorf("header %s is missing", h)
		}
	}
}
