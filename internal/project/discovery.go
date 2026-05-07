package project

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type DiscoverySnapshot struct {
	SchemaVersion int                             `json:"schemaVersion"`
	Modules       map[string][]GeneratedSourceSet `json:"modules,omitempty"`
}

func DiscoverySnapshotPath(prj *Project) string {
	if prj == nil || strings.TrimSpace(prj.RootDir) == "" {
		return ""
	}
	return filepath.Join(prj.RootDir, ".grit", "metadata", "discovery", "generated-sources.json")
}

func LoadDiscoverySnapshot(prj *Project) (DiscoverySnapshot, error) {
	path := DiscoverySnapshotPath(prj)
	if path == "" {
		return DiscoverySnapshot{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DiscoverySnapshot{}, nil
		}
		return DiscoverySnapshot{}, err
	}
	var snapshot DiscoverySnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return DiscoverySnapshot{}, err
	}
	return snapshot, nil
}

func ApplyDiscoverySnapshot(prj *Project, snapshot DiscoverySnapshot) {
	if prj == nil || len(snapshot.Modules) == 0 {
		return
	}
	for i := range prj.Modules {
		sets := snapshot.Modules[prj.Modules[i].Path]
		for j := range sets {
			sets[j].Discovered = true
		}
		prj.Modules[i].GeneratedSources = uniqueGeneratedSourceSets(append(prj.Modules[i].GeneratedSources, sets...))
	}
}

func RefreshDiscoverySnapshot(ctx context.Context, prj *Project) error {
	if prj == nil || prj.DiscoveryMode == "static" {
		return nil
	}
	path := DiscoverySnapshotPath(prj)
	if path == "" {
		return nil
	}
	if !prj.RefreshDiscovery {
		if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) < 24*time.Hour {
			return nil
		}
	}
	wrapper := filepath.Join(prj.RootDir, "gradlew")
	if _, err := os.Stat(wrapper); err != nil {
		if prj.DiscoveryMode == "snapshot" {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	initScript, err := os.CreateTemp("", "grit-discovery-*.gradle")
	if err != nil {
		return err
	}
	initPath := initScript.Name()
	_, writeErr := initScript.WriteString(gradleDiscoveryInitScript(path))
	closeErr := initScript.Close()
	defer func() { _ = os.Remove(initPath) }()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	runCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, wrapper, "-q", "gritDiscoverySnapshot", "--init-script", initPath)
	cmd.Dir = prj.RootDir
	if err := cmd.Run(); err != nil && prj.DiscoveryMode == "snapshot" {
		return err
	}
	return nil
}

func gradleDiscoveryInitScript(outPath string) string {
	escaped := strings.ReplaceAll(outPath, "\\", "\\\\")
	escaped = strings.ReplaceAll(escaped, "'", "\\'")
	return `
import groovy.json.JsonOutput

gradle.projectsLoaded {
  gradle.rootProject {
    tasks.register('gritDiscoverySnapshot') {
      doLast {
        def modules = [:]
        rootProject.allprojects.each { p ->
          def sets = []
          def generatedRoot = new File(p.buildDir, 'generated')
          if (generatedRoot.isDirectory()) {
            generatedRoot.eachDirRecurse { d ->
              def hasSources = d.listFiles()?.any { f -> f.name.endsWith('.kt') || f.name.endsWith('.java') } ?: false
              if (hasSources) {
                sets << [provider:'gradle-discovery', language:'mixed', scope:'main', dirs:[d.absolutePath], discovered:true, producedByGrit:false]
              }
            }
          }
          if (!sets.isEmpty()) {
            modules[p.path] = sets
          }
        }
        def out = new File('` + escaped + `')
        out.parentFile.mkdirs()
        out.text = JsonOutput.prettyPrint(JsonOutput.toJson([schemaVersion:1, modules:modules]))
      }
    }
  }
}
`
}
