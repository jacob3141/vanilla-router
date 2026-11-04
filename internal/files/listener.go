// SPDX-License-Identifier: AGPL-3.0-or-later

package files

import (
	"log/slog"
	"net"
	"syscall"
)

// PeerListener nimmt nur Verbindungen der erlaubten UID an (SO_PEERCRED) und schließt alle anderen.
type PeerListener struct {
	*net.UnixListener
	AllowUID int
	Log      *slog.Logger
}

func (l *PeerListener) Accept() (net.Conn, error) {
	for {
		c, err := l.UnixListener.AcceptUnix()
		if err != nil {
			return nil, err
		}
		uid := -1
		if raw, err := c.SyscallConn(); err == nil {
			raw.Control(func(fd uintptr) {
				if cred, err := syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED); err == nil {
					uid = int(cred.Uid)
				}
			})
		}
		if uid == l.AllowUID {
			return c, nil
		}
		l.Log.Warn("verbindung abgelehnt", "uid", uid)
		c.Close()
	}
}
