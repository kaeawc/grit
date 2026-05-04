package configmodel

import (
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/kaeawc/grit/internal/cachepolicy"
	"github.com/kaeawc/grit/internal/project"
)

type cleanupPathStatter interface {
	Stat(name string) (os.FileInfo, error)
}

type osCleanupPathStatter struct{}

func (osCleanupPathStatter) Stat(name string) (os.FileInfo, error) {
	return os.Stat(name)
}

func (m *Model) DryRunCleanupPlan() cachepolicy.CleanupPlan {
	return m.dryRunCleanupPlan(time.Now(), osCleanupPathStatter{})
}

func (m *Model) dryRunCleanupPlan(now time.Time, statter cleanupPathStatter) cachepolicy.CleanupPlan {
	policy := cachepolicy.DefaultPolicy()
	plan := cachepolicy.CleanupPlan{
		Policy: policy,
		Totals: map[cachepolicy.CleanupDisposition]int{},
		Notes: []string{
			"dry-run only: no files are deleted",
			"classification is limited to persisted model summaries and current semantic roots",
			"path, size, and age accounting only use current filesystem state for records with known paths",
		},
	}
	if m == nil {
		plan.Notes = append(plan.Notes, "model is nil")
		return plan
	}
	if len(m.CachePolicy.ClassPolicies) != 0 {
		plan.Policy = m.CachePolicy
	}
	plan.ModelCacheKey = m.CacheKey()

	protectedActions, protectedArtifacts, protectedProvenance := protectedSummaryRoots(m.Summary)
	records := make([]cachepolicy.CleanupRecord, 0, len(m.ActionSummaries)+len(m.ArtifactSummaries)+len(m.ProvenanceSummaries))

	for _, summary := range m.ActionSummaries {
		record := cachepolicy.CleanupRecord{
			Kind:           "action",
			ID:             summary.ID,
			ModulePath:     summary.ModulePath,
			VariantName:    summary.VariantName,
			CacheKey:       summary.CacheKey,
			RetentionClass: cachepolicy.RetentionClass(summary.RetentionClass),
			Shareability:   cachepolicy.Shareability(summary.Shareability),
		}
		populateCleanupRecordStat(&record, now, statter)
		if protectedActions[summary.ID] {
			record.Disposition = cachepolicy.CleanupDispositionProtected
			record.ReasonCode = cachepolicy.CleanupReasonCurrentSemanticSummary
			record.Reason = "referenced by current semantic summary"
		} else {
			record.Disposition = cachepolicy.CleanupDispositionEvictable
			record.ReasonCode = cachepolicy.CleanupReasonNotCurrentSemanticSummary
			record.Reason = "not referenced by current semantic summary"
		}
		records = append(records, record)
	}
	for _, summary := range m.ArtifactSummaries {
		record := cachepolicy.CleanupRecord{
			Kind:           "artifact",
			ID:             summary.ID,
			ModulePath:     summary.ModulePath,
			VariantName:    summary.VariantName,
			Path:           summary.Path,
			RetentionClass: cachepolicy.RetentionClass(summary.RetentionClass),
			Shareability:   cachepolicy.Shareability(summary.Shareability),
		}
		populateCleanupRecordStat(&record, now, statter)
		if protectedArtifacts[summary.ID] {
			record.Disposition = cachepolicy.CleanupDispositionProtected
			record.ReasonCode = cachepolicy.CleanupReasonReachableMaterializationSummary
			record.Reason = "reachable from current materialization summary"
		} else {
			record.Disposition = cachepolicy.CleanupDispositionEvictable
			record.ReasonCode = cachepolicy.CleanupReasonNotReachableMaterializationSummary
			record.Reason = "not reachable from current materialization summary"
		}
		records = append(records, record)
	}
	for _, summary := range m.ProvenanceSummaries {
		record := cachepolicy.CleanupRecord{
			Kind:           "provenance",
			ID:             summary.MaterializationID,
			ModulePath:     summary.ModulePath,
			VariantName:    summary.VariantName,
			RetentionClass: cachepolicy.RetentionClass(summary.RetentionClass),
			Shareability:   cachepolicy.Shareability(summary.Shareability),
		}
		populateCleanupRecordStat(&record, now, statter)
		if protectedProvenance[summary.MaterializationID] {
			record.Disposition = cachepolicy.CleanupDispositionProtected
			record.ReasonCode = cachepolicy.CleanupReasonCurrentMaterializationRoot
			record.Reason = "current materialization root"
		} else {
			record.Disposition = cachepolicy.CleanupDispositionEvictable
			record.ReasonCode = cachepolicy.CleanupReasonNotCurrentMaterializationRoot
			record.Reason = "not present in current materialization roots"
		}
		records = append(records, record)
	}

	sort.Slice(records, func(i, j int) bool {
		if records[i].RetentionClass != records[j].RetentionClass {
			return records[i].RetentionClass < records[j].RetentionClass
		}
		if records[i].Kind != records[j].Kind {
			return records[i].Kind < records[j].Kind
		}
		return records[i].ID < records[j].ID
	})
	plan.Records = records

	classPlans := map[cachepolicy.RetentionClass]*cachepolicy.CleanupClassPlan{}
	for _, classPolicy := range plan.Policy.ClassPolicies {
		cp := classPolicy
		classPlans[classPolicy.Class] = &cachepolicy.CleanupClassPlan{
			Class:         classPolicy.Class,
			Shareability:  classPolicy.Shareability,
			Policy:        cp,
			RecordsByKind: map[string]int{},
		}
	}
	for _, record := range records {
		classPlan, ok := classPlans[record.RetentionClass]
		if !ok {
			classPlan = &cachepolicy.CleanupClassPlan{
				Class:         record.RetentionClass,
				Shareability:  record.Shareability,
				RecordsByKind: map[string]int{},
			}
			classPlans[record.RetentionClass] = classPlan
		}
		classPlan.RecordCount++
		if record.PathExists == nil || *record.PathExists == false || record.SizeBytes <= 0 {
			classPlan.UnknownSizeCount++
		} else {
			classPlan.TotalBytes += record.SizeBytes
			switch record.Disposition {
			case cachepolicy.CleanupDispositionProtected:
				classPlan.ProtectedBytes += record.SizeBytes
			case cachepolicy.CleanupDispositionEvictable:
				classPlan.EvictableBytes += record.SizeBytes
			}
		}
		classPlan.RecordsByKind[record.Kind]++
		switch record.Disposition {
		case cachepolicy.CleanupDispositionProtected:
			classPlan.ProtectedCount++
		case cachepolicy.CleanupDispositionEvictable:
			classPlan.EvictableCount++
		}
	}

	classPlanList := make([]cachepolicy.CleanupClassPlan, 0, len(classPlans))
	for _, classPlan := range classPlans {
		if classPlan.RecordCount == 0 {
			continue
		}
		if classPlan.UnknownSizeCount > 0 {
			classPlan.Notes = append(classPlan.Notes, fmt.Sprintf("%d records have unknown size or no current path", classPlan.UnknownSizeCount))
			classPlan.Warnings = append(classPlan.Warnings, cachepolicy.CleanupWarning{
				Scope:            cachepolicy.CleanupWarningScopeClass,
				Class:            classPlan.Class,
				Kind:             cachepolicy.CleanupWarningUnknownSize,
				UnknownSizeCount: classPlan.UnknownSizeCount,
				Message:          fmt.Sprintf("%d records have unknown size or no current path", classPlan.UnknownSizeCount),
			})
		}
		if classPlan.Policy.TargetBytes > 0 && classPlan.TotalBytes > classPlan.Policy.TargetBytes {
			classPlan.Notes = append(classPlan.Notes, fmt.Sprintf("target budget exceeded: %d > %d bytes", classPlan.TotalBytes, classPlan.Policy.TargetBytes))
			classPlan.Warnings = append(classPlan.Warnings, cachepolicy.CleanupWarning{
				Scope:         cachepolicy.CleanupWarningScopeClass,
				Class:         classPlan.Class,
				Kind:          cachepolicy.CleanupWarningTargetBudgetExceeded,
				ObservedBytes: classPlan.TotalBytes,
				LimitBytes:    classPlan.Policy.TargetBytes,
				Message:       fmt.Sprintf("target budget exceeded: %d > %d bytes", classPlan.TotalBytes, classPlan.Policy.TargetBytes),
			})
		}
		if classPlan.Policy.HardBytes > 0 && classPlan.TotalBytes > classPlan.Policy.HardBytes {
			classPlan.Notes = append(classPlan.Notes, fmt.Sprintf("hard budget exceeded: %d > %d bytes", classPlan.TotalBytes, classPlan.Policy.HardBytes))
			classPlan.Warnings = append(classPlan.Warnings, cachepolicy.CleanupWarning{
				Scope:         cachepolicy.CleanupWarningScopeClass,
				Class:         classPlan.Class,
				Kind:          cachepolicy.CleanupWarningHardBudgetExceeded,
				ObservedBytes: classPlan.TotalBytes,
				LimitBytes:    classPlan.Policy.HardBytes,
				Message:       fmt.Sprintf("hard budget exceeded: %d > %d bytes", classPlan.TotalBytes, classPlan.Policy.HardBytes),
			})
		}
		classPlanList = append(classPlanList, *classPlan)
	}
	sort.Slice(classPlanList, func(i, j int) bool {
		if classPlanList[i].Policy.EvictionOrder != classPlanList[j].Policy.EvictionOrder {
			return classPlanList[i].Policy.EvictionOrder < classPlanList[j].Policy.EvictionOrder
		}
		return classPlanList[i].Class < classPlanList[j].Class
	})
	plan.ClassPlans = classPlanList

	for _, record := range records {
		plan.Totals[record.Disposition]++
		if record.PathExists != nil && *record.PathExists && record.SizeBytes > 0 {
			plan.KnownBytes += record.SizeBytes
			switch record.Disposition {
			case cachepolicy.CleanupDispositionProtected:
				plan.ProtectedBytes += record.SizeBytes
			case cachepolicy.CleanupDispositionEvictable:
				plan.EvictableBytes += record.SizeBytes
			}
		}
	}
	if plan.Policy.SharedTarget > 0 && plan.KnownBytes > plan.Policy.SharedTarget {
		plan.Notes = append(plan.Notes, fmt.Sprintf("shared target budget exceeded: %d > %d bytes", plan.KnownBytes, plan.Policy.SharedTarget))
		plan.Warnings = append(plan.Warnings, cachepolicy.CleanupWarning{
			Scope:         cachepolicy.CleanupWarningScopePlan,
			Kind:          cachepolicy.CleanupWarningSharedTargetExceeded,
			ObservedBytes: plan.KnownBytes,
			LimitBytes:    plan.Policy.SharedTarget,
			Message:       fmt.Sprintf("shared target budget exceeded: %d > %d bytes", plan.KnownBytes, plan.Policy.SharedTarget),
		})
	}
	if plan.Policy.SharedHard > 0 && plan.KnownBytes > plan.Policy.SharedHard {
		plan.Notes = append(plan.Notes, fmt.Sprintf("shared hard budget exceeded: %d > %d bytes", plan.KnownBytes, plan.Policy.SharedHard))
		plan.Warnings = append(plan.Warnings, cachepolicy.CleanupWarning{
			Scope:         cachepolicy.CleanupWarningScopePlan,
			Kind:          cachepolicy.CleanupWarningSharedHardExceeded,
			ObservedBytes: plan.KnownBytes,
			LimitBytes:    plan.Policy.SharedHard,
			Message:       fmt.Sprintf("shared hard budget exceeded: %d > %d bytes", plan.KnownBytes, plan.Policy.SharedHard),
		})
	}
	return plan
}

func populateCleanupRecordStat(record *cachepolicy.CleanupRecord, now time.Time, statter cleanupPathStatter) {
	if record == nil {
		return
	}
	path := record.Path
	if path == "" {
		return
	}
	if statter == nil {
		statter = osCleanupPathStatter{}
	}
	info, err := statter.Stat(path)
	if err != nil {
		exists := false
		record.PathExists = &exists
		return
	}
	exists := true
	record.PathExists = &exists
	record.SizeBytes = info.Size()
	modifiedAt := info.ModTime()
	record.ModifiedAt = &modifiedAt
	if !modifiedAt.IsZero() && !now.IsZero() && now.After(modifiedAt) {
		record.AgeHours = now.Sub(modifiedAt).Hours()
	}
}

// WorktreeRoots returns a WorktreeRoots snapshot for this model, suitable
// for inclusion in a ManifestRootSet.  The workRoot identifies the worktree
// and now is the recording timestamp.
func (m *Model) WorktreeRoots(workRoot string, now time.Time) cachepolicy.WorktreeRoots {
	wr := cachepolicy.WorktreeRoots{
		WorkRoot:      workRoot,
		ModelCacheKey: m.CacheKey(),
		RecordedAt:    now,
	}
	if m == nil {
		return wr
	}
	actions, artifacts, materializations := protectedSummaryRoots(m.Summary)
	wr.Actions = actions
	wr.Artifacts = artifacts
	wr.Materializations = materializations
	return wr
}

func protectedSummaryRoots(summary project.SemanticGraphSummary) (map[string]bool, map[string]bool, map[string]bool) {
	protectedActions := map[string]bool{}
	protectedArtifacts := map[string]bool{}
	protectedProvenance := map[string]bool{}

	for _, mod := range summary.Modules {
		for _, variant := range mod.Variants {
			if variant.Materialization.ID != "" {
				protectedProvenance[variant.Materialization.ID] = true
			}
			if variant.Materialization.BackingArtifactID != "" {
				protectedArtifacts[variant.Materialization.BackingArtifactID] = true
			}
			for _, id := range variant.Materialization.ProducedArtifactIDs {
				protectedArtifacts[id] = true
			}
			for _, id := range variant.Materialization.ConsumingActionIDs {
				protectedActions[id] = true
			}
			for _, artifact := range variant.Materialization.Artifacts {
				if artifact.ID != "" {
					protectedArtifacts[artifact.ID] = true
				}
				if artifact.ProducedByActionID != "" {
					protectedActions[artifact.ProducedByActionID] = true
				}
			}
			for _, action := range variant.Actions {
				if action.ID != "" {
					protectedActions[action.ID] = true
				}
				for _, id := range action.Inputs {
					protectedArtifacts[id] = true
				}
				for _, id := range action.Outputs {
					protectedArtifacts[id] = true
				}
			}
		}
	}
	return protectedActions, protectedArtifacts, protectedProvenance
}
