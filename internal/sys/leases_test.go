// SPDX-License-Identifier: AGPL-3.0-or-later

package sys

import (
	"strings"
	"testing"
	"time"
)

const leasesSample = `1793736000 02:00:00:00:00:10 192.168.42.20 drucker 01:02:00:00:00:00:10
1793700000 02:00:00:00:00:11 192.168.42.101 * *
0 02:00:00:00:00:12 192.168.42.102 nas *
duid 00:01:00:01:2c:00:00:00:02:00:00:00:00:01
1793736000 1234 fd12::5 laptop 00:01
`

func TestParseLeases(t *testing.T) {
	now := time.Unix(1793710000, 0)
	ls, err := parseLeases(strings.NewReader(leasesSample), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(ls) != 3 {
		t.Fatalf("%d Leases: %+v", len(ls), ls)
	}
	if l := ls[0]; l.MAC != "02:00:00:00:00:10" || l.IP != "192.168.42.20" || l.Hostname != "drucker" || l.State != "active" {
		t.Errorf("%+v", l)
	}
	if l := ls[1]; l.Hostname != "" || l.State != "expired" {
		t.Errorf("abgelaufene Lease ohne Namen: %+v", l)
	}
	if l := ls[2]; !l.Ends.IsZero() || l.State != "active" {
		t.Errorf("unendliche Lease: %+v", l)
	}
}
