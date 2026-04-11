package m2local

import "testing"

func TestDeriveResolutionLockfileNormalizesDeterministically(t *testing.T) {
	t.Parallel()

	lockfile := deriveResolutionLockfile(
		ResolutionReport{
			Selections: []ResolutionSelection{
				{
					Kind:         "variant_selection",
					Coordinate:   "b:m:1.0.0",
					Chosen:       "releaseRuntimeElements",
					Reason:       "best_scored_variant",
					Alternates:   []string{"debugRuntimeElements", "apiElements"},
					Capabilities: []string{"g:cap-b:1.0.0", "g:cap-a:1.0.0"},
				},
				{
					Kind:       "realization_binding",
					Coordinate: "b:m:1.0.0",
					Chosen:     "releaseRuntimeElements",
					Reason:     "resolved_artifact",
					Binding:    "jar",
				},
				{
					Kind:       "module_redirect",
					Coordinate: "a:m:1.0.0",
					Chosen:     "a:m-jvm:1.0.0",
					Reason:     "prefer_jvm_sibling",
				},
			},
			Conflicts: []ResolutionConflict{{
				Kind:        "version_conflict",
				Module:      "g:m",
				Selected:    "1.0.0",
				Discarded:   "2.0.0",
				Coordinates: []string{"g:m:2.0.0", "g:m:1.0.0"},
			}},
		},
		ResolutionReplay{
			Pins: []ResolutionPin{
				{
					Coordinate:    "b:m:1.0.0",
					Variant:       "releaseRuntimeElements",
					Binding:       "jar",
					Capabilities:  []string{"g:cap-b:1.0.0", "g:cap-a:1.0.0"},
					RepositoryURL: "https://repo1.maven.org/maven2/",
				},
				{
					Coordinate: "a:m-jvm:1.0.0",
				},
			},
		},
	)

	if lockfile.SchemaVersion != 1 || lockfile.Format != "m2local-lockfile" {
		t.Fatalf("unexpected lockfile metadata: %#v", lockfile)
	}
	if len(lockfile.Pins) != 2 || lockfile.Pins[0].Coordinate != "a:m-jvm:1.0.0" || lockfile.Pins[1].Coordinate != "b:m:1.0.0" {
		t.Fatalf("expected sorted lockfile pins, got %#v", lockfile.Pins)
	}
	if lockfile.Pins[1].Binding != "jar" {
		t.Fatalf("expected persisted pin binding, got %#v", lockfile.Pins[1])
	}
	if lockfile.Pins[1].RepositoryURL != "https://repo1.maven.org/maven2/" {
		t.Fatalf("expected persisted pin repository url, got %#v", lockfile.Pins[1])
	}
	if got := lockfile.Pins[1].Capabilities; len(got) != 2 || got[0] != "g:cap-a:1.0.0" || got[1] != "g:cap-b:1.0.0" {
		t.Fatalf("expected sorted pin capabilities, got %#v", got)
	}
	if len(lockfile.Selections) != 3 || lockfile.Selections[0].Kind != "module_redirect" || lockfile.Selections[1].Kind != "realization_binding" || lockfile.Selections[2].Kind != "variant_selection" {
		t.Fatalf("expected sorted lockfile selections, got %#v", lockfile.Selections)
	}
	if lockfile.Selections[1].Binding != "jar" {
		t.Fatalf("expected persisted selection binding, got %#v", lockfile.Selections[1])
	}
	if got := lockfile.Selections[2].Alternates; len(got) != 2 || got[0] != "apiElements" || got[1] != "debugRuntimeElements" {
		t.Fatalf("expected sorted alternates, got %#v", got)
	}
	if got := lockfile.Selections[2].Capabilities; len(got) != 2 || got[0] != "g:cap-a:1.0.0" || got[1] != "g:cap-b:1.0.0" {
		t.Fatalf("expected sorted selection capabilities, got %#v", got)
	}
	if len(lockfile.Conflicts) != 1 || lockfile.Conflicts[0].Coordinates[0] != "g:m:1.0.0" || lockfile.Conflicts[0].Coordinates[1] != "g:m:2.0.0" {
		t.Fatalf("expected sorted conflict coordinates, got %#v", lockfile.Conflicts)
	}
}
