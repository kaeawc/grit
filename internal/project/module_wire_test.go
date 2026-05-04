package project

import (
	"path/filepath"
	"testing"
)

func TestDetectWirePlugin(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"id apply form", `plugins { id("com.squareup.wire") }`, true},
		{"groovy id apply form", `plugins { id 'com.squareup.wire' }`, true},
		{"version-catalog plugin alias", `plugins { alias(libs.plugins.square.wire) }`, true},
		{"absent", `plugins { id("com.android.library") }`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectWirePlugin(tc.body); got != tc.want {
				t.Fatalf("detectWirePlugin(%q) = %v, want %v", tc.body, got, tc.want)
			}
		})
	}
}

func TestParseWireConfigSourcePath(t *testing.T) {
	body := `
wire {
  kotlin {
    javaInterop = true
  }

  sourcePath {
    srcDir("src/main/protowire")
  }
}
`
	cfg := parseWireConfig(body, "/tmp/mod")
	if !cfg.KotlinTarget {
		t.Fatalf("expected KotlinTarget=true, got %+v", cfg)
	}
	if !cfg.JavaInterop {
		t.Fatalf("expected JavaInterop=true, got %+v", cfg)
	}
	want := []string{filepath.Join("/tmp/mod", "src/main/protowire")}
	if len(cfg.SourcePaths) != 1 || cfg.SourcePaths[0] != want[0] {
		t.Fatalf("SourcePaths = %v, want %v", cfg.SourcePaths, want)
	}
}

func TestParseWireConfigDefaultsWhenBlockMissing(t *testing.T) {
	cfg := parseWireConfig("plugins { id(\"com.squareup.wire\") }", "/tmp/m")
	if !cfg.KotlinTarget {
		t.Fatalf("expected default KotlinTarget=true, got %+v", cfg)
	}
	want := filepath.Join("/tmp/m", "src", "main", "proto")
	if len(cfg.SourcePaths) != 1 || cfg.SourcePaths[0] != want {
		t.Fatalf("default SourcePaths = %v, want [%s]", cfg.SourcePaths, want)
	}
}

func TestParseWireConfigProtoLibrary(t *testing.T) {
	body := `
wire {
  protoLibrary = true
  kotlin { javaInterop = true }
  sourcePath { srcDir("src/main/protowire") }
  custom { schemaHandlerFactoryClass = "org.signal.wire.Factory" }
}
`
	cfg := parseWireConfig(body, "/tmp/mod")
	if !cfg.ProtoLibrary {
		t.Fatalf("expected ProtoLibrary=true, got %+v", cfg)
	}
	if !cfg.KotlinTarget || !cfg.JavaInterop {
		t.Fatalf("expected kotlin javaInterop, got %+v", cfg)
	}
}
