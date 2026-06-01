package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kaeawc/grit/internal/daemon"
)

// daemonResponseFor unmarshals the JSON envelope written by the
// daemon verbs into a typed Result. Reuses the package's unexported
// responseError so the test mirror stays in sync with the wire shape.
type daemonResponseFor[T any] struct {
	Success bool           `json:"success"`
	Command string         `json:"command"`
	Result  T              `json:"result"`
	Error   *responseError `json:"error,omitempty"`
}

// shortSocketDir returns a freshly-created short-path temp directory.
// macOS limits Unix-socket paths to ~104 bytes; t.TempDir embeds the
// full test name and overflows for the longer ones below.
func shortSocketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "gritcli")
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// waitForDaemon polls daemon.Available with a small interval until the
// socket is reachable or timeout elapses. Returns true on success.
func waitForDaemon(socket string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if daemon.Available(socket) {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

// runDaemonInBackground boots a daemon on the given socket via the
// daemon package directly (rather than via runDaemonStart, which
// would block the caller's goroutine until the daemon stops). Returns
// the server so the test can assert on it and stop it.
func runDaemonInBackground(t *testing.T, socket string, opts ...daemon.Option) *daemon.Server {
	t.Helper()
	defaults := []daemon.Option{daemon.WithVersion("cli-test"), daemon.WithBinaryHash("cli-test-hash")}
	server := daemon.NewServer(socket, append(defaults, opts...)...)
	startErr := make(chan error, 1)
	go func() { startErr <- server.Start(context.Background()) }()

	if !waitForDaemon(socket, 2*time.Second) {
		t.Fatalf("test daemon did not become reachable at %s", socket)
	}
	t.Cleanup(func() {
		_ = server.Stop()
		select {
		case <-startErr:
		case <-time.After(time.Second):
		}
	})
	return server
}

func TestDaemonStatusVerbReturnsStatus(t *testing.T) {
	socket := filepath.Join(shortSocketDir(t), "d.sock")
	runDaemonInBackground(t, socket)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"daemonStatus", "--socket", socket}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, want 0 (stderr=%s)", code, stderr.String())
	}
	var resp daemonResponseFor[daemonStatusResult]
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v\n%s", err, stdout.String())
	}
	if !resp.Success {
		t.Fatalf("response.Success = false, error=%+v", resp.Error)
	}
	if resp.Result.SocketPath != socket {
		t.Fatalf("SocketPath = %q, want %q", resp.Result.SocketPath, socket)
	}
	if resp.Result.Status.Version != "cli-test" {
		t.Fatalf("Status.Version = %q, want %q", resp.Result.Status.Version, "cli-test")
	}
	if resp.Result.Status.BinaryHash != "cli-test-hash" {
		t.Fatalf("Status.BinaryHash = %q, want %q", resp.Result.Status.BinaryHash, "cli-test-hash")
	}
}

func TestDaemonStopVerbShutsDownTheDaemon(t *testing.T) {
	socket := filepath.Join(shortSocketDir(t), "d.sock")
	server := runDaemonInBackground(t, socket)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"daemonStop", "--socket", socket}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, want 0 (stderr=%s)", code, stderr.String())
	}
	select {
	case <-server.Stopped():
	case <-time.After(2 * time.Second):
		t.Fatalf("daemon did not signal Stopped within 2s of daemonStop")
	}
	if daemon.Available(socket) {
		t.Fatalf("socket still reachable after daemonStop")
	}
}

// daemonStart blocks until the parent context is cancelled. The test
// cancels the context after asserting the socket is reachable; the
// command then returns a Success envelope.
func TestDaemonStartBlocksUntilContextCancelled(t *testing.T) {
	socket := filepath.Join(shortSocketDir(t), "d.sock")
	ctx, cancel := context.WithCancel(context.Background())

	var stdout, stderr bytes.Buffer
	exitCode := make(chan int, 1)
	go func() {
		exitCode <- Run(ctx, []string{"daemonStart", "--socket", socket}, &stdout, &stderr)
	}()

	// Wait until the daemon is reachable, then cancel.
	if !waitForDaemon(socket, 2*time.Second) {
		cancel()
		<-exitCode
		t.Fatalf("daemon did not become reachable")
	}

	cancel()
	select {
	case code := <-exitCode:
		if code != 0 {
			t.Fatalf("daemonStart returned %d (stderr=%s)", code, stderr.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("daemonStart did not exit within 2s of context cancel")
	}

	var resp daemonResponseFor[daemonStartResult]
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v\n%s", err, stdout.String())
	}
	if !resp.Success {
		t.Fatalf("response.Success = false, error=%+v", resp.Error)
	}
	if resp.Result.SocketPath != socket {
		t.Fatalf("SocketPath = %q, want %q", resp.Result.SocketPath, socket)
	}
	if resp.Result.BinaryHash == "" {
		t.Fatalf("BinaryHash is empty, want a computed default")
	}
}

// --repo derives the socket path from <repo>/.grit/daemon.sock.
func TestDaemonClientVerbsAcceptRepoFlag(t *testing.T) {
	repo := shortSocketDir(t)
	socket := daemon.DefaultSocketPath(repo)
	if err := os.MkdirAll(filepath.Dir(socket), 0o755); err != nil {
		t.Fatalf("mkdir .grit: %v", err)
	}
	runDaemonInBackground(t, socket)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"daemonStatus", "--repo", repo}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d (stderr=%s)", code, stderr.String())
	}
	var resp daemonResponseFor[daemonStatusResult]
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v\n%s", err, stdout.String())
	}
	if resp.Result.SocketPath != socket {
		t.Fatalf("SocketPath = %q, want %q", resp.Result.SocketPath, socket)
	}
}

// Without --socket or --repo the verbs must fail with a non-zero exit
// code (specifically code 2 — flag/usage error) so callers can detect
// missing arguments.
func TestDaemonVerbsRequireSocketOrRepo(t *testing.T) {
	for _, verb := range []string{"daemonStart", "daemonStop", "daemonStatus"} {
		t.Run(verb, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(context.Background(), []string{verb}, &stdout, &stderr)
			if code == 0 {
				t.Fatalf("expected non-zero exit code for %s without flags", verb)
			}
			if !strings.Contains(stdout.String(), "--socket") {
				t.Fatalf("error message should mention --socket; got: %s", stdout.String())
			}
		})
	}
}

// daemonStatus against an unreachable socket must fail rather than
// hang; the dial timeout in the daemon client guards this.
func TestDaemonStatusFailsWhenSocketUnreachable(t *testing.T) {
	socket := filepath.Join(shortSocketDir(t), "missing.sock")

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"daemonStatus", "--socket", socket}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected non-zero exit code for unreachable daemon")
	}
	var resp daemonResponseFor[json.RawMessage]
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v\n%s", err, stdout.String())
	}
	if resp.Error == nil || !strings.Contains(resp.Error.Message, "dial") {
		t.Fatalf("expected dial error, got %+v", resp.Error)
	}
}
