// SPDX-License-Identifier: AGPL-3.0-or-later

package gen

import (
	"fmt"
	"strings"

	"vanilla-router/internal/config"
)

// pppFiles erzeugt die pppd-Konfiguration für PPPoE und die Zugangsdaten (0600).
// Ohne PPPoE werden die Secrets-Dateien leer geschrieben, damit kein altes Passwort liegen bleibt.
func pppFiles(c *config.Config, sec Secrets) ([]File, error) {
	secrets := header("#")
	var fs []File
	if c.WAN.Type == config.WANPPPoE {
		p := c.WAN.PPPoE
		if sec == nil {
			return nil, fmt.Errorf("PPPoE-Passwort %q: keine Geheimnisse verfügbar", p.PasswordRef)
		}
		pw, err := sec.Get(p.PasswordRef)
		if err != nil {
			return nil, fmt.Errorf("PPPoE-Passwort %q: %w", p.PasswordRef, err)
		}
		if pw == "" || len(pw) > 128 || strings.ContainsAny(pw, "\"\\\n\r\x00") {
			return nil, fmt.Errorf("Das PPPoE-Passwort ist leer, zu lang oder enthält \" bzw. \\ (das kann pppd nicht)")
		}
		secrets += fmt.Sprintf("%q * %q *\n", p.User, pw)
		mtu := 1492
		if c.WAN.MTU != 0 && c.WAN.MTU < mtu {
			mtu = c.WAN.MTU
		}
		var b strings.Builder
		b.WriteString(header("#"))
		fmt.Fprintf(&b, `plugin pppoe.so
nic-%s
ifname %s
user %q
noauth
hide-password
persist
maxfail 0
holdoff 10
lcp-echo-interval 10
lcp-echo-failure 6
defaultroute
noipdefault
usepeerdns
+ipv6
mtu %d
mru %d
noaccomp
default-asyncmap
nodeflate
nobsdcomp
novj
`, pppLowerIf(c), PPPIf, p.User, mtu, mtu)
		fs = append(fs, File{Path: "/etc/ppp/peers/" + PPPPeers, Mode: 0o644, Group: GroupPPP, Content: []byte(b.String())})
	}
	fs = append(fs,
		File{Path: "/etc/ppp/chap-secrets", Mode: 0o600, Group: GroupPPP, Content: []byte(secrets)},
		File{Path: "/etc/ppp/pap-secrets", Mode: 0o600, Group: GroupPPP, Content: []byte(secrets)},
	)
	return fs, nil
}

func misc(c *config.Config) []File {
	var fs []File
	add := func(path, group string, mode uint32, content string) {
		fs = append(fs, File{Path: path, Mode: mode, Group: group, Content: []byte(content)})
	}

	add("/etc/sysctl.d/90-vr.conf", GroupSysctl, 0o644, header("#")+`net.ipv4.ip_forward = 1
net.ipv6.conf.all.forwarding = 1
net.ipv4.conf.all.rp_filter = 1
net.ipv4.conf.default.rp_filter = 1
net.ipv4.conf.all.accept_redirects = 0
net.ipv6.conf.all.accept_redirects = 0
net.ipv4.conf.all.send_redirects = 0
net.ipv4.tcp_syncookies = 1
net.netfilter.nf_conntrack_max = 262144
`)

	// resolved bleibt aktiv (Ubuntu-Standard, D-12), gibt aber Port 53 für dnsmasq frei. Bewusst ohne
	// DNS=127.0.0.1: dnsmasq liest seine Upstreams aus /run/systemd/resolve/resolv.conf, das wäre eine
	// Schleife. /etc/resolv.conf zeigt deshalb auf diese Datei statt auf den Stub (apply.EnsureSystem).
	add("/etc/systemd/resolved.conf.d/10-vr.conf", GroupResolved, 0o644, header("#")+`[Resolve]
DNSStubListener=no
`)

	// SSH: Konfiguration immer erzeugen, ob der Dienst läuft, entscheidet apply (F-SEC-04).
	add("/etc/ssh/sshd_config.d/10-vr.conf", GroupSSH, 0o644, header("#")+`PasswordAuthentication no
KbdInteractiveAuthentication no
PermitRootLogin no
AuthorizedKeysFile /etc/vanilla-router/ssh/%u.keys
AllowUsers admin
X11Forwarding no
AllowAgentForwarding no
`)
	keys := header("#")
	for _, k := range c.Services.SSH.AuthorizedKeys {
		keys += k + "\n"
	}
	add("/etc/vanilla-router/ssh/admin.keys", GroupSSH, 0o644, keys)

	auto := "0"
	if c.Updates.SecurityAuto {
		auto = "1"
	}
	add("/etc/apt/apt.conf.d/20auto-upgrades", GroupAPT, 0o644, header("//")+fmt.Sprintf(`APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "%s";
`, auto))
	reboot := "false"
	if c.Updates.RebootWindow != "" {
		reboot = "true"
	}
	add("/etc/apt/apt.conf.d/52vr-unattended-upgrades", GroupAPT, 0o644, header("//")+fmt.Sprintf(`Unattended-Upgrade::Automatic-Reboot "%s";
Unattended-Upgrade::Automatic-Reboot-Time "%s";
Unattended-Upgrade::Remove-Unused-Kernel-Packages "true";
`, reboot, c.Updates.RebootWindow))
	return fs
}
