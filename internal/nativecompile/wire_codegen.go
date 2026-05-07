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
// are missing from the local Gradle cache) this returns a zero-valued
// result with no error; the caller can still proceed with the regular
// compile step.
func (c *Compiler) runWireCodegen(ctx context.Context, prj *project.Project, mod *project.Module, variantName string, stdout, stderr *os.File) (wireCodegenResult, error) {
	var out wireCodegenResult
	if mod == nil || !mod.UsesWire {
		return out, nil
	}

	cfg := mod.WireConfig
	wireRuntimeVersion := latestCachedVersionFor("com.squareup.wire", "wire-runtime-jvm")
	if wireRuntimeVersion == "" {
		wireRuntimeVersion = latestCachedVersionFor("com.squareup.wire", "wire-runtime")
	}
	// Always surface the runtime classpath when the plugin is applied — the
	// module may consume wire types from another generator without producing
	// any of its own (a JVM utility module is the canonical example).
	if wireRuntimeVersion != "" {
		out.RuntimeClasspath = wireRuntimeClasspath(wireRuntimeVersion)
	}

	protoFiles := discoverProtoFiles(cfg.SourcePaths)
	if len(protoFiles) == 0 {
		return out, nil
	}

	wireVersion := latestCachedVersionFor("com.squareup.wire", "wire-compiler")
	if wireVersion == "" {
		// No wire compiler in the local cache — surface a soft warning via
		// stderr so the user can prime the cache, and skip codegen.
		if stderr != nil {
			_, _ = fmt.Fprintf(stderr, "grit: wire plugin applied on %s but com.squareup.wire:wire-compiler is not in the gradle cache — skipping codegen\n", mod.Path)
		}
		return out, nil
	}

	compilerJar := firstWireCompilerJar(wireVersion)
	if compilerJar == "" {
		if stderr != nil {
			_, _ = fmt.Fprintf(stderr, "grit: wire-compiler-%s.jar not found in the gradle cache — skipping codegen\n", wireVersion)
		}
		return out, nil
	}
	compilerCP := wireCompilerClasspath(wireVersion)
	if len(compilerCP) == 0 {
		if stderr != nil {
			_, _ = fmt.Fprintf(stderr, "grit: wire-compiler classpath could not be assembled from the gradle cache — skipping codegen\n")
		}
		return out, nil
	}

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
	out.RuntimeClasspath = wireRuntimeClasspath(wireVersion)
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

func firstWireCompilerJar(version string) string {
	jars := findGradleArtifactJars("com.squareup.wire", "wire-compiler", version)
	if len(jars) == 0 {
		return ""
	}
	return jars[0]
}

// wireCompilerClasspath returns the runtime jars needed to invoke
// `com.squareup.wire.WireCompiler`. Wire compiler depends on a constellation
// of wire-* sub-modules, kotlinpoet, javapoet, okio, and guava; rather than
// hardcoding versions, we glob the gradle cache for the matching wire
// version (compiler/schema/generators are all released together) and fall
// back to the latest cached versions for transitive deps that are versioned
// independently.
func wireCompilerClasspath(version string) []string {
	parts := mergePaths(
		findGradleArtifactJars("com.squareup.wire", "wire-compiler", version),
		findGradleArtifactJars("com.squareup.wire", "wire-schema", version),
		findGradleArtifactJars("com.squareup.wire", "wire-schema-jvm", version),
		findGradleArtifactJars("com.squareup.wire", "wire-runtime", version),
		findGradleArtifactJars("com.squareup.wire", "wire-runtime-jvm", version),
		findGradleArtifactJars("com.squareup.wire", "wire-kotlin-generator", version),
		findGradleArtifactJars("com.squareup.wire", "wire-java-generator", version),
		findGradleArtifactJars("com.squareup.wire", "wire-swift-generator", version),
		findGradleArtifactJars("com.squareup.wire", "wire-grpc-server-generator", version),
		findGradleArtifactJars("com.squareup.wire", "wire-grpc-client", version),
		findGradleArtifactJars("com.squareup.wire", "wire-grpc-client-jvm", version),
	)
	parts = mergePaths(parts, latestArtifactJars("com.squareup.okio", "okio"))
	parts = mergePaths(parts, latestArtifactJars("com.squareup.okio", "okio-jvm"))
	parts = mergePaths(parts, latestArtifactJars("com.squareup", "kotlinpoet"))
	parts = mergePaths(parts, latestArtifactJars("com.squareup", "kotlinpoet-jvm"))
	parts = mergePaths(parts, latestArtifactJars("com.squareup", "javapoet"))
	parts = mergePaths(parts, latestArtifactJars("com.google.guava", "guava"))
	parts = mergePaths(parts, latestArtifactJars("com.google.guava", "failureaccess"))
	parts = mergePaths(parts, latestArtifactJars("org.jetbrains.kotlin", "kotlin-stdlib"))
	parts = mergePaths(parts, latestArtifactJars("org.jetbrains.kotlin", "kotlin-stdlib-jdk8"))
	parts = mergePaths(parts, latestArtifactJars("org.jetbrains.kotlin", "kotlin-stdlib-jdk7"))
	parts = mergePaths(parts, latestArtifactJars("org.jetbrains.kotlin", "kotlin-reflect"))
	return parts
}

// wireRuntimeClasspath returns the jars needed at compile time by code that
// uses Wire-generated types. wire-runtime is a Kotlin-multiplatform module
// whose JVM artifact is published as wire-runtime-jvm; we add both forms so
// projects on either coordinate convention still work. okio is wire's
// runtime serialization dependency.
func wireRuntimeClasspath(version string) []string {
	jars := mergePaths(
		findGradleArtifactJars("com.squareup.wire", "wire-runtime", version),
		findGradleArtifactJars("com.squareup.wire", "wire-runtime-jvm", version),
	)
	jars = mergePaths(jars,
		latestArtifactJars("com.squareup.okio", "okio-jvm"),
		latestArtifactJars("com.squareup.okio", "okio"),
	)
	return jars
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
