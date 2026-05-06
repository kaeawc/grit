# Signal-Android Gradle coverage

Measured against `/Users/jason/github/Signal-Android` on 2026-05-06.

The Signal-Android worktree was dirty before this pass, including tracked source edits and existing `.grit`/kaze artifacts, so this pass treated that repository as read-only. Source-change warm-cache benchmarks should run from a clean clone or disposable copy before using the numbers as regression gates.

## Repository shape

- Gradle root: `Signal`
- Declared modules parsed by grit: 27
- Parsed variants: 70
- Planned actions: 408
- Repositories parsed: 10, including Google, Maven Central, Gradle Plugin Portal, Signal's SQLCipher repository, Cloudsmith, and filtered JCenter usage.
- Included build: `build-logic`
- Build scripts: mixed `.gradle.kts` and `.gradle`
- App module plugins include Android application, Kotlin Android, Compose, Wire, ktlint, Detekt, translations, and licenses.

## Commands exercised

| Scenario | grit | Gradle | Notes |
| --- | ---: | ---: | --- |
| `inspect --repo Signal-Android` | 0.13s | n/a | parsed repository, modules, variants, repositories, and action graph |
| `projects --repo Signal-Android` | 0.08s | n/a | listed 27 modules |
| `tasks --module :app` | 0.06s | n/a | reported 174 app tasks: 92 supported, 82 unsupported after applying Signal's enabled-variant filter and custom build types |
| `doctor --repo Signal-Android` | 0.04s | n/a | now succeeds without `kotlinc` on `PATH` by using the project Kotlin compiler jar from Gradle cache |
| `:core-util-jvm` `compile`, first grit run | 16.45s | 13.29s | grit includes first-run compiler/cache setup; Gradle used `:core-util-jvm:compileKotlin --no-daemon` after prior root task exploration |
| `:core-util-jvm` `compile`, warm grit run | 0.09s | 2.84s | Gradle reused configuration cache and reported `compileKotlin UP-TO-DATE` |
| `./gradlew tasks --all --no-daemon` | n/a | 18s | succeeded and exposed the full Signal task surface |

## Supported surface found

- Project loading, repository parsing, version catalog parsing, included `build-logic` discovery, and mixed Groovy/Kotlin module discovery work.
- JVM module task reporting works for `:core-util-jvm`: `assemble`, `build`, `clean`, `compile`, `test`, `check`, `buildNeeded`, and `buildDependents`.
- JVM `compile` now dispatches through the CLI and runs successfully for `:core-util-jvm`.
- App module task reporting handles flavored assemble/compile/install/test names for the currently parsed flavor matrix.
- Debug-like android-test compile/install aliases are reported as supported; standalone android-test assemble remains unsupported.

## Improvements made from this pass

- `doctor` no longer fails just because `kotlinc` is absent from `PATH` when the project's `kotlin-compiler-embeddable` jar is present in the Gradle cache.
- `javaToolchains` now reports the cached Kotlin compiler jar when no `kotlinc` executable is available.
- The top-level `compile` command is now accepted by the CLI and routed to native JVM compilation.
- JVM compile planning now includes the `compile` command, not only Android-style compile aliases.
- Signal-style `androidComponents.beforeVariants` filters that assign `variant.enable = variant.name in selectableVariants` are now parsed for local `listOf(...)` string allowlists, so disabled app variants such as `websiteStagingDebug` are no longer advertised or planned.
- Signal-style `buildTypes.create(...)` blocks are now parsed alongside `getByName(...)`, including `initWith(getByName(...))` and single-string `matchingFallbacks += "..."`, so selectable `spinner`, `perf`, `benchmark`, `canary`, and `instrumentation` variants appear with their task aliases.

## Needs improvement

- Root `qa`, module `qa`, `buildQa`, `format`, ktlint, Detekt, and dependency-analysis tasks are not modeled as first-class grit tasks.
- Lint tasks remain unsupported. Signal's lint setup includes custom `lintChecks(project(":lintchecks"))`, baselines, SARIF/HTML outputs, and generated lint model metadata, so a lint executor needs incremental inputs for source, resources, lint checks, baselines, and report outputs.
- Standalone android-test APK assemble tasks remain unsupported even though debug-like android-test compile/install paths exist.
- Release unit-test and release android-test compile/test aliases remain unsupported.
- App package plans need validation on real Signal variants after custom build types and variant filtering are parsed. `assemblePlayProdDebug` currently plans as a single package action over transitive source roots, which is too coarse to trust for incremental compile/package boundaries without execution tests.

## Next component candidates

1. Add first-class ktlint/Detekt task reporting as unsupported or partially supported instead of hiding those task families behind generic Gradle output.
2. Start the lint executor design with cache keys for lint checks, baselines, source/resource roots, generated lint models, and report outputs.
