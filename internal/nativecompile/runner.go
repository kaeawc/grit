package nativecompile

import (
	"context"
	"fmt"
	"os"

	"github.com/kaeawc/grit/internal/proc"
)

// defaultRunner is the package-wide subprocess runner. Production code
// uses proc.OS{}, which delegates to os/exec; tests can swap in a
// proc.Fake via SwapRunner so they don't need real toolchains on PATH.
//
// The streaming-output call sites (runKotlinc, runJavac) keep their
// existing exec.Command path because proc.Runner buffers Stdout/Stderr
// to []byte and we want live output during long compiles. Migrating
// those will require either streaming methods on proc.Runner or
// composing exec.CommandContext with a Runner-installed factory.
var defaultRunner proc.Runner = proc.OS{}

// SwapRunner installs r as the package-wide runner and returns a function
// that restores the previous runner. Tests use t.Cleanup with the
// returned restore function:
//
//	restore := nativecompile.SwapRunner(fake)
//	t.Cleanup(restore)
//
// This is not safe for concurrent use; tests that swap should run
// sequentially or use parallel-safe stubs.
func SwapRunner(r proc.Runner) func() {
	if r == nil {
		r = proc.OS{}
	}
	prev := defaultRunner
	defaultRunner = r
	return func() { defaultRunner = prev }
}

// runBuffered runs cmd via the package runner, mirroring the prior
// "exec + stdout/stderr buffer + diagnostics" pattern. Output buffers
// are written to stdout/stderr after the subprocess exits and routed
// through recordToolDiagnostics. Non-zero exit codes return an error.
//
// Use this for short-lived tools (jar, d8 partial steps, adb) where
// post-hoc output is fine. Long-running tools that need live output
// streaming must keep using exec.CommandContext directly.
func runBuffered(ctx context.Context, name string, cmd proc.Cmd, stdout, stderr *os.File) error {
	res, err := defaultRunner.Run(ctx, cmd)
	if err == nil && res.ExitCode != 0 {
		err = fmt.Errorf("%s exited with %d", name, res.ExitCode)
	}
	if len(res.Stdout) > 0 && stdout != nil {
		_, _ = stdout.Write(res.Stdout)
	}
	if len(res.Stderr) > 0 && stderr != nil {
		_, _ = stderr.Write(res.Stderr)
	}
	recordToolDiagnostics(ctx, name, string(res.Stderr), string(res.Stdout))
	return err
}
