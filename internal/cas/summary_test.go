package cas

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPutAndGetActionSummaryRoundTrip(t *testing.T) {
	s := NewFilesystemStore(t.TempDir())
	ctx := context.Background()

	hash := HashBytes([]byte("action-key"))
	summary := CacheSummary{
		ActionHash: hash,
		ProbeSequence: []ProbeStep{
			{Operation: "GetActionResult", Tier: 0, Outcome: "miss", DurationMs: 1},
			{Operation: "GetActionResult", Tier: 1, Outcome: "hit", DurationMs: 12, Promoted: true, PromotionTargets: []int{0}},
		},
		Outcome:    "hit",
		DurationMs: 13,
		Timestamp:  time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC),
	}

	if err := s.PutActionSummary(ctx, summary); err != nil {
		t.Fatalf("PutActionSummary: %v", err)
	}

	got, err := s.GetActionSummary(ctx, hash)
	if err != nil {
		t.Fatalf("GetActionSummary: %v", err)
	}
	if got.ActionHash != hash {
		t.Fatalf("ActionHash mismatch: got %s want %s", got.ActionHash, hash)
	}
	if got.Outcome != "hit" || got.DurationMs != 13 {
		t.Fatalf("scalar fields lost: %+v", got)
	}
	if len(got.ProbeSequence) != 2 {
		t.Fatalf("ProbeSequence len %d want 2", len(got.ProbeSequence))
	}
	if !got.ProbeSequence[1].Promoted || len(got.ProbeSequence[1].PromotionTargets) != 1 {
		t.Fatalf("promotion fields lost: %+v", got.ProbeSequence[1])
	}
	if !got.Timestamp.Equal(summary.Timestamp) {
		t.Fatalf("Timestamp mismatch: got %s want %s", got.Timestamp, summary.Timestamp)
	}
}

func TestPutActionSummaryRejectsZeroHash(t *testing.T) {
	s := NewFilesystemStore(t.TempDir())
	err := s.PutActionSummary(context.Background(), CacheSummary{Outcome: "hit"})
	if err == nil {
		t.Fatal("expected error for zero ActionHash, got nil")
	}
}

func TestGetActionSummaryReturnsNotFoundWhenMissing(t *testing.T) {
	s := NewFilesystemStore(t.TempDir())
	_, err := s.GetActionSummary(context.Background(), HashBytes([]byte("missing")))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestPutActionSummaryDefaultsTimestamp(t *testing.T) {
	s := NewFilesystemStore(t.TempDir())
	hash := HashBytes([]byte("ts-default"))
	if err := s.PutActionSummary(context.Background(), CacheSummary{ActionHash: hash, Outcome: "miss"}); err != nil {
		t.Fatalf("PutActionSummary: %v", err)
	}
	got, err := s.GetActionSummary(context.Background(), hash)
	if err != nil {
		t.Fatalf("GetActionSummary: %v", err)
	}
	if got.Timestamp.IsZero() {
		t.Fatalf("expected default timestamp, got zero")
	}
}
