package daemon

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Handler runs a single verb. It receives the request's opaque JSON
// args and returns a JSON-marshalable result or an error. Handlers may
// be invoked concurrently across connections; the implementation is
// responsible for any internal locking.
type Handler func(ctx context.Context, args json.RawMessage) (any, error)

// Server accepts daemon Requests on a Unix socket and dispatches each
// to a registered Handler.
type Server struct {
	socketPath string
	version    string
	binaryHash string
	startedAt  time.Time

	// mu guards handlers and listener. Handlers is read-only after
	// Start in practice (Register is for setup), but both fields are
	// touched by Start, Stop, and concurrent connection-handler
	// goroutines, so a single mutex covers them.
	mu       sync.Mutex
	handlers map[string]Handler
	listener net.Listener

	wg       sync.WaitGroup
	stopOnce sync.Once
	stopped  chan struct{}
}

// Option mutates a Server during NewServer. Options exist mainly so
// tests can inject deterministic identity (version + binary hash)
// without touching os.Executable.
type Option func(*Server)

// WithVersion overrides the version string reported by the status verb.
func WithVersion(version string) Option {
	return func(s *Server) { s.version = version }
}

// WithBinaryHash overrides the server's binary hash used in the
// handshake check. Tests pass a known value; production code lets
// NewServer compute it from os.Executable.
func WithBinaryHash(hash string) Option {
	return func(s *Server) { s.binaryHash = hash }
}

// NewServer returns a Server bound to socketPath. The socket file is
// not created until Start. The server registers the built-in status
// and shutdown verbs automatically; callers add their own with
// Register before Start.
func NewServer(socketPath string, opts ...Option) *Server {
	s := &Server{
		socketPath: socketPath,
		handlers:   make(map[string]Handler),
		stopped:    make(chan struct{}),
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.binaryHash == "" {
		s.binaryHash = computeBinaryHash()
	}
	s.registerBuiltins()
	return s
}

// Register installs handler for verb. Re-registering an existing verb
// overwrites it; callers that need stricter behavior should check
// first with Handler.
func (s *Server) Register(verb string, handler Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[verb] = handler
}

// Handler returns the registered handler for verb, or nil.
func (s *Server) Handler(verb string) Handler {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.handlers[verb]
}

// BinaryHash returns the server's identity hash used in the
// ClientBinaryHash handshake.
func (s *Server) BinaryHash() string { return s.binaryHash }

// Start binds the socket and runs the accept loop until Stop is
// called, the context is cancelled, or the built-in shutdown verb
// fires. Start blocks; run it in its own goroutine.
func (s *Server) Start(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Dir(s.socketPath), 0o755); err != nil {
		return fmt.Errorf("daemon: prepare socket dir: %w", err)
	}
	// Remove any stale socket from a previous crashed run. The lock
	// file (out of scope for this scaffolding) is the production
	// answer; for now the bind below will fail with EADDRINUSE if a
	// live process is still listening.
	_ = os.Remove(s.socketPath)
	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("daemon: listen %s: %w", s.socketPath, err)
	}
	s.mu.Lock()
	s.listener = listener
	s.startedAt = time.Now().UTC()
	s.mu.Unlock()

	// Cancel-on-context plumbing: a goroutine watches ctx and closes
	// the listener so Accept unblocks.
	go func() {
		select {
		case <-ctx.Done():
			_ = s.Stop()
		case <-s.stopped:
		}
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			// Listener was closed via Stop or context cancel — the
			// expected exit path. Anything else surfaces.
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("daemon: accept: %w", err)
		}
		s.wg.Add(1)
		go s.handleConn(ctx, conn)
	}
}

// Stop closes the listener and waits for in-flight handlers to drain.
// Safe to call multiple times.
func (s *Server) Stop() error {
	var stopErr error
	s.stopOnce.Do(func() {
		s.mu.Lock()
		listener := s.listener
		s.mu.Unlock()
		if listener != nil {
			if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
				stopErr = err
			}
		}
		_ = os.Remove(s.socketPath)
		close(s.stopped)
	})
	s.wg.Wait()
	return stopErr
}

// Stopped returns a channel that is closed when the server has
// completed Stop.
func (s *Server) Stopped() <-chan struct{} { return s.stopped }

func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer s.wg.Done()
	defer func() { _ = conn.Close() }()

	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil && (err != io.EOF || len(line) == 0) {
		// Client hung up before sending a complete request. There is
		// no one to write back to; drop the connection silently.
		return
	}

	resp := s.dispatch(ctx, line)
	body, err := json.Marshal(resp)
	if err != nil {
		// Last-ditch fallback so the client doesn't time out waiting
		// for a response we couldn't serialize.
		body = []byte(`{"ok":false,"error":"daemon: marshal response failed"}`)
	}
	body = append(body, '\n')
	_, _ = conn.Write(body)
}

func (s *Server) dispatch(ctx context.Context, line []byte) Response {
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		return errorResponse(fmt.Errorf("daemon: decode request: %w", err))
	}
	if req.ClientBinaryHash != "" && req.ClientBinaryHash != s.binaryHash {
		return errorResponse(fmt.Errorf("%s (daemon=%s client=%s)", ErrBinaryHashMismatchPrefix, s.binaryHash, req.ClientBinaryHash))
	}
	handler := s.Handler(req.Verb)
	if handler == nil {
		return errorResponse(fmt.Errorf("daemon: unknown verb %q", req.Verb))
	}
	result, err := handler(ctx, req.Args)
	if err != nil {
		return errorResponse(err)
	}
	if result == nil {
		return Response{OK: true}
	}
	data, err := json.Marshal(result)
	if err != nil {
		return errorResponse(fmt.Errorf("daemon: marshal result: %w", err))
	}
	return Response{OK: true, Data: data}
}

func errorResponse(err error) Response {
	return Response{OK: false, Error: err.Error()}
}

func (s *Server) registerBuiltins() {
	s.handlers[VerbStatus] = func(context.Context, json.RawMessage) (any, error) {
		return StatusResult{
			Version:    s.version,
			BinaryHash: s.binaryHash,
			StartedAt:  s.startedAt,
			UptimeMs:   time.Since(s.startedAt).Milliseconds(),
		}, nil
	}
	s.handlers[VerbShutdown] = func(context.Context, json.RawMessage) (any, error) {
		// Stop in a separate goroutine so the OK response can land on
		// the wire before the listener closes mid-flight.
		go func() { _ = s.Stop() }()
		return nil, nil
	}
}

// computeBinaryHash returns a SHA-256 of the running executable. On
// any failure it returns the empty string, which disables the
// handshake on this server (clients can still send ClientBinaryHash
// but it will never match an empty server hash, so requests will
// fail — symmetric to "daemon couldn't identify itself").
func computeBinaryHash() string {
	path, err := os.Executable()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
