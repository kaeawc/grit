package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaeawc/grit/internal/catalog"
	"github.com/kaeawc/grit/internal/explain"
	"github.com/kaeawc/grit/internal/intellijsync"
	"github.com/kaeawc/grit/internal/m2local"
	"github.com/kaeawc/grit/internal/modulebuild"
	"github.com/kaeawc/grit/internal/perf"
	"github.com/kaeawc/grit/internal/project"
	"github.com/kaeawc/grit/internal/responsepayload"
	"github.com/kaeawc/grit/internal/service"
)

func TestInspectExposesVariantOptimizationModel(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "settings.gradle.kts"), `
rootProject.name = "InspectTest"
include(":app")
`)
	mustWriteFile(t, filepath.Join(root, "build.gradle.kts"), `
plugins {
  alias(libs.plugins.android.application)
}
`)
	appDir := filepath.Join(root, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(appDir, "build.gradle.kts"), `
plugins {
  alias(libs.plugins.android.application)
}

android {
  namespace = "com.example.app"
  compileSdk = 34
  defaultConfig {
    minSdk = 24
    targetSdk = 34
  }
  buildTypes {
    release {
      isMinifyEnabled = true
      isShrinkResources = true
      packageOptimizations {
        package("com.example.placeholder") {
          minifyEnabled = true
        }
      }
    }
  }
}
`)

	var stdout, stderr strings.Builder
	exitCode := Run(context.Background(), []string{"inspect", "--repo", root}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("inspect exited with %d: stderr=%s", exitCode, stderr.String())
	}

	var resp struct {
		Success bool `json:"success"`
		Result  struct {
			SemanticGraph project.SemanticGraphSummary `json:"semanticGraph"`
			Modules       []struct {
				Path             string                    `json:"path"`
				Variants         []project.BuildType       `json:"variants"`
				ResolvedVariants []project.ResolvedVariant `json:"resolvedVariants"`
			} `json:"modules"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Success {
		t.Fatalf("expected inspect success, got %#v", resp)
	}
	if len(resp.Result.Modules) != 1 {
		t.Fatalf("expected one module, got %#v", resp.Result.Modules)
	}
	if resp.Result.SemanticGraph.NodeCount == 0 || resp.Result.SemanticGraph.EdgeCount == 0 {
		t.Fatalf("expected semantic graph summary, got %#v", resp.Result.SemanticGraph)
	}
	if len(resp.Result.SemanticGraph.Modules) != 1 || len(resp.Result.SemanticGraph.Modules[0].Variants) < 2 {
		t.Fatalf("expected semantic graph module summary, got %#v", resp.Result.SemanticGraph)
	}
	var semanticVariant project.SemanticVariantSummary
	foundSemanticRelease := false
	for _, candidate := range resp.Result.SemanticGraph.Modules[0].Variants {
		if candidate.Name == "release" {
			semanticVariant = candidate
			foundSemanticRelease = true
			break
		}
	}
	if !foundSemanticRelease {
		t.Fatalf("expected release semantic variant, got %#v", resp.Result.SemanticGraph.Modules[0].Variants)
	}
	if semanticVariant.Materialization.ID == "" || semanticVariant.Materialization.ArtifactSnapshotID == "" || len(semanticVariant.Materialization.ClasspathSnapshotIDs) == 0 {
		t.Fatalf("expected semantic graph ids, got %#v", semanticVariant)
	}
	if len(resp.Result.Modules[0].Variants) < 2 {
		t.Fatalf("expected debug and release variants in inspect output, got %#v", resp.Result.Modules[0].Variants)
	}
	if len(resp.Result.Modules[0].ResolvedVariants) < 2 {
		t.Fatalf("expected resolved variants in inspect output, got %#v", resp.Result.Modules[0].ResolvedVariants)
	}
	var variant project.BuildType
	foundRelease := false
	for _, candidate := range resp.Result.Modules[0].Variants {
		if candidate.Name == "release" {
			variant = candidate
			foundRelease = true
			break
		}
	}
	if !foundRelease {
		t.Fatalf("expected release variant, got %#v", resp.Result.Modules[0].Variants)
	}
	if !variant.Optimization.MinifyEnabled || !variant.Optimization.ShrinkResources {
		t.Fatalf("expected optimization flags in inspect output, got %#v", variant.Optimization)
	}
	if len(variant.Optimization.PackageOptimizations) != 1 {
		t.Fatalf("expected package optimization placeholder in inspect output, got %#v", variant.Optimization.PackageOptimizations)
	}
}

func TestIntrospectionCommandsExposeResolvedVariants(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "settings.gradle.kts"), `
rootProject.name = "ResolvedVariantTest"
include(":app")
`)
	mustWriteFile(t, filepath.Join(root, "build.gradle.kts"), `plugins {}`)
	appDir := filepath.Join(root, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(appDir, "build.gradle.kts"), `
plugins { alias(libs.plugins.android.application) }

android {
  namespace = "com.example.app"
  compileSdk = 34
  flavorDimensions += "tier"
  productFlavors {
    create("free") { dimension = "tier" }
    create("paid") { dimension = "tier" }
  }
  buildTypes {
    debug {}
    release {}
  }
}
`)

	var stdout, stderr strings.Builder
	if exitCode := Run(context.Background(), []string{"inspect", "--repo", root}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("inspect exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var inspectResp struct {
		Result struct {
			Modules []struct {
				Path             string                    `json:"path"`
				Variants         []project.BuildType       `json:"variants"`
				ResolvedVariants []project.ResolvedVariant `json:"resolvedVariants"`
			} `json:"modules"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &inspectResp); err != nil {
		t.Fatal(err)
	}
	if len(inspectResp.Result.Modules) != 1 {
		t.Fatalf("unexpected inspect result: %#v", inspectResp)
	}
	if len(inspectResp.Result.Modules[0].ResolvedVariants) != 4 {
		t.Fatalf("expected inspect resolved variants, got %#v", inspectResp.Result.Modules[0].ResolvedVariants)
	}
	if !containsResolvedVariant(inspectResp.Result.Modules[0].ResolvedVariants, "freeDebug", "debug", "free") {
		t.Fatalf("expected freeDebug resolved inspect variant, got %#v", inspectResp.Result.Modules[0].ResolvedVariants)
	}
	foundResolvedVariantMetadata := false
	for _, variant := range inspectResp.Result.Modules[0].ResolvedVariants {
		if variant.Name == "freeDebug" {
			foundResolvedVariantMetadata = true
			if variant.MaterializationID == "" || variant.ArtifactSnapshotID == "" || len(variant.ManifestPaths) == 0 || len(variant.SourceRoots) == 0 || len(variant.ProducedArtifactIDs) == 0 {
				t.Fatalf("expected graph-backed inspect resolved variant metadata, got %#v", variant)
			}
			if variant.Namespace != "com.example.app" {
				t.Fatalf("expected namespace metadata in inspect resolved variant, got %#v", variant)
			}
			if len(variant.ProducedArtifactKinds) == 0 || variant.InstallArtifactID == "" {
				t.Fatalf("expected produced-artifact classification metadata in inspect resolved variant, got %#v", variant)
			}
			if len(variant.ConsumerProguardFiles) != 0 || variant.BackingArtifactPath == "" || len(variant.ProducedArtifactPaths) == 0 {
				t.Fatalf("expected artifact-path metadata in inspect resolved variant, got %#v", variant)
			}
			if variant.InstallTask != "installFreeDebug" || variant.UninstallTask != "uninstallFreeDebug" {
				t.Fatalf("expected install/uninstall task metadata in inspect resolved variant, got %#v", variant)
			}
		}
	}
	if !foundResolvedVariantMetadata {
		t.Fatalf("expected freeDebug resolved inspect variant metadata, got %#v", inspectResp.Result.Modules[0].ResolvedVariants)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"properties", "--repo", root, "--module", ":app"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("properties exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var propsResp struct {
		Result struct {
			ResolvedVariants []project.ResolvedVariant `json:"resolvedVariants"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &propsResp); err != nil {
		t.Fatal(err)
	}
	if len(propsResp.Result.ResolvedVariants) != 4 {
		t.Fatalf("expected property resolved variants, got %#v", propsResp.Result.ResolvedVariants)
	}
	if !containsResolvedVariant(propsResp.Result.ResolvedVariants, "freeDebug", "debug", "free") {
		t.Fatalf("expected freeDebug resolved property variant, got %#v", propsResp.Result.ResolvedVariants)
	}
	for _, variant := range propsResp.Result.ResolvedVariants {
		if variant.Name == "freeDebug" {
			if variant.MaterializationID == "" || variant.ArtifactSnapshotID == "" || len(variant.ManifestPaths) == 0 || len(variant.ProducedArtifactIDs) == 0 {
				t.Fatalf("expected property resolved variant metadata, got %#v", variant)
			}
		}
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"outgoingVariants", "--repo", root, "--module", ":app"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("outgoingVariants exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var outgoingResp struct {
		Result struct {
			ResolvedVariants []project.ResolvedVariant `json:"resolvedVariants"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &outgoingResp); err != nil {
		t.Fatal(err)
	}
	if len(outgoingResp.Result.ResolvedVariants) != 4 {
		t.Fatalf("expected outgoing resolved variants, got %#v", outgoingResp.Result.ResolvedVariants)
	}
	if !containsResolvedVariant(outgoingResp.Result.ResolvedVariants, "freeDebug", "debug", "free") {
		t.Fatalf("expected freeDebug resolved outgoing variant, got %#v", outgoingResp.Result.ResolvedVariants)
	}
	for _, variant := range outgoingResp.Result.ResolvedVariants {
		if variant.Name == "freeDebug" {
			if variant.MaterializationID == "" || variant.ArtifactSnapshotID == "" || len(variant.ManifestPaths) == 0 || len(variant.ProducedArtifactIDs) == 0 {
				t.Fatalf("expected outgoing resolved variant metadata, got %#v", variant)
			}
		}
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"classpathProvenance", "--repo", root, "--module", ":app", "--variant", "freeDebug"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("classpathProvenance exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var classpathResp struct {
		Result struct {
			ModelCacheKey string `json:"modelCacheKey"`
			Provenance    struct {
				ModulePath         string   `json:"modulePath"`
				VariantName        string   `json:"variantName"`
				MaterializationID  string   `json:"materializationId"`
				ArtifactSnapshotID string   `json:"artifactSnapshotId"`
				SourceRoots        []string `json:"sourceRoots"`
				ClasspathSnapshots []struct {
					ID               string   `json:"id"`
					NormalizedID     string   `json:"normalizedId"`
					OrderedEntriesID string   `json:"orderedEntriesId"`
					EntriesDigest    string   `json:"entriesDigest"`
					Entries          []string `json:"entries"`
				} `json:"classpathSnapshots"`
			} `json:"provenance"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &classpathResp); err != nil {
		t.Fatal(err)
	}
	if classpathResp.Result.ModelCacheKey == "" || classpathResp.Result.Provenance.ModulePath != ":app" || classpathResp.Result.Provenance.VariantName != "freeDebug" {
		t.Fatalf("unexpected classpath provenance result: %#v", classpathResp.Result)
	}
	if classpathResp.Result.Provenance.MaterializationID == "" || classpathResp.Result.Provenance.ArtifactSnapshotID == "" {
		t.Fatalf("expected graph-backed ids in classpath provenance, got %#v", classpathResp.Result.Provenance)
	}
	if len(classpathResp.Result.Provenance.SourceRoots) == 0 || len(classpathResp.Result.Provenance.ClasspathSnapshots) == 0 {
		t.Fatalf("expected source roots and classpath snapshots, got %#v", classpathResp.Result.Provenance)
	}
	if classpathResp.Result.Provenance.ClasspathSnapshots[0].ID == "" || classpathResp.Result.Provenance.ClasspathSnapshots[0].NormalizedID == "" || classpathResp.Result.Provenance.ClasspathSnapshots[0].OrderedEntriesID == "" || classpathResp.Result.Provenance.ClasspathSnapshots[0].EntriesDigest == "" {
		t.Fatalf("expected structured classpath snapshot metadata, got %#v", classpathResp.Result.Provenance.ClasspathSnapshots)
	}
	if len(classpathResp.Result.Provenance.ClasspathSnapshots[0].Entries) == 0 {
		t.Fatalf("expected classpath snapshot entries, got %#v", classpathResp.Result.Provenance.ClasspathSnapshots)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"classpathSnapshot", "--repo", root, "--module", ":app", "--variant", "freeDebug"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("classpathSnapshot exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var classpathSnapshotResp struct {
		Result struct {
			ModelCacheKey string `json:"modelCacheKey"`
			Snapshot      struct {
				ModulePath         string `json:"modulePath"`
				VariantName        string `json:"variantName"`
				MaterializationID  string `json:"materializationId"`
				ArtifactSnapshotID string `json:"artifactSnapshotId"`
				Snapshot           struct {
					ID               string `json:"id"`
					NormalizedID     string `json:"normalizedId"`
					OrderedEntriesID string `json:"orderedEntriesId"`
					EntriesDigest    string `json:"entriesDigest"`
					Entries          []struct {
						Path       string `json:"path"`
						ArtifactID string `json:"artifactId"`
					} `json:"entries"`
					Decisions []struct {
						OutputPath string `json:"outputPath"`
					} `json:"decisions"`
				} `json:"snapshot"`
			} `json:"snapshot"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &classpathSnapshotResp); err != nil {
		t.Fatal(err)
	}
	if classpathSnapshotResp.Result.ModelCacheKey == "" || classpathSnapshotResp.Result.Snapshot.ModulePath != ":app" || classpathSnapshotResp.Result.Snapshot.VariantName != "freeDebug" {
		t.Fatalf("unexpected classpathSnapshot result: %#v", classpathSnapshotResp.Result)
	}
	if classpathSnapshotResp.Result.Snapshot.MaterializationID == "" || classpathSnapshotResp.Result.Snapshot.ArtifactSnapshotID == "" {
		t.Fatalf("expected ids in classpathSnapshot result, got %#v", classpathSnapshotResp.Result.Snapshot)
	}
	if classpathSnapshotResp.Result.Snapshot.Snapshot.ID == "" || classpathSnapshotResp.Result.Snapshot.Snapshot.NormalizedID == "" || classpathSnapshotResp.Result.Snapshot.Snapshot.OrderedEntriesID == "" || classpathSnapshotResp.Result.Snapshot.Snapshot.EntriesDigest == "" {
		t.Fatalf("expected record metadata in classpathSnapshot result, got %#v", classpathSnapshotResp.Result.Snapshot.Snapshot)
	}
	if len(classpathSnapshotResp.Result.Snapshot.Snapshot.Entries) == 0 || len(classpathSnapshotResp.Result.Snapshot.Snapshot.Decisions) == 0 {
		t.Fatalf("expected entries and decisions in classpathSnapshot result, got %#v", classpathSnapshotResp.Result.Snapshot.Snapshot)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"classpathSnapshotProvenance", "--repo", root, "--snapshot", classpathSnapshotResp.Result.Snapshot.Snapshot.ID}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("classpathSnapshotProvenance exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var classpathSnapshotProvResp struct {
		Result struct {
			ClasspathSnapshotID string `json:"classpathSnapshotId"`
			ModelCacheKey       string `json:"modelCacheKey"`
			Provenance          struct {
				Variants []struct {
					MaterializationID string `json:"materializationId"`
				} `json:"variants"`
				Artifacts []struct {
					ID string `json:"id"`
				} `json:"artifacts"`
				ManifestPaths []string `json:"manifestPaths"`
			} `json:"provenance"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &classpathSnapshotProvResp); err != nil {
		t.Fatal(err)
	}
	if classpathSnapshotProvResp.Result.ModelCacheKey == "" || classpathSnapshotProvResp.Result.ClasspathSnapshotID != classpathSnapshotResp.Result.Snapshot.Snapshot.ID || len(classpathSnapshotProvResp.Result.Provenance.Variants) == 0 || len(classpathSnapshotProvResp.Result.Provenance.Artifacts) == 0 || len(classpathSnapshotProvResp.Result.Provenance.ManifestPaths) == 0 {
		t.Fatalf("unexpected classpathSnapshotProvenance result: %#v", classpathSnapshotProvResp.Result)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"classpathSnapshotConsumers", "--repo", root, "--snapshot", classpathSnapshotResp.Result.Snapshot.Snapshot.ID}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("classpathSnapshotConsumers exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var classpathSnapshotConsumersResp struct {
		Result struct {
			ClasspathSnapshotID string `json:"classpathSnapshotId"`
			ModelCacheKey       string `json:"modelCacheKey"`
			Consumers           struct {
				Variants []struct {
					MaterializationID string `json:"materializationId"`
				} `json:"variants"`
				Actions []struct {
					ID string `json:"id"`
				} `json:"actions"`
				Artifacts []struct {
					ID string `json:"id"`
				} `json:"artifacts"`
				ManifestPaths []string `json:"manifestPaths"`
			} `json:"consumers"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &classpathSnapshotConsumersResp); err != nil {
		t.Fatal(err)
	}
	if classpathSnapshotConsumersResp.Result.ModelCacheKey == "" || classpathSnapshotConsumersResp.Result.ClasspathSnapshotID != classpathSnapshotResp.Result.Snapshot.Snapshot.ID || len(classpathSnapshotConsumersResp.Result.Consumers.Variants) == 0 || len(classpathSnapshotConsumersResp.Result.Consumers.Actions) == 0 || len(classpathSnapshotConsumersResp.Result.Consumers.Artifacts) == 0 || len(classpathSnapshotConsumersResp.Result.Consumers.ManifestPaths) == 0 {
		t.Fatalf("unexpected classpathSnapshotConsumers result: %#v", classpathSnapshotConsumersResp.Result)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"classpathSnapshotByID", "--repo", root, "--snapshot", classpathSnapshotResp.Result.Snapshot.Snapshot.NormalizedID}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("classpathSnapshotByID exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var classpathSnapshotByIDResp struct {
		Result struct {
			ClasspathSnapshotID string `json:"classpathSnapshotId"`
			ModelCacheKey       string `json:"modelCacheKey"`
			Result              struct {
				LookupID    string `json:"lookupId"`
				CanonicalID string `json:"canonicalId"`
				Result      struct {
					Snapshot struct {
						ID string `json:"id"`
					} `json:"snapshot"`
				} `json:"result"`
			} `json:"result"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &classpathSnapshotByIDResp); err != nil {
		t.Fatal(err)
	}
	if classpathSnapshotByIDResp.Result.ModelCacheKey == "" || classpathSnapshotByIDResp.Result.Result.LookupID != classpathSnapshotResp.Result.Snapshot.Snapshot.NormalizedID || classpathSnapshotByIDResp.Result.Result.CanonicalID != classpathSnapshotResp.Result.Snapshot.Snapshot.ID || classpathSnapshotByIDResp.Result.Result.Result.Snapshot.ID != classpathSnapshotResp.Result.Snapshot.Snapshot.ID {
		t.Fatalf("unexpected classpathSnapshotByID result: %#v", classpathSnapshotByIDResp.Result)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"classpathSnapshotConsumersByID", "--repo", root, "--snapshot", classpathSnapshotResp.Result.Snapshot.Snapshot.OrderedEntriesID}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("classpathSnapshotConsumersByID exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var classpathSnapshotConsumersByIDResp struct {
		Result struct {
			ClasspathSnapshotID string `json:"classpathSnapshotId"`
			ModelCacheKey       string `json:"modelCacheKey"`
			Consumers           struct {
				LookupID    string `json:"lookupId"`
				CanonicalID string `json:"canonicalId"`
				Consumers   struct {
					ClasspathSnapshotID string `json:"classpathSnapshotId"`
					Actions             []struct {
						ID string `json:"id"`
					} `json:"actions"`
				} `json:"consumers"`
			} `json:"consumers"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &classpathSnapshotConsumersByIDResp); err != nil {
		t.Fatal(err)
	}
	if classpathSnapshotConsumersByIDResp.Result.ModelCacheKey == "" || classpathSnapshotConsumersByIDResp.Result.Consumers.LookupID != classpathSnapshotResp.Result.Snapshot.Snapshot.OrderedEntriesID || classpathSnapshotConsumersByIDResp.Result.Consumers.CanonicalID != classpathSnapshotResp.Result.Snapshot.Snapshot.ID || classpathSnapshotConsumersByIDResp.Result.Consumers.Consumers.ClasspathSnapshotID != classpathSnapshotResp.Result.Snapshot.Snapshot.ID || len(classpathSnapshotConsumersByIDResp.Result.Consumers.Consumers.Actions) == 0 {
		t.Fatalf("unexpected classpathSnapshotConsumersByID result: %#v", classpathSnapshotConsumersByIDResp.Result)
	}

	classpathArtifactID := ""
	for _, entry := range classpathSnapshotResp.Result.Snapshot.Snapshot.Entries {
		if entry.ArtifactID != "" {
			classpathArtifactID = entry.ArtifactID
			break
		}
	}
	if classpathArtifactID == "" {
		t.Fatalf("expected at least one artifact-backed classpath entry, got %#v", classpathSnapshotResp.Result.Snapshot.Snapshot.Entries)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"classpathEntryLookup", "--repo", root, "--module", ":app", "--variant", "freeDebug", "--path", filepath.Join(root, "app", "src", "main")}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("classpathEntryLookup exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var classpathLookupResp struct {
		Result struct {
			ModelCacheKey string `json:"modelCacheKey"`
			Lookup        struct {
				ModulePath  string `json:"modulePath"`
				VariantName string `json:"variantName"`
				Path        string `json:"path"`
				Entry       struct {
					Path            string `json:"path"`
					NormalizedPath  string `json:"normalizedPath"`
					SelectionReason string `json:"selectionReason"`
				} `json:"entry"`
				Decisions []struct {
					OutputPath string `json:"outputPath"`
				} `json:"decisions"`
			} `json:"lookup"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &classpathLookupResp); err != nil {
		t.Fatal(err)
	}
	if classpathLookupResp.Result.ModelCacheKey == "" || classpathLookupResp.Result.Lookup.ModulePath != ":app" || classpathLookupResp.Result.Lookup.VariantName != "freeDebug" {
		t.Fatalf("unexpected classpathEntryLookup result: %#v", classpathLookupResp.Result)
	}
	if classpathLookupResp.Result.Lookup.Entry.Path == "" || classpathLookupResp.Result.Lookup.Entry.SelectionReason == "" || len(classpathLookupResp.Result.Lookup.Decisions) == 0 {
		t.Fatalf("expected entry and decisions in classpathEntryLookup result, got %#v", classpathLookupResp.Result.Lookup)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"classpathPathConsumers", "--repo", root, "--path", filepath.Join(root, "app", "src", "main")}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("classpathPathConsumers exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var classpathPathConsumersResp struct {
		Result struct {
			ModelCacheKey string `json:"modelCacheKey"`
			Consumers     struct {
				Path      string `json:"path"`
				Consumers []struct {
					ModulePath  string `json:"modulePath"`
					VariantName string `json:"variantName"`
					Entry       struct {
						Path string `json:"path"`
					} `json:"entry"`
				} `json:"consumers"`
			} `json:"consumers"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &classpathPathConsumersResp); err != nil {
		t.Fatal(err)
	}
	if classpathPathConsumersResp.Result.ModelCacheKey == "" || classpathPathConsumersResp.Result.Consumers.Path == "" || len(classpathPathConsumersResp.Result.Consumers.Consumers) == 0 {
		t.Fatalf("unexpected classpathPathConsumers result: %#v", classpathPathConsumersResp.Result)
	}
	if classpathPathConsumersResp.Result.Consumers.Consumers[0].ModulePath != ":app" || classpathPathConsumersResp.Result.Consumers.Consumers[0].VariantName != "freeDebug" {
		t.Fatalf("unexpected classpathPathConsumers coordinates: %#v", classpathPathConsumersResp.Result.Consumers)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"variantMaterialization", "--repo", root, "--module", ":app", "--variant", "freeDebug"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("variantMaterialization exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var variantMatResp struct {
		Result struct {
			ModelCacheKey string `json:"modelCacheKey"`
			Provenance    struct {
				ModulePath      string `json:"modulePath"`
				VariantName     string `json:"variantName"`
				VariantID       string `json:"variantId"`
				Materialization struct {
					MaterializationID  string   `json:"materializationId"`
					ArtifactSnapshotID string   `json:"artifactSnapshotId"`
					ManifestPaths      []string `json:"manifestPaths"`
				} `json:"materialization"`
			} `json:"provenance"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &variantMatResp); err != nil {
		t.Fatal(err)
	}
	if variantMatResp.Result.ModelCacheKey == "" || variantMatResp.Result.Provenance.Materialization.MaterializationID == "" || variantMatResp.Result.Provenance.Materialization.ArtifactSnapshotID == "" {
		t.Fatalf("unexpected variantMaterialization result: %#v", variantMatResp)
	}
	if len(variantMatResp.Result.Provenance.Materialization.ManifestPaths) == 0 {
		t.Fatalf("expected manifest paths in variantMaterialization result, got %#v", variantMatResp.Result.Provenance)
	}

	stdout.Reset()
	stderr.Reset()
	materializationID := variantMatResp.Result.Provenance.Materialization.MaterializationID
	if exitCode := Run(context.Background(), []string{"materializationProvenance", "--repo", root, "--materialization", materializationID}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("materializationProvenance exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var materializationProvResp struct {
		Result struct {
			MaterializationID string `json:"materializationId"`
			ModelCacheKey     string `json:"modelCacheKey"`
			Provenance        struct {
				ModulePath      string `json:"modulePath"`
				VariantName     string `json:"variantName"`
				Materialization struct {
					ID string `json:"id"`
				} `json:"materialization"`
				Artifacts []struct {
					ID string `json:"id"`
				} `json:"artifacts"`
			} `json:"provenance"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &materializationProvResp); err != nil {
		t.Fatal(err)
	}
	if materializationProvResp.Result.ModelCacheKey == "" || materializationProvResp.Result.MaterializationID != materializationID || materializationProvResp.Result.Provenance.Materialization.ID != materializationID {
		t.Fatalf("unexpected materializationProvenance result: %#v", materializationProvResp.Result)
	}
	if materializationProvResp.Result.Provenance.ModulePath != ":app" || materializationProvResp.Result.Provenance.VariantName != "freeDebug" || len(materializationProvResp.Result.Provenance.Artifacts) == 0 {
		t.Fatalf("expected coordinates and artifacts in materializationProvenance result, got %#v", materializationProvResp.Result.Provenance)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"variantCompatibility", "--repo", root, "--module", ":app", "--variant", "freeDebug"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("variantCompatibility exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var variantCompatibilityResp struct {
		Result struct {
			ModelCacheKey             string `json:"modelCacheKey"`
			VariantID                 string `json:"variantId"`
			MaterializationID         string `json:"materializationId"`
			DisplayName               string `json:"displayName"`
			CompileSDK                string `json:"compileSdk"`
			BuildToolsVersion         string `json:"buildToolsVersion"`
			Namespace                 string `json:"namespace"`
			TestInstrumentationRunner string `json:"testInstrumentationRunner"`
			Optimization              struct {
				MinifyEnabled   bool `json:"minifyEnabled"`
				ShrinkResources bool `json:"shrinkResources"`
			} `json:"optimization"`
			ProguardFiles         []string `json:"proguardFiles"`
			ConsumerProguardFiles []string `json:"consumerProguardFiles"`
			ProducedArtifactKinds []string `json:"producedArtifactKinds"`
			ProducedArtifactPaths []string `json:"producedArtifactPaths"`
			InstallArtifactID     string   `json:"installArtifactId"`
			InstallArtifactPath   string   `json:"installArtifactPath"`
			BackingArtifactPath   string   `json:"backingArtifactPath"`
			ResourceArtifactIDs   []string `json:"resourceArtifactIds"`
			ResourceArtifactPaths []string `json:"resourceArtifactPaths"`
			ManifestArtifactIDs   []string `json:"manifestArtifactIds"`
			ManifestArtifactPaths []string `json:"manifestArtifactPaths"`
			InstallTask           string   `json:"installTask"`
			UninstallTask         string   `json:"uninstallTask"`
			Compatibility         struct {
				VariantName    string   `json:"variantName"`
				CoordinateName string   `json:"coordinateName"`
				DisplayName    string   `json:"displayName"`
				SourceSetOrder []string `json:"sourceSetOrder"`
				SourceSetNames []string `json:"sourceSetNames"`
				TaskAliases    []string `json:"taskAliases"`
				ModelSelectors []string `json:"modelSelectors"`
				SyncFragments  []string `json:"syncFragments"`
			} `json:"compatibility"`
			Materialization struct {
				ID                 string `json:"id"`
				ArtifactSnapshotID string `json:"artifactSnapshotId"`
			} `json:"materialization"`
			Provenance struct {
				MaterializationID string   `json:"materializationId"`
				ManifestPaths     []string `json:"manifestPaths"`
			} `json:"provenance"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &variantCompatibilityResp); err != nil {
		t.Fatal(err)
	}
	if variantCompatibilityResp.Result.ModelCacheKey == "" || variantCompatibilityResp.Result.VariantID == "" || variantCompatibilityResp.Result.MaterializationID == "" || variantCompatibilityResp.Result.DisplayName != "Free Debug" {
		t.Fatalf("unexpected variantCompatibility result: %#v", variantCompatibilityResp.Result)
	}
	if variantCompatibilityResp.Result.Compatibility.DisplayName != "Free Debug" || len(variantCompatibilityResp.Result.Compatibility.SourceSetOrder) == 0 || len(variantCompatibilityResp.Result.Compatibility.SourceSetNames) == 0 || len(variantCompatibilityResp.Result.Compatibility.TaskAliases) == 0 || len(variantCompatibilityResp.Result.Compatibility.ModelSelectors) == 0 || len(variantCompatibilityResp.Result.Compatibility.SyncFragments) == 0 {
		t.Fatalf("unexpected compatibility payload: %#v", variantCompatibilityResp.Result.Compatibility)
	}
	if variantCompatibilityResp.Result.Compatibility.VariantName != "freeDebug" || variantCompatibilityResp.Result.Compatibility.CoordinateName != "freeDebug" {
		t.Fatalf("expected compatibility variant naming metadata, got %#v", variantCompatibilityResp.Result.Compatibility)
	}
	if variantCompatibilityResp.Result.Materialization.ID == "" || variantCompatibilityResp.Result.Materialization.ArtifactSnapshotID == "" || variantCompatibilityResp.Result.ModelCacheKey == "" {
		t.Fatalf("expected materialization ids in variantCompatibility result, got %#v", variantCompatibilityResp.Result.Materialization)
	}
	if len(variantCompatibilityResp.Result.Provenance.ManifestPaths) == 0 {
		t.Fatalf("expected provenance data in variantCompatibility result, got %#v", variantCompatibilityResp.Result.Provenance)
	}
	if variantCompatibilityResp.Result.Namespace != "com.example.app" {
		t.Fatalf("expected namespace metadata in variantCompatibility result, got %#v", variantCompatibilityResp.Result)
	}
	if variantCompatibilityResp.Result.CompileSDK != "34" {
		t.Fatalf("expected compile/proguard-consumer metadata in variantCompatibility result, got %#v", variantCompatibilityResp.Result)
	}
	if variantCompatibilityResp.Result.BackingArtifactPath == "" {
		t.Fatalf("expected backing artifact path in variantCompatibility result, got %#v", variantCompatibilityResp.Result)
	}
	if variantCompatibilityResp.Result.Optimization.MinifyEnabled || variantCompatibilityResp.Result.Optimization.ShrinkResources || len(variantCompatibilityResp.Result.ProguardFiles) != 0 {
		t.Fatalf("expected bounded optimization/proguard metadata in variantCompatibility result, got %#v", variantCompatibilityResp.Result)
	}
	if len(variantCompatibilityResp.Result.ProducedArtifactKinds) == 0 || variantCompatibilityResp.Result.InstallArtifactID == "" {
		t.Fatalf("expected produced-artifact classification metadata in variantCompatibility result, got %#v", variantCompatibilityResp.Result)
	}
	if len(variantCompatibilityResp.Result.ProducedArtifactPaths) == 0 || variantCompatibilityResp.Result.BackingArtifactPath == "" {
		t.Fatalf("expected produced-artifact path metadata in variantCompatibility result, got %#v", variantCompatibilityResp.Result)
	}
	if variantCompatibilityResp.Result.InstallTask != "installFreeDebug" || variantCompatibilityResp.Result.UninstallTask != "uninstallFreeDebug" {
		t.Fatalf("expected install/uninstall task metadata in variantCompatibility result, got %#v", variantCompatibilityResp.Result)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"variantSourceSetModel", "--repo", root, "--module", ":app", "--variant", "freeDebug"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("variantSourceSetModel exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var variantSourceSetModelResp struct {
		Result struct {
			ModelCacheKey  string `json:"modelCacheKey"`
			SourceSetModel struct {
				ModulePath           string   `json:"modulePath"`
				VariantName          string   `json:"variantName"`
				CoordinateName       string   `json:"coordinateName"`
				SourceSetOrder       []string `json:"sourceSetOrder"`
				SourceSetNames       []string `json:"sourceSetNames"`
				SourceRoots          []string `json:"sourceRoots"`
				ManifestPaths        []string `json:"manifestPaths"`
				ClasspathSnapshotIDs []string `json:"classpathSnapshotIds"`
			} `json:"sourceSetModel"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &variantSourceSetModelResp); err != nil {
		t.Fatal(err)
	}
	if variantSourceSetModelResp.Result.ModelCacheKey == "" || variantSourceSetModelResp.Result.SourceSetModel.ModulePath != ":app" || variantSourceSetModelResp.Result.SourceSetModel.VariantName != "freeDebug" {
		t.Fatalf("unexpected variantSourceSetModel result: %#v", variantSourceSetModelResp.Result)
	}
	if variantSourceSetModelResp.Result.SourceSetModel.CoordinateName != "freeDebug" || len(variantSourceSetModelResp.Result.SourceSetModel.SourceSetOrder) == 0 || len(variantSourceSetModelResp.Result.SourceSetModel.SourceSetNames) == 0 || len(variantSourceSetModelResp.Result.SourceSetModel.SourceRoots) == 0 || len(variantSourceSetModelResp.Result.SourceSetModel.ManifestPaths) == 0 || len(variantSourceSetModelResp.Result.SourceSetModel.ClasspathSnapshotIDs) == 0 {
		t.Fatalf("expected source-set/source-root metadata in variantSourceSetModel result, got %#v", variantSourceSetModelResp.Result.SourceSetModel)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"dependencyBindingsForVariant", "--repo", root, "--module", ":app", "--variant", "freeDebug"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("dependencyBindingsForVariant exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var dependencyBindingsForVariantResp struct {
		Result struct {
			ModelCacheKey string `json:"modelCacheKey"`
			Bindings      struct {
				ModulePath   string `json:"modulePath"`
				VariantName  string `json:"variantName"`
				Dependencies []struct {
					ModulePath      string `json:"modulePath"`
					VariantName     string `json:"variantName"`
					DependencyLevel string `json:"dependencyLevel"`
				} `json:"dependencies"`
			} `json:"bindings"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &dependencyBindingsForVariantResp); err != nil {
		t.Fatal(err)
	}
	if dependencyBindingsForVariantResp.Result.ModelCacheKey == "" || dependencyBindingsForVariantResp.Result.Bindings.ModulePath != ":app" || dependencyBindingsForVariantResp.Result.Bindings.VariantName != "freeDebug" {
		t.Fatalf("unexpected dependencyBindingsForVariant result: %#v", dependencyBindingsForVariantResp.Result)
	}
	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"dependencyRealizationsForVariant", "--repo", root, "--module", ":app", "--variant", "freeDebug"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("dependencyRealizationsForVariant exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var dependencyRealizationsForVariantResp struct {
		Result struct {
			ModelCacheKey string `json:"modelCacheKey"`
			Realizations  struct {
				ModulePath   string `json:"modulePath"`
				VariantName  string `json:"variantName"`
				Dependencies []struct {
					ModulePath            string   `json:"modulePath"`
					VariantName           string   `json:"variantName"`
					MaterializationID     string   `json:"materializationId"`
					ArtifactSnapshotID    string   `json:"artifactSnapshotId"`
					SelectionReason       string   `json:"selectionReason"`
					SelectionReasons      []string `json:"selectionReasons"`
					ClasspathSnapshotIDs  []string `json:"classpathSnapshotIds"`
					ManifestPaths         []string `json:"manifestPaths"`
					BackingArtifactID     string   `json:"backingArtifactId"`
					BackingArtifactPath   string   `json:"backingArtifactPath"`
					BackingArtifactKind   string   `json:"backingArtifactKind"`
					ProducedArtifactIDs   []string `json:"producedArtifactIds"`
					ProducedArtifactPaths []string `json:"producedArtifactPaths"`
					ProducedArtifactKinds []string `json:"producedArtifactKinds"`
					BackingArtifact       *struct {
						ID   string `json:"id"`
						Kind string `json:"kind"`
						Path string `json:"path"`
					} `json:"backingArtifact"`
					ProducedArtifacts []struct {
						ID   string `json:"id"`
						Kind string `json:"kind"`
						Path string `json:"path"`
					} `json:"producedArtifacts"`
				} `json:"dependencies"`
			} `json:"realizations"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &dependencyRealizationsForVariantResp); err != nil {
		t.Fatal(err)
	}
	if dependencyRealizationsForVariantResp.Result.ModelCacheKey == "" || dependencyRealizationsForVariantResp.Result.Realizations.ModulePath != ":app" || dependencyRealizationsForVariantResp.Result.Realizations.VariantName != "freeDebug" {
		t.Fatalf("unexpected dependencyRealizationsForVariant result: %#v", dependencyRealizationsForVariantResp.Result)
	}
	for _, depRealization := range dependencyRealizationsForVariantResp.Result.Realizations.Dependencies {
		if depRealization.SelectionReason == "" || len(depRealization.SelectionReasons) == 0 || depRealization.BackingArtifactID == "" || depRealization.BackingArtifactPath == "" || depRealization.BackingArtifactKind == "" {
			t.Fatalf("expected richer dependency realization metadata, got %#v", depRealization)
		}
		if len(depRealization.ManifestPaths) == 0 || len(depRealization.ProducedArtifactIDs) == 0 || len(depRealization.ProducedArtifactPaths) == 0 || len(depRealization.ProducedArtifactKinds) == 0 || len(depRealization.ProducedArtifacts) == 0 {
			t.Fatalf("expected manifest and produced artifact detail in dependency realization, got %#v", depRealization)
		}
		if depRealization.BackingArtifact == nil || depRealization.BackingArtifact.ID == "" || depRealization.BackingArtifact.Path == "" {
			t.Fatalf("expected backing artifact summary in dependency realization, got %#v", depRealization)
		}
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"dependencyBindingsForModule", "--repo", root, "--module", ":app"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("dependencyBindingsForModule exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var dependencyBindingsForModuleResp struct {
		Result struct {
			ModelCacheKey string `json:"modelCacheKey"`
			Bindings      struct {
				ModulePath string `json:"modulePath"`
				Variants   []struct {
					VariantName string `json:"variantName"`
				} `json:"variants"`
			} `json:"bindings"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &dependencyBindingsForModuleResp); err != nil {
		t.Fatal(err)
	}
	if dependencyBindingsForModuleResp.Result.ModelCacheKey == "" || dependencyBindingsForModuleResp.Result.Bindings.ModulePath != ":app" || len(dependencyBindingsForModuleResp.Result.Bindings.Variants) == 0 {
		t.Fatalf("unexpected dependencyBindingsForModule result: %#v", dependencyBindingsForModuleResp.Result)
	}
	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"dependencyRealizationsForModule", "--repo", root, "--module", ":app"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("dependencyRealizationsForModule exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var dependencyRealizationsForModuleResp struct {
		Result struct {
			ModelCacheKey string `json:"modelCacheKey"`
			Realizations  struct {
				ModulePath string `json:"modulePath"`
				Variants   []struct {
					VariantName  string `json:"variantName"`
					Dependencies []struct {
						ModulePath string `json:"modulePath"`
					} `json:"dependencies"`
				} `json:"variants"`
			} `json:"realizations"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &dependencyRealizationsForModuleResp); err != nil {
		t.Fatal(err)
	}
	if dependencyRealizationsForModuleResp.Result.ModelCacheKey == "" || dependencyRealizationsForModuleResp.Result.Realizations.ModulePath != ":app" || len(dependencyRealizationsForModuleResp.Result.Realizations.Variants) == 0 {
		t.Fatalf("unexpected dependencyRealizationsForModule result: %#v", dependencyRealizationsForModuleResp.Result)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"variantManifest", "--repo", root, "--module", ":app", "--variant", "freeDebug"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("variantManifest exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var variantManifestResp struct {
		Result struct {
			ModelCacheKey string `json:"modelCacheKey"`
			Manifest      struct {
				ModulePath           string   `json:"modulePath"`
				VariantName          string   `json:"variantName"`
				VariantID            string   `json:"variantId"`
				MaterializationID    string   `json:"materializationId"`
				ArtifactSnapshotID   string   `json:"artifactSnapshotId"`
				SourceRoots          []string `json:"sourceRoots"`
				ManifestPaths        []string `json:"manifestPaths"`
				ClasspathSnapshotIDs []string `json:"classpathSnapshotIds"`
				ClasspathSnapshots   []struct {
					ID string `json:"id"`
				} `json:"classpathSnapshots"`
				ActionIDs []string `json:"actionIds"`
				Actions   []struct {
					ID string `json:"id"`
				} `json:"actions"`
				ProducedArtifactIDs []string `json:"producedArtifactIds"`
				ProducedArtifacts   []struct {
					ID string `json:"id"`
				} `json:"producedArtifacts"`
				BackingArtifactID string `json:"backingArtifactId"`
				BackingArtifact   struct {
					ID string `json:"id"`
				} `json:"backingArtifact"`
			} `json:"manifest"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &variantManifestResp); err != nil {
		t.Fatal(err)
	}
	if variantManifestResp.Result.ModelCacheKey == "" || variantManifestResp.Result.Manifest.ModulePath != ":app" || variantManifestResp.Result.Manifest.VariantName != "freeDebug" {
		t.Fatalf("unexpected variantManifest result: %#v", variantManifestResp.Result)
	}
	if variantManifestResp.Result.Manifest.MaterializationID == "" || variantManifestResp.Result.Manifest.ArtifactSnapshotID == "" {
		t.Fatalf("expected ids in variantManifest result, got %#v", variantManifestResp.Result.Manifest)
	}
	if len(variantManifestResp.Result.Manifest.SourceRoots) == 0 || len(variantManifestResp.Result.Manifest.ManifestPaths) == 0 {
		t.Fatalf("expected source roots and manifest paths in variantManifest result, got %#v", variantManifestResp.Result.Manifest)
	}
	if len(variantManifestResp.Result.Manifest.ClasspathSnapshotIDs) == 0 || len(variantManifestResp.Result.Manifest.ClasspathSnapshots) == 0 || variantManifestResp.Result.Manifest.ClasspathSnapshots[0].ID == "" {
		t.Fatalf("expected classpath snapshot refs in variantManifest result, got %#v", variantManifestResp.Result.Manifest)
	}
	if len(variantManifestResp.Result.Manifest.ActionIDs) == 0 || len(variantManifestResp.Result.Manifest.Actions) == 0 {
		t.Fatalf("expected actions in variantManifest result, got %#v", variantManifestResp.Result.Manifest)
	}
	if len(variantManifestResp.Result.Manifest.ProducedArtifactIDs) == 0 || len(variantManifestResp.Result.Manifest.ProducedArtifacts) == 0 {
		t.Fatalf("expected produced artifacts in variantManifest result, got %#v", variantManifestResp.Result.Manifest)
	}
	if variantManifestResp.Result.Manifest.BackingArtifactID == "" || variantManifestResp.Result.Manifest.BackingArtifact.ID == "" {
		t.Fatalf("expected backing artifact in variantManifest result, got %#v", variantManifestResp.Result.Manifest)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"variantByID", "--repo", root, "--id", variantManifestResp.Result.Manifest.VariantID}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("variantByID exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var variantByIDResp struct {
		Result struct {
			VariantID     string `json:"variantId"`
			ModelCacheKey string `json:"modelCacheKey"`
			Result        struct {
				Module struct {
					ID   string `json:"id"`
					Path string `json:"path"`
				} `json:"module"`
				Variant struct {
					ID string `json:"id"`
				} `json:"variant"`
				Summary struct {
					Name string `json:"name"`
				} `json:"summary"`
				Materializations []struct {
					ID string `json:"id"`
				} `json:"materializations"`
			} `json:"result"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &variantByIDResp); err != nil {
		t.Fatal(err)
	}
	if variantByIDResp.Result.ModelCacheKey == "" || variantByIDResp.Result.VariantID != variantManifestResp.Result.Manifest.VariantID || variantByIDResp.Result.Result.Module.Path != ":app" || variantByIDResp.Result.Result.Variant.ID != variantManifestResp.Result.Manifest.VariantID || variantByIDResp.Result.Result.Summary.Name != "freeDebug" || len(variantByIDResp.Result.Result.Materializations) == 0 {
		t.Fatalf("unexpected variantByID result: %#v", variantByIDResp.Result)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"moduleByID", "--repo", root, "--id", variantByIDResp.Result.Result.Module.ID}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("moduleByID exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var moduleByIDResp struct {
		Result struct {
			ModuleID      string `json:"moduleId"`
			ModelCacheKey string `json:"modelCacheKey"`
			Result        struct {
				Module struct {
					ID   string `json:"id"`
					Path string `json:"path"`
				} `json:"module"`
				Summary struct {
					Path string `json:"path"`
				} `json:"summary"`
				Variants []struct {
					Name string `json:"name"`
				} `json:"variants"`
			} `json:"result"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &moduleByIDResp); err != nil {
		t.Fatal(err)
	}
	if moduleByIDResp.Result.ModelCacheKey == "" || moduleByIDResp.Result.ModuleID != variantByIDResp.Result.Result.Module.ID || moduleByIDResp.Result.Result.Module.Path != ":app" || moduleByIDResp.Result.Result.Summary.Path != ":app" || len(moduleByIDResp.Result.Result.Variants) == 0 {
		t.Fatalf("unexpected moduleByID result: %#v", moduleByIDResp.Result)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"actionByID", "--repo", root, "--id", variantManifestResp.Result.Manifest.Actions[0].ID}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("actionByID exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var actionByIDResp struct {
		Result struct {
			ActionID      string `json:"actionId"`
			ModelCacheKey string `json:"modelCacheKey"`
			Result        struct {
				Action struct {
					ID string `json:"id"`
				} `json:"action"`
				ModulePath  string `json:"modulePath"`
				VariantName string `json:"variantName"`
				Summary     struct {
					ID string `json:"id"`
				} `json:"summary"`
				Inputs []struct {
					ID string `json:"id"`
				} `json:"inputs"`
				Outputs []struct {
					ID string `json:"id"`
				} `json:"outputs"`
			} `json:"result"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &actionByIDResp); err != nil {
		t.Fatal(err)
	}
	if actionByIDResp.Result.ModelCacheKey == "" || actionByIDResp.Result.ActionID != variantManifestResp.Result.Manifest.Actions[0].ID || actionByIDResp.Result.Result.Action.ID != variantManifestResp.Result.Manifest.Actions[0].ID || actionByIDResp.Result.Result.ModulePath != ":app" || actionByIDResp.Result.Result.VariantName != "freeDebug" || actionByIDResp.Result.Result.Summary.ID == "" || len(actionByIDResp.Result.Result.Inputs) == 0 || len(actionByIDResp.Result.Result.Outputs) == 0 {
		t.Fatalf("unexpected actionByID result: %#v", actionByIDResp.Result)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"artifactByID", "--repo", root, "--id", variantManifestResp.Result.Manifest.BackingArtifactID}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("artifactByID exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var artifactByIDResp struct {
		Result struct {
			ArtifactID    string `json:"artifactId"`
			ModelCacheKey string `json:"modelCacheKey"`
			Result        struct {
				Artifact struct {
					ID string `json:"id"`
				} `json:"artifact"`
				ModulePath         string `json:"modulePath"`
				VariantName        string `json:"variantName"`
				MaterializationID  string `json:"materializationId"`
				ArtifactSnapshotID string `json:"artifactSnapshotId"`
				Summary            struct {
					ID string `json:"id"`
				} `json:"summary"`
				Consumers []struct {
					ID string `json:"id"`
				} `json:"consumers"`
			} `json:"result"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &artifactByIDResp); err != nil {
		t.Fatal(err)
	}
	if artifactByIDResp.Result.ModelCacheKey == "" || artifactByIDResp.Result.ArtifactID != variantManifestResp.Result.Manifest.BackingArtifactID || artifactByIDResp.Result.Result.Artifact.ID != variantManifestResp.Result.Manifest.BackingArtifactID || artifactByIDResp.Result.Result.ModulePath != ":app" || artifactByIDResp.Result.Result.VariantName != "freeDebug" || artifactByIDResp.Result.Result.MaterializationID == "" || artifactByIDResp.Result.Result.ArtifactSnapshotID == "" || artifactByIDResp.Result.Result.Summary.ID == "" || len(artifactByIDResp.Result.Result.Consumers) == 0 {
		t.Fatalf("unexpected artifactByID result: %#v", artifactByIDResp.Result)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"materializationByID", "--repo", root, "--id", variantManifestResp.Result.Manifest.MaterializationID}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("materializationByID exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var materializationByIDResp struct {
		Result struct {
			MaterializationID string `json:"materializationId"`
			ModelCacheKey     string `json:"modelCacheKey"`
			Result            struct {
				Materialization struct {
					ID string `json:"id"`
				} `json:"materialization"`
				ModulePath         string `json:"modulePath"`
				VariantName        string `json:"variantName"`
				ArtifactSnapshotID string `json:"artifactSnapshotId"`
				Artifacts          []struct {
					ID string `json:"id"`
				} `json:"artifacts"`
				Actions []struct {
					ID string `json:"id"`
				} `json:"actions"`
			} `json:"result"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &materializationByIDResp); err != nil {
		t.Fatal(err)
	}
	if materializationByIDResp.Result.ModelCacheKey == "" || materializationByIDResp.Result.MaterializationID != variantManifestResp.Result.Manifest.MaterializationID || materializationByIDResp.Result.Result.Materialization.ID != variantManifestResp.Result.Manifest.MaterializationID || materializationByIDResp.Result.Result.ModulePath != ":app" || materializationByIDResp.Result.Result.VariantName != "freeDebug" || materializationByIDResp.Result.Result.ArtifactSnapshotID == "" || len(materializationByIDResp.Result.Result.Artifacts) == 0 || len(materializationByIDResp.Result.Result.Actions) == 0 {
		t.Fatalf("unexpected materializationByID result: %#v", materializationByIDResp.Result)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"materializationConsumers", "--repo", root, "--id", variantManifestResp.Result.Manifest.MaterializationID}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("materializationConsumers exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var materializationConsumersResp struct {
		Result struct {
			MaterializationID string `json:"materializationId"`
			ModelCacheKey     string `json:"modelCacheKey"`
			Consumers         struct {
				MaterializationID  string   `json:"materializationId"`
				ModulePath         string   `json:"modulePath"`
				VariantName        string   `json:"variantName"`
				ArtifactSnapshotID string   `json:"artifactSnapshotId"`
				ManifestPaths      []string `json:"manifestPaths"`
				Actions            []struct {
					ID string `json:"id"`
				} `json:"actions"`
				Artifacts []struct {
					ID string `json:"id"`
				} `json:"artifacts"`
			} `json:"consumers"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &materializationConsumersResp); err != nil {
		t.Fatal(err)
	}
	if materializationConsumersResp.Result.ModelCacheKey == "" || materializationConsumersResp.Result.MaterializationID != variantManifestResp.Result.Manifest.MaterializationID || materializationConsumersResp.Result.Consumers.MaterializationID != variantManifestResp.Result.Manifest.MaterializationID || materializationConsumersResp.Result.Consumers.ModulePath != ":app" || materializationConsumersResp.Result.Consumers.VariantName != "freeDebug" || materializationConsumersResp.Result.Consumers.ArtifactSnapshotID == "" || len(materializationConsumersResp.Result.Consumers.ManifestPaths) == 0 || len(materializationConsumersResp.Result.Consumers.Actions) == 0 || len(materializationConsumersResp.Result.Consumers.Artifacts) == 0 {
		t.Fatalf("unexpected materializationConsumers result: %#v", materializationConsumersResp.Result)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"artifactsForVariant", "--repo", root, "--module", ":app", "--variant", "freeDebug"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("artifactsForVariant exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var artifactsForVariantResp struct {
		Result struct {
			ModelCacheKey      string `json:"modelCacheKey"`
			MaterializationID  string `json:"materializationId"`
			ArtifactSnapshotID string `json:"artifactSnapshotId"`
			Artifacts          []struct {
				ID string `json:"id"`
			} `json:"artifacts"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &artifactsForVariantResp); err != nil {
		t.Fatal(err)
	}
	if artifactsForVariantResp.Result.ModelCacheKey == "" || artifactsForVariantResp.Result.MaterializationID == "" || artifactsForVariantResp.Result.ArtifactSnapshotID == "" {
		t.Fatalf("unexpected artifactsForVariant result: %#v", artifactsForVariantResp.Result)
	}
	if len(artifactsForVariantResp.Result.Artifacts) == 0 {
		t.Fatalf("expected artifacts in artifactsForVariant result, got %#v", artifactsForVariantResp.Result.Artifacts)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"artifactsForModule", "--repo", root, "--module", ":app"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("artifactsForModule exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var artifactsForModuleResp struct {
		Result struct {
			ModelCacheKey       string   `json:"modelCacheKey"`
			Module              string   `json:"module"`
			VariantNames        []string `json:"variantNames"`
			MaterializationIDs  []string `json:"materializationIds"`
			ArtifactSnapshotIDs []string `json:"artifactSnapshotIds"`
			Artifacts           []struct {
				ID string `json:"id"`
			} `json:"artifacts"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &artifactsForModuleResp); err != nil {
		t.Fatal(err)
	}
	if artifactsForModuleResp.Result.ModelCacheKey == "" || artifactsForModuleResp.Result.Module != ":app" {
		t.Fatalf("unexpected artifactsForModule result: %#v", artifactsForModuleResp.Result)
	}
	if len(artifactsForModuleResp.Result.VariantNames) != 4 || len(artifactsForModuleResp.Result.MaterializationIDs) == 0 || len(artifactsForModuleResp.Result.ArtifactSnapshotIDs) == 0 {
		t.Fatalf("expected module variant/materialization ids in artifactsForModule result, got %#v", artifactsForModuleResp.Result)
	}
	if len(artifactsForModuleResp.Result.Artifacts) == 0 {
		t.Fatalf("expected module artifacts in artifactsForModule result, got %#v", artifactsForModuleResp.Result.Artifacts)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"moduleManifest", "--repo", root, "--module", ":app"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("moduleManifest exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var moduleManifestResp struct {
		Result struct {
			ModelCacheKey string `json:"modelCacheKey"`
			Module        string `json:"module"`
			Manifest      struct {
				ModulePath          string   `json:"modulePath"`
				VariantNames        []string `json:"variantNames"`
				MaterializationIDs  []string `json:"materializationIds"`
				ArtifactSnapshotIDs []string `json:"artifactSnapshotIds"`
				ManifestPaths       []string `json:"manifestPaths"`
				SourceRoots         []string `json:"sourceRoots"`
				ProducedArtifactIDs []string `json:"producedArtifactIds"`
				BackingArtifactIDs  []string `json:"backingArtifactIds"`
				Variants            []struct {
					VariantName string `json:"variantName"`
				} `json:"variants"`
			} `json:"manifest"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &moduleManifestResp); err != nil {
		t.Fatal(err)
	}
	if moduleManifestResp.Result.ModelCacheKey == "" || moduleManifestResp.Result.Module != ":app" || moduleManifestResp.Result.Manifest.ModulePath != ":app" {
		t.Fatalf("unexpected moduleManifest result: %#v", moduleManifestResp.Result)
	}
	if len(moduleManifestResp.Result.Manifest.VariantNames) != 4 || len(moduleManifestResp.Result.Manifest.Variants) != 4 {
		t.Fatalf("expected per-variant manifests in moduleManifest result, got %#v", moduleManifestResp.Result.Manifest)
	}
	if len(moduleManifestResp.Result.Manifest.MaterializationIDs) == 0 || len(moduleManifestResp.Result.Manifest.ArtifactSnapshotIDs) == 0 || len(moduleManifestResp.Result.Manifest.ManifestPaths) == 0 {
		t.Fatalf("expected module manifest ids and paths, got %#v", moduleManifestResp.Result.Manifest)
	}
	if len(moduleManifestResp.Result.Manifest.SourceRoots) == 0 || len(moduleManifestResp.Result.Manifest.ProducedArtifactIDs) == 0 || len(moduleManifestResp.Result.Manifest.BackingArtifactIDs) == 0 {
		t.Fatalf("expected module manifest artifact metadata, got %#v", moduleManifestResp.Result.Manifest)
	}

	stdout.Reset()
	stderr.Reset()
	snapshotID := variantMatResp.Result.Provenance.Materialization.ArtifactSnapshotID
	if exitCode := Run(context.Background(), []string{"artifactSnapshotProvenance", "--repo", root, "--snapshot", snapshotID}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("artifactSnapshotProvenance exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var snapshotResp struct {
		Result struct {
			ModelCacheKey      string `json:"modelCacheKey"`
			ArtifactSnapshotID string `json:"artifactSnapshotId"`
			Provenance         struct {
				ArtifactSnapshotID string   `json:"artifactSnapshotId"`
				ManifestPaths      []string `json:"manifestPaths"`
				Variants           []struct {
					MaterializationID string `json:"materializationId"`
				} `json:"variants"`
			} `json:"provenance"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &snapshotResp); err != nil {
		t.Fatal(err)
	}
	if snapshotResp.Result.ModelCacheKey == "" || snapshotResp.Result.ArtifactSnapshotID != snapshotID || snapshotResp.Result.Provenance.ArtifactSnapshotID != snapshotID {
		t.Fatalf("unexpected artifactSnapshotProvenance result: %#v", snapshotResp)
	}
	if len(snapshotResp.Result.Provenance.ManifestPaths) == 0 || len(snapshotResp.Result.Provenance.Variants) == 0 {
		t.Fatalf("expected manifest paths and variants in artifactSnapshotProvenance result: %#v", snapshotResp.Result.Provenance)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"artifactSnapshotConsumers", "--repo", root, "--snapshot", snapshotID}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("artifactSnapshotConsumers exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var snapshotConsumersResp struct {
		Result struct {
			ArtifactSnapshotID string `json:"artifactSnapshotId"`
			ModelCacheKey      string `json:"modelCacheKey"`
			Consumers          struct {
				ArtifactSnapshotID string `json:"artifactSnapshotId"`
				Variants           []struct {
					MaterializationID string `json:"materializationId"`
				} `json:"variants"`
				Actions []struct {
					ID string `json:"id"`
				} `json:"actions"`
			} `json:"consumers"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &snapshotConsumersResp); err != nil {
		t.Fatal(err)
	}
	if snapshotConsumersResp.Result.ModelCacheKey == "" || snapshotConsumersResp.Result.ArtifactSnapshotID != snapshotID || snapshotConsumersResp.Result.Consumers.ArtifactSnapshotID != snapshotID {
		t.Fatalf("unexpected artifactSnapshotConsumers result: %#v", snapshotConsumersResp.Result)
	}
	if len(snapshotConsumersResp.Result.Consumers.Variants) == 0 || len(snapshotConsumersResp.Result.Consumers.Actions) == 0 {
		t.Fatalf("expected variants and actions in artifactSnapshotConsumers result: %#v", snapshotConsumersResp.Result.Consumers)
	}

	stdout.Reset()
	stderr.Reset()
	firstArtifactID := variantManifestResp.Result.Manifest.ProducedArtifactIDs[0]
	if exitCode := Run(context.Background(), []string{"artifactProvenance", "--repo", root, "--artifact", firstArtifactID}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("artifactProvenance exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var artifactProvResp struct {
		Result struct {
			ArtifactID    string `json:"artifactId"`
			ModelCacheKey string `json:"modelCacheKey"`
			Provenance    struct {
				ModulePath  string `json:"modulePath"`
				VariantName string `json:"variantName"`
				Producer    struct {
					ID string `json:"id"`
				} `json:"producer"`
				Artifacts []struct {
					ID string `json:"id"`
				} `json:"artifacts"`
				Inputs []struct {
					ID string `json:"id"`
				} `json:"inputs"`
			} `json:"provenance"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &artifactProvResp); err != nil {
		t.Fatal(err)
	}
	if artifactProvResp.Result.ModelCacheKey == "" || artifactProvResp.Result.ArtifactID != firstArtifactID {
		t.Fatalf("unexpected artifactProvenance result: %#v", artifactProvResp.Result)
	}
	if artifactProvResp.Result.Provenance.Producer.ID == "" || len(artifactProvResp.Result.Provenance.Artifacts) == 0 {
		t.Fatalf("expected producer and artifact context in artifactProvenance result: %#v", artifactProvResp.Result.Provenance)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"artifactConsumers", "--repo", root, "--artifact", firstArtifactID}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("artifactConsumers exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var artifactConsumersResp struct {
		Result struct {
			ArtifactID    string `json:"artifactId"`
			ModelCacheKey string `json:"modelCacheKey"`
			Consumers     struct {
				ModulePath         string `json:"modulePath"`
				VariantName        string `json:"variantName"`
				MaterializationID  string `json:"materializationId"`
				ArtifactSnapshotID string `json:"artifactSnapshotId"`
				Consumers          []struct {
					ID string `json:"id"`
				} `json:"consumers"`
			} `json:"consumers"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &artifactConsumersResp); err != nil {
		t.Fatal(err)
	}
	if artifactConsumersResp.Result.ArtifactID != firstArtifactID || artifactConsumersResp.Result.ModelCacheKey == "" {
		t.Fatalf("unexpected artifactConsumers result: %#v", artifactConsumersResp.Result)
	}
	if artifactConsumersResp.Result.Consumers.ModulePath != ":app" || artifactConsumersResp.Result.Consumers.VariantName != "freeDebug" || artifactConsumersResp.Result.Consumers.MaterializationID == "" || artifactConsumersResp.Result.Consumers.ArtifactSnapshotID == "" {
		t.Fatalf("expected artifact consumer coordinates and ids, got %#v", artifactConsumersResp.Result.Consumers)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"artifactOnClasspath", "--repo", root, "--module", ":app", "--variant", "freeDebug", "--artifact", classpathArtifactID}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("artifactOnClasspath exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var artifactOnClasspathResp struct {
		Result struct {
			ModelCacheKey string `json:"modelCacheKey"`
			Lookup        struct {
				ModulePath         string `json:"modulePath"`
				VariantName        string `json:"variantName"`
				MaterializationID  string `json:"materializationId"`
				ArtifactSnapshotID string `json:"artifactSnapshotId"`
				Present            bool   `json:"present"`
				Artifact           struct {
					ID string `json:"id"`
				} `json:"artifact"`
				Entry struct {
					ArtifactID string `json:"artifactId"`
				} `json:"entry"`
			} `json:"lookup"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &artifactOnClasspathResp); err != nil {
		t.Fatal(err)
	}
	if artifactOnClasspathResp.Result.ModelCacheKey == "" || artifactOnClasspathResp.Result.Lookup.ModulePath != ":app" || artifactOnClasspathResp.Result.Lookup.VariantName != "freeDebug" {
		t.Fatalf("unexpected artifactOnClasspath result: %#v", artifactOnClasspathResp.Result)
	}
	if !artifactOnClasspathResp.Result.Lookup.Present || artifactOnClasspathResp.Result.Lookup.Artifact.ID != classpathArtifactID {
		t.Fatalf("expected artifact-on-classpath match, got %#v", artifactOnClasspathResp.Result.Lookup)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"artifactClasspathConsumers", "--repo", root, "--artifact", classpathArtifactID}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("artifactClasspathConsumers exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var artifactClasspathConsumersResp struct {
		Result struct {
			ArtifactID    string `json:"artifactId"`
			ModelCacheKey string `json:"modelCacheKey"`
			Consumers     struct {
				Artifact struct {
					ID string `json:"id"`
				} `json:"artifact"`
				Consumers []struct {
					ModulePath  string `json:"modulePath"`
					VariantName string `json:"variantName"`
					Present     bool   `json:"present"`
				} `json:"consumers"`
			} `json:"consumers"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &artifactClasspathConsumersResp); err != nil {
		t.Fatal(err)
	}
	if artifactClasspathConsumersResp.Result.ModelCacheKey == "" || artifactClasspathConsumersResp.Result.ArtifactID != classpathArtifactID || artifactClasspathConsumersResp.Result.Consumers.Artifact.ID != classpathArtifactID {
		t.Fatalf("unexpected artifactClasspathConsumers result: %#v", artifactClasspathConsumersResp.Result)
	}
	if len(artifactClasspathConsumersResp.Result.Consumers.Consumers) == 0 || !artifactClasspathConsumersResp.Result.Consumers.Consumers[0].Present {
		t.Fatalf("expected present classpath consumers in artifactClasspathConsumers result: %#v", artifactClasspathConsumersResp.Result.Consumers)
	}

	stdout.Reset()
	stderr.Reset()
	manifestPath := filepath.Join(root, "app", "src", "main", "AndroidManifest.xml")
	if exitCode := Run(context.Background(), []string{"fileOwners", "--repo", root, "--path", manifestPath}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("fileOwners exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var fileOwnersResp struct {
		Result struct {
			ModelCacheKey string `json:"modelCacheKey"`
			Owners        struct {
				Path   string `json:"path"`
				Owners []struct {
					ModulePath  string   `json:"modulePath"`
					VariantName string   `json:"variantName"`
					Kind        string   `json:"kind"`
					Paths       []string `json:"paths"`
				} `json:"owners"`
			} `json:"owners"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &fileOwnersResp); err != nil {
		t.Fatal(err)
	}
	if fileOwnersResp.Result.ModelCacheKey == "" || fileOwnersResp.Result.Owners.Path != manifestPath || len(fileOwnersResp.Result.Owners.Owners) == 0 {
		t.Fatalf("unexpected fileOwners result: %#v", fileOwnersResp.Result)
	}
	foundManifestOwner := false
	for _, owner := range fileOwnersResp.Result.Owners.Owners {
		if owner.ModulePath == ":app" && owner.VariantName == "freeDebug" && owner.Kind == "manifest" {
			foundManifestOwner = true
			break
		}
	}
	if !foundManifestOwner {
		t.Fatalf("expected manifest owner in fileOwners result: %#v", fileOwnersResp.Result.Owners)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"androidCapabilities", "--repo", root, "--module", ":app"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("androidCapabilities exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var androidCapsResp struct {
		Result struct {
			Repo     string `json:"repo"`
			Module   string `json:"module"`
			Variants []struct {
				Name                      string   `json:"name"`
				DisplayName               string   `json:"displayName"`
				CompileSDK                string   `json:"compileSdk"`
				BuildToolsVersion         string   `json:"buildToolsVersion"`
				Namespace                 string   `json:"namespace"`
				VersionCode               string   `json:"versionCode"`
				VersionName               string   `json:"versionName"`
				MinSDK                    string   `json:"minSdk"`
				TargetSDK                 string   `json:"targetSdk"`
				ManifestPaths             []string `json:"manifestPaths"`
				MaterializationID         string   `json:"materializationId"`
				ArtifactSnapshotID        string   `json:"artifactSnapshotId"`
				BackingArtifactID         string   `json:"backingArtifactId"`
				BackingArtifactPath       string   `json:"backingArtifactPath"`
				ProducedArtifactIDs       []string `json:"producedArtifactIds"`
				ProducedArtifactKinds     []string `json:"producedArtifactKinds"`
				InstallArtifactID         string   `json:"installArtifactId"`
				ResourceArtifactIDs       []string `json:"resourceArtifactIds"`
				ManifestArtifactIDs       []string `json:"manifestArtifactIds"`
				TestInstrumentationRunner string   `json:"testInstrumentationRunner"`
				Optimization              struct {
					MinifyEnabled   bool `json:"minifyEnabled"`
					ShrinkResources bool `json:"shrinkResources"`
				} `json:"optimization"`
				ProguardFiles         []string `json:"proguardFiles"`
				ConsumerProguardFiles []string `json:"consumerProguardFiles"`
				ProducedArtifactPaths []string `json:"producedArtifactPaths"`
				InstallArtifactPath   string   `json:"installArtifactPath"`
				ResourceArtifactPaths []string `json:"resourceArtifactPaths"`
				ManifestArtifactPaths []string `json:"manifestArtifactPaths"`
				InstallTask           string   `json:"installTask"`
				UninstallTask         string   `json:"uninstallTask"`
				ProducedArtifacts     []struct {
					ID                 string `json:"id"`
					Kind               string `json:"kind"`
					Path               string `json:"path"`
					ProducedByActionID string `json:"producedByActionId"`
				} `json:"producedArtifacts"`
				AndroidTestPackage       string `json:"androidTestPackage"`
				AndroidTestManifest      string `json:"androidTestManifest"`
				AndroidTestInstallTask   string `json:"androidTestInstallTask"`
				AndroidTestUninstallTask string `json:"androidTestUninstallTask"`
			} `json:"variants"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &androidCapsResp); err != nil {
		t.Fatal(err)
	}
	if androidCapsResp.Result.Repo != root || androidCapsResp.Result.Module != ":app" || len(androidCapsResp.Result.Variants) != 4 {
		t.Fatalf("unexpected androidCapabilities result: %#v", androidCapsResp.Result)
	}
	var freeDebugCaps struct {
		Name                      string   `json:"name"`
		DisplayName               string   `json:"displayName"`
		CompileSDK                string   `json:"compileSdk"`
		BuildToolsVersion         string   `json:"buildToolsVersion"`
		Namespace                 string   `json:"namespace"`
		VersionCode               string   `json:"versionCode"`
		VersionName               string   `json:"versionName"`
		MinSDK                    string   `json:"minSdk"`
		TargetSDK                 string   `json:"targetSdk"`
		ManifestPaths             []string `json:"manifestPaths"`
		MaterializationID         string   `json:"materializationId"`
		ArtifactSnapshotID        string   `json:"artifactSnapshotId"`
		BackingArtifactID         string   `json:"backingArtifactId"`
		BackingArtifactPath       string   `json:"backingArtifactPath"`
		ProducedArtifactIDs       []string `json:"producedArtifactIds"`
		ProducedArtifactKinds     []string `json:"producedArtifactKinds"`
		InstallArtifactID         string   `json:"installArtifactId"`
		ResourceArtifactIDs       []string `json:"resourceArtifactIds"`
		ManifestArtifactIDs       []string `json:"manifestArtifactIds"`
		TestInstrumentationRunner string   `json:"testInstrumentationRunner"`
		Optimization              struct {
			MinifyEnabled   bool `json:"minifyEnabled"`
			ShrinkResources bool `json:"shrinkResources"`
		} `json:"optimization"`
		ProguardFiles         []string `json:"proguardFiles"`
		ConsumerProguardFiles []string `json:"consumerProguardFiles"`
		ProducedArtifactPaths []string `json:"producedArtifactPaths"`
		InstallArtifactPath   string   `json:"installArtifactPath"`
		ResourceArtifactPaths []string `json:"resourceArtifactPaths"`
		ManifestArtifactPaths []string `json:"manifestArtifactPaths"`
		InstallTask           string   `json:"installTask"`
		UninstallTask         string   `json:"uninstallTask"`
		ProducedArtifacts     []struct {
			ID                 string `json:"id"`
			Kind               string `json:"kind"`
			Path               string `json:"path"`
			ProducedByActionID string `json:"producedByActionId"`
		} `json:"producedArtifacts"`
		AndroidTestPackage       string `json:"androidTestPackage"`
		AndroidTestManifest      string `json:"androidTestManifest"`
		AndroidTestInstallTask   string `json:"androidTestInstallTask"`
		AndroidTestUninstallTask string `json:"androidTestUninstallTask"`
	}
	foundAndroidCaps := false
	for _, candidate := range androidCapsResp.Result.Variants {
		if candidate.Name == "freeDebug" {
			freeDebugCaps = candidate
			foundAndroidCaps = true
			break
		}
	}
	if !foundAndroidCaps {
		t.Fatalf("expected freeDebug android capability variant, got %#v", androidCapsResp.Result.Variants)
	}
	if freeDebugCaps.DisplayName != "Free Debug" || len(freeDebugCaps.ManifestPaths) == 0 || freeDebugCaps.MaterializationID == "" || freeDebugCaps.ArtifactSnapshotID == "" {
		t.Fatalf("unexpected freeDebug android capability metadata: %#v", freeDebugCaps)
	}
	if freeDebugCaps.CompileSDK != "34" {
		t.Fatalf("expected compile/proguard-consumer metadata in androidCapabilities result, got %#v", freeDebugCaps)
	}
	if freeDebugCaps.BackingArtifactID == "" || len(freeDebugCaps.ProducedArtifactIDs) == 0 || len(freeDebugCaps.ProducedArtifacts) == 0 {
		t.Fatalf("expected produced artifact metadata in androidCapabilities result, got %#v", freeDebugCaps)
	}
	if freeDebugCaps.Namespace != "com.example.app" {
		t.Fatalf("expected namespace metadata in androidCapabilities result, got %#v", freeDebugCaps)
	}
	if freeDebugCaps.BackingArtifactID == "" {
		t.Fatalf("expected backing artifact metadata in androidCapabilities result, got %#v", freeDebugCaps)
	}
	if freeDebugCaps.Optimization.MinifyEnabled || freeDebugCaps.Optimization.ShrinkResources || len(freeDebugCaps.ProguardFiles) != 0 {
		t.Fatalf("expected bounded optimization/proguard metadata in androidCapabilities result, got %#v", freeDebugCaps)
	}
	if len(freeDebugCaps.ProducedArtifactKinds) == 0 || freeDebugCaps.InstallArtifactID == "" {
		t.Fatalf("expected produced-artifact classification metadata in androidCapabilities result, got %#v", freeDebugCaps)
	}
	if len(freeDebugCaps.ProducedArtifactPaths) == 0 || freeDebugCaps.BackingArtifactPath == "" {
		t.Fatalf("expected produced-artifact path metadata in androidCapabilities result, got %#v", freeDebugCaps)
	}
	if freeDebugCaps.ProducedArtifacts[0].ID == "" || freeDebugCaps.ProducedArtifacts[0].Kind == "" {
		t.Fatalf("expected structured produced artifacts in androidCapabilities result, got %#v", freeDebugCaps.ProducedArtifacts)
	}
	if freeDebugCaps.InstallTask != "installFreeDebug" || freeDebugCaps.UninstallTask != "uninstallFreeDebug" {
		t.Fatalf("expected install/uninstall task metadata in androidCapabilities result, got %#v", freeDebugCaps)
	}
	if freeDebugCaps.AndroidTestPackage != "com.example.app.test" || freeDebugCaps.AndroidTestManifest == "" {
		t.Fatalf("expected androidTest package and manifest path, got %#v", freeDebugCaps)
	}
	if freeDebugCaps.AndroidTestInstallTask != "installFreeDebugAndroidTest" || freeDebugCaps.AndroidTestUninstallTask != "uninstallFreeDebugAndroidTest" {
		t.Fatalf("unexpected androidTest task aliases, got %#v", freeDebugCaps)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"moduleImpact", "--repo", root, "--module", ":app"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("moduleImpact exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var moduleImpactResp struct {
		Result struct {
			Module        string `json:"module"`
			ModelCacheKey string `json:"modelCacheKey"`
			Impact        struct {
				Module     string `json:"module"`
				Dependents []struct {
					Kind string `json:"kind"`
				} `json:"dependents"`
			} `json:"impact"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &moduleImpactResp); err != nil {
		t.Fatal(err)
	}
	if moduleImpactResp.Result.Module != ":app" || moduleImpactResp.Result.ModelCacheKey == "" || moduleImpactResp.Result.Impact.Module != ":app" {
		t.Fatalf("unexpected moduleImpact result: %#v", moduleImpactResp.Result)
	}
}

func TestGraphPlanAndCleanupCommandsExposeServiceResults(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "settings.gradle.kts"), `
rootProject.name = "GraphPlanTest"
include(":app")
`)
	mustWriteFile(t, filepath.Join(root, "build.gradle.kts"), `plugins {}`)
	appDir := filepath.Join(root, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(appDir, "build.gradle.kts"), `
plugins {
  alias(libs.plugins.android.application)
}

android {
  namespace = "com.example.app"
  flavorDimensions += "tier"
  productFlavors {
    create("free") { dimension = "tier" }
    create("paid") { dimension = "tier" }
  }
  buildTypes {
    debug {}
    release {}
  }
}
`)

	var stdout, stderr strings.Builder
	if exitCode := Run(context.Background(), []string{"explainPlan", "--repo", root, "--module", ":app", "--command", "assemble", "--variant", "freeDebug"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("explainPlan exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var explainResp struct {
		Result struct {
			Repo             string   `json:"repo"`
			Module           string   `json:"module"`
			Command          string   `json:"command"`
			RequestedVariant string   `json:"requestedVariant"`
			TargetVariant    string   `json:"targetVariant"`
			VariantExplicit  bool     `json:"variantExplicit"`
			ModelCacheKey    string   `json:"modelCacheKey"`
			ActionIDs        []string `json:"actionIds"`
			Reasons          []string `json:"reasons"`
			Schedule         struct {
				Batches []struct {
					Actions []struct {
						ID          string `json:"id"`
						ModulePath  string `json:"modulePath"`
						VariantName string `json:"variantName"`
					} `json:"actions"`
				} `json:"batches"`
			} `json:"schedule"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &explainResp); err != nil {
		t.Fatal(err)
	}
	if explainResp.Result.Repo != root || explainResp.Result.Module != ":app" || explainResp.Result.Command != "assemble" {
		t.Fatalf("unexpected explainPlan result: %#v", explainResp.Result)
	}
	if explainResp.Result.TargetVariant != "freeDebug" || explainResp.Result.RequestedVariant != "freeDebug" || !explainResp.Result.VariantExplicit {
		t.Fatalf("unexpected explainPlan variant fields: %#v", explainResp.Result)
	}
	if explainResp.Result.ModelCacheKey == "" || len(explainResp.Result.ActionIDs) == 0 || len(explainResp.Result.Reasons) == 0 {
		t.Fatalf("expected plan ids and reasons, got %#v", explainResp.Result)
	}
	if len(explainResp.Result.Schedule.Batches) == 0 || len(explainResp.Result.Schedule.Batches[0].Actions) == 0 || explainResp.Result.Schedule.Batches[0].Actions[0].ID == "" {
		t.Fatalf("expected scheduled actions in explainPlan result, got %#v", explainResp.Result.Schedule)
	}
	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"plannedActionPolicies", "--repo", root, "--module", ":app", "--command", "assemble", "--variant", "freeDebug"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("plannedActionPolicies exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var plannedActionPoliciesResp struct {
		Result struct {
			ModelCacheKey string `json:"modelCacheKey"`
			Module        string `json:"module"`
			Command       string `json:"command"`
			TargetVariant string `json:"targetVariant"`
			Policies      []struct {
				ActionID       string   `json:"actionId"`
				ResourceClass  string   `json:"resourceClass"`
				RetentionClass string   `json:"retentionClass"`
				ProbeOrder     []string `json:"probeOrder"`
			} `json:"policies"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &plannedActionPoliciesResp); err != nil {
		t.Fatal(err)
	}
	if plannedActionPoliciesResp.Result.ModelCacheKey == "" || plannedActionPoliciesResp.Result.Module != ":app" || plannedActionPoliciesResp.Result.Command != "assemble" || plannedActionPoliciesResp.Result.TargetVariant != "freeDebug" || len(plannedActionPoliciesResp.Result.Policies) == 0 {
		t.Fatalf("unexpected plannedActionPolicies result: %#v", plannedActionPoliciesResp.Result)
	}
	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"plannedActionPolicy", "--repo", root, "--module", ":app", "--command", "assemble", "--variant", "freeDebug", "--action", plannedActionPoliciesResp.Result.Policies[0].ActionID}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("plannedActionPolicy exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var plannedActionPolicyResp struct {
		Result struct {
			ModelCacheKey string `json:"modelCacheKey"`
			ActionID      string `json:"actionId"`
			Policy        struct {
				ActionID       string   `json:"actionId"`
				ResourceClass  string   `json:"resourceClass"`
				RetentionClass string   `json:"retentionClass"`
				ProbeOrder     []string `json:"probeOrder"`
			} `json:"policy"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &plannedActionPolicyResp); err != nil {
		t.Fatal(err)
	}
	if plannedActionPolicyResp.Result.ModelCacheKey == "" || plannedActionPolicyResp.Result.ActionID != plannedActionPoliciesResp.Result.Policies[0].ActionID || plannedActionPolicyResp.Result.Policy.ActionID != plannedActionPoliciesResp.Result.Policies[0].ActionID || plannedActionPolicyResp.Result.Policy.ResourceClass == "" || plannedActionPolicyResp.Result.Policy.RetentionClass == "" || len(plannedActionPolicyResp.Result.Policy.ProbeOrder) == 0 {
		t.Fatalf("unexpected plannedActionPolicy result: %#v", plannedActionPolicyResp.Result)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"variantProvenance", "--repo", root, "--module", ":app", "--variant", "freeDebug"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("variantProvenance exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var variantProvResp struct {
		Result struct {
			ModelCacheKey string `json:"modelCacheKey"`
			Provenance    struct {
				Module struct {
					Path string `json:"path"`
				} `json:"module"`
				Variant struct {
					Name string `json:"name"`
				} `json:"variant"`
				Materialization struct {
					ID                 string `json:"id"`
					ArtifactSnapshotID string `json:"artifactSnapshotId"`
				} `json:"materialization"`
			} `json:"provenance"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &variantProvResp); err != nil {
		t.Fatal(err)
	}
	if variantProvResp.Result.ModelCacheKey == "" || variantProvResp.Result.Provenance.Module.Path != ":app" || variantProvResp.Result.Provenance.Variant.Name != "freeDebug" {
		t.Fatalf("unexpected variantProvenance result: %#v", variantProvResp.Result)
	}
	if variantProvResp.Result.Provenance.Materialization.ID == "" || variantProvResp.Result.Provenance.Materialization.ArtifactSnapshotID == "" {
		t.Fatalf("expected materialization ids in variantProvenance result, got %#v", variantProvResp.Result.Provenance)
	}

	stdout.Reset()
	stderr.Reset()
	actionID := explainResp.Result.ActionIDs[0]
	if exitCode := Run(context.Background(), []string{"actionProvenance", "--repo", root, "--action", actionID}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("actionProvenance exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var actionProvResp struct {
		Result struct {
			ModelCacheKey string `json:"modelCacheKey"`
			Provenance    struct {
				Action struct {
					ID string `json:"id"`
				} `json:"action"`
				Inputs []struct {
					ID string `json:"id"`
				} `json:"inputs"`
			} `json:"provenance"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &actionProvResp); err != nil {
		t.Fatal(err)
	}
	if actionProvResp.Result.ModelCacheKey == "" || actionProvResp.Result.Provenance.Action.ID != actionID {
		t.Fatalf("unexpected actionProvenance result: %#v", actionProvResp.Result)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"actionInputs", "--repo", root, "--action", actionID}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("actionInputs exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var actionInputsResp struct {
		Result struct {
			ModelCacheKey string `json:"modelCacheKey"`
			Inputs        struct {
				ModulePath  string `json:"modulePath"`
				VariantName string `json:"variantName"`
				Action      struct {
					ID string `json:"id"`
				} `json:"action"`
				Inputs []struct {
					ID string `json:"id"`
				} `json:"inputs"`
			} `json:"inputs"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &actionInputsResp); err != nil {
		t.Fatal(err)
	}
	if actionInputsResp.Result.ModelCacheKey == "" || actionInputsResp.Result.Inputs.ModulePath != ":app" || actionInputsResp.Result.Inputs.VariantName != "freeDebug" || actionInputsResp.Result.Inputs.Action.ID != actionID {
		t.Fatalf("unexpected actionInputs result: %#v", actionInputsResp.Result)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"actionOutputs", "--repo", root, "--action", actionID}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("actionOutputs exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var actionOutputsResp struct {
		Result struct {
			ModelCacheKey string `json:"modelCacheKey"`
			Outputs       struct {
				ModulePath  string `json:"modulePath"`
				VariantName string `json:"variantName"`
				Action      struct {
					ID string `json:"id"`
				} `json:"action"`
				Outputs []struct {
					ID string `json:"id"`
				} `json:"outputs"`
			} `json:"outputs"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &actionOutputsResp); err != nil {
		t.Fatal(err)
	}
	if actionOutputsResp.Result.ModelCacheKey == "" || actionOutputsResp.Result.Outputs.ModulePath != ":app" || actionOutputsResp.Result.Outputs.VariantName != "freeDebug" || actionOutputsResp.Result.Outputs.Action.ID != actionID {
		t.Fatalf("unexpected actionOutputs result: %#v", actionOutputsResp.Result)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"actionDependencies", "--repo", root, "--action", actionID}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("actionDependencies exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var actionDependenciesResp struct {
		Result struct {
			ModelCacheKey string `json:"modelCacheKey"`
			Dependencies  struct {
				ModulePath  string `json:"modulePath"`
				VariantName string `json:"variantName"`
				Action      struct {
					ID string `json:"id"`
				} `json:"action"`
			} `json:"dependencies"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &actionDependenciesResp); err != nil {
		t.Fatal(err)
	}
	if actionDependenciesResp.Result.ModelCacheKey == "" || actionDependenciesResp.Result.Dependencies.ModulePath != ":app" || actionDependenciesResp.Result.Dependencies.VariantName != "freeDebug" || actionDependenciesResp.Result.Dependencies.Action.ID != actionID {
		t.Fatalf("unexpected actionDependencies result: %#v", actionDependenciesResp.Result)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"actionDependents", "--repo", root, "--action", actionID}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("actionDependents exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var actionDependentsResp struct {
		Result struct {
			ModelCacheKey string `json:"modelCacheKey"`
			Dependents    struct {
				ModulePath  string `json:"modulePath"`
				VariantName string `json:"variantName"`
				Action      struct {
					ID string `json:"id"`
				} `json:"action"`
			} `json:"dependents"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &actionDependentsResp); err != nil {
		t.Fatal(err)
	}
	if actionDependentsResp.Result.ModelCacheKey == "" || actionDependentsResp.Result.Dependents.ModulePath != ":app" || actionDependentsResp.Result.Dependents.VariantName != "freeDebug" || actionDependentsResp.Result.Dependents.Action.ID != actionID {
		t.Fatalf("unexpected actionDependents result: %#v", actionDependentsResp.Result)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"actionsForModule", "--repo", root, "--module", ":app"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("actionsForModule exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var actionsForModuleResp struct {
		Result struct {
			ModelCacheKey string `json:"modelCacheKey"`
			Module        string `json:"module"`
			Actions       []struct {
				ID         string `json:"id"`
				ModulePath string `json:"modulePath"`
			} `json:"actions"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &actionsForModuleResp); err != nil {
		t.Fatal(err)
	}
	if actionsForModuleResp.Result.ModelCacheKey == "" || actionsForModuleResp.Result.Module != ":app" || len(actionsForModuleResp.Result.Actions) == 0 || actionsForModuleResp.Result.Actions[0].ModulePath != ":app" {
		t.Fatalf("unexpected actionsForModule result: %#v", actionsForModuleResp.Result)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"actionsForVariant", "--repo", root, "--module", ":app", "--variant", "freeDebug"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("actionsForVariant exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var actionsForVariantResp struct {
		Result struct {
			ModelCacheKey string `json:"modelCacheKey"`
			Module        string `json:"module"`
			Variant       string `json:"variant"`
			Actions       []struct {
				ID          string `json:"id"`
				VariantName string `json:"variantName"`
			} `json:"actions"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &actionsForVariantResp); err != nil {
		t.Fatal(err)
	}
	if actionsForVariantResp.Result.ModelCacheKey == "" || actionsForVariantResp.Result.Module != ":app" || actionsForVariantResp.Result.Variant != "freeDebug" || len(actionsForVariantResp.Result.Actions) == 0 || actionsForVariantResp.Result.Actions[0].VariantName != "freeDebug" {
		t.Fatalf("unexpected actionsForVariant result: %#v", actionsForVariantResp.Result)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"cleanupPlan", "--repo", root}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("cleanupPlan exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var cleanupResp struct {
		Result struct {
			ModelCacheKey string `json:"modelCacheKey"`
			Plan          struct {
				KnownBytes     int64    `json:"knownBytes"`
				ProtectedBytes int64    `json:"protectedBytes"`
				EvictableBytes int64    `json:"evictableBytes"`
				Notes          []string `json:"notes"`
				Warnings       []struct {
					Scope string `json:"scope"`
					Kind  string `json:"kind"`
				} `json:"warnings"`
				Policy struct {
					CleanupMode string `json:"cleanupMode"`
				} `json:"policy"`
				ClassPlans []struct {
					Class            string `json:"class"`
					RecordCount      int    `json:"recordCount"`
					UnknownSizeCount int    `json:"unknownSizeCount"`
					TotalBytes       int64  `json:"totalBytes"`
					EvictableBytes   int64  `json:"evictableBytes"`
					Warnings         []struct {
						Scope string `json:"scope"`
						Kind  string `json:"kind"`
					} `json:"warnings"`
					Notes []string `json:"notes"`
				} `json:"classPlans"`
				Records []struct {
					ID             string  `json:"id"`
					Disposition    string  `json:"disposition"`
					ReasonCode     string  `json:"reasonCode"`
					RetentionClass string  `json:"retentionClass"`
					PathExists     *bool   `json:"pathExists"`
					SizeBytes      int64   `json:"sizeBytes"`
					AgeHours       float64 `json:"ageHours"`
				} `json:"records"`
			} `json:"plan"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &cleanupResp); err != nil {
		t.Fatal(err)
	}
	if cleanupResp.Result.ModelCacheKey == "" || cleanupResp.Result.Plan.Policy.CleanupMode == "" {
		t.Fatalf("unexpected cleanupPlan result: %#v", cleanupResp.Result)
	}
	if len(cleanupResp.Result.Plan.ClassPlans) == 0 || len(cleanupResp.Result.Plan.Records) == 0 {
		t.Fatalf("expected class plans and records in cleanupPlan result, got %#v", cleanupResp.Result.Plan)
	}
	if len(cleanupResp.Result.Plan.Notes) == 0 {
		t.Fatalf("expected byte accounting and notes in cleanupPlan result, got %#v", cleanupResp.Result.Plan)
	}
	foundStatMetadata := cleanupResp.Result.Plan.KnownBytes > 0 || cleanupResp.Result.Plan.ProtectedBytes > 0 || cleanupResp.Result.Plan.EvictableBytes > 0
	foundReasonCode := false
	foundClassWarnings := false
	for _, record := range cleanupResp.Result.Plan.Records {
		if record.PathExists != nil {
			foundStatMetadata = true
		}
		if record.ReasonCode != "" {
			foundReasonCode = true
		}
	}
	for _, classPlan := range cleanupResp.Result.Plan.ClassPlans {
		if len(classPlan.Warnings) != 0 {
			foundClassWarnings = true
			break
		}
	}
	if !foundStatMetadata {
		t.Fatalf("expected cleanupPlan result to expose stat metadata when paths are knowable, got %#v", cleanupResp.Result.Plan)
	}
	if !foundReasonCode {
		t.Fatalf("expected cleanupPlan result to expose structured reason codes, got %#v", cleanupResp.Result.Plan.Records)
	}
	if !foundClassWarnings {
		t.Fatalf("expected cleanupPlan result to expose structured class warnings, got %#v", cleanupResp.Result.Plan.ClassPlans)
	}
}

func TestRunSummaryCommandsExposePersistedRunSummaryArtifacts(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "settings.gradle.kts"), "include(\":app\")\n")
	mustWriteFile(t, filepath.Join(root, "build.gradle.kts"), "plugins {}\n")
	if err := os.MkdirAll(filepath.Join(root, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(root, "app", "build.gradle.kts"), "plugins {}\n")
	runSummaryDir := filepath.Join(root, "build", "grit", "run-summaries", "_app")
	if err := os.MkdirAll(runSummaryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(service.RunSummaryRecord{
		Command:    "assemble",
		ModulePath: ":app",
		Success:    true,
		Variant:    "freeDebug",
		RunGraphSummary: &service.RunGraphSummary{
			ModuleID:           "module:app",
			VariantID:          "variant:freeDebug",
			MaterializationID:  "materialization:freeDebug",
			ArtifactSnapshotID: "snapshot:freeDebug",
			PlannedActionIDs:   []string{"action:compile"},
			RootActionIDs:      []string{"action:compile"},
			ExecutedActionIDs:  []string{"action:compile"},
		},
		CriticalPathSummary: &service.CriticalPathSummary{
			BatchCount:           1,
			EstimatedDurationMs:  18,
			RepresentativeAction: []string{"action:compile"},
		},
		SchedulerSummary: &service.SchedulerSummary{
			ExecutedBatchCount:  1,
			CriticalPathActions: 1,
			QueueWaitActions:    1,
			TotalQueueWaitMs:    9,
			MaxQueueWaitMs:      9,
			WaitReasonCounts:    map[string]int{"worker-slot": 1},
			CacheResultCounts:   map[string]int{"reused": 1},
			WorkerClasses: []service.SchedulerBreakdownBucket{{
				Key:               "jvm-compile",
				ActionCount:       1,
				CriticalPathCount: 1,
				QueueWaitActions:  1,
				TotalQueueWaitMs:  9,
				MaxQueueWaitMs:    9,
				WaitReasonCounts:  map[string]int{"worker-slot": 1},
				CacheResultCounts: map[string]int{"reused": 1},
			}},
			ResourceClasses: []service.SchedulerBreakdownBucket{{
				Key:               "cpu",
				ActionCount:       1,
				CriticalPathCount: 1,
				QueueWaitActions:  1,
				TotalQueueWaitMs:  9,
				MaxQueueWaitMs:    9,
				WaitReasonCounts:  map[string]int{"worker-slot": 1},
				CacheResultCounts: map[string]int{"reused": 1},
			}},
		},
		PlannedSchedule: &service.PlanScheduleResult{
			ResourceBudgets: []service.PlanResourceBudget{{ResourceClass: "cpu", Capacity: 1}},
			Batches: []service.PlanScheduleBatch{{
				Actions: []service.InspectPlannedAction{{ID: "action:compile", Name: "compileKotlin", Operation: "compile", ModulePath: ":app", VariantName: "freeDebug"}},
			}},
		},
		ActionExecutions: []service.ActionExecution{{
			ActionID:       "action:compile",
			Name:           "compileKotlin",
			Operation:      "compile",
			ModulePath:     ":app",
			VariantName:    "freeDebug",
			BatchIndex:     0,
			CriticalPath:   true,
			QueueWaitMs:    9,
			WaitReason:     "worker-slot",
			WorkerClass:    "jvm-compile",
			ResourceClass:  "cpu",
			ResourceCost:   2,
			MaxParallelism: 1,
			CacheKey:       "compile-key",
			Cacheable:      true,
			ProbeOrder:     []string{"local", "shared"},
			ExecuteOnMiss:  true,
			RetentionClass: "machine-shareable",
			Shareability:   "machine",
			Status:         "reused",
			Timings: perf.List([]perf.TimingEntry{{
				Name:       "compile",
				DurationMs: 5,
				Children: perf.List([]perf.TimingEntry{{
					Name:       "javac",
					DurationMs: 3,
				}}),
			}}),
		}},
		ActionExplanations: []explain.Action{{
			ActionID:  "action:compile",
			Name:      "compileKotlin",
			Operation: "compile",
			Cache: &explain.Timing{
				State: explain.StateReused,
				Basis: "test",
			},
		}},
		CacheProbes: []responsepayload.CacheProbe{{
			ActionID: "action:compile",
			State:    "reused",
			Basis:    "test",
		}},
		CacheProbeRecords: []responsepayload.CacheProbeRecord{{
			ActionID: "action:compile",
			StepName: "compileKotlin",
			State:    "reused",
			Basis:    "test",
		}},
		CacheSummary: &service.CacheSummary{
			TotalActions:   2,
			ReusedActions:  1,
			RebuiltActions: 1,
		},
		DiagnosticSummary: &service.DiagnosticSummary{
			Total:               1,
			BySeverity:          []service.DiagnosticSummaryBucket{{Key: "error", Count: 1}},
			ByCode:              []service.DiagnosticSummaryBucket{{Key: "compile_failure", Count: 1}},
			ByCategory:          []service.DiagnosticSummaryBucket{{Key: "compile", Count: 1}},
			ByTool:              []service.DiagnosticSummaryBucket{{Key: "compile", Count: 1}},
			ByOrigin:            []service.DiagnosticSummaryBucket{{Key: "tool", Count: 1}},
			BySource:            []service.DiagnosticSummaryBucket{{Key: "tool-emitted", Count: 1}},
			ByStream:            []service.DiagnosticSummaryBucket{{Key: "stderr", Count: 1}},
			ByOperation:         []service.DiagnosticSummaryBucket{{Key: "compile", Count: 1}},
			ByWorkerClass:       []service.DiagnosticSummaryBucket{{Key: "jvm-compile", Count: 1}},
			ByFile:              []service.DiagnosticSummaryBucket{{Key: "/repo/app/src/main/java/App.kt", Count: 1}},
			ByRelatedDependency: []service.DiagnosticSummaryBucket{{Key: "com.squareup.okhttp3:okhttp:4.12.0", Count: 1}},
		},
		Diagnostics: []service.DiagnosticRecord{{
			ActionID:          "action:compile",
			ModulePath:        ":app",
			VariantName:       "freeDebug",
			Tool:              "compile",
			Operation:         "compile",
			WorkerClass:       "jvm-compile",
			Origin:            "tool",
			SourceKind:        "tool-emitted",
			Stream:            "stderr",
			Severity:          "error",
			Code:              "compile_failure",
			Category:          "compile",
			Message:           "compilation failed",
			File:              "/repo/app/src/main/java/App.kt",
			Line:              12,
			Column:            4,
			RelatedDependency: "com.squareup.okhttp3:okhttp:4.12.0",
		}},
		Materializations: []project.SemanticMaterializationSummary{{
			ID:                 "materialization:freeDebug",
			ArtifactSnapshotID: "snapshot:freeDebug",
			SourceRoots:        []string{"/repo/app/src/main"},
		}},
		PerfTiming: perf.List([]perf.TimingEntry{{Name: "total", DurationMs: 12}}),
		WrittenAt:  "2026-04-09T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runSummaryDir, "assemble.json"), payload, 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr strings.Builder
	if exitCode := Run(context.Background(), []string{"runSummary", "--repo", root, "--module", ":app", "--command", "assemble"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("runSummary exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var runSummaryResp struct {
		Result struct {
			Repo    string `json:"repo"`
			Module  string `json:"module"`
			Command string `json:"command"`
			Path    string `json:"path"`
			Summary struct {
				Command      string                `json:"command"`
				ModulePath   string                `json:"modulePath"`
				Variant      string                `json:"variant"`
				CacheSummary *service.CacheSummary `json:"cacheSummary"`
			} `json:"summary"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &runSummaryResp); err != nil {
		t.Fatal(err)
	}
	if runSummaryResp.Result.Repo != root || runSummaryResp.Result.Module != ":app" || runSummaryResp.Result.Command != "assemble" || runSummaryResp.Result.Path == "" {
		t.Fatalf("unexpected runSummary result: %#v", runSummaryResp.Result)
	}
	if runSummaryResp.Result.Summary.Command != "assemble" || runSummaryResp.Result.Summary.ModulePath != ":app" || runSummaryResp.Result.Summary.Variant != "freeDebug" {
		t.Fatalf("unexpected runSummary payload: %#v", runSummaryResp.Result.Summary)
	}
	if runSummaryResp.Result.Summary.CacheSummary == nil || runSummaryResp.Result.Summary.CacheSummary.ReusedActions != 1 {
		t.Fatalf("expected cache summary in runSummary payload, got %#v", runSummaryResp.Result.Summary.CacheSummary)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"runSummaries", "--repo", root, "--module", ":app"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("runSummaries exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var runSummariesResp struct {
		Result struct {
			Repo    string `json:"repo"`
			Module  string `json:"module"`
			Entries []struct {
				Path       string `json:"path"`
				ModulePath string `json:"modulePath"`
				Command    string `json:"command"`
				Variant    string `json:"variant"`
			} `json:"entries"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &runSummariesResp); err != nil {
		t.Fatal(err)
	}
	if runSummariesResp.Result.Repo != root || runSummariesResp.Result.Module != ":app" || len(runSummariesResp.Result.Entries) != 1 {
		t.Fatalf("unexpected runSummaries result: %#v", runSummariesResp.Result)
	}
	if runSummariesResp.Result.Entries[0].Command != "assemble" || runSummariesResp.Result.Entries[0].ModulePath != ":app" || runSummariesResp.Result.Entries[0].Variant != "freeDebug" {
		t.Fatalf("unexpected runSummaries entry: %#v", runSummariesResp.Result.Entries[0])
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"runGraphSummary", "--repo", root, "--module", ":app", "--command", "assemble"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("runGraphSummary exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var runGraphSummaryResp struct {
		Result struct {
			Summary struct {
				MaterializationID string   `json:"materializationId"`
				RootActionIDs     []string `json:"rootActionIds"`
			} `json:"summary"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &runGraphSummaryResp); err != nil {
		t.Fatal(err)
	}
	if runGraphSummaryResp.Result.Summary.MaterializationID != "materialization:freeDebug" || len(runGraphSummaryResp.Result.Summary.RootActionIDs) != 1 {
		t.Fatalf("unexpected runGraphSummary payload: %#v", runGraphSummaryResp.Result.Summary)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"criticalPathSummary", "--repo", root, "--module", ":app", "--command", "assemble"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("criticalPathSummary exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var criticalPathSummaryResp struct {
		Result struct {
			Summary struct {
				BatchCount           int      `json:"batchCount"`
				RepresentativeAction []string `json:"representativeActionIds"`
			} `json:"summary"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &criticalPathSummaryResp); err != nil {
		t.Fatal(err)
	}
	if criticalPathSummaryResp.Result.Summary.BatchCount != 1 {
		t.Fatalf("unexpected criticalPathSummary payload: %#v", criticalPathSummaryResp.Result.Summary)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"schedulerSummary", "--repo", root, "--module", ":app", "--command", "assemble"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("schedulerSummary exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var schedulerSummaryResp struct {
		Result struct {
			Summary struct {
				QueueWaitActions  int            `json:"queueWaitActions"`
				WaitReasonCounts  map[string]int `json:"waitReasonCounts"`
				CacheResultCounts map[string]int `json:"cacheResultCounts"`
				WorkerClasses     []struct {
					Key string `json:"key"`
				} `json:"workerClasses"`
				ResourceClasses []struct {
					Key string `json:"key"`
				} `json:"resourceClasses"`
			} `json:"summary"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &schedulerSummaryResp); err != nil {
		t.Fatal(err)
	}
	if schedulerSummaryResp.Result.Summary.QueueWaitActions != 1 || schedulerSummaryResp.Result.Summary.WaitReasonCounts["worker-slot"] != 1 {
		t.Fatalf("unexpected schedulerSummary payload: %#v", schedulerSummaryResp.Result.Summary)
	}
	if schedulerSummaryResp.Result.Summary.CacheResultCounts["reused"] != 1 || len(schedulerSummaryResp.Result.Summary.WorkerClasses) != 1 || schedulerSummaryResp.Result.Summary.WorkerClasses[0].Key != "jvm-compile" || len(schedulerSummaryResp.Result.Summary.ResourceClasses) != 1 || schedulerSummaryResp.Result.Summary.ResourceClasses[0].Key != "cpu" {
		t.Fatalf("expected scheduler breakdowns in payload, got %#v", schedulerSummaryResp.Result.Summary)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"cacheSummary", "--repo", root, "--module", ":app", "--command", "assemble"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("cacheSummary exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var cacheSummaryResp struct {
		Result struct {
			Summary struct {
				TotalActions  int `json:"totalActions"`
				ReusedActions int `json:"reusedActions"`
			} `json:"summary"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &cacheSummaryResp); err != nil {
		t.Fatal(err)
	}
	if cacheSummaryResp.Result.Summary.TotalActions != 2 || cacheSummaryResp.Result.Summary.ReusedActions != 1 {
		t.Fatalf("unexpected cacheSummary payload: %#v", cacheSummaryResp.Result.Summary)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"toolSummary", "--repo", root, "--module", ":app", "--command", "assemble"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("toolSummary exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var toolSummaryResp struct {
		Result struct {
			Summary struct {
				Operations []struct {
					Key         string `json:"key"`
					ActionCount int    `json:"actionCount"`
				} `json:"operations"`
				WorkerClasses []struct {
					Key string `json:"key"`
				} `json:"workerClasses"`
				ResourceClasses []struct {
					Key string `json:"key"`
				} `json:"resourceClasses"`
			} `json:"summary"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &toolSummaryResp); err != nil {
		t.Fatal(err)
	}
	if len(toolSummaryResp.Result.Summary.Operations) != 1 || toolSummaryResp.Result.Summary.Operations[0].Key != "compile" || toolSummaryResp.Result.Summary.Operations[0].ActionCount != 1 {
		t.Fatalf("unexpected toolSummary payload: %#v", toolSummaryResp.Result.Summary)
	}
	if len(toolSummaryResp.Result.Summary.WorkerClasses) != 1 || toolSummaryResp.Result.Summary.WorkerClasses[0].Key != "jvm-compile" {
		t.Fatalf("unexpected toolSummary worker classes: %#v", toolSummaryResp.Result.Summary)
	}
	if len(toolSummaryResp.Result.Summary.ResourceClasses) != 1 || toolSummaryResp.Result.Summary.ResourceClasses[0].Key != "cpu" {
		t.Fatalf("unexpected toolSummary resource classes: %#v", toolSummaryResp.Result.Summary)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"diagnostics", "--repo", root, "--module", ":app", "--command", "assemble"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("diagnostics exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var diagnosticsResp struct {
		Result struct {
			Diagnostics []struct {
				Fingerprint       string `json:"fingerprint"`
				Origin            string `json:"origin"`
				Code              string `json:"code"`
				File              string `json:"file"`
				SourceKind        string `json:"sourceKind"`
				Stream            string `json:"stream"`
				RelatedDependency string `json:"relatedDependency"`
			} `json:"diagnostics"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &diagnosticsResp); err != nil {
		t.Fatal(err)
	}
	if len(diagnosticsResp.Result.Diagnostics) != 1 || diagnosticsResp.Result.Diagnostics[0].Code != "compile_failure" || diagnosticsResp.Result.Diagnostics[0].File == "" {
		t.Fatalf("unexpected diagnostics payload: %#v", diagnosticsResp.Result)
	}
	if diagnosticsResp.Result.Diagnostics[0].Origin != "tool" || diagnosticsResp.Result.Diagnostics[0].Fingerprint == "" || diagnosticsResp.Result.Diagnostics[0].SourceKind != "tool-emitted" || diagnosticsResp.Result.Diagnostics[0].Stream != "stderr" || diagnosticsResp.Result.Diagnostics[0].RelatedDependency == "" {
		t.Fatalf("unexpected diagnostics provenance payload: %#v", diagnosticsResp.Result)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"diagnosticSummary", "--repo", root, "--module", ":app", "--command", "assemble"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("diagnosticSummary exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var diagnosticSummaryResp struct {
		Result struct {
			Summary struct {
				Total      int `json:"total"`
				BySeverity []struct {
					Key string `json:"key"`
				} `json:"bySeverity"`
				ByOrigin []struct {
					Key string `json:"key"`
				} `json:"byOrigin"`
				BySource []struct {
					Key string `json:"key"`
				} `json:"bySource"`
				ByFile []struct {
					Key string `json:"key"`
				} `json:"byFile"`
			} `json:"summary"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &diagnosticSummaryResp); err != nil {
		t.Fatal(err)
	}
	if diagnosticSummaryResp.Result.Summary.Total != 1 || len(diagnosticSummaryResp.Result.Summary.BySeverity) != 1 || diagnosticSummaryResp.Result.Summary.BySeverity[0].Key != "error" {
		t.Fatalf("unexpected diagnosticSummary payload: %#v", diagnosticSummaryResp.Result)
	}
	if len(diagnosticSummaryResp.Result.Summary.ByOrigin) != 1 || diagnosticSummaryResp.Result.Summary.ByOrigin[0].Key != "tool" || len(diagnosticSummaryResp.Result.Summary.BySource) != 1 || diagnosticSummaryResp.Result.Summary.BySource[0].Key != "tool-emitted" || len(diagnosticSummaryResp.Result.Summary.ByFile) != 1 {
		t.Fatalf("unexpected diagnosticSummary source payload: %#v", diagnosticSummaryResp.Result)
	}

	stalePayload, err := json.Marshal(service.RunSummaryRecord{
		Command:    "assemble",
		ModulePath: ":app",
		Success:    false,
		Diagnostics: []service.DiagnosticRecord{
			{Ordinal: 7, ActionID: "action:compile", Tool: "kotlinc", Code: "kotlinc_unused_symbol", Category: "unused-code", RelatedDependency: "z:dep:1.0", Origin: "tool", SourceKind: "tool-emitted", Stream: "stderr", Severity: "warning", Message: "shared message", File: "/repo/app/src/main/java/App.kt", Line: 12},
			{Ordinal: 1, ActionID: "action:compile", Tool: "javac", Code: "javac_cannot_find_symbol", Category: "symbol-resolution", RelatedDependency: "a:dep:1.0", Origin: "tool", SourceKind: "tool-emitted", Stream: "stderr", Severity: "warning", Message: "shared message", File: "/repo/app/src/main/java/App.kt", Line: 12},
		},
		DiagnosticSummary: &service.DiagnosticSummary{
			Total:      1,
			BySeverity: []service.DiagnosticSummaryBucket{{Key: "error", Count: 1}},
		},
		WrittenAt: "2026-04-09T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runSummaryDir, "assemble.json"), stalePayload, 0o644); err != nil {
		t.Fatal(err)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"diagnostics", "--repo", root, "--module", ":app", "--command", "assemble"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("diagnostics with stale summary exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var staleDiagnosticsResp struct {
		Result struct {
			Diagnostics []struct {
				Tool string `json:"tool"`
				Code string `json:"code"`
			} `json:"diagnostics"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &staleDiagnosticsResp); err != nil {
		t.Fatal(err)
	}
	if len(staleDiagnosticsResp.Result.Diagnostics) != 2 || staleDiagnosticsResp.Result.Diagnostics[0].Tool != "javac" || staleDiagnosticsResp.Result.Diagnostics[0].Code != "javac_cannot_find_symbol" {
		t.Fatalf("unexpected stale-summary diagnostics ordering: %#v", staleDiagnosticsResp.Result)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"diagnosticSummary", "--repo", root, "--module", ":app", "--command", "assemble"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("diagnosticSummary with stale summary exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var staleDiagnosticSummaryResp struct {
		Result struct {
			Summary struct {
				Total      int `json:"total"`
				BySeverity []struct {
					Key   string `json:"key"`
					Count int    `json:"count"`
				} `json:"bySeverity"`
				ByCode []struct {
					Key string `json:"key"`
				} `json:"byCode"`
			} `json:"summary"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &staleDiagnosticSummaryResp); err != nil {
		t.Fatal(err)
	}
	if staleDiagnosticSummaryResp.Result.Summary.Total != 2 {
		t.Fatalf("expected stale summary to be recomputed from raw diagnostics, got %#v", staleDiagnosticSummaryResp.Result.Summary)
	}
	if len(staleDiagnosticSummaryResp.Result.Summary.BySeverity) != 1 || staleDiagnosticSummaryResp.Result.Summary.BySeverity[0].Key != "warning" || staleDiagnosticSummaryResp.Result.Summary.BySeverity[0].Count != 2 {
		t.Fatalf("unexpected recomputed stale-summary severity buckets: %#v", staleDiagnosticSummaryResp.Result.Summary.BySeverity)
	}
	if len(staleDiagnosticSummaryResp.Result.Summary.ByCode) != 2 || staleDiagnosticSummaryResp.Result.Summary.ByCode[0].Key != "javac_cannot_find_symbol" || staleDiagnosticSummaryResp.Result.Summary.ByCode[1].Key != "kotlinc_unused_symbol" {
		t.Fatalf("unexpected recomputed stale-summary code buckets: %#v", staleDiagnosticSummaryResp.Result.Summary.ByCode)
	}
	if err := os.WriteFile(filepath.Join(runSummaryDir, "assemble.json"), payload, 0o644); err != nil {
		t.Fatal(err)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"plannedSchedule", "--repo", root, "--module", ":app", "--command", "assemble"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("plannedSchedule exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var plannedScheduleResp struct {
		Result struct {
			Summary struct {
				ResourceBudgets []struct {
					ResourceClass string `json:"resourceClass"`
				} `json:"resourceBudgets"`
				Batches []struct {
					Actions []struct {
						ID string `json:"id"`
					} `json:"actions"`
				} `json:"batches"`
			} `json:"summary"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &plannedScheduleResp); err != nil {
		t.Fatal(err)
	}
	if len(plannedScheduleResp.Result.Summary.ResourceBudgets) != 1 || len(plannedScheduleResp.Result.Summary.Batches) != 1 || len(plannedScheduleResp.Result.Summary.Batches[0].Actions) != 1 {
		t.Fatalf("unexpected plannedSchedule payload: %#v", plannedScheduleResp.Result.Summary)
	}

	driftPayload, err := json.Marshal(service.RunSummaryRecord{
		Command:    "assembleDrift",
		ModulePath: ":app",
		Success:    false,
		RunGraphSummary: &service.RunGraphSummary{
			PlannedActionIDs:  []string{"action:compile", "action:package"},
			RootActionIDs:     []string{"action:package"},
			ExecutedActionIDs: []string{"action:compile", "action:lint"},
		},
		CriticalPathSummary: &service.CriticalPathSummary{
			BatchCount:           2,
			EstimatedDurationMs:  21,
			RepresentativeAction: []string{"action:compile"},
		},
		SchedulerSummary: &service.SchedulerSummary{
			ExecutedBatchCount:  2,
			CriticalPathActions: 1,
			QueueWaitActions:    1,
			MaxQueueWaitMs:      11,
			WaitReasonCounts:    map[string]int{"resource-lock": 1},
		},
		PlannedSchedule: &service.PlanScheduleResult{
			Batches: []service.PlanScheduleBatch{
				{Actions: []service.InspectPlannedAction{{ID: "action:compile", Name: "compileKotlin", Operation: "compile", ModulePath: ":app", VariantName: "debug"}}},
				{Actions: []service.InspectPlannedAction{{ID: "action:package", Name: "packageDebug", Operation: "package", ModulePath: ":app", VariantName: "debug"}}},
			},
		},
		ActionExecutions: []service.ActionExecution{
			{ActionID: "action:compile", Name: "compileKotlin", Operation: "compile", ModulePath: ":app", VariantName: "debug", BatchIndex: 1, CriticalPath: true, QueueWaitMs: 11, WaitReason: "resource-lock", Status: "executed"},
			{ActionID: "action:lint", Name: "lintDebug", Operation: "lint", ModulePath: ":app", VariantName: "debug", BatchIndex: 0, Status: "executed"},
		},
		WrittenAt: "2026-04-09T01:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runSummaryDir, "assembleDrift.json"), driftPayload, 0o644); err != nil {
		t.Fatal(err)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"scheduleDrift", "--repo", root, "--module", ":app", "--command", "assembleDrift"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("scheduleDrift exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var scheduleDriftResp struct {
		Result struct {
			Summary struct {
				PlannedOnlyCount       int      `json:"plannedOnlyCount"`
				ExecutedOnlyCount      int      `json:"executedOnlyCount"`
				BatchMismatchCount     int      `json:"batchMismatchCount"`
				PlannedOnlyActionIDs   []string `json:"plannedOnlyActionIds"`
				ExecutedOnlyActionIDs  []string `json:"executedOnlyActionIds"`
				BatchMismatchActionIDs []string `json:"batchMismatchActionIds"`
				Actions                []struct {
					ActionID           string `json:"actionId"`
					Planned            bool   `json:"planned"`
					Executed           bool   `json:"executed"`
					PlannedBatchIndex  int    `json:"plannedBatchIndex"`
					ExecutedBatchIndex int    `json:"executedBatchIndex"`
					BatchMismatch      bool   `json:"batchMismatch"`
					QueueWaitMs        int64  `json:"queueWaitMs"`
				} `json:"actions"`
			} `json:"summary"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &scheduleDriftResp); err != nil {
		t.Fatal(err)
	}
	if scheduleDriftResp.Result.Summary.PlannedOnlyCount != 1 || scheduleDriftResp.Result.Summary.ExecutedOnlyCount != 1 || scheduleDriftResp.Result.Summary.BatchMismatchCount != 1 {
		t.Fatalf("unexpected scheduleDrift summary: %#v", scheduleDriftResp.Result.Summary)
	}
	if len(scheduleDriftResp.Result.Summary.PlannedOnlyActionIDs) != 1 || scheduleDriftResp.Result.Summary.PlannedOnlyActionIDs[0] != "action:package" || len(scheduleDriftResp.Result.Summary.ExecutedOnlyActionIDs) != 1 || scheduleDriftResp.Result.Summary.ExecutedOnlyActionIDs[0] != "action:lint" || len(scheduleDriftResp.Result.Summary.BatchMismatchActionIDs) != 1 || scheduleDriftResp.Result.Summary.BatchMismatchActionIDs[0] != "action:compile" {
		t.Fatalf("unexpected scheduleDrift action ids: %#v", scheduleDriftResp.Result.Summary)
	}
	if len(scheduleDriftResp.Result.Summary.Actions) != 3 || scheduleDriftResp.Result.Summary.Actions[0].ActionID != "action:compile" || !scheduleDriftResp.Result.Summary.Actions[0].BatchMismatch || scheduleDriftResp.Result.Summary.Actions[0].PlannedBatchIndex != 0 || scheduleDriftResp.Result.Summary.Actions[0].ExecutedBatchIndex != 1 || scheduleDriftResp.Result.Summary.Actions[0].QueueWaitMs != 11 {
		t.Fatalf("unexpected scheduleDrift action rows: %#v", scheduleDriftResp.Result.Summary.Actions)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"actionExecution", "--repo", root, "--module", ":app", "--command", "assemble", "--action", "action:compile"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("actionExecution exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var actionExecutionResp struct {
		Result struct {
			ActionID  string `json:"actionId"`
			Execution struct {
				ActionID     string `json:"actionId"`
				WaitReason   string `json:"waitReason"`
				CriticalPath bool   `json:"criticalPath"`
			} `json:"execution"`
			Explanation *struct {
				ActionID string `json:"actionId"`
			} `json:"explanation"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &actionExecutionResp); err != nil {
		t.Fatal(err)
	}
	if actionExecutionResp.Result.ActionID != "action:compile" || actionExecutionResp.Result.Execution.ActionID != "action:compile" || actionExecutionResp.Result.Execution.WaitReason != "worker-slot" || !actionExecutionResp.Result.Execution.CriticalPath || actionExecutionResp.Result.Explanation == nil {
		t.Fatalf("unexpected actionExecution payload: %#v", actionExecutionResp.Result)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"actionExplanation", "--repo", root, "--module", ":app", "--command", "assemble", "--action", "action:compile"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("actionExplanation exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var actionExplanationResp struct {
		Result struct {
			ActionID    string `json:"actionId"`
			Explanation struct {
				ActionID  string `json:"actionId"`
				Operation string `json:"operation"`
			} `json:"explanation"`
			Execution *struct {
				ActionID string `json:"actionId"`
				Status   string `json:"status"`
			} `json:"execution"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &actionExplanationResp); err != nil {
		t.Fatal(err)
	}
	if actionExplanationResp.Result.ActionID != "action:compile" || actionExplanationResp.Result.Explanation.ActionID != "action:compile" || actionExplanationResp.Result.Explanation.Operation != "compile" || actionExplanationResp.Result.Execution == nil || actionExplanationResp.Result.Execution.Status != "reused" {
		t.Fatalf("unexpected actionExplanation payload: %#v", actionExplanationResp.Result)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"actionExecutions", "--repo", root, "--module", ":app", "--command", "assemble"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("actionExecutions exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var actionExecutionsResp struct {
		Result struct {
			Executions []struct {
				ActionID string `json:"actionId"`
				Status   string `json:"status"`
			} `json:"executions"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &actionExecutionsResp); err != nil {
		t.Fatal(err)
	}
	if len(actionExecutionsResp.Result.Executions) != 1 || actionExecutionsResp.Result.Executions[0].ActionID != "action:compile" || actionExecutionsResp.Result.Executions[0].Status != "reused" {
		t.Fatalf("unexpected actionExecutions payload: %#v", actionExecutionsResp.Result)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"actionExplanations", "--repo", root, "--module", ":app", "--command", "assemble"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("actionExplanations exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var actionExplanationsResp struct {
		Result struct {
			Explanations []struct {
				ActionID  string `json:"actionId"`
				Operation string `json:"operation"`
			} `json:"explanations"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &actionExplanationsResp); err != nil {
		t.Fatal(err)
	}
	if len(actionExplanationsResp.Result.Explanations) != 1 || actionExplanationsResp.Result.Explanations[0].ActionID != "action:compile" || actionExplanationsResp.Result.Explanations[0].Operation != "compile" {
		t.Fatalf("unexpected actionExplanations payload: %#v", actionExplanationsResp.Result)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"cacheProbes", "--repo", root, "--module", ":app", "--command", "assemble"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("cacheProbes exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var cacheProbesResp struct {
		Result struct {
			Probes []struct {
				ActionID string `json:"actionId"`
				State    string `json:"state"`
			} `json:"probes"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &cacheProbesResp); err != nil {
		t.Fatal(err)
	}
	if len(cacheProbesResp.Result.Probes) != 1 || cacheProbesResp.Result.Probes[0].ActionID != "action:compile" || cacheProbesResp.Result.Probes[0].State != "reused" {
		t.Fatalf("unexpected cacheProbes payload: %#v", cacheProbesResp.Result)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"cacheProbeRecords", "--repo", root, "--module", ":app", "--command", "assemble"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("cacheProbeRecords exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var cacheProbeRecordsResp struct {
		Result struct {
			Records []struct {
				ActionID string `json:"actionId"`
				StepName string `json:"stepName"`
				State    string `json:"state"`
			} `json:"records"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &cacheProbeRecordsResp); err != nil {
		t.Fatal(err)
	}
	if len(cacheProbeRecordsResp.Result.Records) != 1 || cacheProbeRecordsResp.Result.Records[0].ActionID != "action:compile" || cacheProbeRecordsResp.Result.Records[0].StepName != "compileKotlin" || cacheProbeRecordsResp.Result.Records[0].State != "reused" {
		t.Fatalf("unexpected cacheProbeRecords payload: %#v", cacheProbeRecordsResp.Result)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"cacheProbes", "--repo", root, "--module", ":app", "--command", "assemble", "--action", "action:does-not-exist"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("cacheProbes filter exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var cacheProbesFilteredResp struct {
		Result struct {
			Probes []struct {
				ActionID string `json:"actionId"`
			} `json:"probes"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &cacheProbesFilteredResp); err != nil {
		t.Fatal(err)
	}
	if len(cacheProbesFilteredResp.Result.Probes) != 0 {
		t.Fatalf("expected --action filter to drop unmatched probes, got %#v", cacheProbesFilteredResp.Result.Probes)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"cacheProbeRecords", "--repo", root, "--module", ":app", "--command", "assemble", "--step", "no-such-step"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("cacheProbeRecords step filter exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var cacheProbeRecordsStepResp struct {
		Result struct {
			Records []struct {
				StepName string `json:"stepName"`
			} `json:"records"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &cacheProbeRecordsStepResp); err != nil {
		t.Fatal(err)
	}
	if len(cacheProbeRecordsStepResp.Result.Records) != 0 {
		t.Fatalf("expected --step filter to drop unmatched records, got %#v", cacheProbeRecordsStepResp.Result.Records)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"cacheProbeRecords", "--repo", root, "--module", ":app", "--command", "assemble", "--action", "action:compile"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("cacheProbeRecords action filter exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var cacheProbeRecordsKeepResp struct {
		Result struct {
			Records []struct {
				ActionID string `json:"actionId"`
			} `json:"records"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &cacheProbeRecordsKeepResp); err != nil {
		t.Fatal(err)
	}
	if len(cacheProbeRecordsKeepResp.Result.Records) != 1 || cacheProbeRecordsKeepResp.Result.Records[0].ActionID != "action:compile" {
		t.Fatalf("expected --action filter to keep matching record, got %#v", cacheProbeRecordsKeepResp.Result.Records)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"reuseDecision", "--repo", root, "--module", ":app", "--command", "assemble", "--action", "action:compile"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("reuseDecision exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var reuseDecisionResp struct {
		Result struct {
			ActionID string `json:"actionId"`
			Decision struct {
				ActionID     string   `json:"actionId"`
				CacheOutcome string   `json:"cacheOutcome"`
				CacheSource  string   `json:"cacheSource"`
				Basis        []string `json:"basis"`
			} `json:"decision"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &reuseDecisionResp); err != nil {
		t.Fatal(err)
	}
	if reuseDecisionResp.Result.ActionID != "action:compile" || reuseDecisionResp.Result.Decision.ActionID != "action:compile" || reuseDecisionResp.Result.Decision.CacheOutcome != "reused" || reuseDecisionResp.Result.Decision.CacheSource != "summary-cache-probe" || len(reuseDecisionResp.Result.Decision.Basis) != 1 || reuseDecisionResp.Result.Decision.Basis[0] != "test" {
		t.Fatalf("unexpected reuseDecision payload: %#v", reuseDecisionResp.Result)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"reuseDecisions", "--repo", root, "--module", ":app", "--command", "assemble"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("reuseDecisions exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var reuseDecisionsResp struct {
		Result struct {
			Decisions []struct {
				ActionID     string `json:"actionId"`
				CacheOutcome string `json:"cacheOutcome"`
			} `json:"decisions"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &reuseDecisionsResp); err != nil {
		t.Fatal(err)
	}
	if len(reuseDecisionsResp.Result.Decisions) != 1 || reuseDecisionsResp.Result.Decisions[0].ActionID != "action:compile" || reuseDecisionsResp.Result.Decisions[0].CacheOutcome != "reused" {
		t.Fatalf("unexpected reuseDecisions payload: %#v", reuseDecisionsResp.Result)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"materializations", "--repo", root, "--module", ":app", "--command", "assemble"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("materializations exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var materializationsResp struct {
		Result struct {
			Materializations []struct {
				ID                 string `json:"id"`
				ArtifactSnapshotID string `json:"artifactSnapshotId"`
			} `json:"materializations"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &materializationsResp); err != nil {
		t.Fatal(err)
	}
	if len(materializationsResp.Result.Materializations) != 1 || materializationsResp.Result.Materializations[0].ID != "materialization:freeDebug" {
		t.Fatalf("unexpected materializations payload: %#v", materializationsResp.Result)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"actionTrace", "--repo", root, "--module", ":app", "--command", "assemble"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("actionTrace exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var actionTraceResp struct {
		Result struct {
			Actions []struct {
				ActionID       string   `json:"actionId"`
				CacheResult    string   `json:"cacheResult"`
				WaitReason     string   `json:"waitReason"`
				ResourceClass  string   `json:"resourceClass"`
				ResourceCost   int      `json:"resourceCost"`
				MaxParallelism int      `json:"maxParallelism"`
				CacheKey       string   `json:"cacheKey"`
				Cacheable      bool     `json:"cacheable"`
				ProbeOrder     []string `json:"probeOrder"`
				ExecuteOnMiss  bool     `json:"executeOnMiss"`
				RetentionClass string   `json:"retentionClass"`
				Shareability   string   `json:"shareability"`
				Substeps       []struct {
					Name  string `json:"name"`
					Depth int    `json:"depth"`
				} `json:"substeps"`
				Timings []struct {
					Name       string `json:"name"`
					DurationMs int64  `json:"durationMs"`
				} `json:"timings"`
			} `json:"actions"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &actionTraceResp); err != nil {
		t.Fatal(err)
	}
	if len(actionTraceResp.Result.Actions) != 1 || actionTraceResp.Result.Actions[0].ActionID != "action:compile" || actionTraceResp.Result.Actions[0].CacheResult != string(explain.StateReused) || actionTraceResp.Result.Actions[0].WaitReason != "worker-slot" || len(actionTraceResp.Result.Actions[0].Timings) != 1 {
		t.Fatalf("unexpected actionTrace payload: %#v", actionTraceResp.Result)
	}
	if actionTraceResp.Result.Actions[0].ResourceClass != "cpu" || actionTraceResp.Result.Actions[0].ResourceCost != 2 || actionTraceResp.Result.Actions[0].MaxParallelism != 1 || actionTraceResp.Result.Actions[0].CacheKey != "compile-key" || !actionTraceResp.Result.Actions[0].Cacheable || !actionTraceResp.Result.Actions[0].ExecuteOnMiss || actionTraceResp.Result.Actions[0].RetentionClass != "machine-shareable" || actionTraceResp.Result.Actions[0].Shareability != "machine" || len(actionTraceResp.Result.Actions[0].ProbeOrder) != 2 {
		t.Fatalf("expected richer actionTrace policy payload: %#v", actionTraceResp.Result.Actions[0])
	}
	if len(actionTraceResp.Result.Actions[0].Substeps) != 2 || actionTraceResp.Result.Actions[0].Substeps[1].Name != "javac" || actionTraceResp.Result.Actions[0].Substeps[1].Depth != 1 {
		t.Fatalf("unexpected actionTrace substeps payload: %#v", actionTraceResp.Result.Actions[0].Substeps)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"perfTiming", "--repo", root, "--module", ":app", "--command", "assemble"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("perfTiming exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var perfTimingResp struct {
		Result struct {
			Timing []struct {
				Name       string `json:"name"`
				DurationMs int64  `json:"durationMs"`
			} `json:"timing"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &perfTimingResp); err != nil {
		t.Fatal(err)
	}
	if len(perfTimingResp.Result.Timing) != 1 || perfTimingResp.Result.Timing[0].DurationMs != 12 {
		t.Fatalf("unexpected perfTiming payload: %#v", perfTimingResp.Result)
	}
}

func TestIntelliJSyncModelCommandExposesSyncModel(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "settings.gradle.kts"), `
rootProject.name = "IntelliJSyncTest"
include(":app", ":lib")
`)
	mustWriteFile(t, filepath.Join(root, "build.gradle.kts"), `plugins {}`)
	appDir := filepath.Join(root, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(appDir, "build.gradle.kts"), `
plugins { alias(libs.plugins.android.application) }

android {
  namespace = "com.example.app"
  flavorDimensions += "tier"
  productFlavors {
    create("free") { dimension = "tier" }
    create("paid") { dimension = "tier" }
  }
  buildTypes {
    debug {}
    release {}
  }
}
`)
	libDir := filepath.Join(root, "lib")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(libDir, "build.gradle.kts"), `plugins {}`)

	var stdout, stderr strings.Builder
	stdout.Reset()
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"intellijSyncModel", "--repo", root}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("intellijSyncModel exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var resp struct {
		Success bool `json:"success"`
		Result  struct {
			Repo          string             `json:"repo"`
			ModelCacheKey string             `json:"modelCacheKey"`
			Model         intellijsync.Model `json:"model"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Success || resp.Result.Repo != root || resp.Result.ModelCacheKey == "" {
		t.Fatalf("unexpected intellijSyncModel response: %#v", resp)
	}
	if resp.Result.Model.Repo != root || resp.Result.Model.ProjectName == "" || len(resp.Result.Model.Modules) == 0 {
		t.Fatalf("unexpected sync model payload: %#v", resp.Result.Model)
	}
	var app *intellijsync.Module
	for i := range resp.Result.Model.Modules {
		if resp.Result.Model.Modules[i].Path == ":app" {
			app = &resp.Result.Model.Modules[i]
			break
		}
	}
	if app == nil || len(app.Variants) < 2 {
		t.Fatalf("expected android module variants in sync model, got %#v", app)
	}
	var freeDebug *intellijsync.Variant
	for i := range app.Variants {
		if app.Variants[i].Name == "freeDebug" {
			freeDebug = &app.Variants[i]
			break
		}
	}
	if freeDebug == nil {
		t.Fatalf("expected freeDebug variant in sync model, got %#v", app.Variants)
	}
	if app.Identity.GraphModuleID == "" || app.Identity.ModulePath != ":app" || app.Identity.IDEModuleID != "app" {
		t.Fatalf("expected module identity projection in sync model JSON, got %#v", app.Identity)
	}
	if freeDebug.Identity.GraphModuleID == "" || freeDebug.Identity.GraphVariantID == "" {
		t.Fatalf("expected graph-backed variant identity projection in sync model JSON, got %#v", freeDebug.Identity)
	}
	if freeDebug.Identity.IDEVariantID != "app/freeDebug" {
		t.Fatalf("expected IDE variant id in sync model JSON, got %#v", freeDebug.Identity)
	}
	if !sameStrings(freeDebug.Identity.IDESourceSetIDs, []string{"app/freeDebug/sourceSet:main", "app/freeDebug/sourceSet:free", "app/freeDebug/sourceSet:debug", "app/freeDebug/sourceSet:freeDebug"}) {
		t.Fatalf("expected source-set identity mapping in sync model JSON, got %#v", freeDebug.Identity)
	}
	if freeDebug.Materialization.ID == "" || freeDebug.Materialization.ArtifactSnapshotID == "" {
		t.Fatalf("expected graph-backed materialization ids in sync model, got %#v", freeDebug.Materialization)
	}
	if len(freeDebug.Materialization.ManifestPaths) == 0 || freeDebug.Materialization.BackingArtifactID == "" {
		t.Fatalf("expected manifest and backing artifact metadata in sync model, got %#v", freeDebug.Materialization)
	}
	if len(freeDebug.Materialization.ProducedArtifactIDs) == 0 || len(freeDebug.Materialization.ProducedArtifacts) == 0 {
		t.Fatalf("expected produced artifact metadata in sync model, got %#v", freeDebug.Materialization)
	}
	if len(freeDebug.ContentRoots) == 0 {
		t.Fatalf("expected content-root projection in sync model JSON, got %#v", freeDebug.ContentRoots)
	}
	if len(freeDebug.TaskCatalog) == 0 {
		t.Fatalf("expected task-catalog projection in sync model JSON, got %#v", freeDebug.TaskCatalog)
	}
	if len(freeDebug.Targets) == 0 {
		t.Fatalf("expected target projection in sync model JSON, got %#v", freeDebug.Targets)
	}
}

func TestInspectExposesRepositoryMetadata(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MAVEN_USER_HOME", "")
	root := repositoryMetadataCLIProject(t)

	var stdout, stderr strings.Builder
	if exitCode := Run(context.Background(), []string{"inspect", "--repo", root}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("inspect exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var resp struct {
		Success bool `json:"success"`
		Result  struct {
			Repositories []project.Repository `json:"repositories"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Success {
		t.Fatalf("expected inspect success, got %#v", resp)
	}
	if len(resp.Result.Repositories) != 3 {
		t.Fatalf("expected 3 repositories, got %#v", resp.Result.Repositories)
	}
	wantSettingsURL := "file://" + filepath.ToSlash(filepath.Join(home, ".m2", "repository"))
	if got := resp.Result.Repositories[0]; got.Name != "mavenLocal" || got.URL != wantSettingsURL || got.Priority != 0 || got.Origin != "settings" || !got.OfflineAllowed {
		t.Fatalf("unexpected settings repository metadata: %#v", got)
	}
	if got := resp.Result.Repositories[1]; got.Name != "mavenCentral" || got.Priority != 1 || got.Origin != "root-build" || got.OfflineAllowed {
		t.Fatalf("unexpected root-build repository metadata: %#v", got)
	}
	wantModuleURL := "file://" + filepath.ToSlash(filepath.Join(root, "local-repo")) + "/"
	if got := resp.Result.Repositories[2]; got.URL != wantModuleURL || got.Priority != 2 || got.Origin != "module-build" || !got.OfflineAllowed {
		t.Fatalf("unexpected module-build repository metadata: %#v", got)
	}
}

func TestVariantImpactCommandExposesDependents(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "settings.gradle.kts"), `
rootProject.name = "VariantImpactTest"
include(":app", ":lib")
`)
	mustWriteFile(t, filepath.Join(root, "build.gradle.kts"), `plugins {}`)

	libDir := filepath.Join(root, "lib")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(libDir, "build.gradle.kts"), `
plugins { alias(libs.plugins.android.library) }
android {
  namespace = "com.example.lib"
  compileSdk = 34
}
`)

	appDir := filepath.Join(root, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(appDir, "build.gradle.kts"), `
plugins { alias(libs.plugins.android.application) }
android {
  namespace = "com.example.app"
  compileSdk = 34
}
dependencies {
  implementation(project(":lib"))
}
`)

	var stdout, stderr strings.Builder
	exitCode := Run(context.Background(), []string{"variantImpact", "--repo", root, "--module", ":lib", "--variant", "debug"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("variantImpact exited with %d: stderr=%s", exitCode, stderr.String())
	}

	var resp struct {
		Success bool `json:"success"`
		Result  struct {
			Repo          string `json:"repo"`
			Module        string `json:"module"`
			Variant       string `json:"variant"`
			ModelCacheKey string `json:"modelCacheKey"`
			Impact        struct {
				Module     string `json:"module"`
				Variant    string `json:"variant"`
				Dependents []struct {
					Kind        string `json:"kind"`
					ModulePath  string `json:"modulePath"`
					VariantName string `json:"variantName"`
					Name        string `json:"name"`
				} `json:"dependents"`
			} `json:"impact"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Success || resp.Result.Repo != root || resp.Result.Module != ":lib" || resp.Result.Variant != "debug" {
		t.Fatalf("unexpected variantImpact result: %#v", resp.Result)
	}
	if resp.Result.ModelCacheKey == "" || resp.Result.Impact.Module != ":lib" || resp.Result.Impact.Variant != "debug" {
		t.Fatalf("unexpected variantImpact payload: %#v", resp.Result.Impact)
	}
	foundApp := false
	for _, dependent := range resp.Result.Impact.Dependents {
		if dependent.ModulePath == ":app" {
			foundApp = true
			break
		}
	}
	if !foundApp {
		t.Fatalf("expected :app in variantImpact dependents, got %#v", resp.Result.Impact.Dependents)
	}
}

func TestIntelliJSyncModelExposesRepositoryMetadata(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MAVEN_USER_HOME", "")
	root := repositoryMetadataCLIProject(t)

	var stdout, stderr strings.Builder
	if exitCode := Run(context.Background(), []string{"intellijSyncModel", "--repo", root}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("intellijSyncModel exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var resp struct {
		Success bool `json:"success"`
		Result  struct {
			Model intellijsync.Model `json:"model"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Success {
		t.Fatalf("expected intellijSyncModel success, got %#v", resp)
	}
	if len(resp.Result.Model.Project.Repositories) != 3 {
		t.Fatalf("expected 3 projected repositories, got %#v", resp.Result.Model.Project.Repositories)
	}
	wantModuleURL := "file://" + filepath.ToSlash(filepath.Join(root, "local-repo")) + "/"
	wantSettingsURL := "file://" + filepath.ToSlash(filepath.Join(home, ".m2", "repository"))
	var sawSettings, sawRoot, sawModule bool
	for _, repo := range resp.Result.Model.Project.Repositories {
		switch repo.Priority {
		case 0:
			sawSettings = repo.Name == "mavenLocal" && repo.URL == wantSettingsURL && repo.Origin == "settings" && repo.OfflineAllowed
		case 1:
			sawRoot = repo.Name == "mavenCentral" && repo.Origin == "root-build" && !repo.OfflineAllowed
		case 2:
			sawModule = repo.URL == wantModuleURL && repo.Origin == "module-build" && repo.OfflineAllowed
		}
	}
	if !sawSettings {
		t.Fatalf("unexpected projected settings repository metadata: %#v", resp.Result.Model.Project.Repositories)
	}
	if !sawRoot {
		t.Fatalf("unexpected projected root-build repository metadata: %#v", resp.Result.Model.Project.Repositories)
	}
	if !sawModule {
		t.Fatalf("unexpected projected module-build repository metadata: %#v", resp.Result.Model.Project.Repositories)
	}
}

func TestTasksAndSigningReportExposeSupportedTaskSurface(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "settings.gradle.kts"), `
rootProject.name = "TaskTest"
include(":app")
`)
	mustWriteFile(t, filepath.Join(root, "build.gradle.kts"), `plugins {}`)
	mustWriteFile(t, filepath.Join(root, "gradle.properties"), `
RELEASE_KEYSTORE_PATH=keys/release.jks
RELEASE_KEYSTORE_PASSWORD=store-pass
RELEASE_KEY_ALIAS=release-key
RELEASE_KEY_PASSWORD=key-pass
`)
	appDir := filepath.Join(root, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(appDir, "build.gradle.kts"), `
plugins {
  alias(libs.plugins.android.application)
}

val releaseStoreFilePath: String? =
    System.getenv("RELEASE_KEYSTORE_PATH") ?: findProperty("RELEASE_KEYSTORE_PATH") as String?
val releaseStorePassword: String? =
    System.getenv("RELEASE_KEYSTORE_PASSWORD") ?: findProperty("RELEASE_KEYSTORE_PASSWORD") as String?
val releaseKeyAlias: String? =
    System.getenv("RELEASE_KEY_ALIAS") ?: findProperty("RELEASE_KEY_ALIAS") as String?
val releaseKeyPassword: String? =
    System.getenv("RELEASE_KEY_PASSWORD") ?: findProperty("RELEASE_KEY_PASSWORD") as String?
val releaseStoreFile: File? =
    releaseStoreFilePath?.let { path ->
      val file = File(path)
      if (file.isAbsolute) file else rootProject.file(path)
    }

android {
  namespace = "com.example.app"
  compileSdk = 34
  defaultConfig {
    minSdk = 24
    targetSdk = 34
  }
  signingConfigs {
    create("release") {
      storeFile = releaseStoreFile
      storePassword = releaseStorePassword
      keyAlias = releaseKeyAlias
      keyPassword = releaseKeyPassword
    }
  }
  buildTypes {
    debug {
      signingConfig = signingConfigs.getByName("debug")
    }
    release {
      signingConfig = signingConfigs.getByName("release")
    }
  }
}
`)

	var stdout, stderr strings.Builder
	exitCode := Run(context.Background(), []string{"tasks", "--repo", root, "--module", ":app"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("tasks exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var tasksResp struct {
		Success bool `json:"success"`
		Result  struct {
			Tasks []project.Task `json:"tasks"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &tasksResp); err != nil {
		t.Fatal(err)
	}
	required := map[string]bool{
		"assembleDebug":                  false,
		"compileDebugSources":            false,
		"compileDebugUnitTestSources":    false,
		"compileDebugAndroidTestSources": false,
		"installRelease":                 false,
		"testDebugUnitTest":              false,
		"check":                          false,
		"signingReport":                  false,
	}
	unsupported := map[string]bool{
		"assembleDebugAndroidTest":      false,
		"assembleReleaseAndroidTest":    false,
		"compileReleaseUnitTestSources": false,
		"lintDebug":                     false,
		"lintRelease":                   false,
	}
	for _, task := range tasksResp.Result.Tasks {
		if _, ok := required[task.Name]; ok && task.Supported {
			required[task.Name] = true
		}
		if _, ok := unsupported[task.Name]; ok && !task.Supported {
			unsupported[task.Name] = true
		}
	}
	for name, seen := range required {
		if !seen {
			t.Fatalf("expected supported task %s in %#v", name, tasksResp.Result.Tasks)
		}
	}
	for name, seen := range unsupported {
		if !seen {
			t.Fatalf("expected unsupported task %s in %#v", name, tasksResp.Result.Tasks)
		}
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = Run(context.Background(), []string{"signingReport", "--repo", root, "--module", ":app"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("signingReport exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var signingResp struct {
		Success bool `json:"success"`
		Result  struct {
			Variants []struct {
				Name           string `json:"name"`
				ResolvedConfig string `json:"resolvedConfig"`
			} `json:"variants"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &signingResp); err != nil {
		t.Fatal(err)
	}
	if !signingResp.Success || len(signingResp.Result.Variants) != 2 {
		t.Fatalf("unexpected signing report: %#v", signingResp)
	}
	if signingResp.Result.Variants[0].ResolvedConfig == "" || signingResp.Result.Variants[1].ResolvedConfig == "" {
		t.Fatalf("expected resolved signing configs, got %#v", signingResp.Result.Variants)
	}
}

func TestResolverReportCommandExposesCachedM2LocalSurface(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	mustWriteFile(t, filepath.Join(root, "settings.gradle.kts"), `
rootProject.name = "ResolverReportTest"
include(":app")
`)
	mustWriteFile(t, filepath.Join(root, "build.gradle.kts"), `plugins {}`)
	appDir := filepath.Join(root, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(appDir, "build.gradle.kts"), `
plugins { alias(libs.plugins.android.application) }
android {
  namespace = "com.example.app"
  compileSdk = 34
}
dependencies {}
`)

	deps := &modulebuild.Dependencies{}
	cachePath, err := m2local.ResolvedCachePath(filepath.Join(home, ".gradle", "caches", "modules-2", "files-2.1"), root, nil, &catalog.Catalog{
		Versions:  map[string]string{},
		Libraries: map[string]catalog.Library{},
		Bundles:   map[string][]string{},
	}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(m2local.ResolvedEnvelope{
		SchemaVersion: 1,
		Format:        "m2local-resolved",
		Topology:      m2local.New(filepath.Join(home, ".gradle", "caches", "modules-2", "files-2.1"), root, nil, nil).Topology(),
		Resolved: m2local.Resolved{
			CompileJars: []string{filepath.Join(root, "deps", "compile.jar")},
			RuntimeJars: []string{filepath.Join(root, "deps", "runtime.jar")},
			Report: m2local.ResolutionReport{
				Selections: []m2local.ResolutionSelection{{
					Kind:       "variant_selection",
					Coordinate: "g:m:1.0.0",
					Chosen:     "releaseRuntimeElements",
					MetadataSource: &m2local.ResolutionMetadataSource{
						Kind:          "module",
						Path:          filepath.Join(root, ".grit", "metadata", "g", "m", "1.0.0", "m-1.0.0.module"),
						RepositoryURL: "https://repo1.maven.org/maven2/",
						Fetched:       true,
					},
				}},
			},
			Replay: m2local.ResolutionReplay{
				Pins: []m2local.ResolutionPin{{
					Coordinate: "g:m:1.0.0",
					Variant:    "releaseRuntimeElements",
				}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	lockfilePath := strings.TrimSuffix(cachePath, ".json") + ".lockfile.json"
	lockfileData, err := json.Marshal(m2local.ResolutionLockfile{
		SchemaVersion: 1,
		Format:        "m2local-lockfile",
		Pins: []m2local.ResolutionPin{{
			Coordinate: "g:m:1.0.0",
			Variant:    "releaseRuntimeElements",
		}},
		Selections: []m2local.ResolutionSelection{{
			Kind:       "variant_selection",
			Coordinate: "g:m:1.0.0",
			Chosen:     "releaseRuntimeElements",
			MetadataSource: &m2local.ResolutionMetadataSource{
				Kind:          "module",
				Path:          filepath.Join(root, ".grit", "metadata", "g", "m", "1.0.0", "m-1.0.0.module"),
				RepositoryURL: "https://repo1.maven.org/maven2/",
				Fetched:       true,
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockfilePath, lockfileData, 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr strings.Builder
	exitCode := Run(context.Background(), []string{"resolverReport", "--repo", root, "--module", ":app"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("resolverReport exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var resp struct {
		Success bool `json:"success"`
		Result  struct {
			Module       string `json:"module"`
			CachePath    string `json:"cachePath"`
			ReportPath   string `json:"reportPath"`
			ReplayPath   string `json:"replayPath"`
			LockfilePath string `json:"lockfilePath"`
			Found        bool   `json:"found"`
			Topology     struct {
				CacheRoot        string `json:"cacheRoot"`
				WorkMetadataRoot string `json:"workMetadataRoot"`
				Layers           []struct {
					Name string `json:"name"`
				} `json:"layers"`
			} `json:"topology"`
			Inputs struct {
				CacheVersion     string              `json:"cacheVersion"`
				CacheKey         string              `json:"cacheKey"`
				CacheStatus      string              `json:"cacheStatus"`
				DependencyScopes map[string][]string `json:"dependencyScopes"`
			} `json:"inputs"`
			Summary struct {
				CompileJarCount int `json:"compileJarCount"`
				SelectionCount  int `json:"selectionCount"`
				PinCount        int `json:"pinCount"`
			} `json:"summary"`
			Report struct {
				Selections []m2local.ResolutionSelection `json:"selections"`
			} `json:"report"`
			Replay struct {
				Pins []m2local.ResolutionPin `json:"pins"`
			} `json:"replay"`
			Lockfile m2local.ResolutionLockfile `json:"lockfile"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Success || !resp.Result.Found || resp.Result.Module != ":app" || resp.Result.CachePath != cachePath {
		t.Fatalf("unexpected resolverReport response: %#v", resp)
	}
	if resp.Result.ReportPath == "" || resp.Result.ReplayPath == "" || resp.Result.LockfilePath == "" {
		t.Fatalf("expected resolverReport artifact paths: %#v", resp.Result)
	}
	if resp.Result.Topology.CacheRoot == "" || resp.Result.Topology.WorkMetadataRoot == "" || len(resp.Result.Topology.Layers) < 3 {
		t.Fatalf("expected richer resolverReport topology: %#v", resp.Result.Topology)
	}
	if _, err := os.Stat(resp.Result.ReportPath); err != nil {
		t.Fatalf("expected resolverReport report artifact to exist: %v", err)
	}
	if _, err := os.Stat(resp.Result.ReplayPath); err != nil {
		t.Fatalf("expected resolverReport replay artifact to exist: %v", err)
	}
	if _, err := os.Stat(resp.Result.LockfilePath); err != nil {
		t.Fatalf("expected resolverReport lockfile artifact to exist: %v", err)
	}
	if resp.Result.Summary.CompileJarCount != 1 || resp.Result.Summary.SelectionCount != 1 || resp.Result.Summary.PinCount != 1 {
		t.Fatalf("unexpected resolverReport summary: %#v", resp.Result.Summary)
	}
	if resp.Result.Inputs.CacheStatus != "hit" || resp.Result.Inputs.CacheVersion != "16" || resp.Result.Inputs.CacheKey == "" {
		t.Fatalf("unexpected resolverReport inputs: %#v", resp.Result.Inputs)
	}
	if len(resp.Result.Report.Selections) != 1 || resp.Result.Report.Selections[0].Chosen != "releaseRuntimeElements" {
		t.Fatalf("unexpected resolverReport report: %#v", resp.Result.Report)
	}
	if resp.Result.Report.Selections[0].MetadataSource == nil || resp.Result.Report.Selections[0].MetadataSource.Kind != "module" {
		t.Fatalf("expected resolverReport metadata provenance: %#v", resp.Result.Report.Selections[0])
	}
	if len(resp.Result.Replay.Pins) != 1 || resp.Result.Replay.Pins[0].Variant != "releaseRuntimeElements" {
		t.Fatalf("unexpected resolverReport replay: %#v", resp.Result.Replay)
	}
	if resp.Result.Lockfile.SchemaVersion != 1 || len(resp.Result.Lockfile.Pins) != 1 {
		t.Fatalf("unexpected resolverReport lockfile: %#v", resp.Result.Lockfile)
	}
}

func TestCacheTopologyCommandExposesCurrentRootsAndLayers(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	cacheRoot := filepath.Join(home, ".gradle", "caches", "modules-2", "files-2.1")
	mustWriteFile(t, filepath.Join(root, "settings.gradle.kts"), `rootProject.name = "CacheTopologyTest"`)
	mustWriteFile(t, filepath.Join(root, "build.gradle.kts"), `plugins {}`)
	if err := os.MkdirAll(cacheRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr strings.Builder
	if exitCode := Run(context.Background(), []string{"cacheTopology", "--repo", root}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("cacheTopology exited with %d: stderr=%s", exitCode, stderr.String())
	}
	var resp struct {
		Success bool `json:"success"`
		Result  struct {
			Repo     string `json:"repo"`
			Topology struct {
				CacheRoot        string `json:"cacheRoot"`
				WorkRoot         string `json:"workRoot"`
				WorkMetadataRoot string `json:"workMetadataRoot"`
				Layers           []struct {
					Name    string `json:"name"`
					Root    string `json:"root"`
					Scope   string `json:"scope"`
					Content string `json:"content"`
					Shared  bool   `json:"shared"`
				} `json:"layers"`
			} `json:"topology"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Success || resp.Result.Repo != root {
		t.Fatalf("unexpected cacheTopology response: %#v", resp)
	}
	if resp.Result.Topology.CacheRoot != cacheRoot || resp.Result.Topology.WorkRoot != root || resp.Result.Topology.WorkMetadataRoot != filepath.Join(root, ".grit", "metadata") {
		t.Fatalf("unexpected cacheTopology roots: %#v", resp.Result.Topology)
	}
	if len(resp.Result.Topology.Layers) < 3 || resp.Result.Topology.Layers[0].Name == "" || resp.Result.Topology.Layers[0].Root == "" {
		t.Fatalf("expected normalized cacheTopology layers: %#v", resp.Result.Topology)
	}
}

func TestProjectIntrospectionCommands(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "settings.gradle.kts"), `
rootProject.name = "IntrospectionTest"
include(":app", ":lib")
`)
	mustWriteFile(t, filepath.Join(root, "build.gradle.kts"), `plugins {}`)
	mustWriteFile(t, filepath.Join(root, "gradle.properties"), "FOO=bar\n")

	appDir := filepath.Join(root, "app")
	libDir := filepath.Join(root, "lib")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(appDir, "build.gradle.kts"), `
plugins { alias(libs.plugins.android.application) }
android {
  namespace = "com.example.app"
  compileSdk = 34
  defaultConfig {
    applicationId = "com.example.app"
    minSdk = 24
    targetSdk = 34
  }
}
dependencies {
  implementation(projects.lib)
  implementation(libs.okhttp)
}
`)
	mustWriteFile(t, filepath.Join(libDir, "build.gradle.kts"), `
plugins { alias(libs.plugins.android.library) }
android {
  namespace = "com.example.lib"
  compileSdk = 34
}
`)

	for _, args := range [][]string{
		{"projects", "--repo", root},
		{"buildEnvironment", "--repo", root},
		{"outgoingVariants", "--repo", root, "--module", ":app"},
		{"resolvableConfigurations", "--repo", root, "--module", ":app"},
		{"dependencyInsight", "--repo", root, "--module", ":app", "--dependency", "okhttp"},
	} {
		var stdout, stderr strings.Builder
		if exitCode := Run(context.Background(), args, &stdout, &stderr); exitCode != 0 {
			t.Fatalf("%v exited with %d: stderr=%s", args, exitCode, stderr.String())
		}
	}
}

func TestCompileCommandDispatchesAsNativeBuild(t *testing.T) {
	var stdout, stderr strings.Builder
	exitCode := Run(context.Background(), []string{"compile", "--repo", filepath.Join(t.TempDir(), "missing"), "--module", ":lib"}, &stdout, &stderr)
	if exitCode == 0 {
		t.Fatalf("expected compile to fail on missing repo")
	}
	var resp struct {
		Command string `json:"command"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Command != "compile" {
		t.Fatalf("expected compile dispatch, got %#v", resp)
	}
	if strings.Contains(resp.Error.Message, "unknown command") {
		t.Fatalf("compile should dispatch as a native build command, got %#v", resp.Error.Message)
	}
}

func TestTasksUnknownModuleReturnsStructuredError(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "settings.gradle.kts"), `
rootProject.name = "ModuleLookupTest"
include(":app")
`)
	mustWriteFile(t, filepath.Join(root, "build.gradle.kts"), `plugins {}`)
	appDir := filepath.Join(root, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(appDir, "build.gradle.kts"), `
plugins { alias(libs.plugins.android.application) }
android {
  namespace = "com.example.app"
  compileSdk = 34
}
`)

	var stdout, stderr strings.Builder
	exitCode := Run(context.Background(), []string{"tasks", "--repo", root, "--module", ":missing"}, &stdout, &stderr)
	if exitCode == 0 {
		t.Fatalf("expected non-zero exit code, stdout=%s stderr=%s", stdout.String(), stderr.String())
	}

	var resp struct {
		Success bool `json:"success"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Success {
		t.Fatalf("expected failure response, got %#v", resp)
	}
	if resp.Error.Message != "module :missing not found" {
		t.Fatalf("unexpected error message: %#v", resp.Error.Message)
	}
}

func TestNativeResultMarshalIncludesCacheProbes(t *testing.T) {
	t.Parallel()

	result := nativeResult{
		Repo:    "/repo",
		Module:  ":app",
		Variant: "debug",
		ActionExecutions: []service.ActionExecution{{
			ActionID:     "action:compile",
			CriticalPath: true,
			QueueWaitMs:  18,
			WaitReason:   "worker-slot",
			CacheProbe: &responsepayload.CacheProbe{
				ActionID: "action:compile",
				State:    "reused",
				Basis:    "cache-probes",
				Detail:   "1 cache hit, 0 cache misses",
			},
			CacheProbeTrail: []responsepayload.CacheProbeRecord{{
				ActionID: "action:compile",
				StepName: "compileKotlin",
				Order:    0,
				State:    "reused",
				Basis:    "shared-cache-hit",
				Detail:   "restored compiled classes from shared cache",
			}},
		}},
		CacheProbes: []responsepayload.CacheProbe{{
			ActionID: "action:compile",
			State:    "reused",
			Basis:    "cache-probes",
			Detail:   "1 cache hit, 0 cache misses",
		}},
		CacheProbeRecords: []responsepayload.CacheProbeRecord{{
			ActionID: "action:compile",
			StepName: "compileKotlin",
			Order:    0,
			State:    "reused",
			Basis:    "shared-cache-hit",
			Detail:   "restored compiled classes from shared cache",
		}},
	}
	payload, err := json.Marshal(response{
		Success: true,
		Command: "build",
		Result:  resultJSON(result),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var resp struct {
		Result struct {
			CacheProbes []struct {
				ActionID string `json:"actionId"`
				State    string `json:"state"`
				Basis    string `json:"basis"`
				Detail   string `json:"detail"`
			} `json:"cacheProbes"`
			CacheProbeRecords []struct {
				ActionID string `json:"actionId"`
				StepName string `json:"stepName"`
				Order    int    `json:"order"`
				State    string `json:"state"`
				Basis    string `json:"basis"`
				Detail   string `json:"detail"`
			} `json:"cacheProbeRecords"`
			ActionExecutions []struct {
				ActionID     string `json:"actionId"`
				BatchIndex   int    `json:"batchIndex"`
				CriticalPath bool   `json:"criticalPath"`
				QueueWaitMs  int64  `json:"queueWaitMs"`
				WaitReason   string `json:"waitReason"`
				CacheProbe   struct {
					ActionID string `json:"actionId"`
					State    string `json:"state"`
					Basis    string `json:"basis"`
					Detail   string `json:"detail"`
				} `json:"cacheProbe"`
				CacheProbeTrail []struct {
					ActionID string `json:"actionId"`
					StepName string `json:"stepName"`
					Order    int    `json:"order"`
					State    string `json:"state"`
					Basis    string `json:"basis"`
					Detail   string `json:"detail"`
				} `json:"cacheProbeTrail"`
			} `json:"actionExecutions"`
		} `json:"result"`
	}
	if err := json.Unmarshal(payload, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Result.CacheProbes) != 1 || resp.Result.CacheProbes[0].State != "reused" {
		t.Fatalf("unexpected cache probes: %#v", resp.Result.CacheProbes)
	}
	if len(resp.Result.CacheProbeRecords) != 1 || resp.Result.CacheProbeRecords[0].StepName != "compileKotlin" {
		t.Fatalf("unexpected cache probe records: %#v", resp.Result.CacheProbeRecords)
	}
	if len(resp.Result.ActionExecutions) != 1 || resp.Result.ActionExecutions[0].CacheProbe.State != "reused" {
		t.Fatalf("unexpected action execution probe: %#v", resp.Result.ActionExecutions)
	}
	if resp.Result.ActionExecutions[0].BatchIndex != 0 {
		t.Fatalf("unexpected action execution batch index: %#v", resp.Result.ActionExecutions)
	}
	if !resp.Result.ActionExecutions[0].CriticalPath || resp.Result.ActionExecutions[0].QueueWaitMs != 18 || resp.Result.ActionExecutions[0].WaitReason != "worker-slot" {
		t.Fatalf("unexpected action execution scheduling fields: %#v", resp.Result.ActionExecutions)
	}
	if len(resp.Result.ActionExecutions[0].CacheProbeTrail) != 1 || resp.Result.ActionExecutions[0].CacheProbeTrail[0].StepName != "compileKotlin" {
		t.Fatalf("unexpected action execution probe trail: %#v", resp.Result.ActionExecutions)
	}
}

func TestNativeResultMarshalIncludesRunSummaryPath(t *testing.T) {
	var resp struct {
		Result struct {
			RunSummaryPath string `json:"runSummaryPath"`
		} `json:"result"`
	}
	payload, err := json.Marshal(response{
		Success: true,
		Command: "build",
		Result: resultJSON(nativeResult{
			Repo:           "/repo",
			Module:         ":app",
			RunSummaryPath: "/repo/build/grit/run-summaries/_app/build.json",
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(payload, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Result.RunSummaryPath != "/repo/build/grit/run-summaries/_app/build.json" {
		t.Fatalf("unexpected run summary path: %#v", resp)
	}
}

func TestNativeResultMarshalIncludesResolvedVariantMetadata(t *testing.T) {
	var resp struct {
		Result struct {
			TargetResolvedVariant  *project.ResolvedVariant  `json:"targetResolvedVariant"`
			TargetResolvedVariants []project.ResolvedVariant `json:"targetResolvedVariants"`
		} `json:"result"`
	}
	payload, err := json.Marshal(response{
		Success: true,
		Command: "build",
		Result: resultJSON(nativeResult{
			Repo:   "/repo",
			Module: ":app",
			TargetResolvedVariant: &project.ResolvedVariant{
				ModulePath:         ":app",
				Name:               "freeDebug",
				MaterializationID:  "materialization-freeDebug",
				ArtifactSnapshotID: "artifact-freeDebug",
				ProducedArtifactIDs: []string{
					"artifact-apk",
				},
				Coordinate: project.VariantCoordinate{
					BuildType: "debug",
					Flavors:   []string{"free"},
				},
			},
			TargetResolvedVariants: []project.ResolvedVariant{{
				ModulePath:         ":app",
				Name:               "freeDebug",
				MaterializationID:  "materialization-freeDebug",
				ArtifactSnapshotID: "artifact-freeDebug",
				ProducedArtifactIDs: []string{
					"artifact-apk",
				},
				Coordinate: project.VariantCoordinate{
					BuildType: "debug",
					Flavors:   []string{"free"},
				},
			}},
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(payload, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Result.TargetResolvedVariant == nil || resp.Result.TargetResolvedVariant.Name != "freeDebug" {
		t.Fatalf("unexpected resolved variant metadata: %#v", resp.Result)
	}
	if resp.Result.TargetResolvedVariant.MaterializationID == "" || resp.Result.TargetResolvedVariant.ArtifactSnapshotID == "" || len(resp.Result.TargetResolvedVariant.ProducedArtifactIDs) == 0 {
		t.Fatalf("expected graph-backed resolved variant metadata: %#v", resp.Result)
	}
	if len(resp.Result.TargetResolvedVariants) != 1 || !containsResolvedVariant(resp.Result.TargetResolvedVariants, "freeDebug", "debug", "free") {
		t.Fatalf("unexpected resolved variants metadata: %#v", resp.Result)
	}
}

func TestResolveIntelliJTasksCommandReturnsNormalizedBuildRequests(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "settings.gradle.kts"), `
rootProject.name = "ResolveIntelliJTasksTest"
include(":app")
`)
	mustWriteFile(t, filepath.Join(root, "build.gradle.kts"), `plugins {}`)
	appDir := filepath.Join(root, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(appDir, "build.gradle.kts"), `
plugins { alias(libs.plugins.android.application) }
android {
  namespace = "com.example.app"
  flavorDimensions += "tier"
  productFlavors {
    create("free") { dimension = "tier" }
  }
  buildTypes {
    debug {}
  }
}
`)

	var stdout, stderr strings.Builder
	exitCode := Run(context.Background(), []string{
		"resolveIntelliJTasks",
		"--repo", root,
		"--module", ":app",
		"--task", "assembleFreeDebug",
		"--task", "compileFreeDebugUnitTestSources",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("resolveIntelliJTasks exited with %d: stderr=%s", exitCode, stderr.String())
	}

	var resp struct {
		Success bool `json:"success"`
		Result  struct {
			Repo      string   `json:"repo"`
			Module    string   `json:"module"`
			TaskNames []string `json:"taskNames"`
			Requests  []struct {
				ModulePath       string `json:"modulePath"`
				Command          string `json:"command"`
				RequestedVariant string `json:"requestedVariant"`
				VariantExplicit  bool   `json:"variantExplicit"`
			} `json:"requests"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Success {
		t.Fatalf("expected success response, got %#v", resp)
	}
	if resp.Result.Repo != root || resp.Result.Module != ":app" || len(resp.Result.TaskNames) != 2 {
		t.Fatalf("unexpected resolveIntelliJTasks result: %#v", resp.Result)
	}
	if len(resp.Result.Requests) != 2 {
		t.Fatalf("expected 2 normalized requests, got %#v", resp.Result.Requests)
	}
	if resp.Result.Requests[0].Command != "assemble-debug" || resp.Result.Requests[0].RequestedVariant != "freeDebug" || !resp.Result.Requests[0].VariantExplicit {
		t.Fatalf("unexpected assemble request: %#v", resp.Result.Requests[0])
	}
	if resp.Result.Requests[1].Command != "compileDebugUnitTestSources" || resp.Result.Requests[1].RequestedVariant != "freeDebug" || !resp.Result.Requests[1].VariantExplicit {
		t.Fatalf("unexpected unit-test request: %#v", resp.Result.Requests[1])
	}
}

func TestNativeResultMarshalIncludesRunGraphSummary(t *testing.T) {
	var resp struct {
		Result struct {
			RunGraphSummary     *service.RunGraphSummary     `json:"runGraphSummary"`
			CriticalPathSummary *service.CriticalPathSummary `json:"criticalPathSummary"`
			PlannedSchedule     *service.PlanScheduleResult  `json:"plannedSchedule"`
			CacheSummary        *service.CacheSummary        `json:"cacheSummary"`
			SchedulerSummary    *service.SchedulerSummary    `json:"schedulerSummary"`
		} `json:"result"`
	}
	payload, err := json.Marshal(response{
		Success: true,
		Command: "build",
		Result: resultJSON(nativeResult{
			Repo:   "/repo",
			Module: ":app",
			RunGraphSummary: &service.RunGraphSummary{
				ModuleID:           "module-app",
				VariantID:          "variant-debug",
				MaterializationID:  "materialization-debug",
				ArtifactSnapshotID: "artifact-debug",
				PlannedActionIDs:   []string{"a1", "a2"},
				RootActionIDs:      []string{"a1"},
				ExecutedActionIDs:  []string{"a1"},
			},
			CriticalPathSummary: &service.CriticalPathSummary{
				BatchCount:           2,
				EstimatedDurationMs:  12,
				RepresentativeAction: []string{"a1", "a2"},
			},
			PlannedSchedule: &service.PlanScheduleResult{
				ResourceBudgets: []service.PlanResourceBudget{{ResourceClass: "cpu", Capacity: 4}},
				Batches: []service.PlanScheduleBatch{{
					Actions: []service.InspectPlannedAction{{ID: "a1"}},
				}},
			},
			CacheSummary: &service.CacheSummary{
				TotalActions:   2,
				ReusedActions:  1,
				RebuiltActions: 1,
			},
			SchedulerSummary: &service.SchedulerSummary{
				ExecutedBatchCount:  2,
				CriticalPathActions: 1,
				QueueWaitActions:    1,
				TotalQueueWaitMs:    5,
				WaitReasonCounts:    map[string]int{"queue-pressure": 1},
			},
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(payload, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Result.RunGraphSummary == nil || resp.Result.RunGraphSummary.MaterializationID != "materialization-debug" || len(resp.Result.RunGraphSummary.RootActionIDs) != 1 {
		t.Fatalf("unexpected run graph summary: %#v", resp.Result.RunGraphSummary)
	}
	if resp.Result.CriticalPathSummary == nil || resp.Result.CriticalPathSummary.BatchCount != 2 || len(resp.Result.CriticalPathSummary.RepresentativeAction) != 2 {
		t.Fatalf("unexpected critical path summary: %#v", resp.Result.CriticalPathSummary)
	}
	if resp.Result.PlannedSchedule == nil || len(resp.Result.PlannedSchedule.ResourceBudgets) != 1 || len(resp.Result.PlannedSchedule.Batches) != 1 {
		t.Fatalf("unexpected planned schedule: %#v", resp.Result.PlannedSchedule)
	}
	if resp.Result.CacheSummary == nil || resp.Result.CacheSummary.TotalActions != 2 || resp.Result.CacheSummary.ReusedActions != 1 {
		t.Fatalf("unexpected cache summary: %#v", resp.Result.CacheSummary)
	}
	if resp.Result.SchedulerSummary == nil || resp.Result.SchedulerSummary.ExecutedBatchCount != 2 || resp.Result.SchedulerSummary.WaitReasonCounts["queue-pressure"] != 1 {
		t.Fatalf("unexpected scheduler summary: %#v", resp.Result.SchedulerSummary)
	}
}

func mustWriteFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func repositoryMetadataCLIProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "settings.gradle.kts"), `
rootProject.name = "RepositoryMetadataTest"
dependencyResolutionManagement {
  repositories {
    mavenLocal()
  }
}
include(":app")
`)
	mustWriteFile(t, filepath.Join(root, "build.gradle.kts"), `
plugins {}

allprojects {
  repositories {
    mavenCentral()
  }
}
`)
	appDir := filepath.Join(root, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	moduleRepo := "file://" + filepath.ToSlash(filepath.Join(root, "local-repo"))
	mustWriteFile(t, filepath.Join(appDir, "build.gradle.kts"), fmt.Sprintf(`
plugins { alias(libs.plugins.android.application) }

repositories {
  maven {
    url = uri(%q)
  }
}

android {
  namespace = "com.example.app"
  compileSdk = 34
}
`, moduleRepo))
	return root
}

func containsResolvedVariant(variants []project.ResolvedVariant, name, buildType, flavor string) bool {
	for _, variant := range variants {
		if variant.Name != name {
			continue
		}
		if variant.Coordinate.BuildType != buildType {
			continue
		}
		if len(variant.Coordinate.Flavors) != 1 || variant.Coordinate.Flavors[0] != flavor {
			continue
		}
		return true
	}
	return false
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
