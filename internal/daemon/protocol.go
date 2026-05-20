// Package daemon implements a long-lived grit process that accepts
// requests on a Unix socket. CLI verbs can prefer the daemon when a
// socket is reachable and fall back to in-process execution otherwise.
// This package is the transport scaffolding; no grit verbs are wired
// in yet, only the built-in `status` and `shutdown`.
//
// The wire protocol is line-delimited JSON: one request object per
// line, one response object per line. Each connection serves a single
// request — the server closes the connection after writing the
// response.
package daemon

import (
	"encoding/json"
	"path/filepath"
	"time"
)

// Built-in verbs every server registers automatically. Higher layers
// register their own verbs with Server.Register.
const (
	VerbStatus   = "status"
	VerbShutdown = "shutdown"
)

// ErrBinaryHashMismatchPrefix is the prefix the daemon returns when a
// request carries a ClientBinaryHash that does not match the server's
// own identity. Callers detect this prefix to log a short "binary
// diverged" warning and fall back to in-process execution. The full
// error reads "binary hash mismatch (daemon=<x> client=<y>)" so logs
// carry both hashes for diagnostics.
const ErrBinaryHashMismatchPrefix = "binary hash mismatch"

// Request is one verb invocation on the wire.
//
// ClientBinaryHash is optional: empty disables the handshake (client
// is opting into "trust whatever daemon is running"). Non-empty values
// must match the server's BinaryHash or the request is rejected with
// ErrBinaryHashMismatchPrefix.
type Request struct {
	Verb             string          `json:"verb"`
	ClientBinaryHash string          `json:"clientBinaryHash,omitempty"`
	Args             json.RawMessage `json:"args,omitempty"`
}

// Response is the wire form of a verb result. OK=false carries an
// Error message and an empty Data; OK=true carries the verb's
// JSON-marshaled return value in Data.
type Response struct {
	OK    bool            `json:"ok"`
	Error string          `json:"error,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
}

// StatusResult is the payload returned by the built-in status verb.
type StatusResult struct {
	Version    string    `json:"version"`
	BinaryHash string    `json:"binaryHash"`
	StartedAt  time.Time `json:"startedAt"`
	UptimeMs   int64     `json:"uptimeMs"`
}

// DefaultSocketPath returns the conventional socket path for a grit
// repo rooted at root: <root>/.grit/daemon.sock.
func DefaultSocketPath(root string) string {
	return filepath.Join(root, ".grit", "daemon.sock")
}
