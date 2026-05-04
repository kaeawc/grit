package nativecompile

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kaeawc/grit/internal/proc"
)

// TestRunCmdHonorsSwappedRunner exercises the proc.Runner seam: tests
// install a proc.Fake that scripts canned output for "echo", and runCmd
// (which goes through runBuffered → defaultRunner) returns those bytes
// without spawning a real subprocess.
func TestRunCmdHonorsSwappedRunner(t *testing.T) {
	fake := proc.NewFake()
	fake.OnExact("echo", []string{"ignored"}, proc.Response{
		Result: proc.Result{Stdout: []byte("from-fake\n"), ExitCode: 0},
	})
	restore := SwapRunner(fake)
	t.Cleanup(restore)

	stdoutPath := filepath.Join(t.TempDir(), "stdout.log")
	stderrPath := filepath.Join(t.TempDir(), "stderr.log")
	stdout, err := os.Create(stdoutPath)
	if err != nil {
		t.Fatalf("create stdout: %v", err)
	}
	t.Cleanup(func() { _ = stdout.Close() })
	stderr, err := os.Create(stderrPath)
	if err != nil {
		t.Fatalf("create stderr: %v", err)
	}
	t.Cleanup(func() { _ = stderr.Close() })

	if err := runCmd(context.Background(), "echo", []string{"ignored"}, stdout, stderr); err != nil {
		t.Fatalf("runCmd: %v", err)
	}
	if err := stdout.Sync(); err != nil {
		t.Fatalf("sync: %v", err)
	}
	got, err := os.ReadFile(stdoutPath)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if string(got) != "from-fake\n" {
		t.Fatalf("expected fake stdout, got %q", got)
	}

	calls := fake.Calls()
	if len(calls) != 1 || calls[0].Name != "echo" {
		t.Fatalf("expected one fake call for echo, got %+v", calls)
	}
}
