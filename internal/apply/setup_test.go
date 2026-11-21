// SPDX-License-Identifier: AGPL-3.0-or-later

package apply

import (
	"os"
	"path/filepath"
	"testing"
)

// Setup replaces Ubuntu's link to the stub, but keeps the file that apply wrote (gen.misc): complete
// runs Setup again after apply, and the link would cut containers off from dnsmasq.
func TestSetupResolvConf(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prepare func(p string)
		link    string // expected link target, "" = the regular file stays
	}{
		{"stub link", func(p string) { os.Symlink("../run/systemd/resolve/stub-resolv.conf", p) }, "../run/systemd/resolve/resolv.conf"},
		{"missing", func(string) {}, "../run/systemd/resolve/resolv.conf"},
		{"written by apply", func(p string) { os.WriteFile(p, []byte("nameserver 127.0.0.1\n"), 0o644) }, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			os.MkdirAll(filepath.Join(root, "etc"), 0o755)
			p := filepath.Join(root, "etc", "resolv.conf")
			tc.prepare(p)
			a := &Applier{Root: root, Run: &completeRunner{}}
			a.Setup(false, "")
			target, err := os.Readlink(p)
			if tc.link != "" {
				if err != nil || target != tc.link {
					t.Fatalf("link = %q, %v; want %q", target, err, tc.link)
				}
				return
			}
			if b, err := os.ReadFile(p); err != nil || string(b) != "nameserver 127.0.0.1\n" {
				t.Fatalf("file was changed: %q, %v", b, err)
			}
		})
	}
}
