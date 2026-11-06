// SPDX-License-Identifier: AGPL-3.0-or-later

// Package helper is the privileged part of the web interface. It runs as root, listens on
// a Unix socket and only executes actions registered in a fixed list. Each action checks its
// parameters itself, regardless of what the web server has already checked.
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

// A connection carries exactly one request and one response, each as one JSON line.
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

// UserError is an error whose text may be shown as is in the web interface.
// All other errors only appear there as "internal error", details are in the journal.
type UserError struct{ Msg string }

func (e *UserError) Error() string { return e.Msg }

func Userf(format string, a ...any) error { return &UserError{Msg: fmt.Sprintf(format, a...)} }

// Client talks to the helper.
type Client struct {
	Socket  string
	Timeout time.Duration
}

// Call executes an action. out may be nil.
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
		return fmt.Errorf("helper not reachable: %w", err)
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
		return fmt.Errorf("helper response unreadable: %w", err)
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
		return 0, errors.New("message too big")
	}
	if len(p) > l.n {
		p = p[:l.n]
	}
	n, err := l.r.Read(p)
	l.n -= n
	return n, err
}
