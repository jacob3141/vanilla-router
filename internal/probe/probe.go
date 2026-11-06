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

// Options controls a probe.
type Options struct {
	Timeout  time.Duration // total duration, default 5 s
	Interval time.Duration // repeat requests this often, default 1 s
	Grace    time.Duration // after the first answer, wait this long for more, default 500 ms
	VLANs    []int         // PPPoE discovery on these VLANs, 0 = without tag. Default {0, 7} (7: Telekom)
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

// Result is the result of a probe.
type Result struct {
	DHCP  *Offer
	PPPoE []PADO // at most one entry per VLAN and access server
}

// Answered reports whether anything answered.
func (r Result) Answered() bool { return r.DHCP != nil || len(r.PPPoE) > 0 }

// conn is the socket interface (replaced in tests).
type conn interface {
	Send([]byte) error
	Recv(time.Duration) ([]byte, error)
	Close() error
}

// Run tries DHCP and PPPoE on the interface (index, MAC). No address is taken
// and no state in the system changes. The interface must be turned on (netdetect.SetUp).
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
