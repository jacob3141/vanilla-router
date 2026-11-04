// SPDX-License-Identifier: AGPL-3.0-or-later

package apply

import (
	"fmt"
	"strings"
	"time"

	"vanilla-router/internal/config"
	"vanilla-router/internal/gen"
)

// HealthCheck prüft einmal, ob der Router seine Grundaufgabe erfüllt: Firewall geladen, DNS/DHCP läuft,
// Heimnetz hat seine Adresse. Das Internet wird bewusst nicht geprüft (Modem kann aus sein).
func HealthCheck(r Runner, c *config.Config) error {
	if _, err := r.Run(nil, "nft", "list", "table", "inet", "vr_filter"); err != nil {
		return fmt.Errorf("Firewall ist nicht geladen")
	}
	if _, err := r.Run(nil, "systemctl", "is-active", "--quiet", "vr-dnsmasq.service"); err != nil {
		return fmt.Errorf("DNS/DHCP-Dienst läuft nicht")
	}
	out, err := r.Run(nil, "ip", "-4", "-o", "addr", "show", "dev", gen.LANIf)
	if err != nil || !strings.Contains(string(out), c.LAN.IPv4) {
		return fmt.Errorf("Heimnetz-Adresse %s fehlt an %s", c.LAN.IPv4, gen.LANIf)
	}
	return nil
}

// WaitHealthy wiederholt HealthCheck bis zum Erfolg oder bis timeout.
func WaitHealthy(r Runner, c *config.Config, timeout time.Duration, sleep func(time.Duration)) error {
	if sleep == nil {
		sleep = time.Sleep
	}
	deadline := time.Now().Add(timeout)
	for {
		err := HealthCheck(r, c)
		if err == nil || time.Now().After(deadline) {
			return err
		}
		sleep(5 * time.Second)
	}
}
