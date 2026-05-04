package nativecompile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/kaeawc/grit/internal/modulebuild"
	"github.com/kaeawc/grit/internal/project"
)

func loadModuleSnapshot(prj *project.Project, mod *project.Module, variantName string) (moduleSnapshot, bool) {
	snapshotPath := moduleSnapshotPath(prj, mod, variantName)
	compileStampPath := filepath.Join(filepath.Dir(filepath.Join(prj.RootDir, "build", "grit", moduleOutputRelPath(mod.Path), variantName, "classes")), "compile.stamp")
	if !pathIsFile(snapshotPath) || !pathIsFile(compileStampPath) {
		localInputs := moduleLocalSnapshotInputs(prj, mod)
		sharedSnapshot := sharedModuleSnapshotPath(prj, mod, variantName, localInputs)
		if !restoreSharedModuleSnapshot(snapshotPath, sharedSnapshot) || !pathIsFile(compileStampPath) {
			return moduleSnapshot{}, false
		}
	}
	localInputs := moduleLocalSnapshotInputs(prj, mod)
	if !outputsNewerThanInputs(snapshotPath, localInputs) {
		return moduleSnapshot{}, false
	}
	data, err := os.ReadFile(snapshotPath)
	if err != nil {
		return moduleSnapshot{}, false
	}
	var snapshot moduleSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return moduleSnapshot{}, false
	}
	for depPath, expected := range snapshot.DirectDepStamps {
		depStampPath := moduleCompileStampPath(prj, depPath, variantName)
		if !stampMatches(depStampPath, expected) {
			return moduleSnapshot{}, false
		}
	}
	return snapshot, true
}

func saveModuleSnapshot(prj *project.Project, mod *project.Module, variantName string, deps *modulebuild.Dependencies, out compiledModule) error {
	snapshot := moduleSnapshot{
		RuntimeInputs:    append([]string{}, out.runtimeInputs...),
		AndroidResources: append([]androidResourceArtifact{}, out.androidResources...),
		DirectDepStamps:  map[string]string{},
	}
	for _, depPath := range directProjectDepPaths(deps) {
		stampPath := moduleCompileStampPath(prj, depPath, variantName)
		data, err := os.ReadFile(stampPath)
		if err != nil {
			continue
		}
		snapshot.DirectDepStamps[depPath] = strings.TrimSpace(string(data))
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	localSnapshotPath := moduleSnapshotPath(prj, mod, variantName)
	if err := writeFileIfChanged(localSnapshotPath, payload); err != nil {
		return err
	}
	sharedSnapshotPath := sharedModuleSnapshotPath(prj, mod, variantName, moduleLocalSnapshotInputs(prj, mod))
	return writeSharedModuleSnapshot(sharedSnapshotPath, payload)
}

func directProjectDepPaths(deps *modulebuild.Dependencies) []string {
	seen := map[string]bool{}
	var out []string
	for _, refs := range [][]modulebuild.Ref{deps.Main, deps.Debug, deps.CompileOnly, deps.RuntimeOnly, deps.Test, deps.TestCompileOnly, deps.TestRuntimeOnly} {
		for _, ref := range refs {
			if ref.Kind != "project" || seen[ref.Value] {
				continue
			}
			seen[ref.Value] = true
			out = append(out, ref.Value)
		}
	}
	return out
}

func moduleSnapshotPath(prj *project.Project, mod *project.Module, variantName string) string {
	return filepath.Join(prj.RootDir, "build", "grit", moduleOutputRelPath(mod.Path), variantName, "module-snapshot.json")
}

func sharedModuleSnapshotPath(prj *project.Project, mod *project.Module, variantName string, localInputs []string) string {
	sum := sha256.New()
	sum.Write([]byte("module-snapshot-v1"))
	sum.Write([]byte{0})
	sum.Write([]byte(prj.RootDir))
	sum.Write([]byte{0})
	sum.Write([]byte(mod.Path))
	sum.Write([]byte{0})
	sum.Write([]byte(variantName))
	sum.Write([]byte{0})
	for _, input := range localInputs {
		sum.Write([]byte(cacheIdentityForInput(input)))
		sum.Write([]byte{0})
	}
	return filepath.Join(sharedNativeCacheRoot(), "module-snapshots", hex.EncodeToString(sum.Sum(nil))+".json")
}

func moduleCompileStampPath(prj *project.Project, modulePath, variantName string) string {
	return filepath.Join(prj.RootDir, "build", "grit", moduleOutputRelPath(modulePath), variantName, "compile.stamp")
}

func moduleLocalSnapshotInputs(prj *project.Project, mod *project.Module) []string {
	return []string{
		mod.BuildFile,
		prj.SettingsFile,
		prj.RootBuildFile,
	}
}

func restoreSharedModuleSnapshot(localPath, sharedPath string) bool {
	if !pathIsFile(sharedPath) {
		return false
	}
	return copyFile(sharedPath, localPath) == nil
}

func writeSharedModuleSnapshot(path string, data []byte) error {
	return writeFileIfChanged(path, data)
}
