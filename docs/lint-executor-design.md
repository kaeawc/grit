# Lint executor design

This design targets Android lint support for repositories like Signal-Android, where lint uses custom checks, generated lint models, baselines, and multiple report formats. The executor should be incremental from the first implementation: every run is keyed by normalized inputs, and no task should be considered reusable unless its complete input set is known.

## Scope

- Support module-level `lint`, variant lint tasks such as `lintPlayProdDebug`, `lintVitalRelease`, and baseline update tasks as separate actions.
- Treat lint execution as unsupported until the executor can produce reports and diagnostics from declared inputs without delegating to Gradle.
- Keep ktlint and Detekt separate; their task families are now reported, but they need their own executors and cache keys.

## Incremental inputs

Each lint action cache key should include:

- Tool identity: Android Gradle Plugin version, lint CLI or embedded lint jars, Java toolchain, and JVM arguments that affect lint behavior.
- Module model: namespace, compile SDK, min/target SDK, merged manifest placeholders, variant build type/flavor coordinates, and dependency graph for the lint target.
- Source and resource roots: main, flavor, build type, variant, generated source roots, `res/`, `assets/`, AIDL, RenderScript if present, and generated resources.
- Manifests: source manifests, merged manifest output, manifest merge inputs, and manifest placeholder values.
- Classpath: compile classpath, annotation-generated classes used by lint, lint model metadata, and project dependency outputs that lint consumes.
- Lint configuration: `lint {}` options, disabled checks, severity overrides, `lint.xml`, Gradle properties that affect lint, baseline path and contents, and report configuration.
- Custom checks: `lintChecks(...)` project coordinates, source/config for those projects, produced lint-check jars, and transitive dependencies for those jars.
- Reports: requested output formats and paths, including XML, SARIF, HTML, text, and baseline-update outputs.

## Execution model

1. Build or reuse lint-check jars first. They should be normal compile/package actions whose output artifact IDs flow into lint action inputs.
2. Materialize lint model metadata into deterministic files under grit build output. The metadata action should be separately cacheable because many lint variants share model inputs.
3. Run lint per target variant with an action key composed from the normalized inputs above.
4. Store report artifacts in CAS and copy them to requested output paths only after the action completes.
5. Convert lint findings into the existing diagnostics payload so `explain`, run summaries, and future IDE sync can consume the same structure.

## Cache policy

- A warm run with no input changes must reuse the lint model, lint-check jars, and lint reports.
- Editing one Kotlin/Java source file should invalidate only lint actions whose source-set order includes that file.
- Editing a resource or manifest should invalidate variants that merge that resource or manifest.
- Editing `lint-baseline.xml`, `lint.xml`, or `lint {}` options should invalidate affected lint actions but not compile/package actions.
- Editing the `:lintchecks` project should invalidate the lint-check jar and all lint actions that depend on it.
- Changing only report output paths should not invalidate analysis if the report content is otherwise identical; the executor should reuse analysis and recopy reports.

## Signal-Android notes

Signal's `:app` module currently uses:

- `lintChecks(project(":lintchecks"))`
- `lint-baseline.xml`
- SARIF output under `build/reports/lint-results.sarif`
- variant-specific baseline update tasks for selectable variants
- generated lint model metadata tasks exposed by AGP

The first implementation should target one debug-like Signal variant, then validate source-only, resource-only, manifest-only, baseline-only, and lint-check-only edits before broadening to all selectable variants.
