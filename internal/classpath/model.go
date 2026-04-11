package classpath

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kaeawc/grit/internal/identity"
	"github.com/kaeawc/grit/internal/materialization"
)

type Scope string

const (
	ScopeUnknown     Scope = ""
	ScopeCompile     Scope = "compile"
	ScopeRuntime     Scope = "runtime"
	ScopeTest        Scope = "test"
	ScopeTransformed Scope = "transformed"
)

type Origin string

const (
	OriginUnknown   Origin = ""
	OriginSource    Origin = "source"
	OriginArtifact  Origin = "artifact"
	OriginGenerated Origin = "generated"
	OriginToolchain Origin = "toolchain"
	OriginSynthetic Origin = "synthetic"
)

type Entry struct {
	Path            string                     `json:"path,omitempty"`
	NormalizedPath  string                     `json:"normalizedPath,omitempty"`
	Origin          Origin                     `json:"origin,omitempty"`
	ArtifactID      string                     `json:"artifactId,omitempty"`
	ModuleID        string                     `json:"moduleId,omitempty"`
	VariantID       string                     `json:"variantId,omitempty"`
	FamilyKey       string                     `json:"familyKey,omitempty"`
	SelectionReason string                     `json:"selectionReason,omitempty"`
	Provenance      materialization.Provenance `json:"provenance,omitempty"`
}

type NormalizationDecision struct {
	InputPath    string `json:"inputPath,omitempty"`
	OutputPath   string `json:"outputPath,omitempty"`
	Rule         string `json:"rule,omitempty"`
	Reason       string `json:"reason,omitempty"`
	FamilyKey    string `json:"familyKey,omitempty"`
	Dropped      bool   `json:"dropped,omitempty"`
	CanonicalKey string `json:"canonicalKey,omitempty"`
}

type Snapshot struct {
	ID          string                     `json:"id,omitempty"`
	Scope       Scope                      `json:"scope,omitempty"`
	ModuleID    string                     `json:"moduleId,omitempty"`
	VariantID   string                     `json:"variantId,omitempty"`
	ToolchainID string                     `json:"toolchainId,omitempty"`
	Entries     []Entry                    `json:"entries,omitempty"`
	Decisions   []NormalizationDecision    `json:"decisions,omitempty"`
	Provenance  materialization.Provenance `json:"provenance,omitempty"`
}

type EntryRecord struct {
	ID              string                     `json:"id,omitempty"`
	Digest          string                     `json:"digest,omitempty"`
	Order           int                        `json:"order,omitempty"`
	Path            string                     `json:"path,omitempty"`
	NormalizedPath  string                     `json:"normalizedPath,omitempty"`
	Origin          Origin                     `json:"origin,omitempty"`
	ArtifactID      string                     `json:"artifactId,omitempty"`
	ModuleID        string                     `json:"moduleId,omitempty"`
	VariantID       string                     `json:"variantId,omitempty"`
	FamilyKey       string                     `json:"familyKey,omitempty"`
	SelectionReason string                     `json:"selectionReason,omitempty"`
	Provenance      materialization.Provenance `json:"provenance,omitempty"`
}

type Record struct {
	ID                string                     `json:"id,omitempty"`
	NormalizedID      string                     `json:"normalizedId,omitempty"`
	OrderedEntriesID  string                     `json:"orderedEntriesId,omitempty"`
	EntriesDigest     string                     `json:"entriesDigest,omitempty"`
	Scope             Scope                      `json:"scope,omitempty"`
	ModuleID          string                     `json:"moduleId,omitempty"`
	VariantID         string                     `json:"variantId,omitempty"`
	ToolchainID       string                     `json:"toolchainId,omitempty"`
	Entries           []EntryRecord              `json:"entries,omitempty"`
	NormalizedEntries []string                   `json:"normalizedEntries,omitempty"`
	Decisions         []NormalizationDecision    `json:"decisions,omitempty"`
	Provenance        materialization.Provenance `json:"provenance,omitempty"`
}

func Normalize(scope Scope, moduleID, variantID, toolchainID string, entries []Entry, provenance materialization.Provenance) Snapshot {
	snapshot := Snapshot{
		Scope:       scope,
		ModuleID:    strings.TrimSpace(moduleID),
		VariantID:   strings.TrimSpace(variantID),
		ToolchainID: strings.TrimSpace(toolchainID),
		Provenance:  canonicalizeProvenance(provenance),
	}
	seen := map[string]struct{}{}
	for _, entry := range entries {
		normalized, decision := normalizeEntry(entry)
		if decision.CanonicalKey == "" {
			decision.CanonicalKey = canonicalKey(normalized)
		}
		if decision.CanonicalKey == "" {
			decision.Dropped = true
			decision.Reason = firstNonEmpty(decision.Reason, "empty-entry")
			snapshot.Decisions = append(snapshot.Decisions, decision)
			continue
		}
		if _, ok := seen[decision.CanonicalKey]; ok {
			decision.Dropped = true
			decision.Reason = "duplicate-entry"
			if decision.Rule == "" && decision.FamilyKey != "" {
				decision.Rule = "family-key"
			}
			snapshot.Decisions = append(snapshot.Decisions, decision)
			continue
		}
		seen[decision.CanonicalKey] = struct{}{}
		snapshot.Entries = append(snapshot.Entries, normalized)
		snapshot.Decisions = append(snapshot.Decisions, decision)
	}
	snapshot.ID = fingerprint(snapshotFingerprintPayload{
		Type:        "classpath-snapshot",
		Scope:       scope,
		ModuleID:    snapshot.ModuleID,
		VariantID:   snapshot.VariantID,
		ToolchainID: snapshot.ToolchainID,
		Entries:     snapshot.Entries,
		Decisions:   snapshot.Decisions,
		Provenance:  snapshot.Provenance,
	})
	return snapshot
}

func (s Snapshot) Fingerprint() string {
	if s.ID != "" {
		return s.ID
	}
	return fingerprint(snapshotFingerprintPayload{
		Type:        "classpath-snapshot",
		Scope:       s.Scope,
		ModuleID:    s.ModuleID,
		VariantID:   s.VariantID,
		ToolchainID: s.ToolchainID,
		Entries:     s.Entries,
		Decisions:   s.Decisions,
		Provenance:  canonicalizeProvenance(s.Provenance),
	})
}

func (s Snapshot) EntryPaths() []string {
	out := make([]string, 0, len(s.Entries))
	for _, entry := range s.Entries {
		out = append(out, entry.NormalizedPath)
	}
	return out
}

func (s Snapshot) Has(path string) bool {
	target := canonicalPath(path)
	for _, entry := range s.Entries {
		if entry.NormalizedPath == target || entry.Path == path {
			return true
		}
	}
	return false
}

func (s Snapshot) Record() Record {
	normalizedEntries := s.EntryPaths()
	records := make([]EntryRecord, 0, len(s.Entries))
	for i, entry := range s.Entries {
		digest := fingerprint(entryFingerprintPayload{
			Path:            entry.Path,
			NormalizedPath:  entry.NormalizedPath,
			Origin:          entry.Origin,
			ArtifactID:      entry.ArtifactID,
			ModuleID:        entry.ModuleID,
			VariantID:       entry.VariantID,
			FamilyKey:       entry.FamilyKey,
			SelectionReason: entry.SelectionReason,
			Provenance:      canonicalizeProvenance(entry.Provenance),
		})
		records = append(records, EntryRecord{
			ID:              fingerprint(recordEntryIdentityPayload{SnapshotID: s.Fingerprint(), Order: i, Digest: digest}),
			Digest:          digest,
			Order:           i,
			Path:            entry.Path,
			NormalizedPath:  entry.NormalizedPath,
			Origin:          entry.Origin,
			ArtifactID:      entry.ArtifactID,
			ModuleID:        entry.ModuleID,
			VariantID:       entry.VariantID,
			FamilyKey:       entry.FamilyKey,
			SelectionReason: entry.SelectionReason,
			Provenance:      canonicalizeProvenance(entry.Provenance),
		})
	}
	return Record{
		ID:               s.Fingerprint(),
		NormalizedID:     identity.NewClasspathSnapshotID(normalizedEntries...).String(),
		OrderedEntriesID: identity.NewClasspathSnapshotFromOrderedEntries(normalizedEntries...).String(),
		EntriesDigest: fingerprint(recordEntriesFingerprintPayload{
			Type:    "classpath-record-entries",
			Entries: records,
		}),
		Scope:             s.Scope,
		ModuleID:          strings.TrimSpace(s.ModuleID),
		VariantID:         strings.TrimSpace(s.VariantID),
		ToolchainID:       strings.TrimSpace(s.ToolchainID),
		Entries:           records,
		NormalizedEntries: append([]string(nil), normalizedEntries...),
		Decisions:         append([]NormalizationDecision(nil), s.Decisions...),
		Provenance:        canonicalizeProvenance(s.Provenance),
	}
}

func (r Record) EntryPaths() []string {
	return append([]string(nil), r.NormalizedEntries...)
}

func (r Record) Has(path string) bool {
	_, ok := r.Lookup(path)
	return ok
}

func (r Record) Lookup(path string) (EntryRecord, bool) {
	target := canonicalPath(path)
	for _, entry := range r.Entries {
		if entry.NormalizedPath == target || entry.Path == path {
			return entry, true
		}
	}
	return EntryRecord{}, false
}

func (s Snapshot) Reference() materialization.ClasspathSnapshotReference {
	return s.Record().Reference()
}

func (r Record) Reference() materialization.ClasspathSnapshotReference {
	return materialization.ClasspathSnapshotReference{
		ID:               strings.TrimSpace(r.ID),
		NormalizedID:     strings.TrimSpace(r.NormalizedID),
		OrderedEntriesID: strings.TrimSpace(r.OrderedEntriesID),
		EntriesDigest:    strings.TrimSpace(r.EntriesDigest),
		EntryCount:       len(r.Entries),
		Entries:          append([]string(nil), r.NormalizedEntries...),
	}
}

func normalizeEntry(entry Entry) (Entry, NormalizationDecision) {
	out := entry
	out.Path = strings.TrimSpace(out.Path)
	out.NormalizedPath = strings.TrimSpace(out.NormalizedPath)
	out.ArtifactID = strings.TrimSpace(out.ArtifactID)
	out.ModuleID = strings.TrimSpace(out.ModuleID)
	out.VariantID = strings.TrimSpace(out.VariantID)
	out.FamilyKey = strings.TrimSpace(out.FamilyKey)
	out.SelectionReason = strings.TrimSpace(out.SelectionReason)
	out.Provenance = canonicalizeProvenance(out.Provenance)

	decision := NormalizationDecision{
		InputPath:  out.Path,
		OutputPath: out.NormalizedPath,
		FamilyKey:  out.FamilyKey,
	}

	if out.NormalizedPath == "" && out.Path != "" {
		out.NormalizedPath = canonicalPath(out.Path)
		decision.OutputPath = out.NormalizedPath
		decision.Rule = "clean-path"
		decision.Reason = "normalized filesystem path"
	}
	if out.NormalizedPath == "" {
		decision.CanonicalKey = canonicalKey(out)
		return out, decision
	}
	if decision.Rule == "" && out.Path != out.NormalizedPath {
		decision.Rule = "normalized-path"
		decision.Reason = "preserved normalized path"
	}
	if out.FamilyKey != "" {
		decision.Rule = firstNonEmpty(decision.Rule, "family-key")
		if decision.Reason == "" {
			decision.Reason = "collapsed by family key"
		}
	}
	decision.CanonicalKey = canonicalKey(out)
	return out, decision
}

func canonicalKey(entry Entry) string {
	key := strings.TrimSpace(entry.FamilyKey)
	if key == "" {
		key = canonicalPath(entry.NormalizedPath)
	}
	if key == "" {
		key = canonicalPath(entry.Path)
	}
	if key == "" {
		return ""
	}
	if entry.FamilyKey != "" {
		return "family:" + key
	}
	parts := []string{
		key,
		string(entry.Origin),
		entry.ArtifactID,
		entry.ModuleID,
		entry.VariantID,
		entry.SelectionReason,
	}
	return strings.Join(parts, "\x00")
}

func canonicalPath(v string) string {
	if v == "" {
		return ""
	}
	return filepath.Clean(v)
}

func canonicalizeProvenance(v materialization.Provenance) materialization.Provenance {
	out := materialization.Provenance{
		Producer: strings.TrimSpace(v.Producer),
		Subject:  strings.TrimSpace(v.Subject),
		Inputs:   canonicalizeReferences(v.Inputs),
		Reasons:  canonicalizeStrings(v.Reasons),
	}
	return out
}

func canonicalizeReferences(v []materialization.Reference) []materialization.Reference {
	if len(v) == 0 {
		return nil
	}
	out := append([]materialization.Reference(nil), v...)
	sort.SliceStable(out, func(i, j int) bool {
		return referenceKey(out[i]) < referenceKey(out[j])
	})
	return out
}

func canonicalizeStrings(v []string) []string {
	if len(v) == 0 {
		return nil
	}
	out := append([]string(nil), v...)
	sort.Strings(out)
	return out
}

type snapshotFingerprintPayload struct {
	Type        string                     `json:"type"`
	Scope       Scope                      `json:"scope,omitempty"`
	ModuleID    string                     `json:"moduleId,omitempty"`
	VariantID   string                     `json:"variantId,omitempty"`
	ToolchainID string                     `json:"toolchainId,omitempty"`
	Entries     []Entry                    `json:"entries,omitempty"`
	Decisions   []NormalizationDecision    `json:"decisions,omitempty"`
	Provenance  materialization.Provenance `json:"provenance,omitempty"`
}

type entryFingerprintPayload struct {
	Path            string                     `json:"path,omitempty"`
	NormalizedPath  string                     `json:"normalizedPath,omitempty"`
	Origin          Origin                     `json:"origin,omitempty"`
	ArtifactID      string                     `json:"artifactId,omitempty"`
	ModuleID        string                     `json:"moduleId,omitempty"`
	VariantID       string                     `json:"variantId,omitempty"`
	FamilyKey       string                     `json:"familyKey,omitempty"`
	SelectionReason string                     `json:"selectionReason,omitempty"`
	Provenance      materialization.Provenance `json:"provenance,omitempty"`
}

type recordEntryIdentityPayload struct {
	SnapshotID string `json:"snapshotId,omitempty"`
	Order      int    `json:"order,omitempty"`
	Digest     string `json:"digest,omitempty"`
}

type recordEntriesFingerprintPayload struct {
	Type    string        `json:"type"`
	Entries []EntryRecord `json:"entries,omitempty"`
}

func fingerprint(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("classpath fingerprint marshal: %v", err))
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:])
}

func referenceKey(v materialization.Reference) string {
	return strings.Join([]string{v.Kind, v.ID, v.Path, v.Digest}, "\x00")
}

func firstNonEmpty(v string, fallback string) string {
	if strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}
