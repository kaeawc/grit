package materialization

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type Mode string

const (
	ModeUnknown        Mode = ""
	ModeSourceBacked   Mode = "source-backed"
	ModeArtifactBacked Mode = "artifact-backed"
	ModeHybrid         Mode = "hybrid"
)

type BindingReason string

const (
	BindingReasonUnknown           BindingReason = ""
	BindingReasonRequestedSource   BindingReason = "requested-source"
	BindingReasonRequestedArtifact BindingReason = "requested-artifact"
	BindingReasonLocalInvalidation BindingReason = "local-invalidation"
	BindingReasonArtifactSnapshot  BindingReason = "artifact-snapshot"
	BindingReasonMissingArtifact   BindingReason = "missing-artifact"
	BindingReasonExplicitOverride  BindingReason = "explicit-override"
)

type Reference struct {
	Kind   string `json:"kind,omitempty"`
	ID     string `json:"id,omitempty"`
	Path   string `json:"path,omitempty"`
	Digest string `json:"digest,omitempty"`
}

type Provenance struct {
	Producer string      `json:"producer,omitempty"`
	Subject  string      `json:"subject,omitempty"`
	Inputs   []Reference `json:"inputs,omitempty"`
	Reasons  []string    `json:"reasons,omitempty"`
}

type ArtifactSnapshot struct {
	ID              string      `json:"id,omitempty"`
	LogicalModuleID string      `json:"logicalModuleId,omitempty"`
	VariantID       string      `json:"variantId,omitempty"`
	Artifacts       []Reference `json:"artifacts,omitempty"`
	Inputs          []Reference `json:"inputs,omitempty"`
	Provenance      Provenance  `json:"provenance,omitempty"`
}

type ClasspathSnapshotReference struct {
	ID               string   `json:"id,omitempty"`
	NormalizedID     string   `json:"normalizedId,omitempty"`
	OrderedEntriesID string   `json:"orderedEntriesId,omitempty"`
	EntriesDigest    string   `json:"entriesDigest,omitempty"`
	EntryCount       int      `json:"entryCount,omitempty"`
	Entries          []string `json:"entries,omitempty"`
}

type BindingDecision struct {
	EdgeID                     string        `json:"edgeId,omitempty"`
	UpstreamModuleID           string        `json:"upstreamModuleId,omitempty"`
	UpstreamVariantID          string        `json:"upstreamVariantId,omitempty"`
	SelectedMode               Mode          `json:"selectedMode,omitempty"`
	Reason                     BindingReason `json:"reason,omitempty"`
	Detail                     string        `json:"detail,omitempty"`
	LocalInvalidation          bool          `json:"localInvalidation,omitempty"`
	SelectedArtifactSnapshotID string        `json:"selectedArtifactSnapshotId,omitempty"`
}

type Materialization struct {
	ID                   string                       `json:"id,omitempty"`
	LogicalModuleID      string                       `json:"logicalModuleId,omitempty"`
	VariantID            string                       `json:"variantId,omitempty"`
	Mode                 Mode                         `json:"mode,omitempty"`
	SourceRoots          []string                     `json:"sourceRoots,omitempty"`
	ArtifactSnapshotID   string                       `json:"artifactSnapshotId,omitempty"`
	ClasspathSnapshotIDs []string                     `json:"classpathSnapshotIds,omitempty"`
	ClasspathSnapshots   []ClasspathSnapshotReference `json:"classpathSnapshots,omitempty"`
	Bindings             []BindingDecision            `json:"bindings,omitempty"`
	Provenance           Provenance                   `json:"provenance,omitempty"`
}

func NewArtifactSnapshot(logicalModuleID, variantID string, artifacts, inputs []Reference, provenance Provenance) ArtifactSnapshot {
	snapshot := ArtifactSnapshot{
		LogicalModuleID: logicalModuleID,
		VariantID:       variantID,
		Artifacts:       canonicalizeReferences(artifacts),
		Inputs:          canonicalizeReferences(inputs),
		Provenance:      canonicalizeProvenance(provenance),
	}
	snapshot.ID = fingerprint(snapshotFingerprintPayload{
		Type:            "artifact-snapshot",
		LogicalModuleID: logicalModuleID,
		VariantID:       variantID,
		Artifacts:       snapshot.Artifacts,
		Inputs:          snapshot.Inputs,
		Provenance:      snapshot.Provenance,
	})
	return snapshot
}

func NewMaterialization(logicalModuleID, variantID string, mode Mode, sourceRoots []string, artifactSnapshotID string, classpathSnapshotIDs []string, bindings []BindingDecision, provenance Provenance) Materialization {
	materialization := Materialization{
		LogicalModuleID:      logicalModuleID,
		VariantID:            variantID,
		Mode:                 mode,
		SourceRoots:          canonicalizeStrings(sourceRoots),
		ArtifactSnapshotID:   artifactSnapshotID,
		ClasspathSnapshotIDs: canonicalizeStrings(classpathSnapshotIDs),
		Bindings:             canonicalizeBindings(bindings),
		Provenance:           canonicalizeProvenance(provenance),
	}
	materialization.ID = fingerprint(materializationFingerprintPayload{
		Type:                 "materialization",
		LogicalModuleID:      logicalModuleID,
		VariantID:            variantID,
		Mode:                 mode,
		SourceRoots:          materialization.SourceRoots,
		ArtifactSnapshotID:   artifactSnapshotID,
		ClasspathSnapshotIDs: materialization.ClasspathSnapshotIDs,
		Bindings:             materialization.Bindings,
		Provenance:           materialization.Provenance,
	})
	return materialization
}

func (m Materialization) IsSourceBacked() bool {
	return m.Mode == ModeSourceBacked
}

func (m Materialization) IsArtifactBacked() bool {
	return m.Mode == ModeArtifactBacked
}

func (m Materialization) WithClasspathSnapshots(refs []ClasspathSnapshotReference) Materialization {
	out := m
	out.ClasspathSnapshots = canonicalizeClasspathSnapshotReferences(refs)
	if len(out.ClasspathSnapshotIDs) == 0 {
		for _, ref := range out.ClasspathSnapshots {
			if strings.TrimSpace(ref.ID) != "" {
				out.ClasspathSnapshotIDs = append(out.ClasspathSnapshotIDs, strings.TrimSpace(ref.ID))
			}
		}
		out.ClasspathSnapshotIDs = canonicalizeStrings(out.ClasspathSnapshotIDs)
	}
	return out
}

func (m Materialization) ClasspathSnapshot(id string) (ClasspathSnapshotReference, bool) {
	id = strings.TrimSpace(id)
	for _, ref := range m.ClasspathSnapshots {
		if ref.ID == id {
			return ref, true
		}
	}
	return ClasspathSnapshotReference{}, false
}

func (m Materialization) Fingerprint() string {
	if m.ID != "" {
		return m.ID
	}
	return fingerprint(materializationFingerprintPayload{
		Type:                 "materialization",
		LogicalModuleID:      m.LogicalModuleID,
		VariantID:            m.VariantID,
		Mode:                 m.Mode,
		SourceRoots:          canonicalizeStrings(m.SourceRoots),
		ArtifactSnapshotID:   m.ArtifactSnapshotID,
		ClasspathSnapshotIDs: canonicalizeStrings(m.ClasspathSnapshotIDs),
		Bindings:             canonicalizeBindings(m.Bindings),
		Provenance:           canonicalizeProvenance(m.Provenance),
	})
}

func (a ArtifactSnapshot) Fingerprint() string {
	if a.ID != "" {
		return a.ID
	}
	return fingerprint(snapshotFingerprintPayload{
		Type:            "artifact-snapshot",
		LogicalModuleID: a.LogicalModuleID,
		VariantID:       a.VariantID,
		Artifacts:       canonicalizeReferences(a.Artifacts),
		Inputs:          canonicalizeReferences(a.Inputs),
		Provenance:      canonicalizeProvenance(a.Provenance),
	})
}

func canonicalizeStrings(v []string) []string {
	if len(v) == 0 {
		return nil
	}
	out := append([]string(nil), v...)
	sort.Strings(out)
	return out
}

func canonicalizeReferences(v []Reference) []Reference {
	if len(v) == 0 {
		return nil
	}
	out := append([]Reference(nil), v...)
	sort.SliceStable(out, func(i, j int) bool {
		return referenceKey(out[i]) < referenceKey(out[j])
	})
	return out
}

func canonicalizeBindings(v []BindingDecision) []BindingDecision {
	if len(v) == 0 {
		return nil
	}
	out := append([]BindingDecision(nil), v...)
	sort.SliceStable(out, func(i, j int) bool {
		return bindingKey(out[i]) < bindingKey(out[j])
	})
	return out
}

func canonicalizeClasspathSnapshotReferences(v []ClasspathSnapshotReference) []ClasspathSnapshotReference {
	if len(v) == 0 {
		return nil
	}
	out := append([]ClasspathSnapshotReference(nil), v...)
	for i := range out {
		out[i].ID = strings.TrimSpace(out[i].ID)
		out[i].NormalizedID = strings.TrimSpace(out[i].NormalizedID)
		out[i].OrderedEntriesID = strings.TrimSpace(out[i].OrderedEntriesID)
		out[i].EntriesDigest = strings.TrimSpace(out[i].EntriesDigest)
		out[i].Entries = canonicalizeStrings(out[i].Entries)
		if out[i].EntryCount == 0 && len(out[i].Entries) > 0 {
			out[i].EntryCount = len(out[i].Entries)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return classpathSnapshotReferenceKey(out[i]) < classpathSnapshotReferenceKey(out[j])
	})
	return out
}

func canonicalizeProvenance(v Provenance) Provenance {
	out := Provenance{
		Producer: strings.TrimSpace(v.Producer),
		Subject:  strings.TrimSpace(v.Subject),
		Inputs:   canonicalizeReferences(v.Inputs),
		Reasons:  canonicalizeStrings(v.Reasons),
	}
	return out
}

type snapshotFingerprintPayload struct {
	Type            string      `json:"type"`
	LogicalModuleID string      `json:"logicalModuleId,omitempty"`
	VariantID       string      `json:"variantId,omitempty"`
	Artifacts       []Reference `json:"artifacts,omitempty"`
	Inputs          []Reference `json:"inputs,omitempty"`
	Provenance      Provenance  `json:"provenance,omitempty"`
}

type materializationFingerprintPayload struct {
	Type                 string            `json:"type"`
	LogicalModuleID      string            `json:"logicalModuleId,omitempty"`
	VariantID            string            `json:"variantId,omitempty"`
	Mode                 Mode              `json:"mode,omitempty"`
	SourceRoots          []string          `json:"sourceRoots,omitempty"`
	ArtifactSnapshotID   string            `json:"artifactSnapshotId,omitempty"`
	ClasspathSnapshotIDs []string          `json:"classpathSnapshotIds,omitempty"`
	Bindings             []BindingDecision `json:"bindings,omitempty"`
	Provenance           Provenance        `json:"provenance,omitempty"`
}

func fingerprint(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("materialization fingerprint marshal: %v", err))
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:])
}

func referenceKey(v Reference) string {
	return strings.Join([]string{v.Kind, v.ID, v.Path, v.Digest}, "\x00")
}

func bindingKey(v BindingDecision) string {
	return strings.Join([]string{
		v.EdgeID,
		v.UpstreamModuleID,
		v.UpstreamVariantID,
		string(v.SelectedMode),
		string(v.Reason),
		v.Detail,
		fmt.Sprintf("%t", v.LocalInvalidation),
		v.SelectedArtifactSnapshotID,
	}, "\x00")
}

func classpathSnapshotReferenceKey(v ClasspathSnapshotReference) string {
	return strings.Join([]string{
		v.ID,
		v.NormalizedID,
		v.OrderedEntriesID,
		v.EntriesDigest,
		strings.Join(v.Entries, "\x1f"),
		fmt.Sprintf("%d", v.EntryCount),
	}, "\x00")
}
