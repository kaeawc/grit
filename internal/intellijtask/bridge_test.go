package intellijtask

import "testing"

func TestResolveAndroidTaskNames(t *testing.T) {
	req := Request{
		Settings: Settings{
			ExternalProjectPath: "/repo",
			ModulePath:          ":app",
			ModuleKind:          ModuleKindAndroidApplication,
			TaskNames:           []string{"assembleDebug", "testDebugUnitTest"},
		},
	}
	got, err := req.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 requests, got %d: %#v", len(got), got)
	}
	if got[0].Command != "assemble-debug" || got[0].RequestedVariant != "debug" || !got[0].VariantExplicit {
		t.Fatalf("unexpected assemble request: %#v", got[0])
	}
	if got[1].Command != "test-debug-unit" || got[1].RequestedVariant != "debug" || !got[1].VariantExplicit {
		t.Fatalf("unexpected test request: %#v", got[1])
	}
	if got[0].Command != "assemble-debug" || got[0].RequestedVariant != "debug" {
		t.Fatalf("unexpected normalized request: %#v", got[0])
	}
}

func TestResolveFlavoredAndroidTaskNames(t *testing.T) {
	req := Request{
		Settings: Settings{
			ExternalProjectPath: "/repo",
			ModulePath:          ":app",
			ModuleKind:          ModuleKindAndroidApplication,
			TaskNames: []string{
				"assembleFreeDebug",
				"installFreeRelease",
				"installFreeDebugAndroidTest",
				"testFreeDebugUnitTest",
				"compileFreeDebugSources",
				"compileFreeDebugUnitTestSources",
				"uninstallFreeDebugAndroidTest",
			},
		},
	}
	got, err := req.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 7 {
		t.Fatalf("expected 7 requests, got %d: %#v", len(got), got)
	}
	assertRequest := func(taskName, command, variant string) {
		t.Helper()
		for _, item := range got {
			if item.TaskName != taskName {
				continue
			}
			if item.Command != command || item.RequestedVariant != variant || !item.VariantExplicit {
				t.Fatalf("unexpected request for %s: %#v", taskName, item)
			}
			return
		}
		t.Fatalf("missing request for %s: %#v", taskName, got)
	}
	assertRequest("assembleFreeDebug", "assemble-debug", "freeDebug")
	assertRequest("installFreeRelease", "install-release", "freeRelease")
	assertRequest("installFreeDebugAndroidTest", "install-android-tests", "freeDebug")
	assertRequest("testFreeDebugUnitTest", "test-debug-unit", "freeDebug")
	assertRequest("compileFreeDebugSources", "compile-debug", "freeDebug")
	assertRequest("compileFreeDebugUnitTestSources", "compileDebugUnitTestSources", "freeDebug")
	assertRequest("uninstallFreeDebugAndroidTest", "uninstall-android-tests", "freeDebug")
}

func TestResolveJvmBuildTaskDefaultsToMain(t *testing.T) {
	req := Request{
		Settings: Settings{
			ModulePath: ":protocol",
			ModuleKind: ModuleKindJvmLibrary,
			TaskNames:  []string{"build"},
		},
	}
	got, err := req.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 request, got %d: %#v", len(got), got)
	}
	if got[0].Command != "build" || got[0].RequestedVariant != "main" || got[0].VariantExplicit {
		t.Fatalf("unexpected jvm request: %#v", got[0])
	}
}

func TestResolveQualifiedTaskInfersModulePath(t *testing.T) {
	req := Request{
		Settings: Settings{
			ModuleKind: ModuleKindAndroidApplication,
			TaskNames:  []string{":playground:app:assembleRelease"},
		},
	}
	got, err := req.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 request, got %d: %#v", len(got), got)
	}
	if got[0].ModulePath != ":playground:app" {
		t.Fatalf("expected inferred module path, got %#v", got[0])
	}
	if got[0].Command != "assemble-release" || got[0].RequestedVariant != "release" || !got[0].VariantExplicit {
		t.Fatalf("unexpected release request: %#v", got[0])
	}
}

func TestResolveRejectsVariantConflict(t *testing.T) {
	req := Request{
		Settings: Settings{
			ModulePath:       ":app",
			ModuleKind:       ModuleKindAndroidApplication,
			TaskNames:        []string{"assembleDebug"},
			RequestedVariant: "release",
			VariantExplicit:  true,
		},
	}
	if _, err := req.Resolve(); err == nil {
		t.Fatal("expected conflict error")
	}
}

func TestResolveRejectsFlavoredVariantConflict(t *testing.T) {
	req := Request{
		Settings: Settings{
			ModulePath:       ":app",
			ModuleKind:       ModuleKindAndroidApplication,
			TaskNames:        []string{"assembleFreeDebug"},
			RequestedVariant: "freeRelease",
			VariantExplicit:  true,
		},
	}
	if _, err := req.Resolve(); err == nil {
		t.Fatal("expected flavored conflict error")
	}
}
