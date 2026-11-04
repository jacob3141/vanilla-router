// SPDX-License-Identifier: AGPL-3.0-or-later

package netdetect

import (
	"errors"
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

// LinkEvent ist eine Zustandsmeldung des Kernels zu einem Interface (RTM_NEWLINK/RTM_DELLINK).
type LinkEvent struct {
	Index     int
	Name      string
	Up        bool // IFF_UP (administrativ an)
	LowerUp   bool // IFF_LOWER_UP: Kabel steckt, Gegenstelle aktiv
	Carrier   Tri  // IFLA_CARRIER, falls mitgeschickt
	OperState string
	Removed   bool // RTM_DELLINK, z. B. USB-Adapter abgezogen
}

// HasLink fasst die Signale zusammen. IFLA_CARRIER hat Vorrang, sonst IFF_LOWER_UP.
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

// ParseLinkMessages wertet einen von Netlink gelesenen Puffer aus. Nachrichten anderer Typen
// werden übersprungen. Ausgelagert, damit es ohne Kernel testbar ist.
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
			return nil, errors.New("netlink: ifinfomsg zu kurz")
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

// LinkWatcher liest Link-Ereignisse aus der Netlink-Gruppe RTMGRP_LINK. Braucht keine Root-Rechte.
type LinkWatcher struct {
	fd int
}

// NewLinkWatcher öffnet den Netlink-Socket. Zuerst öffnen, dann den Anfangszustand lesen
// (ListPorts), damit kein Ereignis dazwischen verloren geht.
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

// Read wartet höchstens timeout auf Ereignisse. Ohne Ereignis kommt (nil, nil) zurück.
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
			// Puffer übergelaufen, Ereignisse fehlen. Der Aufrufer muss den Zustand neu lesen.
			return nil, ErrResync
		}
		return nil, err
	}
	return ParseLinkMessages(buf[:n])
}

// ErrResync bedeutet, dass Ereignisse verloren gingen und der Zustand neu gelesen werden muss.
var ErrResync = errors.New("netlink: Ereignisse verloren, Zustand neu lesen")

// Close schließt den Socket.
func (w *LinkWatcher) Close() error { return syscall.Close(w.fd) }

// SetUp schaltet ein Interface administrativ ein (wie `ip link set <name> up`), ohne Adresse.
// Erst dann meldet der Treiber, ob ein Kabel steckt. Braucht CAP_NET_ADMIN.
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
		return fmt.Errorf("%s: Flags lesen: %w", name, e)
	}
	if req.Flags&iffUp != 0 {
		return nil
	}
	req.Flags |= iffUp
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.SIOCSIFFLAGS, uintptr(unsafe.Pointer(&req))); e != 0 {
		return fmt.Errorf("%s: einschalten: %w", name, e)
	}
	return nil
}
