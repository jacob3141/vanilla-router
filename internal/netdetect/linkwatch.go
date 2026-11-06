// SPDX-License-Identifier: AGPL-3.0-or-later

package netdetect

import (
	"errors"
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

// LinkEvent is a state message from the kernel about an interface (RTM_NEWLINK/RTM_DELLINK).
type LinkEvent struct {
	Index     int
	Name      string
	Up        bool // IFF_UP (administratively on)
	LowerUp   bool // IFF_LOWER_UP: cable plugged in, other end active
	Carrier   Tri  // IFLA_CARRIER, if sent along
	OperState string
	Removed   bool // RTM_DELLINK, e.g. USB adapter unplugged
}

// HasLink combines the signals. IFLA_CARRIER takes precedence, otherwise IFF_LOWER_UP.
func (e LinkEvent) HasLink() bool {
	if e.Removed {
		return false
	}
	if e.Carrier != Unknown {
		return e.Carrier == Yes
	}
	return e.LowerUp
}

const (
	iffUp      = 0x1
	iffLowerUp = 0x10000

	iflaIfname    = 3
	iflaOperstate = 16
	iflaCarrier   = 33

	rtmgrpLink = 0x1
)

var operStates = map[uint8]string{0: "unknown", 1: "notpresent", 2: "down", 3: "lowerlayerdown",
	4: "testing", 5: "dormant", 6: "up"}

// ParseLinkMessages evaluates a buffer read from netlink. Messages of other types
// are skipped. Separated out, so it can be tested without a kernel.
func ParseLinkMessages(buf []byte) ([]LinkEvent, error) {
	msgs, err := syscall.ParseNetlinkMessage(buf)
	if err != nil {
		return nil, err
	}
	var evs []LinkEvent
	for _, m := range msgs {
		if m.Header.Type != syscall.RTM_NEWLINK && m.Header.Type != syscall.RTM_DELLINK {
			continue
		}
		if len(m.Data) < syscall.SizeofIfInfomsg {
			return nil, errors.New("netlink: ifinfomsg too short")
		}
		info := (*syscall.IfInfomsg)(unsafe.Pointer(&m.Data[0]))
		ev := LinkEvent{
			Index:   int(info.Index),
			Up:      info.Flags&iffUp != 0,
			LowerUp: info.Flags&iffLowerUp != 0,
			Removed: m.Header.Type == syscall.RTM_DELLINK,
		}
		attrs, err := syscall.ParseNetlinkRouteAttr(&m)
		if err != nil {
			return nil, err
		}
		for _, a := range attrs {
			switch a.Attr.Type {
			case iflaIfname:
				ev.Name = cString(a.Value)
			case iflaOperstate:
				if len(a.Value) > 0 {
					ev.OperState = operStates[a.Value[0]]
				}
			case iflaCarrier:
				if len(a.Value) > 0 {
					ev.Carrier = No
					if a.Value[0] != 0 {
						ev.Carrier = Yes
					}
				}
			}
		}
		evs = append(evs, ev)
	}
	return evs, nil
}

func cString(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

// LinkWatcher reads link events from the netlink group RTMGRP_LINK. Needs no root.
type LinkWatcher struct {
	fd int
}

// NewLinkWatcher opens the netlink socket. Open first, then read the initial state
// (ListPorts), so no event in between gets lost.
func NewLinkWatcher() (*LinkWatcher, error) {
	fd, err := syscall.Socket(syscall.AF_NETLINK, syscall.SOCK_RAW|syscall.SOCK_CLOEXEC, syscall.NETLINK_ROUTE)
	if err != nil {
		return nil, fmt.Errorf("netlink-Socket: %w", err)
	}
	if err := syscall.Bind(fd, &syscall.SockaddrNetlink{Family: syscall.AF_NETLINK, Groups: rtmgrpLink}); err != nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("netlink-Bind: %w", err)
	}
	return &LinkWatcher{fd: fd}, nil
}

// Read waits at most timeout for events. Without an event it returns (nil, nil).
func (w *LinkWatcher) Read(timeout time.Duration) ([]LinkEvent, error) {
	tv := syscall.NsecToTimeval(timeout.Nanoseconds())
	if tv.Sec == 0 && tv.Usec == 0 {
		tv.Usec = 1
	}
	if err := syscall.SetsockoptTimeval(w.fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv); err != nil {
		return nil, err
	}
	buf := make([]byte, 64*1024)
	n, _, err := syscall.Recvfrom(w.fd, buf, 0)
	if err != nil {
		if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EINTR) {
			return nil, nil
		}
		if errors.Is(err, syscall.ENOBUFS) {
			// Buffer overflowed, events are missing. The caller must read the state again.
			return nil, ErrResync
		}
		return nil, err
	}
	return ParseLinkMessages(buf[:n])
}

// ErrResync means that events were lost and the state must be read again.
var ErrResync = errors.New("netlink: events lost, read state again")

// Close closes the socket.
func (w *LinkWatcher) Close() error { return syscall.Close(w.fd) }

// SetUp turns an interface on administratively (like `ip link set <name> up`), without an address.
// Only then does the driver report whether a cable is plugged in. Needs CAP_NET_ADMIN.
func SetUp(name string) error {
	if len(name) >= syscall.IFNAMSIZ {
		return fmt.Errorf("Interface-Name zu lang: %q", name)
	}
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer syscall.Close(fd)
	var req struct {
		Name  [syscall.IFNAMSIZ]byte
		Flags uint16
		_     [22]byte
	}
	copy(req.Name[:], name)
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.SIOCGIFFLAGS, uintptr(unsafe.Pointer(&req))); e != 0 {
		return fmt.Errorf("%s: reading flags: %w", name, e)
	}
	if req.Flags&iffUp != 0 {
		return nil
	}
	req.Flags |= iffUp
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.SIOCSIFFLAGS, uintptr(unsafe.Pointer(&req))); e != 0 {
		return fmt.Errorf("%s: turning on: %w", name, e)
	}
	return nil
}
