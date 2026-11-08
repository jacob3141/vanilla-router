// SPDX-License-Identifier: AGPL-3.0-or-later

package helper

import (
	"os"
	"strings"
	"testing"
)

const sshTestKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBeispielSchluesselNurFuerTests admin@pc"

func TestSSHSwitch(t *testing.T) {
	s := newDryServer(t, nil)
	if m, err := call(t, s, "ssh.get", nil); err != nil || m["enabled"] != false {
		t.Fatalf("%v %v", m, err)
	}
	if _, err := call(t, s, "ssh.set", map[string]any{"enabled": true, "keys": []string{}}); err == nil {
		t.Fatal("SSH without a key accepted")
	}
	if _, err := call(t, s, "ssh.set", map[string]any{"enabled": true, "keys": []string{"not a key"}}); err == nil {
		t.Fatal("invalid key accepted")
	}
	if _, err := call(t, s, "ssh.set", map[string]any{"enabled": true, "keys": []string{"  " + sshTestKey + " ", ""}}); err != nil {
		t.Fatal(err)
	}
	c := loadCfg(t, s)
	if !c.Services.SSH.Enabled || len(c.Services.SSH.AuthorizedKeys) != 1 || c.Services.SSH.AuthorizedKeys[0] != sshTestKey {
		t.Fatalf("%+v", c.Services.SSH)
	}
	keys, err := os.ReadFile(s.Path("/etc/vanilla-router/ssh/admin.keys"))
	if err != nil || !strings.Contains(string(keys), sshTestKey) {
		t.Fatalf("%s %v", keys, err)
	}
	fi, err := os.Stat(s.Path("/etc/sudoers.d/vr-admin"))
	if err != nil || fi.Mode().Perm() != 0o440 {
		t.Fatalf("sudoers: %v %v", fi, err)
	}
	// Off again: the sudo rule goes away with it
	if _, err := call(t, s, "ssh.set", map[string]any{"enabled": false, "keys": []string{sshTestKey}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.Path("/etc/sudoers.d/vr-admin")); !os.IsNotExist(err) {
		t.Error("sudo rule left behind")
	}
}
