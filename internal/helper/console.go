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
	"vanilla-router/internal/i18n"

	"vanilla-router/internal/config"
)

// IssueFile is shown by agetty before login on the console (util-linux reads /run/issue.d).
const IssueFile = "/run/issue.d/50-vanilla-router.issue"

// WriteIssue writes the address and, as long as the web interface is not set up, the
// setup code to the router's console (F-INST-17). Anyone who connects a screen thus sees
// how to continue, without logging in. Empty code = already set up.
func WriteIssue(path, fqdn, ip, code, locale string) error {
	i18n.SetLocale(locale)
	var b strings.Builder
	b.WriteString("\n  vanilla-router\n\n")
	b.WriteString(i18n.T(fmt.Sprintf("  Web interface:  https://%s   (or https://%s)\n", fqdn, ip)))
	if code != "" {
		b.WriteString(i18n.T(fmt.Sprintf("  Setup code for the first login:  %s\n", code)))
		b.WriteString(i18n.T("  Open this address on a device in your home network.\n"))
	}
	b.WriteString("\n")
	// "\" is a control character for agetty. Name, address and code are validated and never contain it.
	if strings.Contains(b.String(), `\`) {
		return fmt.Errorf("unexpected character")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := config.WriteFileAtomic(path, []byte(b.String()), 0o644); err != nil {
		return err
	}
	exec.Command("agetty", "--reload").Run() // redraw running login prompts
	return nil
}

func (s *Server) registerConsole() {
	// After the first password is set: take the code off the console, it is no longer valid.
	s.handle("console.setup_done", func(ctx context.Context, _ json.RawMessage) (any, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		c, err := s.loadConfig()
		if err != nil {
			return nil, err
		}
		_, router, _ := c.LANPrefix()
		return nil, WriteIssue(s.Path(IssueFile), c.System.Hostname+"."+c.DNS.Domain, router.String(), "", c.System.Locale)
	})
}
