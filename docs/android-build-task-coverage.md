# android-build task coverage

Measured against `/Users/jason/kaeawc/android-build` on 2026-05-05.

## Supported and measured

These tasks execute through grit and were benchmarked against the nearest Gradle task or task set.

| Scenario | grit | Gradle | Notes |
| --- | ---: | ---: | --- |
| `assembleDebug`, no source change | 0.13s | 0.76s | grit action duration 123ms |
| `assembleRelease`, no source change | 0.03s | 1.45s | release command now routes to release |
| `build` equivalent, no source change | 0.14s | 4.71s | Gradle task set: assemble debug, assemble release, test debug unit |
| `testDebugUnitTest`, no source change | 0.09s | included in 2.57s combined sample | unit compile and JUnit run reused |
| `compileDebugAndroidTestSources`, no source change | 0.07s | included in 2.57s combined sample | android-test compile reused |
| main Kotlin comment edit -> `assembleDebug` | 0.12s | 7.57s | single file: `Widget.kt` |
| resource XML comment edit -> `assembleDebug` | 0.12s | 0.85s | single file: `strings.xml` |
| unit-test Kotlin comment edit -> `testDebugUnitTest` | 0.09s | 0.53s | single file: `FakeClockTest.kt` |
| android-test Kotlin comment edit -> `compileDebugAndroidTestSources` | 0.06s | 0.40s | single file: `ExampleInstrumentedTest.kt` |

## Supported but not benchmarked here

- `compileDebugSources`
- `compileReleaseSources`
- `compileDebugUnitTestSources`
- `assembleUnitTest`
- `check`
- `buildNeeded`
- `buildDependents`
- install and uninstall tasks (`installDebug`, `installRelease`, `installDebugAndroidTest`, `uninstall*`) because they require device state.
- reporting/help tasks (`tasks`, `dependencies`, `dependencyInsight`, `properties`, `buildEnvironment`, `javaToolchains`, etc.).

## Not currently supported

- `bundle`, `bundleDebug`, `bundleRelease`, and related bundle/APKS extraction tasks.
- `lint`, `lintDebug`, `lintRelease`, `lintVitalRelease`, and `lintFix`.
- direct `assemble*AndroidTest` APK build tasks. `installDebugAndroidTest` can build and install an android-test APK, but the standalone assemble task is not wired as a first-class action.
- connected/device test tasks (`connectedAndroidTest`, `connectedDebugAndroidTest`, `connectedCheck`, `deviceAndroidTest`, `deviceCheck`).
- Play publishing tasks from Gradle Play Publisher.
- dependency-analysis plugin tasks such as `buildHealth`, `computeAllDependencies`, and duplicate dependency reports.
- release unit-test and release android-test task aliases (`testReleaseUnitTest`, `compileReleaseUnitTestSources`, `compileReleaseAndroidTestSources`).

## Improvements made from this pass

- Native CLI commands no longer default `--variant` to `debug`, so release-specific commands infer `release`.
- `compileDebugAndroidTestSources` is now exposed by the CLI.
- `tasks` no longer marks lint or standalone android-test assemble tasks as supported before execution exists.
- Unit-test and android-test compilation now keep semantic source fingerprints and can skip dependency resolution/compilation for comment/whitespace-only single-file test source edits.

## Remaining work

- Add first-class bundle/AAB actions with incremental resource, dex, and signing reuse.
- Add a real lint executor and cache lint analysis/report outputs incrementally.
- Add standalone android-test APK assemble actions instead of only supporting the install path.
- Generalize variant-specific unit-test and android-test CLI aliases beyond debug.
- Extend incremental source compilation beyond comment/whitespace-only edits; meaningful Kotlin ABI changes still need finer-grained dependency tracking to avoid full test-source recompilation.
