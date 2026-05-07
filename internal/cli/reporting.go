package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/kaeawc/grit/internal/configmodel"
	"github.com/kaeawc/grit/internal/intellijtask"
	"github.com/kaeawc/grit/internal/perf"
	"github.com/kaeawc/grit/internal/project"
	"github.com/kaeawc/grit/internal/service"
)

type stringSliceFlag []string

func (s *stringSliceFlag) String() string {
	return strings.Join(*s, ",")
}

func (s *stringSliceFlag) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	*s = append(*s, value)
	return nil
}

func runIntelliJSyncModel(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("intellijSyncModel", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("intellijSyncModel", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	model, err := cmd.svc.IntelliJSyncModel(ctx, prj)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(intellijSyncModelResult{
		Repo:          prj.RootDir,
		ModelCacheKey: model.CacheKey,
		Model:         model,
	}))
}

func runSigningReport(_ context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("signingReport", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("signingReport", flag.ContinueOnError)
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
	return cmd.success(resultJSON(cmd.svc.SigningReport(mod, prj)))
}

func runProjects(_ context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("projects", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("projects", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(cmd.svc.Projects(prj)))
}

func runProperties(_ context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("properties", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("properties", flag.ContinueOnError)
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
	return cmd.success(resultJSON(cmd.svc.Properties(mod, prj)))
}

func runDependencies(_ context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("dependencies", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("dependencies", flag.ContinueOnError)
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
	deps, err := cmd.svc.Dependencies(mod, prj)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(deps))
}

func runBuildEnvironment(_ context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("buildEnvironment", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("buildEnvironment", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(cmd.svc.BuildEnvironment(prj)))
}

func runArtifactTransforms(_ context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("artifactTransforms", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("artifactTransforms", flag.ContinueOnError)
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
	return cmd.success(resultJSON(cmd.svc.ArtifactTransforms(mod, prj)))
}

func runDependencyInsight(_ context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("dependencyInsight", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("dependencyInsight", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", ":app", "Android module path")
	query := fs.String("dependency", "", "Substring to match in dependency refs")
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
	result, err := cmd.svc.DependencyInsight(mod, prj, *query)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runResolveIntelliJTasks(_ context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("resolveIntelliJTasks", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("resolveIntelliJTasks", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", "", "Module path")
	var taskNames stringSliceFlag
	fs.Var(&taskNames, "task", "IntelliJ task name")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	if len(taskNames) == 0 {
		return cmd.fail(2, fmt.Errorf("at least one task is required"))
	}
	if strings.TrimSpace(*modulePath) == "" {
		return cmd.fail(2, fmt.Errorf("module is required"))
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.ResolveIntelliJTaskRequests(prj, intellijtask.Request{
		Settings: intellijtask.Settings{
			ModulePath: *modulePath,
			TaskNames:  append([]string(nil), taskNames...),
		},
	})
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(resolveIntelliJTasksResult{
		Repo:      prj.RootDir,
		Module:    *modulePath,
		TaskNames: append([]string(nil), taskNames...),
		Requests:  append([]service.BuildRequest(nil), result...),
	}))
}

func runClasspathSnapshot(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("classpathSnapshot", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("classpathSnapshot", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", ":app", "Android or JVM module path")
	variant := fs.String("variant", "debug", "Variant name")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.ClasspathSnapshot(ctx, prj, *modulePath, *variant)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runClasspathSnapshotByID(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	return runEntityByIDLookup(ctx, args, stdout, stderr, tracker, start,
		"classpathSnapshotByID", "snapshot", "Classpath snapshot ID",
		(*service.Service).ClasspathSnapshotByID)
}

func runClasspathSnapshotProvenance(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	return runEntityByIDLookup(ctx, args, stdout, stderr, tracker, start,
		"classpathSnapshotProvenance", "snapshot", "Classpath snapshot ID",
		(*service.Service).ClasspathSnapshotProvenance)
}

func runClasspathSnapshotConsumersByID(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	return runEntityByIDLookup(ctx, args, stdout, stderr, tracker, start,
		"classpathSnapshotConsumersByID", "snapshot", "Classpath snapshot ID",
		(*service.Service).ClasspathSnapshotConsumersByID)
}

func runClasspathSnapshotConsumers(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	return runEntityByIDLookup(ctx, args, stdout, stderr, tracker, start,
		"classpathSnapshotConsumers", "snapshot", "Classpath snapshot ID",
		(*service.Service).ClasspathSnapshotConsumers)
}

func runClasspathEntryLookup(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("classpathEntryLookup", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("classpathEntryLookup", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", ":app", "Android or JVM module path")
	variant := fs.String("variant", "debug", "Variant name")
	path := fs.String("path", "", "Path to look up on the classpath")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	if strings.TrimSpace(*path) == "" {
		return cmd.fail(2, fmt.Errorf("path is required"))
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.ClasspathEntryLookup(ctx, prj, *modulePath, *variant, *path)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runClasspathPathConsumers(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	return runEntityByIDLookup(ctx, args, stdout, stderr, tracker, start,
		"classpathPathConsumers", "path", "Path to inspect on derived classpaths",
		(*service.Service).ClasspathPathConsumers)
}

func runArtifactOnClasspath(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("artifactOnClasspath", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("artifactOnClasspath", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", ":app", "Android or JVM module path")
	variant := fs.String("variant", "debug", "Variant name")
	artifactID := fs.String("artifact", "", "Artifact ID")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	if strings.TrimSpace(*artifactID) == "" {
		return cmd.fail(2, fmt.Errorf("artifact is required"))
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.ArtifactOnClasspath(ctx, prj, *modulePath, *variant, *artifactID)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runArtifactClasspathConsumers(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	return runEntityByIDLookup(ctx, args, stdout, stderr, tracker, start,
		"artifactClasspathConsumers", "artifact", "Artifact ID",
		(*service.Service).ArtifactClasspathConsumers)
}

func runFileOwners(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	return runEntityByIDLookup(ctx, args, stdout, stderr, tracker, start,
		"fileOwners", "path", "File path to inspect",
		(*service.Service).FileOwners)
}

func runModuleByID(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	return runEntityByIDLookup(ctx, args, stdout, stderr, tracker, start,
		"moduleByID", "id", "Logical module ID",
		(*service.Service).ModuleByID)
}

func runVariantByID(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	return runEntityByIDLookup(ctx, args, stdout, stderr, tracker, start,
		"variantByID", "id", "Variant ID",
		(*service.Service).VariantByID)
}

func runActionByID(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	return runEntityByIDLookup(ctx, args, stdout, stderr, tracker, start,
		"actionByID", "id", "Action ID",
		(*service.Service).ActionByID)
}

func runArtifactByID(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	return runEntityByIDLookup(ctx, args, stdout, stderr, tracker, start,
		"artifactByID", "id", "Artifact ID",
		(*service.Service).ArtifactByID)
}

func runMaterializationByID(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	return runEntityByIDLookup(ctx, args, stdout, stderr, tracker, start,
		"materializationByID", "id", "Materialization ID",
		(*service.Service).MaterializationByID)
}

func runMaterializationConsumers(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	return runEntityByIDLookup(ctx, args, stdout, stderr, tracker, start,
		"materializationConsumers", "id", "Materialization ID",
		(*service.Service).MaterializationConsumers)
}

func runActionInputs(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	return runEntityByIDLookup(ctx, args, stdout, stderr, tracker, start,
		"actionInputs", "action", "Action ID",
		(*service.Service).ActionInputs)
}

func runActionOutputs(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	return runEntityByIDLookup(ctx, args, stdout, stderr, tracker, start,
		"actionOutputs", "action", "Action ID",
		(*service.Service).ActionOutputs)
}

func runActionDependencies(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	return runEntityByIDLookup(ctx, args, stdout, stderr, tracker, start,
		"actionDependencies", "action", "Action ID",
		(*service.Service).ActionDependencies)
}

func runActionDependents(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	return runEntityByIDLookup(ctx, args, stdout, stderr, tracker, start,
		"actionDependents", "action", "Action ID",
		(*service.Service).ActionDependents)
}

func runActionsForModule(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("actionsForModule", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("actionsForModule", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", "", "Module path")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	if strings.TrimSpace(*modulePath) == "" {
		return cmd.fail(2, fmt.Errorf("module is required"))
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.ActionsForModule(ctx, prj, *modulePath)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runActionsForVariant(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("actionsForVariant", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("actionsForVariant", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", "", "Module path")
	variant := fs.String("variant", "", "Variant name")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	if strings.TrimSpace(*modulePath) == "" {
		return cmd.fail(2, fmt.Errorf("module is required"))
	}
	if strings.TrimSpace(*variant) == "" {
		return cmd.fail(2, fmt.Errorf("variant is required"))
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.ActionsForVariant(ctx, prj, *modulePath, *variant)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runVariantMaterialization(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("variantMaterialization", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("variantMaterialization", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", ":app", "Android or JVM module path")
	variant := fs.String("variant", "debug", "Variant name")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.VariantMaterialization(ctx, prj, *modulePath, *variant)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(variantMaterializationResult{
		Repo:          result.Repo,
		Module:        result.Provenance.ModulePath,
		Variant:       result.Provenance.VariantName,
		ModelCacheKey: result.ModelCacheKey,
		Provenance:    result.Provenance,
	}))
}

func runVariantSourceSetModel(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("variantSourceSetModel", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("variantSourceSetModel", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", ":app", "Android or JVM module path")
	variant := fs.String("variant", "debug", "Variant name")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.VariantSourceSetModel(ctx, prj, *modulePath, *variant)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runDependencyBindingsForVariant(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("dependencyBindingsForVariant", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("dependencyBindingsForVariant", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", ":app", "Android or JVM module path")
	variant := fs.String("variant", "debug", "Variant name")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.DependencyBindingsForVariant(ctx, prj, *modulePath, *variant)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runDependencyBindingsForModule(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("dependencyBindingsForModule", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("dependencyBindingsForModule", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", ":app", "Android or JVM module path")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.DependencyBindingsForModule(ctx, prj, *modulePath)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runDependencyRealizationsForVariant(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("dependencyRealizationsForVariant", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("dependencyRealizationsForVariant", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", ":app", "Android or JVM module path")
	variant := fs.String("variant", "debug", "Variant name")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.DependencyRealizationsForVariant(ctx, prj, *modulePath, *variant)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runDependencyRealizationsForModule(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("dependencyRealizationsForModule", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("dependencyRealizationsForModule", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", ":app", "Android or JVM module path")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.DependencyRealizationsForModule(ctx, prj, *modulePath)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runPlannedActionPolicy(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("plannedActionPolicy", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("plannedActionPolicy", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", ":app", "Android or JVM module path")
	command := fs.String("command", "", "Build command")
	variant := fs.String("variant", "", "Requested variant name")
	actionID := fs.String("action", "", "Action ID")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	if strings.TrimSpace(*command) == "" {
		return cmd.fail(2, fmt.Errorf("command is required"))
	}
	if strings.TrimSpace(*actionID) == "" {
		return cmd.fail(2, fmt.Errorf("action is required"))
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	mod, err := cmd.requireModule(prj, *modulePath)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.PlannedActionPolicy(ctx, prj, mod, *command, *variant, *actionID, hasOption(args, "--variant"))
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runPlannedActionPolicies(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("plannedActionPolicies", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("plannedActionPolicies", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", ":app", "Android or JVM module path")
	command := fs.String("command", "", "Build command")
	variant := fs.String("variant", "", "Requested variant name")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	if strings.TrimSpace(*command) == "" {
		return cmd.fail(2, fmt.Errorf("command is required"))
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	mod, err := cmd.requireModule(prj, *modulePath)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.PlannedActionPolicies(ctx, prj, mod, *command, *variant, hasOption(args, "--variant"))
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runMaterializationProvenance(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	return runEntityByIDLookup(ctx, args, stdout, stderr, tracker, start,
		"materializationProvenance", "materialization", "Materialization ID",
		(*service.Service).MaterializationProvenance)
}

func runVariantCompatibility(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("variantCompatibility", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("variantCompatibility", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", ":app", "Android or JVM module path")
	variant := fs.String("variant", "debug", "Variant name")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.VariantCompatibility(ctx, prj, *modulePath, *variant)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runArtifactsForVariant(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("artifactsForVariant", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("artifactsForVariant", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", ":app", "Android or JVM module path")
	variant := fs.String("variant", "debug", "Variant name")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.ArtifactsForVariant(ctx, prj, *modulePath, *variant)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(artifactsForVariantResult{
		Repo:               result.Repo,
		Module:             result.Module,
		Variant:            result.Variant,
		ModelCacheKey:      result.ModelCacheKey,
		MaterializationID:  result.MaterializationID,
		ArtifactSnapshotID: result.ArtifactSnapshotID,
		Artifacts:          append([]configmodel.ArtifactSummary(nil), result.Artifacts...),
	}))
}

func runArtifactsForModule(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("artifactsForModule", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("artifactsForModule", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", ":app", "Android or JVM module path")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.ArtifactsForModule(ctx, prj, *modulePath)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runModuleManifest(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("moduleManifest", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("moduleManifest", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", ":app", "Android or JVM module path")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.ModuleManifest(ctx, prj, *modulePath)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(moduleManifestResult{
		Repo:          result.Repo,
		Module:        result.Manifest.ModulePath,
		ModelCacheKey: result.ModelCacheKey,
		Manifest:      result.Manifest,
	}))
}

func runVariantManifest(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("variantManifest", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("variantManifest", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", ":app", "Android or JVM module path")
	variant := fs.String("variant", "debug", "Variant name")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.VariantManifest(ctx, prj, *modulePath, *variant)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(variantManifestResult{
		Repo:          result.Repo,
		Module:        result.Manifest.ModulePath,
		Variant:       result.Manifest.VariantName,
		ModelCacheKey: result.ModelCacheKey,
		Manifest:      result.Manifest,
	}))
}

func runArtifactSnapshotProvenance(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("artifactSnapshotProvenance", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("artifactSnapshotProvenance", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	snapshotID := fs.String("snapshot", "", "Artifact snapshot ID")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	if strings.TrimSpace(*snapshotID) == "" {
		return cmd.fail(2, fmt.Errorf("snapshot is required"))
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.ArtifactSnapshotProvenance(ctx, prj, *snapshotID)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(artifactSnapshotProvenanceResult{
		Repo:               result.Repo,
		ArtifactSnapshotID: result.Provenance.ArtifactSnapshotID,
		ModelCacheKey:      result.ModelCacheKey,
		Provenance:         result.Provenance,
	}))
}

func runArtifactSnapshotConsumers(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	return runEntityByIDLookup(ctx, args, stdout, stderr, tracker, start,
		"artifactSnapshotConsumers", "snapshot", "Artifact snapshot ID",
		(*service.Service).ArtifactSnapshotConsumers)
}

func runArtifactProvenance(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("artifactProvenance", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("artifactProvenance", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	artifactID := fs.String("artifact", "", "Artifact ID")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	if strings.TrimSpace(*artifactID) == "" {
		return cmd.fail(2, fmt.Errorf("artifact is required"))
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.ArtifactProvenance(ctx, prj, *artifactID)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(artifactProvenanceResult{
		Repo:          result.Repo,
		ArtifactID:    *artifactID,
		ModelCacheKey: result.ModelCacheKey,
		Provenance:    result.Provenance,
	}))
}

func runArtifactConsumers(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("artifactConsumers", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("artifactConsumers", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	artifactID := fs.String("artifact", "", "Artifact ID")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	if strings.TrimSpace(*artifactID) == "" {
		return cmd.fail(2, fmt.Errorf("artifact is required"))
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.ArtifactConsumers(ctx, prj, *artifactID)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(artifactConsumersResult{
		Repo:          result.Repo,
		ArtifactID:    *artifactID,
		ModelCacheKey: result.ModelCacheKey,
		Consumers:     result.Consumers,
	}))
}

func runVariantImpact(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("variantImpact", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("variantImpact", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", ":app", "Android or JVM module path")
	variant := fs.String("variant", "debug", "Variant name")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.VariantImpact(ctx, prj, *modulePath, *variant)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(variantImpactResult{
		Repo:          prj.RootDir,
		Module:        result.Module,
		Variant:       result.Variant,
		ModelCacheKey: result.ModelCacheKey,
		Impact:        result,
	}))
}

func runModuleImpact(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("moduleImpact", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("moduleImpact", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", ":app", "Android or JVM module path")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.ModuleImpact(ctx, prj, *modulePath)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(moduleImpactResult{
		Repo:          prj.RootDir,
		Module:        result.Module,
		ModelCacheKey: result.ModelCacheKey,
		Impact:        result,
	}))
}

func runResolverReport(_ context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("resolverReport", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("resolverReport", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", ":app", "Android or JVM module path")
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
	result, err := cmd.svc.ResolverReport(mod, prj)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(resolverReportResult{
		Repo:         result.Repo,
		Module:       result.Module,
		CachePath:    result.CachePath,
		ReportPath:   result.ReportPath,
		ReplayPath:   result.ReplayPath,
		LockfilePath: result.LockfilePath,
		Found:        result.Found,
		Topology:     result.Topology,
		Inputs:       result.Inputs,
		Summary: resolverReportSummary{
			CompileJarCount:     result.Summary.CompileJarCount,
			RuntimeJarCount:     result.Summary.RuntimeJarCount,
			TestJarCount:        result.Summary.TestJarCount,
			AndroidLibraryCount: result.Summary.AndroidLibraryCount,
			SelectionCount:      result.Summary.SelectionCount,
			ConflictCount:       result.Summary.ConflictCount,
			PinCount:            result.Summary.PinCount,
		},
		Report:   result.Report,
		Replay:   result.Replay,
		Lockfile: result.Lockfile,
	}))
}

func runCacheTopology(_ context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("cacheTopology", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("cacheTopology", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.CacheTopology(prj)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runExplainPlan(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("explainPlan", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("explainPlan", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", ":app", "Android or JVM module path")
	command := fs.String("command", "", "Build command")
	variant := fs.String("variant", "", "Requested variant name")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	if strings.TrimSpace(*command) == "" {
		return cmd.fail(2, fmt.Errorf("command is required"))
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	mod, err := cmd.requireModule(prj, *modulePath)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.ExplainPlan(ctx, prj, mod, *command, *variant, hasOption(args, "--variant"))
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runVariantProvenance(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("variantProvenance", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("variantProvenance", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", ":app", "Android or JVM module path")
	variant := fs.String("variant", "debug", "Variant name")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	if strings.TrimSpace(*variant) == "" {
		return cmd.fail(2, fmt.Errorf("variant is required"))
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.VariantProvenance(ctx, prj, *modulePath, *variant)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runActionProvenance(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("actionProvenance", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("actionProvenance", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	actionID := fs.String("action", "", "Action ID")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	if strings.TrimSpace(*actionID) == "" {
		return cmd.fail(2, fmt.Errorf("action is required"))
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.ActionProvenance(ctx, prj, *actionID)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runCleanupPlan(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("cleanupPlan", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("cleanupPlan", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.CleanupPlan(ctx, prj)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runRunSummary(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("runSummary", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("runSummary", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", "", "Module path")
	command := fs.String("command", "", "Command name")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	if strings.TrimSpace(*modulePath) == "" {
		return cmd.fail(2, fmt.Errorf("module is required"))
	}
	if strings.TrimSpace(*command) == "" {
		return cmd.fail(2, fmt.Errorf("command is required"))
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.RunSummary(ctx, prj, *modulePath, *command)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runRunSummaries(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("runSummaries", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("runSummaries", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", "", "Optional module path")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.RunSummaries(ctx, prj, *modulePath)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runRunGraphSummary(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("runGraphSummary", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("runGraphSummary", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", "", "Module path")
	command := fs.String("command", "", "Build command")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	if strings.TrimSpace(*modulePath) == "" {
		return cmd.fail(2, fmt.Errorf("module is required"))
	}
	if strings.TrimSpace(*command) == "" {
		return cmd.fail(2, fmt.Errorf("command is required"))
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.RunGraphSummary(ctx, prj, *modulePath, *command)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runCriticalPathSummary(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("criticalPathSummary", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("criticalPathSummary", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", "", "Module path")
	command := fs.String("command", "", "Build command")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	if strings.TrimSpace(*modulePath) == "" {
		return cmd.fail(2, fmt.Errorf("module is required"))
	}
	if strings.TrimSpace(*command) == "" {
		return cmd.fail(2, fmt.Errorf("command is required"))
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.CriticalPathSummary(ctx, prj, *modulePath, *command)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runSchedulerSummary(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("schedulerSummary", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("schedulerSummary", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", "", "Module path")
	command := fs.String("command", "", "Build command")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	if strings.TrimSpace(*modulePath) == "" {
		return cmd.fail(2, fmt.Errorf("module is required"))
	}
	if strings.TrimSpace(*command) == "" {
		return cmd.fail(2, fmt.Errorf("command is required"))
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.SchedulerSummary(ctx, prj, *modulePath, *command)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runCacheSummary(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("cacheSummary", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("cacheSummary", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", "", "Module path")
	command := fs.String("command", "", "Build command")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	if strings.TrimSpace(*modulePath) == "" {
		return cmd.fail(2, fmt.Errorf("module is required"))
	}
	if strings.TrimSpace(*command) == "" {
		return cmd.fail(2, fmt.Errorf("command is required"))
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.CacheSummary(ctx, prj, *modulePath, *command)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runToolSummary(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("toolSummary", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("toolSummary", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", "", "Module path")
	command := fs.String("command", "", "Build command")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	if strings.TrimSpace(*modulePath) == "" {
		return cmd.fail(2, fmt.Errorf("module is required"))
	}
	if strings.TrimSpace(*command) == "" {
		return cmd.fail(2, fmt.Errorf("command is required"))
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.ToolSummary(ctx, prj, *modulePath, *command)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runDiagnostics(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("diagnostics", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("diagnostics", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", ":app", "Android or JVM module path")
	command := fs.String("command", "", "Build command")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	if strings.TrimSpace(*command) == "" {
		return cmd.fail(2, fmt.Errorf("command is required"))
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.Diagnostics(ctx, prj, *modulePath, *command)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runDiagnosticSummary(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("diagnosticSummary", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("diagnosticSummary", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", ":app", "Android or JVM module path")
	command := fs.String("command", "", "Build command")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	if strings.TrimSpace(*command) == "" {
		return cmd.fail(2, fmt.Errorf("command is required"))
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.DiagnosticSummary(ctx, prj, *modulePath, *command)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runPlannedSchedule(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("plannedSchedule", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("plannedSchedule", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", "", "Module path")
	command := fs.String("command", "", "Build command")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	if strings.TrimSpace(*modulePath) == "" {
		return cmd.fail(2, fmt.Errorf("module is required"))
	}
	if strings.TrimSpace(*command) == "" {
		return cmd.fail(2, fmt.Errorf("command is required"))
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.PlannedSchedule(ctx, prj, *modulePath, *command)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runScheduleDrift(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("scheduleDrift", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("scheduleDrift", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", "", "Module path")
	command := fs.String("command", "", "Build command")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	if strings.TrimSpace(*modulePath) == "" {
		return cmd.fail(2, fmt.Errorf("module is required"))
	}
	if strings.TrimSpace(*command) == "" {
		return cmd.fail(2, fmt.Errorf("command is required"))
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.ScheduleDrift(ctx, prj, *modulePath, *command)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runActionExecution(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("actionExecution", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("actionExecution", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", "", "Module path")
	command := fs.String("command", "", "Build command")
	actionID := fs.String("action", "", "Action ID")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	if strings.TrimSpace(*modulePath) == "" {
		return cmd.fail(2, fmt.Errorf("module is required"))
	}
	if strings.TrimSpace(*command) == "" {
		return cmd.fail(2, fmt.Errorf("command is required"))
	}
	if strings.TrimSpace(*actionID) == "" {
		return cmd.fail(2, fmt.Errorf("action is required"))
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.ActionExecution(ctx, prj, *modulePath, *command, *actionID)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runActionExplanation(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("actionExplanation", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("actionExplanation", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", "", "Module path")
	command := fs.String("command", "", "Build command")
	actionID := fs.String("action", "", "Action ID")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	if strings.TrimSpace(*modulePath) == "" {
		return cmd.fail(2, fmt.Errorf("module is required"))
	}
	if strings.TrimSpace(*command) == "" {
		return cmd.fail(2, fmt.Errorf("command is required"))
	}
	if strings.TrimSpace(*actionID) == "" {
		return cmd.fail(2, fmt.Errorf("action is required"))
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.ActionExplanation(ctx, prj, *modulePath, *command, *actionID)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runActionExecutions(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("actionExecutions", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("actionExecutions", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", "", "Module path")
	command := fs.String("command", "", "Build command")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	if strings.TrimSpace(*modulePath) == "" {
		return cmd.fail(2, fmt.Errorf("module is required"))
	}
	if strings.TrimSpace(*command) == "" {
		return cmd.fail(2, fmt.Errorf("command is required"))
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.ActionExecutions(ctx, prj, *modulePath, *command)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runActionExplanations(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("actionExplanations", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("actionExplanations", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", "", "Module path")
	command := fs.String("command", "", "Build command")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	if strings.TrimSpace(*modulePath) == "" {
		return cmd.fail(2, fmt.Errorf("module is required"))
	}
	if strings.TrimSpace(*command) == "" {
		return cmd.fail(2, fmt.Errorf("command is required"))
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.ActionExplanations(ctx, prj, *modulePath, *command)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runCacheProbes(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("cacheProbes", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("cacheProbes", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", "", "Module path")
	command := fs.String("command", "", "Build command")
	actionFilter := fs.String("action", "", "Filter probes to those matching this action ID (exact match)")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	if strings.TrimSpace(*modulePath) == "" {
		return cmd.fail(2, fmt.Errorf("module is required"))
	}
	if strings.TrimSpace(*command) == "" {
		return cmd.fail(2, fmt.Errorf("command is required"))
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.CacheProbes(ctx, prj, *modulePath, *command)
	if err != nil {
		return cmd.fail(1, err)
	}
	if filter := strings.TrimSpace(*actionFilter); filter != "" {
		filtered := result.Probes[:0:0]
		for _, p := range result.Probes {
			if p.ActionID == filter {
				filtered = append(filtered, p)
			}
		}
		result.Probes = filtered
	}
	return cmd.success(resultJSON(result))
}

func runCacheProbeRecords(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("cacheProbeRecords", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("cacheProbeRecords", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", "", "Module path")
	command := fs.String("command", "", "Build command")
	actionFilter := fs.String("action", "", "Filter records to those matching this action ID (exact match)")
	stepFilter := fs.String("step", "", "Filter records to those matching this probe step name (exact match)")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	if strings.TrimSpace(*modulePath) == "" {
		return cmd.fail(2, fmt.Errorf("module is required"))
	}
	if strings.TrimSpace(*command) == "" {
		return cmd.fail(2, fmt.Errorf("command is required"))
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.CacheProbeRecords(ctx, prj, *modulePath, *command)
	if err != nil {
		return cmd.fail(1, err)
	}
	actionF := strings.TrimSpace(*actionFilter)
	stepF := strings.TrimSpace(*stepFilter)
	if actionF != "" || stepF != "" {
		filtered := result.Records[:0:0]
		for _, r := range result.Records {
			if actionF != "" && r.ActionID != actionF {
				continue
			}
			if stepF != "" && r.StepName != stepF {
				continue
			}
			filtered = append(filtered, r)
		}
		result.Records = filtered
	}
	return cmd.success(resultJSON(result))
}

func runReuseDecision(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("reuseDecision", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("reuseDecision", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", "", "Module path")
	command := fs.String("command", "", "Build command")
	actionID := fs.String("action", "", "Action ID")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	if strings.TrimSpace(*modulePath) == "" {
		return cmd.fail(2, fmt.Errorf("module is required"))
	}
	if strings.TrimSpace(*command) == "" {
		return cmd.fail(2, fmt.Errorf("command is required"))
	}
	if strings.TrimSpace(*actionID) == "" {
		return cmd.fail(2, fmt.Errorf("action is required"))
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.ReuseDecision(ctx, prj, *modulePath, *command, *actionID)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runReuseDecisions(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("reuseDecisions", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("reuseDecisions", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", "", "Module path")
	command := fs.String("command", "", "Build command")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	if strings.TrimSpace(*modulePath) == "" {
		return cmd.fail(2, fmt.Errorf("module is required"))
	}
	if strings.TrimSpace(*command) == "" {
		return cmd.fail(2, fmt.Errorf("command is required"))
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.ReuseDecisions(ctx, prj, *modulePath, *command)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runMaterializations(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("materializations", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("materializations", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", "", "Module path")
	command := fs.String("command", "", "Build command")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	if strings.TrimSpace(*modulePath) == "" {
		return cmd.fail(2, fmt.Errorf("module is required"))
	}
	if strings.TrimSpace(*command) == "" {
		return cmd.fail(2, fmt.Errorf("command is required"))
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.Materializations(ctx, prj, *modulePath, *command)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runActionTrace(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("actionTrace", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("actionTrace", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", "", "Module path")
	command := fs.String("command", "", "Build command")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	if strings.TrimSpace(*modulePath) == "" {
		return cmd.fail(2, fmt.Errorf("module is required"))
	}
	if strings.TrimSpace(*command) == "" {
		return cmd.fail(2, fmt.Errorf("command is required"))
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.ActionTrace(ctx, prj, *modulePath, *command)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runPerfTiming(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("perfTiming", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("perfTiming", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", ":app", "Android or JVM module path")
	command := fs.String("command", "", "Command name")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	if strings.TrimSpace(*command) == "" {
		return cmd.fail(2, fmt.Errorf("command is required"))
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.PerfTiming(ctx, prj, *modulePath, *command)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runClasspathProvenance(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("classpathProvenance", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("classpathProvenance", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", ":app", "Android module path")
	variant := fs.String("variant", "debug", "Variant name")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	result, err := cmd.svc.ClasspathProvenance(ctx, prj, *modulePath, *variant)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(result))
}

func runAndroidCapabilities(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("androidCapabilities", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("androidCapabilities", flag.ContinueOnError)
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
	result, err := cmd.svc.AndroidCapabilities(ctx, prj, *modulePath)
	if err != nil {
		return cmd.fail(1, err)
	}
	return cmd.success(resultJSON(androidCapabilityReportResult{
		Repo:     result.Repo,
		Module:   result.Module,
		Variants: androidCapabilityVariants(result.Variants),
	}))
}

func androidCapabilityVariants(items []service.AndroidCapabilityVariantResult) []androidCapabilityVariantResult {
	out := make([]androidCapabilityVariantResult, 0, len(items))
	for _, item := range items {
		out = append(out, androidCapabilityVariantResult{
			Name:                      item.Name,
			DisplayName:               item.DisplayName,
			BuildType:                 item.BuildType,
			Flavors:                   append([]string(nil), item.Flavors...),
			CompileSDK:                item.CompileSDK,
			BuildToolsVersion:         item.BuildToolsVersion,
			Namespace:                 item.Namespace,
			ApplicationID:             item.ApplicationID,
			ApplicationIDSuffix:       item.ApplicationIDSuffix,
			VersionCode:               item.VersionCode,
			VersionName:               item.VersionName,
			VersionNameSuffix:         item.VersionNameSuffix,
			MinSDK:                    item.MinSDK,
			TargetSDK:                 item.TargetSDK,
			TestInstrumentationRunner: item.TestInstrumentationRunner,
			Optimization:              item.Optimization,
			ProguardFiles:             append([]string(nil), item.ProguardFiles...),
			ConsumerProguardFiles:     append([]string(nil), item.ConsumerProguardFiles...),
			ManifestPaths:             append([]string(nil), item.ManifestPaths...),
			MaterializationID:         item.MaterializationID,
			ArtifactSnapshotID:        item.ArtifactSnapshotID,
			ClasspathSnapshotIDs:      append([]string(nil), item.ClasspathSnapshotIDs...),
			SourceRoots:               append([]string(nil), item.SourceRoots...),
			BackingArtifactID:         item.BackingArtifactID,
			BackingArtifactPath:       item.BackingArtifactPath,
			ProducedArtifactIDs:       append([]string(nil), item.ProducedArtifactIDs...),
			ProducedArtifactPaths:     append([]string(nil), item.ProducedArtifactPaths...),
			ProducedArtifacts:         append([]project.ResolvedVariantArtifact(nil), item.ProducedArtifacts...),
			ProducedArtifactKinds:     append([]string(nil), item.ProducedArtifactKinds...),
			InstallArtifactID:         item.InstallArtifactID,
			InstallArtifactPath:       item.InstallArtifactPath,
			ResourceArtifactIDs:       append([]string(nil), item.ResourceArtifactIDs...),
			ResourceArtifactPaths:     append([]string(nil), item.ResourceArtifactPaths...),
			ManifestArtifactIDs:       append([]string(nil), item.ManifestArtifactIDs...),
			ManifestArtifactPaths:     append([]string(nil), item.ManifestArtifactPaths...),
			Installable:               item.Installable,
			Testable:                  item.Testable,
			Debuggable:                item.Debuggable,
			SigningConfigured:         item.SigningConfigured,
			SigningConfig:             item.SigningConfig,
			DexMode:                   item.DexMode,
			MinifyEnabled:             item.MinifyEnabled,
			ShrinkResources:           item.ShrinkResources,
			InstallTask:               item.InstallTask,
			UninstallTask:             item.UninstallTask,
			AndroidTestPackage:        item.AndroidTestPackage,
			AndroidTestManifest:       item.AndroidTestManifest,
			AndroidTestInstallTask:    item.AndroidTestInstallTask,
			AndroidTestUninstallTask:  item.AndroidTestUninstallTask,
		})
	}
	return out
}
