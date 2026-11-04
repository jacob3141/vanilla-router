// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import "net/http"

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
