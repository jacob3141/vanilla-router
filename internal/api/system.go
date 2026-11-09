// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"net/http"
	"regexp"
	"strconv"
)

func (s *Server) registerSystem() {
	s.route("GET /api/services", s.get("services.list"))
	s.route("POST /api/services/restart", s.forward("service.restart", "Service restarted."))
	s.route("GET /api/services/logs", s.withQuery("service.logs.get", "unit"))

	s.route("GET /api/containers", s.get("docker.list"))
	s.route("POST /api/containers/action", s.forward("docker.action", "Done."))
	s.route("POST /api/containers", s.forward("docker.create", "The container is being set up."))
	s.route("GET /api/containers/job", s.get("docker.job.get"))
	s.route("POST /api/containers/remove", s.forward("docker.remove", "Removed."))
	s.route("GET /api/containers/images", s.get("docker.images.list"))
	s.route("POST /api/containers/image/remove", s.forward("docker.image.remove", "Removed."))
	s.route("GET /api/containers/volumes", s.get("docker.volumes.list"))
	s.route("POST /api/containers/volume", s.forward("docker.volume.create.full", "Created."))
	for _, r := range [][3]string{
		{"/api/containers/inspect", "docker.inspect.get", ""}, {"/api/containers/top", "docker.top.get", ""}, {"/api/containers/stats", "docker.stats.get", ""},
		{"/api/containers/rename", "docker.rename", "Renamed."}, {"/api/containers/commit", "docker.commit", "Done."},
		{"/api/containers/spec", "docker.spec.get", ""}, {"/api/containers/replace", "docker.replace", "The container is being replaced."},
		{"/api/containers/network/connect", "docker.network.connect", "Done."},
		{"/api/containers/image/pull", "docker.image.pull", "Started."}, {"/api/containers/image/tag", "docker.image.tag", "Done."}, {"/api/containers/image/push", "docker.image.push", "Started."},
		{"/api/containers/image/build", "docker.image.build", "Started."}, {"/api/containers/image/export", "docker.image.export", "Started."}, {"/api/containers/image/import", "docker.image.import", "Started."},
		{"/api/containers/image/prune", "docker.image.prune", "Done."}, {"/api/containers/image/inspect", "docker.image.inspect.get", ""},
		{"/api/containers/volume/prune", "docker.volume.prune", "Done."}, {"/api/containers/volume/browse", "docker.volume.browse.get", ""},
		{"/api/docker/registry/login", "docker.registry.login", "Logged in."}, {"/api/docker/registry/logout", "docker.registry.logout", "Logged out."},
		{"/api/containers/term/open", "docker.term.open", ""}, {"/api/containers/term/read", "docker.term.read", ""}, {"/api/containers/term/write", "docker.term.write", ""},
		{"/api/containers/term/resize", "docker.term.resize", ""}, {"/api/containers/term/close", "docker.term.close", ""},
		{"/api/containers/updates/check", "docker.updates.check", "Started."},
		{"/api/docker/webhook", "docker.webhook.create", "Created."}, {"/api/docker/webhook/remove", "docker.webhook.remove", "Removed."},
		{"/api/stacks/git", "docker.stack.git.deploy", "Started."},
		{"/api/stacks/get", "docker.stack.get", ""}, {"/api/stacks/deploy", "docker.stack.deploy", "Started."}, {"/api/stacks/start", "docker.stack.start", "Done."}, {"/api/stacks/stop", "docker.stack.stop", "Done."},
		{"/api/stacks/remove", "docker.stack.remove", "Removed."}, {"/api/stacks/logs", "docker.stack.logs.get", ""}, {"/api/stacks/template", "docker.template.save", "Saved."}, {"/api/stacks/template/remove", "docker.template.remove", "Removed."},
	} {
		s.route("POST "+r[0], s.forward(r[1], r[2]))
	}
	s.route("GET /api/stacks", s.get("docker.stacks.list"))
	s.route("GET /api/containers/updates", s.get("docker.updates.get"))
	s.route("GET /api/docker/webhooks", s.get("docker.webhooks.list"))
	s.route("GET /api/stacks/catalog", s.get("docker.catalog.get"))
	// A webhook has no session: the long random token in the address is the permission.
	s.mux.HandleFunc("POST /api/webhook/{token}", s.handleWebhook)
	s.route("GET /api/stacks/templates", s.get("docker.templates.list"))
	s.route("GET /api/docker/host", s.get("docker.host.get"))
	s.route("GET /api/docker/events", s.get("docker.events.list"))
	s.route("GET /api/docker/registries", s.get("docker.registries.list"))
	s.route("POST /api/containers/volume/remove", s.forward("docker.volume.remove", "Removed."))
	s.route("GET /api/containers/networks", s.get("docker.networks.get"))
	s.route("POST /api/containers/network", s.forward("docker.network.create", "Created."))
	s.route("POST /api/containers/network/remove", s.forward("docker.network.remove", "Removed."))
	s.route("GET /api/containers/logs", s.withQuery("docker.logs.get", "name"))

	s.route("GET /api/system/updates", s.get("system.updates.get"))
	s.route("POST /api/system/updates", s.forward("system.update.start", "Update started."))
	s.route("GET /api/system/updates/status", s.get("system.update.status"))
	s.route("POST /api/system/power", s.forward("system.power", "Running …"))
	s.route("GET /api/system/audit", s.get("system.audit.get"))
	s.route("POST /api/system/audit", s.forward("system.audit.run", "Audit finished."))

	s.route("POST /api/tools/ping", s.forward("tool.ping", ""))
	s.route("POST /api/tools/trace", s.forward("tool.trace", ""))
	s.route("POST /api/tools/dns", s.forward("tool.dns", ""))

	s.route("POST /api/settings/password", s.handlePasswordChange)
	s.route("POST /api/settings/logout-all", s.handleLogoutAll)
}

// get calls a read-only helper action without parameters.
func (s *Server) get(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var out any
		if err := s.call(r, action, nil, &out); err != nil {
			s.helperError(w, err)
			return
		}
		writeJSON(w, out)
	}
}

// withQuery passes a name and line count from the URL to a log action.
func (s *Server) withQuery(action, key string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		lines, _ := strconv.Atoi(r.URL.Query().Get("lines"))
		var out any
		if err := s.call(r, action, map[string]any{key: r.URL.Query().Get(key), "lines": lines}, &out); err != nil {
			s.helperError(w, err)
			return
		}
		writeJSON(w, out)
	}
}

func (s *Server) handlePasswordChange(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if wait := s.cfg.Limiter.Wait(ip); wait > 0 {
		tooMany(w, wait)
		return
	}
	var req struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if !s.cfg.Store.Verify(req.Current) {
		s.cfg.Limiter.Fail(ip)
		s.cfg.Log.Warn("auth: failed login from " + ip)
		writeError(w, http.StatusForbidden, "The current password is not correct.")
		return
	}
	if err := s.cfg.Store.SetPassword(req.New); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.cfg.Log.Info("auth: password changed", "ip", ip)
	// End all sessions, this one gets a new one
	s.cfg.Sessions.DeleteAll()
	s.setSessionCookie(w, s.cfg.Sessions.Create(ip))
	writeJSON(w, map[string]any{"ok": true, "message": "Password changed. All other sessions have been signed out."})
}

func (s *Server) handleLogoutAll(w http.ResponseWriter, r *http.Request) {
	s.cfg.Sessions.DeleteAll()
	s.setSessionCookie(w, s.cfg.Sessions.Create(clientIP(r)))
	writeJSON(w, map[string]any{"ok": true, "message": "All other sessions have been signed out."})
}

var webhookTokenRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if !webhookTokenRe.MatchString(token) {
		writeError(w, http.StatusNotFound, "Unknown webhook.")
		return
	}
	if err := s.call(r, "docker.webhook.fire", map[string]string{"token": token}, nil); err != nil {
		writeError(w, http.StatusConflict, "The webhook could not be carried out.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
