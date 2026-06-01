package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/kaeawc/grit/internal/daemon"
	"github.com/kaeawc/grit/internal/perf"
)

// runDaemonStart binds a Unix socket and runs the daemon accept loop
// until the parent context is cancelled or a peer sends the built-in
// shutdown verb. Useful for `grit daemon &` invocations and for tests
// that drive the daemon through a cancelable context.
//
// The verb does not return its response until the server has stopped.
// Callers that want a stay-resident daemon should background the
// process and check daemonStatus to confirm it's reachable.
func runDaemonStart(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("daemonStart", stdout, stderr, tracker, start)
	socketPath, fsErr := parseDaemonClientFlags("daemonStart", args)
	if fsErr != nil {
		return cmd.fail(2, fsErr)
	}
	// WithVersion is left at its zero value: no grit version constant
	// exists yet (cmd/grit has no build-stamp ldflag). Reporting
	// runtime.Version() would actively mislead `daemonStatus` callers
	// who reasonably expect the daemon's grit identity, not the Go
	// toolchain.
	server := daemon.NewServer(socketPath)
	if err := server.Start(ctx); err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(daemonStartResult{
		SocketPath: socketPath,
		BinaryHash: server.BinaryHash(),
	}))
}

// runDaemonStop dials the daemon at the given socket and sends the
// built-in shutdown verb. Returns success once the daemon has
// acknowledged.
func runDaemonStop(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("daemonStop", stdout, stderr, tracker, start)
	socketPath, fsErr := parseDaemonClientFlags("daemonStop", args)
	if fsErr != nil {
		return cmd.fail(2, fsErr)
	}
	if err := daemon.Call(ctx, socketPath, daemon.VerbShutdown, "", nil, nil); err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(daemonStopResult{SocketPath: socketPath}))
}

// runDaemonStatus dials the daemon at the given socket and renders the
// StatusResult returned by the built-in status verb.
func runDaemonStatus(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("daemonStatus", stdout, stderr, tracker, start)
	socketPath, fsErr := parseDaemonClientFlags("daemonStatus", args)
	if fsErr != nil {
		return cmd.fail(2, fsErr)
	}
	var status daemon.StatusResult
	if err := daemon.Call(ctx, socketPath, daemon.VerbStatus, "", nil, &status); err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(daemonStatusResult{
		SocketPath: socketPath,
		Status:     status,
	}))
}

// parseDaemonClientFlags is the shared --socket/--repo flag parser
// for every daemon verb. The verb identifies which daemon to talk to
// (or, for daemonStart, where to bind); per-verb behavior beyond that
// is independent.
func parseDaemonClientFlags(name string, args []string) (string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	socket := fs.String("socket", "", "Unix socket path (default: <repo>/.grit/daemon.sock)")
	repo := fs.String("repo", "", "Project root used to derive the default socket path")
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	if *socket != "" {
		return *socket, nil
	}
	if *repo != "" {
		return daemon.DefaultSocketPath(*repo), nil
	}
	return "", fmt.Errorf("--socket or --repo is required")
}

type daemonStartResult struct {
	SocketPath string `json:"socketPath"`
	BinaryHash string `json:"binaryHash"`
}

type daemonStopResult struct {
	SocketPath string `json:"socketPath"`
}

type daemonStatusResult struct {
	SocketPath string              `json:"socketPath"`
	Status     daemon.StatusResult `json:"status"`
}
