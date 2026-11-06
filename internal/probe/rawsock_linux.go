// SPDX-License-Identifier: AGPL-3.0-or-later

package probe

import (
	"encoding/binary"
	"errors"
	"fmt"
	"syscall"
	"time"
)

const (
	ethPAll           = 0x0003
	solPacket         = 263
	packetAuxdata     = 8
	packetOutgoing    = 4
	tpStatusVLANValid = 1 << 4
)

func htons(v uint16) uint16 { return v<<8 | v>>8 }

// rawConn is an AF_PACKET/SOCK_RAW socket on exactly one interface. It sees all frames,
// also with a VLAN tag, without a VLAN interface having to be created. Needs CAP_NET_RAW.
type rawConn struct {
	fd      int
	ifindex int
	buf     []byte
	oob     []byte
}

func openRaw(ifindex int) (*rawConn, error) {
	fd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_RAW|syscall.SOCK_CLOEXEC, int(htons(ethPAll)))
	if err != nil {
		return nil, fmt.Errorf("AF_PACKET socket (root required): %w", err)
	}
	if err := syscall.SetsockoptInt(fd, solPacket, packetAuxdata, 1); err != nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("PACKET_AUXDATA: %w", err)
	}
	if err := syscall.Bind(fd, &syscall.SockaddrLinklayer{Protocol: htons(ethPAll), Ifindex: ifindex}); err != nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("Bind an Interface %d: %w", ifindex, err)
	}
	return &rawConn{fd: fd, ifindex: ifindex, buf: make([]byte, 9216), oob: make([]byte, 256)}, nil
}

func (c *rawConn) Send(frame []byte) error {
	if len(frame) < 14 {
		return errShort
	}
	sa := &syscall.SockaddrLinklayer{Ifindex: c.ifindex, Halen: 6, Protocol: htons(binary.BigEndian.Uint16(frame[12:14]))}
	copy(sa.Addr[:], frame[0:6])
	return syscall.Sendto(c.fd, frame, 0, sa)
}

// Recv returns the next received (not self-sent) frame or (nil, nil) after timeout.
// If the driver removed the VLAN tag (VLAN offload), it is put back from PACKET_AUXDATA.
func (c *rawConn) Recv(timeout time.Duration) ([]byte, error) {
	if timeout <= 0 {
		timeout = time.Millisecond
	}
	tv := syscall.NsecToTimeval(timeout.Nanoseconds())
	if err := syscall.SetsockoptTimeval(c.fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	for {
		n, oobn, _, from, err := syscall.Recvmsg(c.fd, c.buf, c.oob, 0)
		if err != nil {
			if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EINTR) {
				if time.Now().Before(deadline) && errors.Is(err, syscall.EINTR) {
					continue
				}
				return nil, nil
			}
			return nil, err
		}
		if ll, ok := from.(*syscall.SockaddrLinklayer); ok && ll.Pkttype == packetOutgoing {
			if time.Now().After(deadline) {
				return nil, nil
			}
			continue
		}
		frame := append([]byte(nil), c.buf[:n]...)
		if tci, ok := vlanFromAux(c.oob[:oobn]); ok {
			frame = insertVLAN(frame, tci)
		}
		return frame, nil
	}
}

func vlanFromAux(oob []byte) (uint16, bool) {
	msgs, err := syscall.ParseSocketControlMessage(oob)
	if err != nil {
		return 0, false
	}
	for _, m := range msgs {
		if m.Header.Level != solPacket || m.Header.Type != packetAuxdata || len(m.Data) < 20 {
			continue
		}
		// struct tpacket_auxdata { u32 status, len, snaplen; u16 mac, net, vlan_tci, vlan_tpid; }
		status := binary.NativeEndian.Uint32(m.Data[0:4])
		tci := binary.NativeEndian.Uint16(m.Data[16:18])
		if status&tpStatusVLANValid != 0 && tci&0x0fff != 0 {
			return tci, true
		}
	}
	return 0, false
}

func (c *rawConn) Close() error { return syscall.Close(c.fd) }
