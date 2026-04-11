package configmodel

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kaeawc/grit/internal/cachepolicy"
	"github.com/kaeawc/grit/internal/project"
)

type fakeCleanupPathStatter struct {
	infos map[string]os.FileInfo
	errs  map[string]error
}

func (f fakeCleanupPathStatter) Stat(name string) (os.FileInfo, error) {
	if err, ok := f.errs[name]; ok {
		return nil, err
	}
	if info, ok := f.infos[name]; ok {
		return info, nil
	}
	return nil, os.ErrNotExist
}

type fakeCleanupFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
}

func (f fakeCleanupFileInfo) Name() string       { return f.name }
func (f fakeCleanupFileInfo) Size() int64        { return f.size }
func (f fakeCleanupFileInfo) Mode() os.FileMode  { return f.mode }
func (f fakeCleanupFileInfo) ModTime() time.Time { return f.modTime }
func (f fakeCleanupFileInfo) IsDir() bool        { return false }
func (f fakeCleanupFileInfo) Sys() any           { return nil }

func TestModelDryRunCleanupPlanClassifiesProtectedAndEvictableRecords(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "in.jar")
	outputPath := filepath.Join(dir, "out.jar")
	orphanPath := filepath.Join(dir, "orphan.jar")
	if err := os.WriteFile(inputPath, []byte("input-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outputPath, []byte("output-payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orphanPath, []byte("orphan"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(orphanPath, old, old); err != nil {
		t.Fatal(err)
	}
	model := &Model{
		CacheKeyValue: "model-key",
		CachePolicy: func() cachepolicy.Policy {
			policy := cachepolicy.DefaultPolicy()
			diagnostic := policy.ClassPolicies[cachepolicy.RetentionClassDiagnostic]
			diagnostic.TargetBytes = 1
			diagnostic.HardBytes = 4
			policy.ClassPolicies[cachepolicy.RetentionClassDiagnostic] = diagnostic
			policy.SharedTarget = 1
			policy.SharedHard = 4
			return policy
		}(),
		Summary: project.SemanticGraphSummary{
			Modules: []project.SemanticModuleSummary{{
				Path: ":app",
				Variants: []project.SemanticVariantSummary{{
					Name: "debug",
					Actions: []project.SemanticActionSummary{{
						ID:      "action.compile",
						Inputs:  []string{"artifact.input"},
						Outputs: []string{"artifact.output"},
					}},
					Materialization: project.SemanticMaterializationSummary{
						ID:                  "mat.debug",
						BackingArtifactID:   "artifact.input",
						ProducedArtifactIDs: []string{"artifact.output"},
						ConsumingActionIDs:  []string{"action.compile"},
						Artifacts: []project.SemanticArtifactSummary{{
							ID:                 "artifact.output",
							ProducedByActionID: "action.compile",
						}},
					},
				}},
			}},
		},
		ActionSummaries: []ActionSummary{
			{
				ID:             "action.compile",
				ModulePath:     ":app",
				VariantName:    "debug",
				CacheKey:       "compile-key",
				RetentionClass: string(cachepolicy.RetentionClassMachineShareable),
				Shareability:   string(cachepolicy.ShareabilityMachine),
			},
			{
				ID:             "action.orphan",
				ModulePath:     ":app",
				VariantName:    "debug",
				CacheKey:       "orphan-key",
				RetentionClass: string(cachepolicy.RetentionClassDiagnostic),
				Shareability:   string(cachepolicy.ShareabilityMachine),
			},
		},
		ArtifactSummaries: []ArtifactSummary{
			{
				ID:             "artifact.input",
				ModulePath:     ":app",
				VariantName:    "debug",
				Path:           inputPath,
				RetentionClass: string(cachepolicy.RetentionClassMachineShareable),
				Shareability:   string(cachepolicy.ShareabilityMachine),
			},
			{
				ID:             "artifact.output",
				ModulePath:     ":app",
				VariantName:    "debug",
				Path:           outputPath,
				RetentionClass: string(cachepolicy.RetentionClassMachineShareable),
				Shareability:   string(cachepolicy.ShareabilityMachine),
			},
			{
				ID:             "artifact.orphan",
				ModulePath:     ":app",
				VariantName:    "debug",
				Path:           orphanPath,
				RetentionClass: string(cachepolicy.RetentionClassDiagnostic),
				Shareability:   string(cachepolicy.ShareabilityMachine),
			},
		},
		ProvenanceSummaries: []ProvenanceSummary{
			{
				MaterializationID: "mat.debug",
				ModulePath:        ":app",
				VariantName:       "debug",
				RetentionClass:    string(cachepolicy.RetentionClassWorktreeEphemeral),
				Shareability:      string(cachepolicy.ShareabilityWorktreeOnly),
			},
			{
				MaterializationID: "mat.orphan",
				ModulePath:        ":app",
				VariantName:       "debug",
				RetentionClass:    string(cachepolicy.RetentionClassDiagnostic),
				Shareability:      string(cachepolicy.ShareabilityMachine),
			},
		},
	}

	plan := model.DryRunCleanupPlan()
	if plan.ModelCacheKey != "model-key" {
		t.Fatalf("expected model cache key, got %#v", plan)
	}
	if len(plan.Records) != 7 {
		t.Fatalf("expected 7 cleanup records, got %#v", plan.Records)
	}
	if got := plan.Totals[cachepolicy.CleanupDispositionProtected]; got != 4 {
		t.Fatalf("expected 4 protected records, got %d", got)
	}
	if got := plan.Totals[cachepolicy.CleanupDispositionEvictable]; got != 3 {
		t.Fatalf("expected 3 evictable records, got %d", got)
	}
	if plan.KnownBytes == 0 || plan.ProtectedBytes == 0 || plan.EvictableBytes == 0 {
		t.Fatalf("expected byte accounting in cleanup plan, got %#v", plan)
	}

	recordsByID := map[string]cachepolicy.CleanupRecord{}
	for _, record := range plan.Records {
		recordsByID[record.ID] = record
	}
	if recordsByID["action.compile"].Disposition != cachepolicy.CleanupDispositionProtected {
		t.Fatalf("expected compile action to be protected, got %#v", recordsByID["action.compile"])
	}
	if recordsByID["action.compile"].ReasonCode != cachepolicy.CleanupReasonCurrentSemanticSummary {
		t.Fatalf("expected structured reason code on compile action, got %#v", recordsByID["action.compile"])
	}
	if recordsByID["action.orphan"].Disposition != cachepolicy.CleanupDispositionEvictable {
		t.Fatalf("expected orphan action to be evictable, got %#v", recordsByID["action.orphan"])
	}
	if recordsByID["artifact.orphan"].ReasonCode != cachepolicy.CleanupReasonNotReachableMaterializationSummary {
		t.Fatalf("expected structured reason code on orphan artifact, got %#v", recordsByID["artifact.orphan"])
	}
	if recordsByID["artifact.orphan"].Disposition != cachepolicy.CleanupDispositionEvictable {
		t.Fatalf("expected orphan artifact to be evictable, got %#v", recordsByID["artifact.orphan"])
	}
	if recordsByID["mat.debug"].Disposition != cachepolicy.CleanupDispositionProtected {
		t.Fatalf("expected live provenance to be protected, got %#v", recordsByID["mat.debug"])
	}
	if recordsByID["artifact.input"].PathExists == nil || !*recordsByID["artifact.input"].PathExists || recordsByID["artifact.input"].SizeBytes == 0 || recordsByID["artifact.input"].ModifiedAt == nil {
		t.Fatalf("expected stat metadata on known artifact path, got %#v", recordsByID["artifact.input"])
	}
	if recordsByID["artifact.orphan"].AgeHours < 24 {
		t.Fatalf("expected ageHours on old artifact path, got %#v", recordsByID["artifact.orphan"])
	}
	if recordsByID["action.compile"].PathExists != nil {
		t.Fatalf("expected unknown path stats for pathless action, got %#v", recordsByID["action.compile"])
	}

	foundDiagnostic := false
	for _, classPlan := range plan.ClassPlans {
		if classPlan.Class != cachepolicy.RetentionClassDiagnostic {
			continue
		}
		foundDiagnostic = true
		if classPlan.EvictableCount != 3 {
			t.Fatalf("expected diagnostic class to account for 3 evictable records, got %#v", classPlan)
		}
		if classPlan.UnknownSizeCount == classPlan.RecordCount {
			t.Fatalf("expected some known-size accounting for diagnostic class, got %#v", classPlan)
		}
		if classPlan.TotalBytes == 0 || classPlan.EvictableBytes == 0 {
			t.Fatalf("expected per-class byte totals, got %#v", classPlan)
		}
		if len(classPlan.Notes) == 0 {
			t.Fatalf("expected pressure/unknown-size notes on class plan, got %#v", classPlan)
		}
		if len(classPlan.Warnings) == 0 {
			t.Fatalf("expected structured warnings on class plan, got %#v", classPlan)
		}
	}
	if !foundDiagnostic {
		t.Fatalf("expected diagnostic class plan, got %#v", plan.ClassPlans)
	}
	if len(plan.Notes) < 3 {
		t.Fatalf("expected shared budget notes on cleanup plan, got %#v", plan.Notes)
	}
	if len(plan.Warnings) < 2 {
		t.Fatalf("expected structured shared-budget warnings on cleanup plan, got %#v", plan.Warnings)
	}
	if plan.Warnings[0].Scope != cachepolicy.CleanupWarningScopePlan {
		t.Fatalf("expected plan-scoped warning, got %#v", plan.Warnings[0])
	}
}

func TestModelDryRunCleanupPlanAcceptsFakeStatterAndStableClock(t *testing.T) {
	const orphanPath = "/cache/orphan.jar"
	const livePath = "/cache/live.jar"
	now := time.Date(2026, time.April, 11, 10, 0, 0, 0, time.UTC)
	modifiedAt := now.Add(-6 * time.Hour)

	model := &Model{
		CacheKeyValue: "model-key",
		Summary: project.SemanticGraphSummary{
			Modules: []project.SemanticModuleSummary{{
				Path: ":app",
				Variants: []project.SemanticVariantSummary{{
					Name: "debug",
					Actions: []project.SemanticActionSummary{{
						ID:      "action.compile",
						Outputs: []string{"artifact.live"},
					}},
					Materialization: project.SemanticMaterializationSummary{
						ID:                  "mat.debug",
						ProducedArtifactIDs: []string{"artifact.live"},
						ConsumingActionIDs:  []string{"action.compile"},
					},
				}},
			}},
		},
		ActionSummaries: []ActionSummary{{
			ID:             "action.compile",
			ModulePath:     ":app",
			VariantName:    "debug",
			RetentionClass: string(cachepolicy.RetentionClassMachineShareable),
			Shareability:   string(cachepolicy.ShareabilityMachine),
		}},
		ArtifactSummaries: []ArtifactSummary{
			{
				ID:             "artifact.live",
				ModulePath:     ":app",
				VariantName:    "debug",
				Path:           livePath,
				RetentionClass: string(cachepolicy.RetentionClassMachineShareable),
				Shareability:   string(cachepolicy.ShareabilityMachine),
			},
			{
				ID:             "artifact.orphan",
				ModulePath:     ":app",
				VariantName:    "debug",
				Path:           orphanPath,
				RetentionClass: string(cachepolicy.RetentionClassDiagnostic),
				Shareability:   string(cachepolicy.ShareabilityMachine),
			},
		},
	}

	plan := model.dryRunCleanupPlan(now, fakeCleanupPathStatter{
		infos: map[string]os.FileInfo{
			livePath:   fakeCleanupFileInfo{name: "live.jar", size: 12, mode: 0o644, modTime: modifiedAt},
			orphanPath: fakeCleanupFileInfo{name: "orphan.jar", size: 7, mode: 0o644, modTime: modifiedAt},
		},
	})

	recordsByID := map[string]cachepolicy.CleanupRecord{}
	for _, record := range plan.Records {
		recordsByID[record.ID] = record
	}
	if got := recordsByID["artifact.orphan"].AgeHours; got < 5.9 || got > 6.1 {
		t.Fatalf("expected fake clock to drive ageHours, got %#v", recordsByID["artifact.orphan"])
	}
	if recordsByID["artifact.live"].ReasonCode != cachepolicy.CleanupReasonReachableMaterializationSummary {
		t.Fatalf("expected protected artifact reason code, got %#v", recordsByID["artifact.live"])
	}
	if recordsByID["artifact.orphan"].ReasonCode != cachepolicy.CleanupReasonNotReachableMaterializationSummary {
		t.Fatalf("expected evictable artifact reason code, got %#v", recordsByID["artifact.orphan"])
	}
	if recordsByID["artifact.orphan"].PathExists == nil || !*recordsByID["artifact.orphan"].PathExists {
		t.Fatalf("expected fake statter pathExists metadata, got %#v", recordsByID["artifact.orphan"])
	}

	missingPlan := model.dryRunCleanupPlan(now, fakeCleanupPathStatter{
		errs: map[string]error{orphanPath: errors.New("boom")},
	})
	missingRecordsByID := map[string]cachepolicy.CleanupRecord{}
	for _, record := range missingPlan.Records {
		missingRecordsByID[record.ID] = record
	}
	if missingRecordsByID["artifact.orphan"].PathExists == nil || *missingRecordsByID["artifact.orphan"].PathExists {
		t.Fatalf("expected fake statter errors to mark missing path metadata, got %#v", missingRecordsByID["artifact.orphan"])
	}
}
