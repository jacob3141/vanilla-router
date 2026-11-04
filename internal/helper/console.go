// SPDX-License-Identifier: AGPL-3.0-or-later

package helper

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"vanilla-router/internal/config"
)

// IssueFile zeigt agetty vor der Anmeldung auf der Konsole an (util-linux liest /run/issue.d).
const IssueFile = "/run/issue.d/50-vanilla-router.issue"

// WriteIssue schreibt Adresse und, solange die Oberfläche nicht eingerichtet ist, den
// Einrichtungscode auf die Konsole des Routers (F-INST-17). Wer einen Bildschirm anschließt, sieht
// so ohne Anmeldung, wie es weitergeht. code leer = schon eingerichtet.
func WriteIssue(path, fqdn, ip, code string) error {
	var b strings.Builder
	b.WriteString("\n  vanilla-router\n\n")
	fmt.Fprintf(&b, "  Weboberfläche:  https://%s   (oder https://%s)\n", fqdn, ip)
	if code != "" {
		fmt.Fprintf(&b, "  Einrichtungscode für die erste Anmeldung:  %s\n", code)
		b.WriteString("  Öffnen Sie die Adresse auf einem Gerät im Heimnetz.\n")
	}
	b.WriteString("\n")
	// „\“ ist für agetty ein Steuerzeichen. Name, Adresse und Code sind geprüft und enthalten es nie.
	if strings.Contains(b.String(), `\`) {
		return fmt.Errorf("unerwartetes Zeichen")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := config.WriteFileAtomic(path, []byte(b.String()), 0o644); err != nil {
		return err
	}
	exec.Command("agetty", "--reload").Run() // laufende Anmeldeaufforderungen neu zeichnen
	return nil
}

func (s *Server) registerConsole() {
	// Nach dem Setzen des ersten Passworts: Code von der Konsole nehmen, er gilt nicht mehr.
	s.handle("console.setup_done", func(ctx context.Context, _ json.RawMessage) (any, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		c, err := s.loadConfig()
		if err != nil {
			return nil, err
		}
		_, router, _ := c.LANPrefix()
		return nil, WriteIssue(s.Path(IssueFile), c.System.Hostname+"."+c.DNS.Domain, router.String(), "")
	})
}
