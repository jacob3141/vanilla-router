// SPDX-License-Identifier: AGPL-3.0-or-later

package helper

import (
	"os"
	"strings"
	"testing"

	"vanilla-router/internal/gen"
)

func TestFileShareSwitch(t *testing.T) {
	s := newDryServer(t, nil)
	if m, err := call(t, s, "fileshare.get", nil); err != nil || m["enabled"] != false {
		t.Fatalf("%v %v", m, err)
	}
	if _, err := os.Stat(s.Path(gen.FileShareConf)); !os.IsNotExist(err) {
		t.Fatal("configuration generated while off")
	}
	if _, err := call(t, s, "fileshare.set", map[string]any{"enabled": true, "password": "short"}); err == nil {
		t.Fatal("short password accepted")
	}
	if _, err := call(t, s, "fileshare.set", map[string]any{"enabled": true, "password": "a\nlong password"}); err == nil {
		t.Fatal("line break accepted")
	}
	if _, err := call(t, s, "fileshare.set", map[string]any{"enabled": true, "password": "a good password"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(s.Path(gen.FileShareConf))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"hosts allow = 127.0.0.1 192.168.42.0/24", "server min protocol = SMB3", "smb encrypt = required", "valid users = smbuser", "path = /media/shares/guest"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("missing %q:\n%s", want, b)
		}
	}
	if strings.Contains(string(b), "a good password") {
		t.Error("password in the configuration")
	}
	raw, _ := os.ReadFile(s.Path("/etc/vanilla-router/config.json"))
	if strings.Contains(string(raw), "a good password") {
		t.Error("password in config.json")
	}
	if _, err := call(t, s, "fileshare.set", map[string]any{"enabled": false}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.Path(gen.FileShareConf)); !os.IsNotExist(err) {
		t.Error("configuration left behind")
	}
}
