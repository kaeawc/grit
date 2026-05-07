package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/kaeawc/grit/internal/perf"
	"github.com/kaeawc/grit/internal/project"
	"github.com/kaeawc/grit/internal/service"
)

// runEntityByIDLookup is the shared body for verbs that take --repo and a
// single id-shaped flag, dispatch to a service method, and emit the JSON
// result. It folds the eight cookie-cutter ByID/ConsumersByID handlers into
// one place.
func runEntityByIDLookup[R any](
	ctx context.Context,
	args []string,
	stdout, stderr io.Writer,
	tracker perf.Tracker,
	start time.Time,
	verb, idFlag, idDesc string,
	lookup func(svc *service.Service, ctx context.Context, prj *project.Project, id string) (R, error),
) int {
	cmd := newCommandState(verb, stdout, stderr, tracker, start)
	fs := flag.NewFlagSet(verb, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	id := fs.String(idFlag, "", idDesc)
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	if strings.TrimSpace(*id) == "" {
		return cmd.fail(2, fmt.Errorf("%s is required", idFlag))
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := lookup(cmd.svc, ctx, prj, *id)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}
