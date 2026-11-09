// SPDX-License-Identifier: AGPL-3.0-or-later

package helper

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseImageRef(t *testing.T) {
	for in, want := range map[string][3]string{
		"nginx":                     {"docker.io", "library/nginx", "latest"},
		"nginx:1.27":                {"docker.io", "library/nginx", "1.27"},
		"jellyfin/jellyfin:10":      {"docker.io", "jellyfin/jellyfin", "10"},
		"ghcr.io/owner/app:v1":      {"ghcr.io", "owner/app", "v1"},
		"registry.local:5000/a/b:c": {"registry.local:5000", "a/b", "c"},
		"localhost/a":               {"localhost", "a", "latest"},
	} {
		h, r, tag, ok := parseImageRef(in)
		if !ok || [3]string{h, r, tag} != want {
			t.Errorf("%s: %s %s %s", in, h, r, tag)
		}
	}
	if _, _, _, ok := parseImageRef("nginx@sha256:abc"); ok {
		t.Error("a pinned image has no newer version")
	}
}

// A registry that wants a token first, like Docker Hub and GitHub.
func TestRemoteDigestTokenFlow(t *testing.T) {
	var basic string
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			basic = r.Header.Get("Authorization")
			if r.URL.Query().Get("scope") != "repository:a/b:pull" || r.URL.Query().Get("service") != "reg" {
				t.Errorf("token request: %s", r.URL.RawQuery)
			}
			fmt.Fprint(w, `{"token":"tok"}`)
		case r.Header.Get("Authorization") != "Bearer tok":
			w.Header().Set("Www-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="reg",scope="repository:a/b:pull"`, srv.URL))
			w.WriteHeader(http.StatusUnauthorized)
		case r.URL.Path == "/v2/a/b/manifests/1" && r.Method == http.MethodHead:
			w.Header().Set("Docker-Content-Digest", "sha256:abc")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	d, err := remoteDigest(context.Background(), srv.Client(), srv.URL, "a/b", "1", "dXNlcjpwYXNz")
	if err != nil || d != "sha256:abc" {
		t.Fatalf("%q %v", d, err)
	}
	if basic != "Basic dXNlcjpwYXNz" {
		t.Errorf("%q", basic)
	}
	if _, err := remoteDigest(context.Background(), srv.Client(), srv.URL, "a/b", "missing", ""); err == nil {
		t.Error("missing tag accepted")
	}
}

func TestGitInputs(t *testing.T) {
	for _, bad := range []string{"", "git@github.com:a/b.git", "ssh://h/a", "file:///etc", "ext::sh -c x", "https://user:pw@h/a", "-u"} {
		if checkGitURL(bad) == nil {
			t.Errorf("url %q accepted", bad)
		}
	}
	if checkGitURL("https://github.com/a/b.git") != nil {
		t.Error("https refused")
	}
	for _, bad := range []string{"", "/etc/passwd", "../x", "a/../../x", "a\\b"} {
		if checkGitPath(bad) == nil {
			t.Errorf("path %q accepted", bad)
		}
	}
	if checkGitPath("deploy/compose.yml") != nil {
		t.Error("relative path refused")
	}
	s := newDryServer(t, nil)
	for _, p := range []map[string]any{
		{"name": "A B", "url": "https://h/r", "path": "c.yml"},
		{"name": "ok", "url": "ssh://h/r", "path": "c.yml"},
		{"name": "ok", "url": "https://h/r", "path": "../c.yml"},
		{"name": "ok", "url": "https://h/r", "path": "c.yml", "ref": "--upload-pack=x"},
		{"name": "ok", "url": "https://h/r", "path": "c.yml", "interval": -1},
	} {
		if _, err := call(t, s, "docker.stack.git.deploy", p); err == nil {
			t.Errorf("accepted: %v", p)
		}
	}
}

func TestWebhooks(t *testing.T) {
	s := newDryServer(t, nil)
	res, err := call(t, s, "docker.webhook.create", map[string]string{"kind": "stack", "name": "web"})
	if err != nil {
		t.Fatal(err)
	}
	token := res["token"].(string)
	if len(token) != 64 {
		t.Fatalf("%q", token)
	}
	raw, _ := s.handlers["docker.webhooks.list"](context.Background(), nil)
	if list := raw.([]dockerWebhook); len(list) != 1 || list[0].Hash != "" {
		t.Fatalf("the list must not hold the secret: %+v", list)
	}
	if _, err := call(t, s, "docker.webhook.fire", map[string]string{"token": strings.Repeat("0", 64)}); err == nil {
		t.Error("unknown token accepted")
	}
	if _, err := call(t, s, "docker.webhook.fire", map[string]string{"token": token}); err != nil {
		t.Error(err)
	}
	for _, bad := range []map[string]string{{"kind": "host", "name": "x"}, {"kind": "stack", "name": "a b"}} {
		if _, err := call(t, s, "docker.webhook.create", bad); err == nil {
			t.Errorf("accepted: %v", bad)
		}
	}
	id := raw.([]dockerWebhook)[0].ID
	if _, err := call(t, s, "docker.webhook.remove", map[string]string{"id": id}); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, s)
	if _, err := call(t, s, "docker.webhook.fire", map[string]string{"token": token}); err == nil {
		t.Error("removed webhook still works")
	}
}

func waitIdle(t *testing.T, s *Server) {
	t.Helper()
	for range 100 {
		m, _ := call(t, s, "docker.job.get", nil)
		if m["state"] != "running" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("job does not end")
}

// A real pseudo terminal: in a dry run the session is a local shell.
func TestTerminalSession(t *testing.T) {
	s := newDryServer(t, nil)
	for _, bad := range []map[string]string{{"name": "a b", "mode": "exec", "shell": "/bin/sh"}, {"name": "a", "mode": "exec", "shell": "sh;rm"}, {"name": "a", "mode": "run"}} {
		if _, err := call(t, s, "docker.term.open", bad); err == nil {
			t.Errorf("accepted: %v", bad)
		}
	}
	res, err := call(t, s, "docker.term.open", map[string]string{"name": "a", "mode": "exec", "shell": "/bin/sh"})
	if err != nil {
		t.Fatal(err)
	}
	id := res["id"].(string)
	if _, err := call(t, s, "docker.term.resize", map[string]any{"id": id, "rows": 30, "cols": 100}); err != nil {
		t.Fatal(err)
	}
	if _, err := call(t, s, "docker.term.resize", map[string]any{"id": id, "rows": 1, "cols": 100}); err == nil {
		t.Error("impossible size accepted")
	}
	if _, err := call(t, s, "docker.term.write", map[string]any{"id": id, "data": []byte("stty size; echo mark$((40+2))\n")}); err != nil {
		t.Fatal(err)
	}
	got := ""
	for i := 0; i < 40 && !strings.Contains(got, "mark42"); i++ {
		r, err := call(t, s, "docker.term.read", map[string]any{"id": id})
		if err != nil {
			t.Fatal(err)
		}
		if d, _ := r["data"].(string); d != "" {
			b, _ := base64.StdEncoding.DecodeString(d)
			got += string(b)
		}
	}
	if !strings.Contains(got, "mark42") || !strings.Contains(got, "30 100") {
		t.Errorf("%q", got)
	}
	if _, err := call(t, s, "docker.term.close", map[string]string{"id": id}); err != nil {
		t.Fatal(err)
	}
	if _, err := call(t, s, "docker.term.read", map[string]any{"id": id}); err == nil {
		t.Error("a closed terminal can still be read")
	}
}
