// SPDX-License-Identifier: AGPL-3.0-or-later

package helper

import "testing"

func TestValidTarget(t *testing.T) {
	for _, ok := range []string{"8.8.8.8", "2001:4860:4860::8888", "ubuntu.com", "drucker.home.arpa", "localhost", "ubuntu.com."} {
		if _, err := validTarget(ok); err != nil {
			t.Errorf("%q abgelehnt: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "-f", "--help", "-c100 8.8.8.8", "8.8.8.8;id", "a b", "$(id)", "`id`", "a..b", ".ubuntu.com", "x/../y", "ubuntu.com\n-f"} {
		if _, err := validTarget(bad); err == nil {
			t.Errorf("%q akzeptiert", bad)
		}
	}
}

func TestLastLinesSkipsBanner(t *testing.T) {
	out := "Internet Systems Consortium DHCP Server 4.4.3\nCopyright 2004-2022 Internet Systems Consortium.\nAll rights reserved.\nFor info, please visit https://www.isc.org/software/dhcp/\n/etc/dhcp/x.conf line 3: semicolon expected.\nConfiguration file errors encountered -- exiting\n"
	got := lastLines(out, 2)
	if got != "/etc/dhcp/x.conf line 3: semicolon expected. · Configuration file errors encountered -- exiting" {
		t.Errorf("lastLines = %q", got)
	}
}
