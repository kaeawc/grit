package cli

import (
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/kaeawc/grit/internal/env"
	"github.com/kaeawc/grit/internal/perf"
	"github.com/kaeawc/grit/internal/responsepayload"
)

func runDoctor(_ context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("doctor", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	var report env.Report
	if err := tracker.Track("checkEnvironment", func() error {
		report = env.Check(prj)
		return nil
	}); err != nil {
		return cmd.fail(1, err)
	}
	ok := true
	result := doctorResult{Items: make([]doctorItem, 0, len(report.Items))}
	for _, item := range report.Items {
		if !item.OK {
			ok = false
		}
		result.Items = append(result.Items, doctorItem{Name: item.Name, Detail: item.Detail, OK: item.OK})
	}
	if !ok {
		return cmd.failWithResult(1, errors.New("one or more required tools are missing"), resultJSON(result))
	}
	return cmd.success(resultJSON(result))
}

func runJavaToolchains(_ context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("javaToolchains", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("javaToolchains", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	kotlinc, _ := exec.LookPath("kotlinc")
	if kotlinc == "" {
		kotlinc = env.LocateKotlinCompiler(prj)
	}
	return cmd.success(resultJSON(javaToolchainsResult{
		Repo: prj.RootDir,
		Java: responsepayload.JavaToolchainInfo{JavaHome: os.Getenv("JAVA_HOME")},
		Kotlin: responsepayload.KotlinToolchainInfo{
			Kotlinc: kotlinc,
			Plugins: responsepayload.KotlinToolchainPlugins{
				Compose:       env.LocateComposeCompilerPlugin(),
				Serialization: env.LocateSerializationCompilerPlugin(),
			},
		},
	}))
}

func runKotlinDslAccessorsReport(_ context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("kotlinDslAccessorsReport", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("kotlinDslAccessorsReport", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", ":app", "Android module path")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	mod, err := cmd.requireModule(prj, *modulePath)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(cmd.svc.KotlinDslAccessorsReport(mod, prj)))
}

func runOutgoingVariants(_ context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("outgoingVariants", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("outgoingVariants", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", ":app", "Android module path")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	mod, err := cmd.requireModule(prj, *modulePath)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(cmd.svc.OutgoingVariants(mod, prj)))
}

func runResolvableConfigurations(_ context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("resolvableConfigurations", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("resolvableConfigurations", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", ":app", "Android module path")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	mod, err := cmd.requireModule(prj, *modulePath)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(cmd.svc.ResolvableConfigurations(mod, prj)))
}
