// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"net/http"
	"os"

	"vanilla-router/internal/gen"
	"vanilla-router/internal/sys"
)

// AuditScript is the check script of the security audit extension (F-SEC-08).
const AuditScript = "/usr/lib/vanilla-router/audit-collect.sh"

// features reports which extensions are installed. Secondary roles only appear in the web interface
// if they exist (PLAN.md 3.8).
func (s *Server) features() map[string]bool {
	exists := func(p string) bool { _, err := os.Stat(p); return p != "" && err == nil }
	return map[string]bool{
		"containers": exists("/usr/bin/docker"),
		"files":      exists(s.cfg.Files),
		"home":       exists(s.cfg.Home),
		"audit":      exists(AuditScript),
		// An access point that is set up stays visible even if its adapter is gone, so the page can explain that.
		"wifi": s.cfg.FakeWiFi || sys.WiFiPresent("/") || exists(gen.WiFiConf),
	}
}

// handleAbout returns the version, license and location of the source (F-UI-07, AGPL §13).
func (s *Server) handleAbout(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{
		"name":    "vanilla-router",
		"version": s.cfg.Version,
		"license": "AGPL-3.0-or-later",
		"source":  s.cfg.SourceURL,
		"notice": "Free software under the GNU Affero General Public License, version 3 or later. " +
			"No warranty. The source of exactly this version is available at the given address.",
	})
}
