package nativecompile

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/kaeawc/grit/internal/project"
)

func TestDiscoverProtoFiles(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.proto"), "syntax = \"proto3\";\n")
	mustWrite(t, filepath.Join(dir, "nested", "b.proto"), "syntax = \"proto3\";\n")
	mustWrite(t, filepath.Join(dir, "ignore.txt"), "x")

	got := discoverProtoFiles([]string{dir})
	sort.Strings(got)
	want := []string{filepath.Join(dir, "a.proto"), filepath.Join(dir, "nested", "b.proto")}
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("discoverProtoFiles = %v, want %v", got, want)
	}
}

func TestBuildWireArgsKotlinTarget(t *testing.T) {
	cfg := project.WireConfig{
		SourcePaths:  []string{"/repo/mod/src/main/protowire"},
		KotlinTarget: true,
		JavaInterop:  true,
	}
	args := buildWireArgs(cfg, []string{"/repo/mod/src/main/protowire/foo.proto"}, "/out/gen")

	want := map[string]bool{
		"--proto_path=/repo/mod/src/main/protowire": true,
		"--kotlin_out=/out/gen":                     true,
		"--java_interop":                            true,
		"foo.proto":                                 true,
	}
	for _, arg := range args {
		delete(want, arg)
	}
	if len(want) > 0 {
		t.Fatalf("missing args from %v; remaining=%v", args, want)
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "--java_out=") {
			t.Fatalf("unexpected --java_out arg without java target: %v", args)
		}
	}
}

func TestBuildWireArgsBothTargets(t *testing.T) {
	cfg := project.WireConfig{
		SourcePaths:  []string{"/p"},
		KotlinTarget: true,
		JavaTarget:   true,
	}
	args := buildWireArgs(cfg, []string{"/p/x.proto"}, "/g")
	hasKotlin := false
	hasJava := false
	for _, a := range args {
		if strings.HasPrefix(a, "--kotlin_out=") {
			hasKotlin = true
		}
		if strings.HasPrefix(a, "--java_out=") {
			hasJava = true
		}
	}
	if !hasKotlin || !hasJava {
		t.Fatalf("expected both kotlin and java outs, got %v", args)
	}
}

func TestWireConfigFingerprintStable(t *testing.T) {
	cfg := project.WireConfig{
		SourcePaths:  []string{"/a", "/b"},
		KotlinTarget: true,
		JavaInterop:  true,
	}
	a := wireConfigFingerprint(cfg)
	b := wireConfigFingerprint(cfg)
	if a != b {
		t.Fatalf("fingerprint not stable: %s vs %s", a, b)
	}

	cfg2 := cfg
	cfg2.JavaInterop = false
	if wireConfigFingerprint(cfg2) == a {
		t.Fatalf("fingerprint did not change when JavaInterop flipped")
	}
}

// runWireCodegen should be a no-op when the module does not apply Wire.
func TestRunWireCodegenSkipsWhenPluginAbsent(t *testing.T) {
	c := &Compiler{}
	mod := &project.Module{Path: ":x", Dir: t.TempDir()}
	prj := &project.Project{RootDir: t.TempDir()}
	out, err := c.runWireCodegen(t.Context(), prj, mod, "debug", os.Stderr, os.Stderr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.GeneratedDir != "" || len(out.ProtoFiles) > 0 {
		t.Fatalf("expected zero result for non-wire module, got %+v", out)
	}
}

// When the plugin is applied but no proto files exist on disk, codegen
// should silently no-op and return no error.
func TestRunWireCodegenNoProtos(t *testing.T) {
	c := &Compiler{}
	dir := t.TempDir()
	mod := &project.Module{
		Path:     ":x",
		Dir:      dir,
		UsesWire: true,
		WireConfig: project.WireConfig{
			SourcePaths:  []string{filepath.Join(dir, "src", "main", "protowire")},
			KotlinTarget: true,
		},
	}
	prj := &project.Project{RootDir: t.TempDir()}
	out, err := c.runWireCodegen(t.Context(), prj, mod, "debug", os.Stderr, os.Stderr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.GeneratedDir != "" {
		t.Fatalf("expected no generated dir when there are no protos, got %q", out.GeneratedDir)
	}
}

func mustWrite(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
