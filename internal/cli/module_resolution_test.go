package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaeawc/grit/internal/project"
)

type moduleSpec struct {
	Path string
	Type string
}

func projectWithModules(specs []moduleSpec) *project.Project {
	prj := &project.Project{}
	for _, spec := range specs {
		prj.Modules = append(prj.Modules, project.Module{Path: spec.Path, Type: spec.Type})
	}
	return prj
}

func TestTasksResolvesDefaultModuleFromNonAppApplication(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "settings.gradle.kts"), `
rootProject.name = "DefaultModuleResolutionTest"
include(":client")
`)
	mustWriteFile(t, filepath.Join(root, "build.gradle.kts"), `plugins {}`)
	clientDir := filepath.Join(root, "client")
	if err := os.MkdirAll(clientDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(clientDir, "build.gradle.kts"), `
plugins { alias(libs.plugins.android.application) }
android {
  namespace = "com.example.client"
  compileSdk = 34
}
`)

	var stdout, stderr strings.Builder
	exitCode := Run(context.Background(), []string{"tasks", "--repo", root}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("tasks exited with %d: stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}
	var resp struct {
		Success bool `json:"success"`
		Result  struct {
			Module string `json:"module"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Success {
		t.Fatalf("expected success, got %s", stdout.String())
	}
	if resp.Result.Module != ":client" {
		t.Fatalf("expected default module to resolve to :client, got %q", resp.Result.Module)
	}
}

func TestTasksReportsCandidatesWhenMultipleApplications(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "settings.gradle.kts"), `
rootProject.name = "MultiAppResolutionTest"
include(":alpha")
include(":beta")
`)
	mustWriteFile(t, filepath.Join(root, "build.gradle.kts"), `plugins {}`)
	for _, name := range []string{"alpha", "beta"} {
		moduleDir := filepath.Join(root, name)
		if err := os.MkdirAll(moduleDir, 0o755); err != nil {
			t.Fatal(err)
		}
		mustWriteFile(t, filepath.Join(moduleDir, "build.gradle.kts"), `
plugins { alias(libs.plugins.android.application) }
android {
  namespace = "com.example.`+name+`"
  compileSdk = 34
}
`)
	}

	var stdout, stderr strings.Builder
	exitCode := Run(context.Background(), []string{"tasks", "--repo", root}, &stdout, &stderr)
	if exitCode == 0 {
		t.Fatalf("expected ambiguous-module failure, got stdout=%s", stdout.String())
	}
	var resp struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &resp); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.Error.Message, ":alpha") || !strings.Contains(resp.Error.Message, ":beta") {
		t.Fatalf("expected error to list candidate modules, got %q", resp.Error.Message)
	}
	if !strings.Contains(resp.Error.Message, "--module") {
		t.Fatalf("expected error to mention --module flag, got %q", resp.Error.Message)
	}
}

func TestTasksReportsCandidatesWhenNoApplicationModule(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "settings.gradle.kts"), `
rootProject.name = "LibraryOnlyTest"
include(":core")
include(":utils")
`)
	mustWriteFile(t, filepath.Join(root, "build.gradle.kts"), `plugins {}`)
	for _, name := range []string{"core", "utils"} {
		moduleDir := filepath.Join(root, name)
		if err := os.MkdirAll(moduleDir, 0o755); err != nil {
			t.Fatal(err)
		}
		mustWriteFile(t, filepath.Join(moduleDir, "build.gradle.kts"), `
plugins { alias(libs.plugins.android.library) }
android {
  namespace = "com.example.`+name+`"
  compileSdk = 34
}
`)
	}

	var stdout, stderr strings.Builder
	exitCode := Run(context.Background(), []string{"tasks", "--repo", root}, &stdout, &stderr)
	if exitCode == 0 {
		t.Fatalf("expected resolution failure with library-only project, got stdout=%s", stdout.String())
	}
	var resp struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &resp); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.Error.Message, ":core") || !strings.Contains(resp.Error.Message, ":utils") {
		t.Fatalf("expected error to list candidate modules, got %q", resp.Error.Message)
	}
}

func TestTasksPicksSoleLibraryModuleAsDefault(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "settings.gradle.kts"), `
rootProject.name = "SingleLibraryTest"
include(":core")
`)
	mustWriteFile(t, filepath.Join(root, "build.gradle.kts"), `plugins {}`)
	coreDir := filepath.Join(root, "core")
	if err := os.MkdirAll(coreDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(coreDir, "build.gradle.kts"), `
plugins { alias(libs.plugins.android.library) }
android {
  namespace = "com.example.core"
  compileSdk = 34
}
`)

	var stdout, stderr strings.Builder
	exitCode := Run(context.Background(), []string{"tasks", "--repo", root}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("tasks exited with %d: stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}
	var resp struct {
		Result struct {
			Module string `json:"module"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Result.Module != ":core" {
		t.Fatalf("expected sole library module to be default, got %q", resp.Result.Module)
	}
}

func TestCompileAllModulesRejectsConflictWithModuleFlag(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "settings.gradle.kts"), `include(":app")`)
	mustWriteFile(t, filepath.Join(root, "build.gradle.kts"), `plugins {}`)
	appDir := filepath.Join(root, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(appDir, "build.gradle.kts"), `
plugins { alias(libs.plugins.android.application) }
android { namespace = "com.example.app"; compileSdk = 34 }
`)

	var stdout, stderr strings.Builder
	exitCode := Run(context.Background(), []string{"compile", "--repo", root, "--all-modules", "--module", ":app"}, &stdout, &stderr)
	if exitCode == 0 {
		t.Fatalf("expected conflict failure, got %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "--all-modules conflicts with --module") {
		t.Fatalf("expected conflict message, got %s", stdout.String())
	}
}

func TestCompileAllModulesFansOutAcrossSettingsModules(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "settings.gradle.kts"), `
rootProject.name = "AllModulesFanOutTest"
include(":app")
include(":core")
include(":utils")
`)
	mustWriteFile(t, filepath.Join(root, "build.gradle.kts"), `plugins {}`)
	appDir := filepath.Join(root, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(appDir, "build.gradle.kts"), `
plugins { alias(libs.plugins.android.application) }
android { namespace = "com.example.app"; compileSdk = 34 }
`)
	for _, name := range []string{"core", "utils"} {
		moduleDir := filepath.Join(root, name)
		if err := os.MkdirAll(moduleDir, 0o755); err != nil {
			t.Fatal(err)
		}
		mustWriteFile(t, filepath.Join(moduleDir, "build.gradle.kts"), `
plugins { alias(libs.plugins.android.library) }
android { namespace = "com.example.`+name+`"; compileSdk = 34 }
`)
	}

	var stdout, stderr strings.Builder
	Run(context.Background(), []string{"compile-debug", "--repo", root, "--all-modules"}, &stdout, &stderr)

	var resp struct {
		Command string `json:"command"`
		Result  struct {
			Repo    string `json:"repo"`
			Modules []struct {
				Module  string `json:"module"`
				Success bool   `json:"success"`
				Error   string `json:"error,omitempty"`
			} `json:"modules"`
			Failed []string `json:"failed,omitempty"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &resp); err != nil {
		t.Fatalf("failed to parse response: %v\nstdout=%s", err, stdout.String())
	}
	if resp.Command != "compile-debug" {
		t.Fatalf("unexpected command in response: %q", resp.Command)
	}
	if len(resp.Result.Modules) != 3 {
		t.Fatalf("expected fan-out across 3 modules, got %d: %+v", len(resp.Result.Modules), resp.Result.Modules)
	}
	seen := make(map[string]bool, len(resp.Result.Modules))
	for _, m := range resp.Result.Modules {
		seen[m.Module] = true
	}
	for _, want := range []string{":app", ":core", ":utils"} {
		if !seen[want] {
			t.Fatalf("expected module %s in fan-out result, got %+v", want, resp.Result.Modules)
		}
	}
}

func TestResolveModulePathPickHelpers(t *testing.T) {
	t.Run("single application", func(t *testing.T) {
		prj := projectWithModules([]moduleSpec{{":app", "android-application"}})
		got, err := resolveModulePath(prj, "")
		if err != nil {
			t.Fatal(err)
		}
		if got != ":app" {
			t.Fatalf("expected :app, got %q", got)
		}
	})
	t.Run("explicit module passes through", func(t *testing.T) {
		prj := projectWithModules([]moduleSpec{{":app", "android-application"}, {":lib", "android-library"}})
		got, err := resolveModulePath(prj, ":lib")
		if err != nil {
			t.Fatal(err)
		}
		if got != ":lib" {
			t.Fatalf("expected :lib, got %q", got)
		}
	})
	t.Run("single library defaults", func(t *testing.T) {
		prj := projectWithModules([]moduleSpec{{":lib", "android-library"}})
		got, err := resolveModulePath(prj, "")
		if err != nil {
			t.Fatal(err)
		}
		if got != ":lib" {
			t.Fatalf("expected :lib, got %q", got)
		}
	})
	t.Run("multiple applications error", func(t *testing.T) {
		prj := projectWithModules([]moduleSpec{{":a", "android-application"}, {":b", "android-application"}})
		_, err := resolveModulePath(prj, "")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), ":a") || !strings.Contains(err.Error(), ":b") {
			t.Fatalf("expected candidates in error, got %v", err)
		}
	})
	t.Run("no modules", func(t *testing.T) {
		prj := projectWithModules(nil)
		_, err := resolveModulePath(prj, "")
		if err == nil {
			t.Fatal("expected error")
		}
	})
}
