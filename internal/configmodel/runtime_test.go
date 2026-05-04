package configmodel

import (
	"testing"

	"github.com/kaeawc/grit/internal/responsepayload"
)

func TestRecordRuntimeObservationsPersistsRemoteBytesAndProbe(t *testing.T) {
	root := t.TempDir()
	store := NewStore(nil)

	err := store.RecordRuntimeObservations(root, "model-key", []RuntimeActionObservation{{
		ActionID:        "action:compile",
		CacheProbe:      &responsepayload.CacheProbe{ActionID: "action:compile", State: "reused", Basis: "shared-cache-hit"},
		RemoteBytesRead: 2048,
	}})
	if err != nil {
		t.Fatalf("RecordRuntimeObservations: %v", err)
	}

	state, err := loadRuntimeState(root, "model-key")
	if err != nil {
		t.Fatalf("loadRuntimeState: %v", err)
	}
	if got := state.ActionRemoteBytes["action:compile"]; got != 2048 {
		t.Fatalf("expected recorded remote bytes, got %d", got)
	}
	if probe, ok := state.ActionCacheProbes["action:compile"]; !ok || probe.State != "reused" {
		t.Fatalf("expected recorded cache probe, got %#v ok=%v", probe, ok)
	}
}

func TestRecordRuntimeObservationsDoesNotClearObservedBytesOnZeroRead(t *testing.T) {
	root := t.TempDir()
	store := NewStore(nil)

	err := store.RecordRuntimeObservations(root, "model-key", []RuntimeActionObservation{{
		ActionID:        "action:compile",
		RemoteBytesRead: 4096,
	}})
	if err != nil {
		t.Fatalf("initial RecordRuntimeObservations: %v", err)
	}
	err = store.RecordRuntimeObservations(root, "model-key", []RuntimeActionObservation{{
		ActionID:        "action:compile",
		RemoteBytesRead: 0,
	}})
	if err != nil {
		t.Fatalf("zero-read RecordRuntimeObservations: %v", err)
	}

	state, err := loadRuntimeState(root, "model-key")
	if err != nil {
		t.Fatalf("loadRuntimeState: %v", err)
	}
	if got := state.ActionRemoteBytes["action:compile"]; got != 4096 {
		t.Fatalf("expected zero-byte observation to preserve prior bytes, got %d", got)
	}
}
