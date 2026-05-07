package nativecompile

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/kaeawc/grit/internal/project"
)

func TestKotlincArgs(t *testing.T) {
	t.Setenv("HOME", "/home/test")

	got := kotlincArgs(
		"/sdk/android.jar",
		[]string{"A.kt", "B.kt"},
		"out/classes",
		[]string{"lib-a.jar", "lib-b.jar"},
		[]string{"plugin-a.jar"},
		[]string{"plugin:androidx.compose.compiler.plugins.kotlin:suppressKotlinVersionCompatibilityCheck=true"},
		true,
		[]string{"-Xexplicit-api=strict"},
	)
	want := []string{
		"-jvm-target", "21",
		"-no-stdlib",
		"-no-reflect",
		"-Xcontext-parameters",
		"-d", "out/classes",
		"-opt-in=kotlin.time.ExperimentalTime",
		"-opt-in=kotlin.RequiresOptIn",
		"-opt-in=kotlinx.coroutines.ExperimentalCoroutinesApi",
		"-opt-in=kotlin.ExperimentalUnsignedTypes",
		"-opt-in=kotlinx.coroutines.FlowPreview",
		"-Xplugin=plugin-a.jar",
		"-P", "plugin:androidx.compose.compiler.plugins.kotlin:suppressKotlinVersionCompatibilityCheck=true",
		"-Xexplicit-api=strict",
		"-classpath", "/sdk/android.jar" + string(os.PathListSeparator) + "lib-a.jar" + string(os.PathListSeparator) + "lib-b.jar",
		"A.kt", "B.kt",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected kotlinc args:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestJUnitPlatformArgs(t *testing.T) {
	got := junitPlatformArgs([]string{"TestOne", "TestTwo"}, []string{"rt-a.jar", "rt-b.jar"})
	want := []string{"-cp", "rt-a.jar" + string(os.PathListSeparator) + "rt-b.jar", "grit.junit.PlatformRunner", "TestOne", "TestTwo"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected junit platform args: got %#v want %#v", got, want)
	}
}

func TestJavacArgs(t *testing.T) {
	got := javacArgs([]string{"A.java"}, "out/classes", []string{"lib-a.jar"})
	want := []string{"-source", "21", "-target", "21", "-d", "out/classes", "-classpath", "lib-a.jar", "A.java"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected javac args: got %#v want %#v", got, want)
	}
}

func TestJarArgs(t *testing.T) {
	got := jarArgs("out/module.jar")
	want := []string{"cf", "out/module.jar", "."}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected jar args: got %#v want %#v", got, want)
	}
}

func TestD8Args(t *testing.T) {
	libArgs := d8LibraryArgs("/sdk/android.jar", []string{"a.jar", "b.jar"}, "out/lib-dex")
	libWant := []string{
		"--lib", "/sdk/android.jar",
		"--min-api", "27",
		"--map-diagnostics", "info", "warning",
		"--output", "out/lib-dex",
		"a.jar", "b.jar",
	}
	if !reflect.DeepEqual(libArgs, libWant) {
		t.Fatalf("unexpected library d8 args: got %#v want %#v", libArgs, libWant)
	}

	appArgs := d8AppArgs("/sdk/android.jar", "classes.jar", []string{"lib-a.jar", "lib-b.jar"}, "out/app-dex")
	appWant := []string{
		"--lib", "/sdk/android.jar",
		"--min-api", "27",
		"--map-diagnostics", "info", "warning",
		"--output", "out/app-dex",
		"--classpath", "lib-a.jar",
		"--classpath", "lib-b.jar",
		"classes.jar",
	}
	if !reflect.DeepEqual(appArgs, appWant) {
		t.Fatalf("unexpected app d8 args: got %#v want %#v", appArgs, appWant)
	}
}

func TestD8ReleaseArgs(t *testing.T) {
	libArgs := d8ReleaseLibraryArgs("/sdk/android.jar", "24", []string{"a.jar", "b.jar"}, "out/lib-dex")
	libWant := []string{
		"--release",
		"--lib", "/sdk/android.jar",
		"--min-api", "24",
		"--map-diagnostics", "info", "warning",
		"--output", "out/lib-dex",
		"a.jar", "b.jar",
	}
	if !reflect.DeepEqual(libArgs, libWant) {
		t.Fatalf("unexpected release library d8 args: got %#v want %#v", libArgs, libWant)
	}

	appArgs := d8ReleaseAppArgs("/sdk/android.jar", "24", "classes.jar", []string{"lib-a.jar", "lib-b.jar"}, "out/app-dex")
	appWant := []string{
		"--release",
		"--lib", "/sdk/android.jar",
		"--min-api", "24",
		"--map-diagnostics", "info", "warning",
		"--output", "out/app-dex",
		"--classpath", "lib-a.jar",
		"--classpath", "lib-b.jar",
		"classes.jar",
	}
	if !reflect.DeepEqual(appArgs, appWant) {
		t.Fatalf("unexpected release app d8 args: got %#v want %#v", appArgs, appWant)
	}
}

func TestR8Args(t *testing.T) {
	t.Setenv("HOME", "/home/test")

	tc := &androidDexToolchain{Version: "8.6.27", Source: "dependency", JarPath: "/deps/r8-8.6.27.jar"}
	mod := &project.Module{MinSDK: "24"}
	variant := project.BuildType{ProguardFiles: []string{"a.pro", "b.pro"}}
	got := r8Args(tc, "/sdk/android.jar", mod, variant, "classes.jar", "dex-out", []string{"rt-a.jar", "rt-b.jar"}, "generated-rules.pro")
	want := []string{
		"-cp", "/deps/r8-8.6.27.jar",
		"com.android.tools.r8.R8",
		"--release",
		"--lib", "/sdk/android.jar",
		"--min-api", "24",
		"--map-diagnostics", "info", "warning",
		"--output", "dex-out",
		"--pg-compat",
		"--pg-conf", "a.pro",
		"--pg-conf", "b.pro",
		"--pg-conf", "generated-rules.pro",
		"rt-a.jar", "rt-b.jar",
		"classes.jar",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected r8 args: got %#v want %#v", got, want)
	}
	for _, arg := range got {
		if strings.Contains(arg, "build-tools/36.0.0") {
			t.Fatalf("r8 args must not contain hard-coded build-tools path: %#v", got)
		}
	}
}

func TestADBInstallArgs(t *testing.T) {
	got := adbInstallArgs("device-123", "app.apk")
	want := []string{"-s", "device-123", "install", "-r", "app.apk"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected adb args: got %#v want %#v", got, want)
	}
	if got := adbInstallArgs("", "app.apk"); !reflect.DeepEqual(got, []string{"install", "-r", "app.apk"}) {
		t.Fatalf("unexpected adb args without serial: %#v", got)
	}
}

func TestADBUninstallArgs(t *testing.T) {
	got := adbUninstallArgs("device-123", "com.example.app.test")
	want := []string{"-s", "device-123", "uninstall", "com.example.app.test"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected adb uninstall args: got %#v want %#v", got, want)
	}
	if got := adbUninstallArgs("", "com.example.app.test"); !reflect.DeepEqual(got, []string{"uninstall", "com.example.app.test"}) {
		t.Fatalf("unexpected adb uninstall args without serial: %#v", got)
	}
}
