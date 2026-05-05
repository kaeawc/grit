package configmodel

import (
	"github.com/kaeawc/grit/internal/project"
	"github.com/kaeawc/grit/internal/responsepayload"
)

func cloneSemanticGraphSummary(summary project.SemanticGraphSummary) project.SemanticGraphSummary {
	summary.Modules = cloneSemanticModuleSummaries(summary.Modules)
	return summary
}

func cloneSemanticModuleSummary(summary project.SemanticModuleSummary) project.SemanticModuleSummary {
	summary.ConsumerProguardFiles = append([]string(nil), summary.ConsumerProguardFiles...)
	summary.Plugins = append([]string(nil), summary.Plugins...)
	summary.Tasks = append([]string(nil), summary.Tasks...)
	summary.DependsOn = append([]string(nil), summary.DependsOn...)
	summary.DependencyClosure = append([]string(nil), summary.DependencyClosure...)
	summary.Variants = cloneSemanticVariantSummaries(summary.Variants)
	return summary
}

func cloneSemanticModuleSummaries(summaries []project.SemanticModuleSummary) []project.SemanticModuleSummary {
	if len(summaries) == 0 {
		return nil
	}
	out := make([]project.SemanticModuleSummary, 0, len(summaries))
	for _, summary := range summaries {
		out = append(out, cloneSemanticModuleSummary(summary))
	}
	return out
}

func cloneSemanticVariantSummary(summary project.SemanticVariantSummary) project.SemanticVariantSummary {
	summary.Compatibility = cloneVariantCompatibility(summary.Compatibility)
	summary.Flavors = append([]string(nil), summary.Flavors...)
	summary.Coordinate.Flavors = append([]string(nil), summary.Coordinate.Flavors...)
	summary.MissingDimensions = cloneStringSliceMap(summary.MissingDimensions)
	summary.Optimization = cloneVariantOptimization(summary.Optimization)
	summary.ProguardFiles = append([]string(nil), summary.ProguardFiles...)
	summary.ConsumerProguardFiles = append([]string(nil), summary.ConsumerProguardFiles...)
	summary.SourceSetOrder = append([]string(nil), summary.SourceSetOrder...)
	summary.SourceSetNames = append([]string(nil), summary.SourceSetNames...)
	summary.TaskAliases = append([]string(nil), summary.TaskAliases...)
	summary.ModelSelectors = append([]string(nil), summary.ModelSelectors...)
	summary.SyncFragments = append([]string(nil), summary.SyncFragments...)
	summary.DependsOnVariants = append([]string(nil), summary.DependsOnVariants...)
	summary.DependencyProvenance = append([]project.SemanticDependencyProvenance(nil), summary.DependencyProvenance...)
	summary.TaskProjections = append([]string(nil), summary.TaskProjections...)
	summary.Actions = cloneSemanticActionSummaries(summary.Actions)
	summary.Materialization = cloneSemanticMaterializationSummary(summary.Materialization)
	return summary
}

func cloneSemanticVariantSummaries(summaries []project.SemanticVariantSummary) []project.SemanticVariantSummary {
	if len(summaries) == 0 {
		return nil
	}
	out := make([]project.SemanticVariantSummary, 0, len(summaries))
	for _, summary := range summaries {
		out = append(out, cloneSemanticVariantSummary(summary))
	}
	return out
}

func cloneVariantCompatibility(compatibility project.VariantCompatibility) project.VariantCompatibility {
	compatibility.SourceSetOrder = append([]string(nil), compatibility.SourceSetOrder...)
	compatibility.SourceSetNames = append([]string(nil), compatibility.SourceSetNames...)
	compatibility.TaskAliases = append([]string(nil), compatibility.TaskAliases...)
	compatibility.ModelSelectors = append([]string(nil), compatibility.ModelSelectors...)
	compatibility.SyncFragments = append([]string(nil), compatibility.SyncFragments...)
	return compatibility
}

func cloneVariantOptimization(optimization project.VariantOptimization) project.VariantOptimization {
	if len(optimization.PackageOptimizations) == 0 {
		return optimization
	}
	items := optimization.PackageOptimizations
	optimization.PackageOptimizations = make([]project.PackageOptimization, 0, len(items))
	for _, item := range items {
		if item.MinifyEnabled != nil {
			value := *item.MinifyEnabled
			item.MinifyEnabled = &value
		}
		if item.ShrinkResources != nil {
			value := *item.ShrinkResources
			item.ShrinkResources = &value
		}
		optimization.PackageOptimizations = append(optimization.PackageOptimizations, item)
	}
	return optimization
}

func cloneSemanticMaterializationSummary(summary project.SemanticMaterializationSummary) project.SemanticMaterializationSummary {
	summary.ClasspathSnapshotIDs = append([]string(nil), summary.ClasspathSnapshotIDs...)
	summary.SourceRoots = append([]string(nil), summary.SourceRoots...)
	summary.ProducedArtifactIDs = append([]string(nil), summary.ProducedArtifactIDs...)
	summary.ProducedArtifactPaths = append([]string(nil), summary.ProducedArtifactPaths...)
	summary.ProducedArtifactKinds = append([]string(nil), summary.ProducedArtifactKinds...)
	summary.ResourceArtifactIDs = append([]string(nil), summary.ResourceArtifactIDs...)
	summary.ResourceArtifactPaths = append([]string(nil), summary.ResourceArtifactPaths...)
	summary.ManifestArtifactIDs = append([]string(nil), summary.ManifestArtifactIDs...)
	summary.ManifestArtifactPaths = append([]string(nil), summary.ManifestArtifactPaths...)
	summary.ConsumingActionIDs = append([]string(nil), summary.ConsumingActionIDs...)
	summary.Artifacts = append([]project.SemanticArtifactSummary(nil), summary.Artifacts...)
	return summary
}

func cloneSemanticActionSummaries(summaries []project.SemanticActionSummary) []project.SemanticActionSummary {
	if len(summaries) == 0 {
		return nil
	}
	out := make([]project.SemanticActionSummary, 0, len(summaries))
	for _, summary := range summaries {
		summary.Inputs = append([]string(nil), summary.Inputs...)
		summary.Outputs = append([]string(nil), summary.Outputs...)
		if summary.LastCacheProbe != nil {
			summary.LastCacheProbe = cloneCacheProbe(summary.LastCacheProbe)
		}
		out = append(out, summary)
	}
	return out
}

func cloneCacheProbe(probe *responsepayload.CacheProbe) *responsepayload.CacheProbe {
	if probe == nil {
		return nil
	}
	cloned := *probe
	return &cloned
}

func cloneStringSliceMap(in map[string][]string) map[string][]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string][]string, len(in))
	for key, values := range in {
		out[key] = append([]string(nil), values...)
	}
	return out
}
