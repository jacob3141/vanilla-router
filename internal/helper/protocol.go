// SPDX-License-Identifier: AGPL-3.0-or-later

// Package helper ist der privilegierte Teil der Oberfläche. Er läuft als root, lauscht auf
// einem Unix-Socket und führt nur fest registrierte Aktionen aus. Jede Aktion prüft ihre
// Parameter selbst, unabhängig davon, was der Webserver schon geprüft hat.
package helper

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"
)

// Eine Verbindung trägt genau eine Anfrage und eine Antwort, je als eine JSON-Zeile.
type Request struct {
	Action string          `json:"action"`
	Params json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	OK    bool            `json:"ok"`
	Error string          `json:"error,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
}

const maxMessage = 1 << 20

// UserError ist ein Fehler, dessen Text so in der Oberfläche angezeigt werden darf.
// Alle anderen Fehler erscheinen dort nur als „interner Fehler“, Details stehen im Journal.
type UserError struct{ Msg string }

func (e *UserError) Error() string { return e.Msg }

func Userf(format string, a ...any) error { return &UserError{Msg: fmt.Sprintf(format, a...)} }

// Client spricht mit dem Helper.
type Client struct {
	Socket  string
	Timeout time.Duration
}

// Call führt eine Aktion aus. out darf nil sein.
func (c *Client) Call(ctx context.Context, action string, params any, out any) error {
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", c.Socket)
	if err != nil {
		return fmt.Errorf("Helper nicht erreichbar: %w", err)
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		conn.SetDeadline(dl)
	}

	req := Request{Action: action}
	if params != nil {
		if req.Params, err = json.Marshal(params); err != nil {
			return err
		}
	}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return err
	}
	r := bufio.NewReader(&limitedReader{conn, maxMessage})
	var resp Response
	if err := json.NewDecoder(r).Decode(&resp); err != nil {
		return fmt.Errorf("Antwort des Helpers unlesbar: %w", err)
	}
	if !resp.OK {
		return &UserError{Msg: resp.Error}
	}
	if out != nil && len(resp.Data) > 0 {
		return json.Unmarshal(resp.Data, out)
	}
	return nil
}

type limitedReader struct {
	r net.Conn
	n int
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.n <= 0 {
		return 0, errors.New("Nachricht zu groß")
	}
	if len(p) > l.n {
		p = p[:l.n]
	}
	n, err := l.r.Read(p)
	l.n -= n
	return n, err
}
