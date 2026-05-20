package cli

import (
	"context"
	"flag"
	"io"
	"time"

	"github.com/kaeawc/grit/internal/perf"
)

func runInspect(_ context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("inspect", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(cmd.svc.Inspect(prj)))
}

func runTasks(_ context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("tasks", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("tasks", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", "", moduleFlagUsage)
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	resolvedModule, err := resolveModulePath(prj, *modulePath)
	if err != nil {
		return cmd.fail(1, err)
	}
	mod, err := cmd.requireModule(prj, resolvedModule)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(cmd.svc.Tasks(mod, prj)))
}
