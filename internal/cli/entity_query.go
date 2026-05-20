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

// runEntityByIDLookup handles verbs that take --repo and a single id-shaped
// flag, dispatch to a service method, and emit the JSON result.
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

// runModuleVariantLookup handles verbs that take --repo --module --variant
// (with the conventional :app / debug defaults) and dispatch to a service
// method.
func runModuleVariantLookup[R any](
	ctx context.Context,
	args []string,
	stdout, stderr io.Writer,
	tracker perf.Tracker,
	start time.Time,
	verb string,
	lookup func(svc *service.Service, ctx context.Context, prj *project.Project, modulePath, variantName string) (R, error),
) int {
	cmd := newCommandState(verb, stdout, stderr, tracker, start)
	fs := flag.NewFlagSet(verb, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", "", moduleFlagUsage)
	variant := fs.String("variant", "debug", "Variant name")
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
	result, err := lookup(cmd.svc, ctx, prj, resolvedModule, *variant)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

// runModuleLookup handles verbs that take --repo --module (with the conventional
// :app default) and dispatch to a service method.
func runModuleLookup[R any](
	ctx context.Context,
	args []string,
	stdout, stderr io.Writer,
	tracker perf.Tracker,
	start time.Time,
	verb string,
	lookup func(svc *service.Service, ctx context.Context, prj *project.Project, modulePath string) (R, error),
) int {
	cmd := newCommandState(verb, stdout, stderr, tracker, start)
	fs := flag.NewFlagSet(verb, flag.ContinueOnError)
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
	result, err := lookup(cmd.svc, ctx, prj, resolvedModule)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

// runRequireModuleLookup handles verbs that take --repo --module (default :app)
// and dispatch to a service method whose signature is `(mod, prj) (R, error)`.
// The module is validated up front via cmd.requireModule before the service
// call. Service methods that don't return an error wrap with a one-liner
// adapter at the call site.
func runRequireModuleLookup[R any](
	args []string,
	stdout, stderr io.Writer,
	tracker perf.Tracker,
	start time.Time,
	verb string,
	lookup func(svc *service.Service, mod *project.Module, prj *project.Project) (R, error),
) int {
	cmd := newCommandState(verb, stdout, stderr, tracker, start)
	fs := flag.NewFlagSet(verb, flag.ContinueOnError)
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
	result, err := lookup(cmd.svc, mod, prj)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}
