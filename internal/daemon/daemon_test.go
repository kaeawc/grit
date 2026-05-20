package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// shortTempDir returns a freshly-created temp directory with a short
// path. macOS limits sockaddr_un.sun_path to ~104 bytes and t.TempDir
// embeds the full test name in its path, which overflows for longer
// test names.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "gritd")
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// startTestServer boots a Server on a short-path Unix socket with a
// known binary hash and version, runs Start in a goroutine, waits for
// the socket to be reachable, and registers t.Cleanup to Stop it.
func startTestServer(t *testing.T, opts ...Option) (*Server, string) {
	t.Helper()
	socket := filepath.Join(shortTempDir(t), "d.sock")
	defaults := []Option{WithVersion("test-1.0"), WithBinaryHash("deadbeef")}
	server := NewServer(socket, append(defaults, opts...)...)

	startErr := make(chan error, 1)
	go func() { startErr <- server.Start(context.Background()) }()

	if !waitForSocket(socket, 2*time.Second) {
		t.Fatalf("daemon socket %s never appeared", socket)
	}
	t.Cleanup(func() {
		_ = server.Stop()
		// Drain the Start goroutine so the test doesn't leak it.
		select {
		case err := <-startErr:
			if err != nil {
				t.Logf("server.Start returned: %v", err)
			}
		case <-time.After(time.Second):
			t.Logf("server.Start did not exit within 1s")
		}
	})
	return server, socket
}

func waitForSocket(path string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if Available(path) {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func TestStatusRoundTrip(t *testing.T) {
	_, socket := startTestServer(t)

	var got StatusResult
	if err := Call(context.Background(), socket, VerbStatus, "deadbeef", nil, &got); err != nil {
		t.Fatalf("Call status: %v", err)
	}
	if got.Version != "test-1.0" {
		t.Fatalf("Version = %q, want %q", got.Version, "test-1.0")
	}
	if got.BinaryHash != "deadbeef" {
		t.Fatalf("BinaryHash = %q, want %q", got.BinaryHash, "deadbeef")
	}
	if got.StartedAt.IsZero() {
		t.Fatalf("StartedAt is zero")
	}
}

func TestRegisterCustomVerb(t *testing.T) {
	server, socket := startTestServer(t)

	type echoArgs struct {
		Msg string `json:"msg"`
	}
	type echoResult struct {
		Echoed string `json:"echoed"`
	}
	server.Register("echo", func(_ context.Context, raw json.RawMessage) (any, error) {
		var a echoArgs
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, err
		}
		return echoResult{Echoed: a.Msg}, nil
	})

	var got echoResult
	if err := Call(context.Background(), socket, "echo", "deadbeef", echoArgs{Msg: "hi"}, &got); err != nil {
		t.Fatalf("Call echo: %v", err)
	}
	if got.Echoed != "hi" {
		t.Fatalf("Echoed = %q, want %q", got.Echoed, "hi")
	}
}

func TestBinaryHashHandshakeRejectsMismatch(t *testing.T) {
	_, socket := startTestServer(t)

	err := Call(context.Background(), socket, VerbStatus, "cafebabe", nil, nil)
	if err == nil {
		t.Fatalf("expected error for mismatched binary hash")
	}
	if !strings.HasPrefix(err.Error(), ErrBinaryHashMismatchPrefix) {
		t.Fatalf("error = %q, want prefix %q", err.Error(), ErrBinaryHashMismatchPrefix)
	}
	// Both sides' hashes should appear in the diagnostic.
	if !strings.Contains(err.Error(), "deadbeef") || !strings.Contains(err.Error(), "cafebabe") {
		t.Fatalf("error %q must carry both hashes", err.Error())
	}
}

// An empty ClientBinaryHash opts out of the handshake — useful for
// clients that don't yet know which binary they're talking to.
func TestEmptyClientBinaryHashSkipsHandshake(t *testing.T) {
	_, socket := startTestServer(t)

	var got StatusResult
	if err := Call(context.Background(), socket, VerbStatus, "", nil, &got); err != nil {
		t.Fatalf("Call with empty hash: %v", err)
	}
	if got.Version == "" {
		t.Fatalf("expected status to succeed without handshake")
	}
}

func TestUnknownVerbReturnsError(t *testing.T) {
	_, socket := startTestServer(t)

	err := Call(context.Background(), socket, "no-such-verb", "deadbeef", nil, nil)
	if err == nil {
		t.Fatalf("expected error for unknown verb")
	}
	if !strings.Contains(err.Error(), "unknown verb") {
		t.Fatalf("error = %q, want substring %q", err.Error(), "unknown verb")
	}
}

func TestHandlerErrorPropagates(t *testing.T) {
	server, socket := startTestServer(t)
	server.Register("boom", func(context.Context, json.RawMessage) (any, error) {
		return nil, errors.New("kaboom")
	})

	err := Call(context.Background(), socket, "boom", "deadbeef", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "kaboom") {
		t.Fatalf("expected error containing 'kaboom', got %v", err)
	}
}

// The built-in shutdown verb stops the server. Subsequent calls fail
// because the socket has been removed.
func TestShutdownVerbStopsServer(t *testing.T) {
	server, socket := startTestServer(t)

	if err := Call(context.Background(), socket, VerbShutdown, "deadbeef", nil, nil); err != nil {
		t.Fatalf("Call shutdown: %v", err)
	}
	select {
	case <-server.Stopped():
	case <-time.After(2 * time.Second):
		t.Fatalf("server did not signal Stopped within 2s")
	}
	if Available(socket) {
		t.Fatalf("socket still reachable after shutdown")
	}
}

// Concurrent clients must each get the right response. Exercises the
// per-connection goroutine and the handlers map's read safety.
func TestConcurrentCallsAreIsolated(t *testing.T) {
	server, socket := startTestServer(t)
	server.Register("identity", func(_ context.Context, raw json.RawMessage) (any, error) {
		return json.RawMessage(raw), nil
	})

	const workers = 16
	var wg sync.WaitGroup
	wg.Add(workers)
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			defer wg.Done()
			var got int
			if err := Call(context.Background(), socket, "identity", "deadbeef", i, &got); err != nil {
				errs <- err
				return
			}
			if got != i {
				errs <- errors.New("response did not match worker id")
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent call failed: %v", err)
	}
}

// Available is the fallback signal callers use to decide between
// daemon path and in-process path; it must reflect the socket's real
// state.
func TestAvailableReflectsSocketLifecycle(t *testing.T) {
	if Available(filepath.Join(shortTempDir(t), "missing.sock")) {
		t.Fatalf("Available(missing socket) = true")
	}
	server, _ := startTestServer(t, WithVersion("avail"), WithBinaryHash("a"))
	socket := server.socketPath
	if !Available(socket) {
		t.Fatalf("Available(running) = false")
	}
	if err := server.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if Available(socket) {
		t.Fatalf("Available(stopped) = true")
	}
}

func TestNewServerComputesBinaryHashByDefault(t *testing.T) {
	server := NewServer(filepath.Join(shortTempDir(t), "d.sock"), WithVersion("default-hash"))
	// In the test binary, os.Executable resolves to the test binary
	// itself; we don't assert a specific value, only that something
	// non-empty was computed.
	if server.BinaryHash() == "" {
		t.Fatalf("expected non-empty default binary hash")
	}
}

// Garbage on the wire must be rejected with a decode error rather
// than crashing the connection-handler goroutine.
func TestServerRejectsGarbageRequest(t *testing.T) {
	_, socket := startTestServer(t)
	conn, err := dialUnix(socket)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write([]byte("not json\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	resp, err := readResponse(conn)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resp.OK {
		t.Fatalf("expected OK=false for garbage request, got %+v", resp)
	}
	if !strings.Contains(resp.Error, "decode request") {
		t.Fatalf("error = %q, want 'decode request' substring", resp.Error)
	}
}

// DefaultSocketPath should follow the project convention of putting
// the socket under <root>/.grit/.
func TestDefaultSocketPathFollowsConvention(t *testing.T) {
	got := DefaultSocketPath("/some/repo")
	want := filepath.Join("/some/repo", ".grit", "daemon.sock")
	if got != want {
		t.Fatalf("DefaultSocketPath = %q, want %q", got, want)
	}
}

func dialUnix(socketPath string) (net.Conn, error) {
	return (&net.Dialer{Timeout: time.Second}).DialContext(context.Background(), "unix", socketPath)
}

func readResponse(conn net.Conn) (Response, error) {
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return Response{}, err
	}
	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return Response{}, err
	}
	return resp, nil
}
