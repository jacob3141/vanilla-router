// SPDX-License-Identifier: AGPL-3.0-or-later

package probe

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"net"
	"strconv"
	"time"
)

// Options steuert eine Probe.
type Options struct {
	Timeout  time.Duration // Gesamtdauer, Vorgabe 5 s
	Interval time.Duration // Anfragen so oft wiederholen, Vorgabe 1 s
	Grace    time.Duration // nach der ersten Antwort noch so lange auf weitere warten, Vorgabe 500 ms
	VLANs    []int         // PPPoE-Discovery auf diesen VLANs, 0 = ohne Tag. Vorgabe {0, 7} (7: Telekom)
	NoDHCP   bool
	NoPPPoE  bool
}

func (o Options) withDefaults() Options {
	if o.Timeout <= 0 {
		o.Timeout = 5 * time.Second
	}
	if o.Interval <= 0 {
		o.Interval = time.Second
	}
	if o.Grace <= 0 {
		o.Grace = 500 * time.Millisecond
	}
	if o.VLANs == nil {
		o.VLANs = []int{0, 7}
	}
	return o
}

// Result ist das Ergebnis einer Probe.
type Result struct {
	DHCP  *Offer
	PPPoE []PADO // höchstens ein Eintrag je VLAN und Zugangsserver
}

// Answered meldet, ob irgendetwas geantwortet hat.
func (r Result) Answered() bool { return r.DHCP != nil || len(r.PPPoE) > 0 }

// conn ist die Socket-Schnittstelle (in Tests ersetzt).
type conn interface {
	Send([]byte) error
	Recv(time.Duration) ([]byte, error)
	Close() error
}

// Run probiert auf dem Interface (Index, MAC) DHCP und PPPoE. Es wird keine Adresse übernommen
// und kein Zustand im System verändert. Das Interface muss eingeschaltet sein (netdetect.SetUp).
func Run(ctx context.Context, ifindex int, mac net.HardwareAddr, opts Options) (Result, error) {
	c, err := openRaw(ifindex)
	if err != nil {
		return Result{}, err
	}
	defer c.Close()
	return run(ctx, c, mac, opts.withDefaults(), time.Now)
}

func run(ctx context.Context, c conn, mac net.HardwareAddr, o Options, now func() time.Time) (Result, error) {
	var rnd [12]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return Result{}, err
	}
	xid := binary.BigEndian.Uint32(rnd[0:4])
	ipID := binary.BigEndian.Uint16(rnd[4:6])
	hostUniq := rnd[4:12]

	var res Result
	start := now()
	end := start.Add(o.Timeout)
	var nextSend time.Time
	seen := map[string]bool{}

	for {
		t := now()
		if !t.Before(end) {
			return res, nil
		}
		if err := ctx.Err(); err != nil {
			return res, err
		}
		if !t.Before(nextSend) {
			if !o.NoDHCP && res.DHCP == nil {
				if err := c.Send(BuildDiscoverFrame(mac, xid, ipID)); err != nil {
					return res, err
				}
			}
			if !o.NoPPPoE {
				for _, v := range o.VLANs {
					if err := c.Send(BuildPADIFrame(mac, v, hostUniq)); err != nil {
						return res, err
					}
				}
			}
			nextSend = t.Add(o.Interval)
		}
		wait := min(nextSend.Sub(t), end.Sub(t), 250*time.Millisecond)
		frame, err := c.Recv(wait)
		if err != nil {
			return res, err
		}
		if frame == nil {
			continue
		}
		before := res.Answered()
		if !o.NoDHCP && res.DHCP == nil {
			if off, err := ParseOfferFrame(frame, mac, xid); err == nil {
				res.DHCP = &off
			}
		}
		if !o.NoPPPoE {
			if p, err := ParsePADOFrame(frame, mac, hostUniq); err == nil {
				key := p.ACMAC.String() + "/" + strconv.Itoa(p.VLAN)
				if !seen[key] {
					seen[key] = true
					res.PPPoE = append(res.PPPoE, p)
				}
			}
		}
		if !before && res.Answered() {
			if g := now().Add(o.Grace); g.Before(end) {
				end = g
			}
		}
	}
}
