// SPDX-License-Identifier: AGPL-3.0-or-later

package apply

import (
	"fmt"
	"strings"
	"time"

	"vanilla-router/internal/config"
	"vanilla-router/internal/gen"
)

// HealthCheck checks once whether the router does its basic job: firewall loaded, DNS/DHCP running,
// home network has its address. The internet is deliberately not checked (the modem may be off).
func HealthCheck(r Runner, c *config.Config) error {
	if _, err := r.Run(nil, "nft", "list", "table", "inet", "vr_filter"); err != nil {
		return fmt.Errorf("firewall is not loaded")
	}
	if _, err := r.Run(nil, "systemctl", "is-active", "--quiet", "vr-dnsmasq.service"); err != nil {
		return fmt.Errorf("DNS/DHCP service is not running")
	}
	out, err := r.Run(nil, "ip", "-4", "-o", "addr", "show", "dev", gen.LANIf)
	if err != nil || !strings.Contains(string(out), c.LAN.IPv4) {
		return fmt.Errorf("home network address %s is missing on %s", c.LAN.IPv4, gen.LANIf)
	}
	return nil
}

// WaitHealthy repeats HealthCheck until it succeeds or until timeout.
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
