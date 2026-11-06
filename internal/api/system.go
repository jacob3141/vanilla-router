// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"net/http"
	"strconv"
)

func (s *Server) registerSystem() {
	s.route("GET /api/services", s.get("services.list"))
	s.route("POST /api/services/restart", s.forward("service.restart", "Service restarted."))
	s.route("GET /api/services/logs", s.withQuery("service.logs.get", "unit"))

	s.route("GET /api/containers", s.get("docker.list"))
	s.route("POST /api/containers/action", s.forward("docker.action", "Done."))
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
