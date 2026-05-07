package nativecompile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kaeawc/grit/internal/dependencywiring"
	"github.com/kaeawc/grit/internal/project"
)

// wireCodegenResult describes the outcome of running the Wire compiler
// against a module. The generated source directory is added to the
// kotlinc input set, and the proto-related cache inputs are folded into
// the compile cache key so changes invalidate stale outputs.
type wireCodegenResult struct {
	GeneratedDir     string
	ProtoFiles       []string
	CacheInputs      []string
	WireVersion      string
	CompilerJar      string
	CompilerCP       []string
	RuntimeClasspath []string
}

// runWireCodegen scans the module's wire source roots, runs the Wire
// compiler if proto files are present, and returns paths to the generated
// sources plus the runtime jars that must join the kotlinc classpath.
//
// When the plugin is applied but no .proto files exist (or wire artifacts
// cannot be resolved through dependency wiring) this returns a zero-valued
// result with no error; the caller can still proceed with the regular
// compile step.
func (c *Compiler) runWireCodegen(ctx context.Context, prj *project.Project, mod *project.Module, variantName string, resolver dependencywiring.DependencyResolver, stdout, stderr *os.File) (wireCodegenResult, error) {
	var out wireCodegenResult
	if mod == nil || !mod.UsesWire {
		return out, nil
	}

	cfg := mod.WireConfig
	wireVersion, err := wirePluginVersion(prj)
	if err != nil {
		return out, fmt.Errorf("resolve wire plugin version: %w", err)
	}
	if wireVersion == "" {
		if stderr != nil {
			_, _ = fmt.Fprintf(stderr, "grit: wire plugin applied on %s but com.squareup.wire plugin version could not be resolved from the version catalog — skipping codegen\n", mod.Path)
		}
		return out, nil
	}
	// Always surface the runtime classpath when the plugin is applied — the
	// module may consume wire types from another generator without producing
	// any of its own (a JVM utility module is the canonical example).
	runtimeCP, err := resolveWireRuntimeClasspath(resolver, wireVersion)
	if err != nil {
		return out, fmt.Errorf("resolve wire runtime classpath: %w", err)
	}
	out.RuntimeClasspath = runtimeCP

	protoFiles := discoverProtoFiles(cfg.SourcePaths)
	if len(protoFiles) == 0 {
		return out, nil
	}

	compilerCP, err := resolveWireCompilerClasspath(resolver, wireVersion)
	if err != nil {
		return out, fmt.Errorf("resolve wire compiler classpath: %w", err)
	}
	if len(compilerCP) == 0 {
		if stderr != nil {
			_, _ = fmt.Fprintf(stderr, "grit: wire-compiler classpath could not be resolved through dependency wiring — skipping codegen\n")
		}
		return out, nil
	}
	compilerJar := firstPathContaining(compilerCP, "com.squareup.wire", "wire-compiler")

	generatedDir := wireGeneratedSourceDir(prj, mod, variantName)
	if err := os.RemoveAll(generatedDir); err != nil && !os.IsNotExist(err) {
		return out, fmt.Errorf("clean wire generated dir: %w", err)
	}
	if err := os.MkdirAll(generatedDir, 0o755); err != nil {
		return out, fmt.Errorf("create wire generated dir: %w", err)
	}

	args := buildWireArgs(cfg, protoFiles, generatedDir)
	if err := runJavaMain(ctx, compilerCP, "com.squareup.wire.WireCompiler", args, stdout, stderr); err != nil {
		return out, fmt.Errorf("wire-compiler failed for %s: %w", mod.Path, err)
	}

	cacheInputs := append([]string(nil), protoFiles...)
	cacheInputs = append(cacheInputs, "wire-compiler:"+wireVersion)
	cacheInputs = append(cacheInputs, "wire-config:"+wireConfigFingerprint(cfg))

	out.GeneratedDir = generatedDir
	out.ProtoFiles = protoFiles
	out.CacheInputs = cacheInputs
	out.WireVersion = wireVersion
	out.CompilerJar = compilerJar
	out.CompilerCP = compilerCP
	out.RuntimeClasspath = runtimeCP
	return out, nil
}

// buildWireArgs constructs the command-line flags for WireCompiler.main(...).
// Source directories are passed via --proto_path; relative file names are
// passed positionally so wire only emits sources for files that physically
// live in the module (rather than every type discovered transitively).
func buildWireArgs(cfg project.WireConfig, protoFiles []string, generatedDir string) []string {
	var args []string

	for _, sp := range cfg.SourcePaths {
		args = append(args, "--proto_path="+sp)
	}
	for _, pp := range cfg.ProtoPaths {
		args = append(args, "--proto_path="+pp)
	}

	if cfg.JavaTarget {
		args = append(args, "--java_out="+generatedDir)
	}
	// Default Kotlin output unless the script explicitly opted into Java only.
	if cfg.KotlinTarget || !cfg.JavaTarget {
		args = append(args, "--kotlin_out="+generatedDir)
	}

	if cfg.JavaInterop {
		args = append(args, "--java_interop")
	}

	for _, inc := range cfg.Includes {
		args = append(args, "--includes="+inc)
	}
	for _, exc := range cfg.Excludes {
		args = append(args, "--excludes="+exc)
	}

	for _, proto := range protoFiles {
		args = append(args, relativeProtoPath(cfg, proto))
	}
	return args
}

func relativeProtoPath(cfg project.WireConfig, proto string) string {
	for _, root := range cfg.SourcePaths {
		if rel, err := filepath.Rel(root, proto); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return filepath.Base(proto)
}

func discoverProtoFiles(roots []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d == nil || d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".proto") {
				return nil
			}
			if _, ok := seen[path]; ok {
				return nil
			}
			seen[path] = struct{}{}
			out = append(out, path)
			return nil
		})
	}
	sort.Strings(out)
	return out
}

func wireGeneratedSourceDir(prj *project.Project, mod *project.Module, variantName string) string {
	moduleSegment := strings.Trim(strings.ReplaceAll(mod.Path, ":", "_"), "_")
	if moduleSegment == "" {
		moduleSegment = filepath.Base(mod.Dir)
	}
	return filepath.Join(prj.RootDir, "build", "grit", "generated", "wire", moduleSegment, variantName)
}

func wirePluginVersion(prj *project.Project) (string, error) {
	version, err := dependencywiring.ResolvePluginVersion(prj, "wire", "square.wire", "square-wire", "com.squareup.wire")
	if err != nil || version != "" {
		return version, err
	}
	if prj != nil && prj.VersionCatalogData != nil {
		for _, key := range []string{"wire", "square-wire", "square.wire"} {
			if v := strings.TrimSpace(prj.VersionCatalogData[key]); v != "" {
				return v, nil
			}
		}
	}
	return "", nil
}

func resolveWireCompilerClasspath(resolver dependencywiring.DependencyResolver, version string) ([]string, error) {
	return dependencywiring.ResolveRawClasspath(resolver, "com.squareup.wire:wire-compiler:"+version)
}

func resolveWireRuntimeClasspath(resolver dependencywiring.DependencyResolver, version string) ([]string, error) {
	return dependencywiring.ResolveRawClasspath(resolver, "com.squareup.wire:wire-runtime-jvm:"+version)
}

func firstPathContaining(paths []string, parts ...string) string {
	for _, path := range paths {
		match := true
		for _, part := range parts {
			if !strings.Contains(path, part) {
				match = false
				break
			}
		}
		if match {
			return path
		}
	}
	return ""
}

func latestArtifactJars(group, module string) []string {
	version := latestCachedVersionFor(group, module)
	if version == "" {
		return nil
	}
	return findGradleArtifactJars(group, module, version)
}

func wireConfigFingerprint(cfg project.WireConfig) string {
	sum := sha256.New()
	for _, sp := range cfg.SourcePaths {
		sum.Write([]byte("src:"))
		sum.Write([]byte(sp))
		sum.Write([]byte{0})
	}
	for _, pp := range cfg.ProtoPaths {
		sum.Write([]byte("proto:"))
		sum.Write([]byte(pp))
		sum.Write([]byte{0})
	}
	_, _ = fmt.Fprintf(sum, "k:%v;j:%v;ji:%v;lib:%v;",
		cfg.KotlinTarget, cfg.JavaTarget, cfg.JavaInterop, cfg.ProtoLibrary)
	for _, inc := range cfg.Includes {
		sum.Write([]byte("inc:"))
		sum.Write([]byte(inc))
		sum.Write([]byte{0})
	}
	for _, exc := range cfg.Excludes {
		sum.Write([]byte("exc:"))
		sum.Write([]byte(exc))
		sum.Write([]byte{0})
	}
	return hex.EncodeToString(sum.Sum(nil))
}
