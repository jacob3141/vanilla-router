// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"net/http"
	"os"
)

// AuditScript ist das Prüfskript der Erweiterung Sicherheits-Audit (F-SEC-08).
const AuditScript = "/usr/lib/vanilla-router/audit-collect.sh"

// features meldet, welche Erweiterungen installiert sind. Nebenrollen erscheinen in der Oberfläche
// nur, wenn es sie gibt (PLAN.md 3.8).
func (s *Server) features() map[string]bool {
	exists := func(p string) bool { _, err := os.Stat(p); return p != "" && err == nil }
	return map[string]bool{
		"containers": exists("/usr/bin/docker"),
		"files":      exists(s.cfg.Files),
		"audit":      exists(AuditScript),
	}
}

// handleAbout liefert Version, Lizenz und den Ort des Quelltexts (F-UI-07, AGPL §13).
func (s *Server) handleAbout(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{
		"name":    "vanilla-router",
		"version": s.cfg.Version,
		"license": "AGPL-3.0-or-later",
		"source":  s.cfg.SourceURL,
		"notice": "Freie Software unter der GNU Affero General Public License, Version 3 oder später. " +
			"Ohne Gewährleistung. Den Quelltext genau dieser Fassung gibt es unter der angegebenen Adresse.",
	})
}
