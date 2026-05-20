package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"time"
)

// dialTimeout caps how long Call waits for the daemon to accept a
// connection. Short — if the socket isn't there, the caller should
// fall back to in-process execution rather than hang.
const dialTimeout = 2 * time.Second

// Available reports whether a daemon appears reachable at socketPath.
// Absence is not an error: callers use this to decide whether to send
// a request or fall back to in-process execution.
func Available(socketPath string) bool {
	if socketPath == "" {
		return false
	}
	if _, err := os.Stat(socketPath); err != nil {
		return false
	}
	conn, err := (&net.Dialer{Timeout: 200 * time.Millisecond}).DialContext(context.Background(), "unix", socketPath)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// Call sends one Request to the daemon at socketPath and decodes the
// Data field of the Response into out (which may be nil for verbs
// that return no payload). When the daemon replies with OK=false the
// Error string is returned as a Go error verbatim — including the
// ErrBinaryHashMismatchPrefix-prefixed string callers can match
// against to decide whether to fall back to in-process execution.
func Call(ctx context.Context, socketPath, verb, clientBinaryHash string, args any, out any) error {
	dialer := &net.Dialer{Timeout: dialTimeout}
	conn, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return fmt.Errorf("daemon: dial %s: %w", socketPath, err)
	}
	defer func() { _ = conn.Close() }()
	// Propagate ctx's deadline to the socket so a daemon that accepts
	// then hangs cannot deadlock the client past the caller's bound.
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	var rawArgs json.RawMessage
	if args != nil {
		buf, err := json.Marshal(args)
		if err != nil {
			return fmt.Errorf("daemon: marshal args: %w", err)
		}
		rawArgs = buf
	}
	body, err := json.Marshal(Request{
		Verb:             verb,
		ClientBinaryHash: clientBinaryHash,
		Args:             rawArgs,
	})
	if err != nil {
		return fmt.Errorf("daemon: marshal request: %w", err)
	}
	body = append(body, '\n')
	if _, err := conn.Write(body); err != nil {
		return fmt.Errorf("daemon: write request: %w", err)
	}

	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return fmt.Errorf("daemon: read response: %w", err)
	}
	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return fmt.Errorf("daemon: decode response: %w", err)
	}
	if !resp.OK {
		return errors.New(resp.Error)
	}
	if out == nil || len(resp.Data) == 0 {
		return nil
	}
	if err := json.Unmarshal(resp.Data, out); err != nil {
		return fmt.Errorf("daemon: decode data: %w", err)
	}
	return nil
}
