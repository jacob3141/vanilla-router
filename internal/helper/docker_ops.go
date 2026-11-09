// SPDX-License-Identifier: AGPL-3.0-or-later

package helper

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// Webhooks, stacks from Git, the catalog of application templates and the check for newer images.

const (
	webhooksFile = "/var/lib/vanilla-router/docker-webhooks.json"
	catalogURL   = "https://raw.githubusercontent.com/portainer/templates/v3/templates.json"
	catalogTTL   = time.Hour
	gitMetaName  = "git.json"
	maxDownload  = 8 << 20
)

// dockerOps are the long-running operations, shared by the pages, webhooks and the Git watcher.
type dockerOps struct {
	startJob func(string, func(context.Context) string) error
	replace  func(ctx context.Context, old string, pull bool, spec dockerCreate) error
	deploy   func(name string, pull bool, before func(context.Context) string) error
}

var (
	gitRefRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,200}$`)
	gitUserRe = regexp.MustCompile(`^[^\s/:@]{1,200}$`)
)

type gitMeta struct {
	URL       string    `json:"url"`
	Ref       string    `json:"ref"`
	Path      string    `json:"path"`
	User      string    `json:"user,omitempty"`
	Token     string    `json:"token,omitempty"`
	Interval  int       `json:"interval"` // minutes, 0 = only by hand
	LastCheck time.Time `json:"last_check"`
}

func (s *Server) stackDir(n string) string { return s.Path(filepath.Join(stacksDir, n)) }

func (s *Server) readGit(n string) *gitMeta {
	b, err := os.ReadFile(filepath.Join(s.stackDir(n), gitMetaName))
	if err != nil {
		return nil
	}
	var m gitMeta
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	return &m
}

func (s *Server) writeGit(n string, m *gitMeta) error {
	b, _ := json.Marshal(m)
	return os.WriteFile(filepath.Join(s.stackDir(n), gitMetaName), b, 0o600)
}

// stackCompose returns the project folder and the Compose file of a stack.
func (s *Server) stackCompose(n string) (dir, file string) {
	if m := s.readGit(n); m != nil {
		file = filepath.Join(s.stackDir(n), "repo", filepath.FromSlash(m.Path))
		return filepath.Dir(file), file
	}
	return s.stackDir(n), filepath.Join(s.stackDir(n), "compose.yaml")
}

func (s *Server) stackOwned(n string) bool {
	_, file := s.stackCompose(n)
	_, err := os.Stat(file)
	return err == nil || s.readGit(n) != nil
}

func (s *Server) composeArgs(n string, args ...string) []string {
	dir, file := s.stackCompose(n)
	return append([]string{"compose", "-p", n, "--project-directory", dir, "-f", file, "--env-file", filepath.Join(s.stackDir(n), ".env")}, args...)
}

// checkGitURL allows only plain web addresses. Git itself knows others that run programs.
func checkGitURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
		return Userf("Please enter the web address of the repository, starting with https://, without a user name.")
	}
	return nil
}

func checkGitPath(p string) error {
	if p == "" || strings.HasPrefix(p, "/") || filepath.Clean(p) != p || strings.HasPrefix(p, "..") || strings.ContainsAny(p, "\\\r\n") {
		return Userf("The path of the Compose file must be relative to the repository.")
	}
	return nil
}

func gitRun(ctx context.Context, m *gitMeta, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/root", "GIT_TERMINAL_PROMPT=0", "GIT_ALLOW_PROTOCOL=https:http", "LC_ALL=C.UTF-8"}
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if m.Token != "" {
		text = strings.ReplaceAll(text, m.Token, "***")
	}
	if err != nil {
		return text, errors.New(lastLines(text, 1))
	}
	return text, nil
}

func (m *gitMeta) cloneURL() string {
	u, _ := url.Parse(m.URL)
	if m.Token != "" {
		user := m.User
		if user == "" {
			user = "git"
		}
		u.User = url.UserPassword(user, m.Token)
	}
	return u.String()
}

// gitSync brings the working copy to the latest commit and says whether anything changed.
func (s *Server) gitSync(ctx context.Context, n string) (bool, error) {
	m := s.readGit(n)
	if m == nil {
		return false, errors.New("this stack does not come from Git")
	}
	if s.cfg.DryRun {
		return true, nil
	}
	repo := filepath.Join(s.stackDir(n), "repo")
	changed := false
	if _, err := os.Stat(filepath.Join(repo, ".git")); err != nil {
		os.RemoveAll(repo)
		args := []string{"clone", "--depth", "1"}
		if m.Ref != "" {
			args = append(args, "--branch", m.Ref)
		}
		if _, err := gitRun(ctx, m, s.stackDir(n), append(args, "--", m.cloneURL(), "repo")...); err != nil {
			return false, err
		}
		changed = true
	} else {
		ref := m.Ref
		if ref == "" {
			ref = "HEAD"
		}
		if _, err := gitRun(ctx, m, repo, "fetch", "--depth", "1", "origin", ref); err != nil {
			return false, err
		}
		head, err1 := gitRun(ctx, m, repo, "rev-parse", "HEAD")
		next, err2 := gitRun(ctx, m, repo, "rev-parse", "FETCH_HEAD")
		if err := errors.Join(err1, err2); err != nil {
			return false, err
		}
		if head != next {
			if _, err := gitRun(ctx, m, repo, "reset", "--hard", "FETCH_HEAD"); err != nil {
				return false, err
			}
			changed = true
		}
	}
	// A repository must not reach outside itself through a link
	_, file := s.stackCompose(n)
	real, err := filepath.EvalSymlinks(file)
	root, rerr := filepath.EvalSymlinks(repo)
	if err != nil || rerr != nil || !strings.HasPrefix(real, root+string(filepath.Separator)) {
		return false, errors.New("the Compose file was not found in the repository")
	}
	return changed, nil
}

// gitWatcher looks for new commits in stacks that asked for it.
func (s *Server) gitWatcher(ops dockerOps) {
	for range time.Tick(time.Minute) {
		ents, _ := os.ReadDir(s.Path(stacksDir))
		for _, e := range ents {
			n := e.Name()
			m := s.readGit(n)
			if m == nil || m.Interval <= 0 || time.Since(m.LastCheck) < time.Duration(m.Interval)*time.Minute {
				continue
			}
			m.LastCheck = time.Now()
			s.writeGit(n, m)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			changed, err := s.gitSync(ctx, n)
			cancel()
			if err != nil {
				s.cfg.Log.Warn("stack: checking the repository failed", "stack", n, "err", err)
				continue
			}
			if changed {
				s.cfg.Log.Info("stack: new commit, deploying", "stack", n)
				if err := ops.deploy(n, true, nil); err != nil {
					s.cfg.Log.Warn("stack: deployment not started", "stack", n, "err", err)
				}
			}
		}
	}
}

// --- Webhooks ------------------------------------------------------------------------

type dockerWebhook struct {
	ID      string    `json:"id"`
	Hash    string    `json:"hash,omitempty"`
	Kind    string    `json:"kind"` // container or stack
	Name    string    `json:"name"`
	Created time.Time `json:"created"`
}

func (s *Server) readHooks() []dockerWebhook {
	var l []dockerWebhook
	if b, err := os.ReadFile(s.Path(webhooksFile)); err == nil {
		json.Unmarshal(b, &l)
	}
	return l
}

func (s *Server) writeHooks(l []dockerWebhook) error {
	b, _ := json.MarshalIndent(l, "", " ")
	os.MkdirAll(filepath.Dir(s.Path(webhooksFile)), 0o755)
	return os.WriteFile(s.Path(webhooksFile), b, 0o600)
}

func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

func (s *Server) registerDockerOps(ops dockerOps) {
	var hookMu sync.Mutex

	s.handle("docker.webhooks.list", func(ctx context.Context, _ json.RawMessage) (any, error) {
		hookMu.Lock()
		defer hookMu.Unlock()
		out := []dockerWebhook{}
		for _, h := range s.readHooks() {
			h.Hash = ""
			out = append(out, h)
		}
		return out, nil
	})
	s.handle("docker.webhook.create", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if (p.Kind != "container" && p.Kind != "stack") || !containerNameRe.MatchString(p.Name) {
			return nil, Userf("Invalid webhook.")
		}
		var b, idb [32]byte
		rand.Read(b[:])
		rand.Read(idb[:8])
		token := hex.EncodeToString(b[:])
		hookMu.Lock()
		defer hookMu.Unlock()
		l := s.readHooks()
		l = append(l, dockerWebhook{ID: hex.EncodeToString(idb[:8]), Hash: hashToken(token), Kind: p.Kind, Name: p.Name, Created: time.Now()})
		if err := s.writeHooks(l); err != nil {
			return nil, err
		}
		return map[string]string{"token": token}, nil
	})
	s.handle("docker.webhook.remove", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			ID string `json:"id"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		hookMu.Lock()
		defer hookMu.Unlock()
		return nil, s.writeHooks(slices.DeleteFunc(s.readHooks(), func(h dockerWebhook) bool { return h.ID == p.ID }))
	})
	s.handle("docker.webhook.fire", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Token string `json:"token"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		want := hashToken(p.Token)
		hookMu.Lock()
		var hit *dockerWebhook
		for _, h := range s.readHooks() {
			if subtle.ConstantTimeCompare([]byte(h.Hash), []byte(want)) == 1 {
				h := h
				hit = &h
			}
		}
		hookMu.Unlock()
		if hit == nil {
			return nil, Userf("Unknown webhook.")
		}
		if hit.Kind == "container" {
			spec, _, err := s.specOf(ctx, hit.Name)
			if err != nil {
				return nil, err
			}
			return nil, ops.replace(ctx, hit.Name, true, spec)
		}
		name := hit.Name
		var before func(context.Context) string
		if s.readGit(name) != nil {
			before = func(bg context.Context) string {
				if _, err := s.gitSync(bg, name); err != nil {
					return "The repository could not be read: " + err.Error()
				}
				return ""
			}
		}
		return nil, ops.deploy(name, true, before)
	})

	// Stacks from Git
	s.handle("docker.stack.git.deploy", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Name     string `json:"name"`
			URL      string `json:"url"`
			Ref      string `json:"ref"`
			Path     string `json:"path"`
			User     string `json:"user"`
			Token    string `json:"token"`
			Env      string `json:"env"`
			Interval int    `json:"interval"`
			Pull     bool   `json:"pull"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if !dockerStackRe.MatchString(p.Name) {
			return nil, Userf("A stack name may contain lowercase letters, digits, underscores and hyphens.")
		}
		p.URL, p.Ref, p.Path = strings.TrimSpace(p.URL), strings.TrimSpace(p.Ref), strings.TrimSpace(p.Path)
		if p.Path == "" {
			p.Path = "docker-compose.yml"
		}
		if err := checkGitURL(p.URL); err != nil {
			return nil, err
		}
		if err := checkGitPath(p.Path); err != nil {
			return nil, err
		}
		if p.Ref != "" && !gitRefRe.MatchString(p.Ref) {
			return nil, Userf("\"%s\" is not a valid branch or tag.", p.Ref)
		}
		if p.User != "" && !gitUserRe.MatchString(p.User) {
			return nil, Userf("Invalid user name.")
		}
		if p.Interval < 0 || p.Interval > 7*24*60 {
			return nil, Userf("The interval must be between 0 and one week, in minutes.")
		}
		if len(p.Env) > maxCompose {
			return nil, Userf("The environment is too long.")
		}
		dir := s.stackDir(p.Name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
		old := s.readGit(p.Name)
		m := &gitMeta{URL: p.URL, Ref: p.Ref, Path: p.Path, User: p.User, Token: p.Token, Interval: p.Interval, LastCheck: time.Now()}
		if p.Token == "" && old != nil && old.URL == p.URL {
			m.Token = old.Token
		}
		if old != nil && (old.URL != m.URL || old.Ref != m.Ref) {
			os.RemoveAll(filepath.Join(dir, "repo"))
		}
		if err := s.writeGit(p.Name, m); err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(p.Env), 0o600); err != nil {
			return nil, err
		}
		os.Remove(filepath.Join(dir, "compose.yaml"))
		name := p.Name
		return nil, ops.deploy(name, p.Pull, func(bg context.Context) string {
			if _, err := s.gitSync(bg, name); err != nil {
				return "The repository could not be read: " + err.Error()
			}
			return ""
		})
	})

	// Application templates
	var catMu sync.Mutex
	var cat any
	var catAt time.Time
	s.handle("docker.catalog.get", func(ctx context.Context, _ json.RawMessage) (any, error) {
		catMu.Lock()
		defer catMu.Unlock()
		if s.cfg.DryRun {
			return []map[string]any{
				{"id": 1, "type": 1, "title": "Demo app", "description": "A simulated container template", "categories": []string{"Demo"}, "image": "example/app:1",
					"env": []map[string]string{{"name": "TOKEN", "label": "Token", "default": "x"}}, "volumes": []map[string]string{{"container": "/data"}}},
				{"id": 2, "type": 3, "title": "Demo stack", "description": "A simulated Compose template", "categories": []string{"Demo"}, "repository": map[string]string{"url": "https://example.com/r.git", "stackfile": "compose.yml"}},
			}, nil
		}
		if cat != nil && time.Since(catAt) < catalogTTL {
			return cat, nil
		}
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, catalogURL, nil)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, Userf("The list of templates could not be loaded.")
		}
		defer res.Body.Close()
		var doc struct {
			Templates []struct {
				ID          int               `json:"id"`
				Type        int               `json:"type"`
				Title       string            `json:"title"`
				Description string            `json:"description"`
				Categories  []string          `json:"categories"`
				Platform    string            `json:"platform"`
				Image       string            `json:"image"`
				Restart     string            `json:"restart_policy"`
				Env         []json.RawMessage `json:"env"`
				Volumes     []json.RawMessage `json:"volumes"`
				Repository  json.RawMessage   `json:"repository"`
			} `json:"templates"`
		}
		if err := json.NewDecoder(io.LimitReader(res.Body, maxDownload)).Decode(&doc); err != nil {
			return nil, Userf("The list of templates could not be read.")
		}
		out := []map[string]any{}
		for _, t := range doc.Templates {
			if (t.Type != 1 && t.Type != 3) || (t.Platform != "" && t.Platform != "linux") {
				continue
			}
			out = append(out, map[string]any{"id": t.ID, "type": t.Type, "title": t.Title, "description": t.Description, "categories": t.Categories,
				"image": t.Image, "restart": t.Restart, "env": t.Env, "volumes": t.Volumes, "repository": t.Repository})
		}
		cat, catAt = out, time.Now()
		return cat, nil
	})

	// Newer images
	var upMu sync.Mutex
	updates := map[string]string{}
	s.handle("docker.updates.get", func(ctx context.Context, _ json.RawMessage) (any, error) {
		upMu.Lock()
		defer upMu.Unlock()
		return updates, nil
	})
	s.handle("docker.updates.check", func(ctx context.Context, _ json.RawMessage) (any, error) {
		return nil, ops.startJob("Check for updates", func(bg context.Context) string {
			if s.cfg.DryRun {
				upMu.Lock()
				updates = map[string]string{"example/app:1": "update"}
				upMu.Unlock()
				return ""
			}
			list, err := listContainers(bg)
			if err != nil {
				return "Docker is not responding."
			}
			res := map[string]string{}
			client := &http.Client{Timeout: 20 * time.Second}
			auths := registryAuths()
			for _, c := range list {
				if _, seen := res[c.Image]; seen {
					continue
				}
				res[c.Image] = imageStatus(bg, client, auths, c.Image)
			}
			upMu.Lock()
			updates = res
			upMu.Unlock()
			return ""
		})
	})
}

// --- Newer images ----------------------------------------------------------------------

func registryAuths() map[string]string {
	out := map[string]string{}
	b, err := os.ReadFile("/root/.docker/config.json")
	if err != nil {
		return out
	}
	var c struct {
		Auths map[string]struct {
			Auth string `json:"auth"`
		} `json:"auths"`
	}
	if json.Unmarshal(b, &c) == nil {
		for h, a := range c.Auths {
			out[strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(h, "https://"), "http://"), "/v1/")] = a.Auth
		}
	}
	return out
}

// parseImageRef splits an image name into registry host, repository and tag.
func parseImageRef(image string) (host, repo, tag string, ok bool) {
	if strings.Contains(image, "@") {
		return "", "", "", false // pinned to a digest, nothing can be newer
	}
	name := image
	first, rest, slash := strings.Cut(name, "/")
	if slash && (strings.ContainsAny(first, ".:") || first == "localhost") {
		host, name = first, rest
	} else {
		host = "docker.io"
	}
	tag = "latest"
	if i := strings.LastIndex(name, ":"); i >= 0 {
		name, tag = name[:i], name[i+1:]
	}
	if host == "docker.io" && !strings.Contains(name, "/") {
		name = "library/" + name
	}
	return host, name, tag, name != ""
}

func registryBase(host string) string {
	if host == "docker.io" {
		return "https://registry-1.docker.io"
	}
	return "https://" + host
}

var authParamRe = regexp.MustCompile(`(\w+)="([^"]*)"`)

// remoteDigest asks a registry for the digest of a tag. Registries answer the first request with where to get a token.
func remoteDigest(ctx context.Context, c *http.Client, base, repo, tag, basic string) (string, error) {
	head := func(token string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, base+"/v2/"+repo+"/manifests/"+tag, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", strings.Join([]string{"application/vnd.oci.image.index.v1+json", "application/vnd.docker.distribution.manifest.list.v2+json",
			"application/vnd.docker.distribution.manifest.v2+json", "application/vnd.oci.image.manifest.v1+json"}, ", "))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		return c.Do(req)
	}
	res, err := head("")
	if err != nil {
		return "", err
	}
	res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized {
		params := map[string]string{}
		for _, m := range authParamRe.FindAllStringSubmatch(res.Header.Get("Www-Authenticate"), -1) {
			params[m[1]] = m[2]
		}
		if params["realm"] == "" {
			return "", errors.New("no way to authenticate")
		}
		u, err := url.Parse(params["realm"])
		if err != nil {
			return "", err
		}
		q := u.Query()
		q.Set("service", params["service"])
		q.Set("scope", params["scope"])
		u.RawQuery = q.Encode()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if basic != "" {
			req.Header.Set("Authorization", "Basic "+basic)
		}
		tr, err := c.Do(req)
		if err != nil {
			return "", err
		}
		defer tr.Body.Close()
		var t struct {
			Token       string `json:"token"`
			AccessToken string `json:"access_token"`
		}
		if err := json.NewDecoder(io.LimitReader(tr.Body, 1<<16)).Decode(&t); err != nil {
			return "", err
		}
		if t.Token == "" {
			t.Token = t.AccessToken
		}
		if res, err = head(t.Token); err != nil {
			return "", err
		}
		res.Body.Close()
	}
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", res.StatusCode)
	}
	return res.Header.Get("Docker-Content-Digest"), nil
}

// imageStatus is update, current or unknown.
func imageStatus(ctx context.Context, c *http.Client, auths map[string]string, image string) string {
	host, repo, tag, ok := parseImageRef(image)
	if !ok {
		return "unknown"
	}
	out, err := run(ctx, "docker", "image", "inspect", "--format", "{{json .RepoDigests}}", "--", image)
	if err != nil {
		return "unknown"
	}
	var local []string
	if json.Unmarshal([]byte(out), &local) != nil || len(local) == 0 {
		return "unknown" // built here, it has no counterpart in a registry
	}
	basic := auths[host]
	if basic == "" && host == "docker.io" {
		basic = auths["index.docker.io"]
	}
	digest, err := remoteDigest(ctx, c, registryBase(host), repo, tag, basic)
	if err != nil || digest == "" {
		return "unknown"
	}
	if slices.ContainsFunc(local, func(d string) bool { return strings.HasSuffix(d, "@"+digest) }) {
		return "current"
	}
	return "update"
}
