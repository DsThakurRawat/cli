package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/entireio/cli/cmd/entire/cli/agent"
	"github.com/entireio/cli/cmd/entire/cli/checkpoint"
	"github.com/entireio/cli/cmd/entire/cli/checkpoint/id"
)

func TestComputeCheckpointDiff_FilesAddedAndRemoved(t *testing.T) {
	t.Parallel()

	a := &checkpoint.CheckpointSummary{
		CheckpointID:     id.MustCheckpointID("aaaaaaaaaaaa"),
		FilesTouched:     []string{"src/a.go", "src/b.go"},
		CheckpointsCount: 1,
	}
	b := &checkpoint.CheckpointSummary{
		CheckpointID:     id.MustCheckpointID("bbbbbbbbbbbb"),
		FilesTouched:     []string{"src/b.go", "src/c.go"},
		CheckpointsCount: 2,
	}

	d := computeCheckpointDiff(a, b)

	if got, want := d.FilesAdded, []string{"src/c.go"}; !equalStringSlices(got, want) {
		t.Errorf("FilesAdded = %v, want %v", got, want)
	}
	if got, want := d.FilesRemoved, []string{"src/a.go"}; !equalStringSlices(got, want) {
		t.Errorf("FilesRemoved = %v, want %v", got, want)
	}
	if d.SessionsDelta != 1 {
		t.Errorf("SessionsDelta = %d, want 1", d.SessionsDelta)
	}
}

func TestComputeCheckpointDiff_TokenDelta(t *testing.T) {
	t.Parallel()

	a := &checkpoint.CheckpointSummary{
		CheckpointID: id.MustCheckpointID("aaaaaaaaaaaa"),
		TokenUsage: &agent.TokenUsage{
			InputTokens:         100,
			OutputTokens:        50,
			CacheReadTokens:     200,
			CacheCreationTokens: 25,
		},
	}
	b := &checkpoint.CheckpointSummary{
		CheckpointID: id.MustCheckpointID("bbbbbbbbbbbb"),
		TokenUsage: &agent.TokenUsage{
			InputTokens:         150,
			OutputTokens:        100,
			CacheReadTokens:     300,
			CacheCreationTokens: 50,
		},
	}

	d := computeCheckpointDiff(a, b)

	if d.TokensDelta.Input != 50 {
		t.Errorf("Input delta = %d, want 50", d.TokensDelta.Input)
	}
	if d.TokensDelta.Output != 50 {
		t.Errorf("Output delta = %d, want 50", d.TokensDelta.Output)
	}
	if d.TokensDelta.CacheRead != 100 {
		t.Errorf("CacheRead delta = %d, want 100", d.TokensDelta.CacheRead)
	}
	if d.TokensDelta.CacheWrite != 25 {
		t.Errorf("CacheWrite delta = %d, want 25", d.TokensDelta.CacheWrite)
	}
	if d.TokensDelta.Total != 225 {
		t.Errorf("Total delta = %d, want 225", d.TokensDelta.Total)
	}
}

func TestComputeCheckpointDiff_IdenticalInputsHaveEmptyDiff(t *testing.T) {
	t.Parallel()

	a := &checkpoint.CheckpointSummary{
		CheckpointID:     id.MustCheckpointID("aaaaaaaaaaaa"),
		FilesTouched:     []string{"x.go", "y.go"},
		CheckpointsCount: 3,
	}
	b := &checkpoint.CheckpointSummary{
		CheckpointID:     id.MustCheckpointID("bbbbbbbbbbbb"),
		FilesTouched:     []string{"y.go", "x.go"}, // order intentionally different
		CheckpointsCount: 3,
	}

	d := computeCheckpointDiff(a, b)

	if len(d.FilesAdded) != 0 {
		t.Errorf("FilesAdded = %v, want empty", d.FilesAdded)
	}
	if len(d.FilesRemoved) != 0 {
		t.Errorf("FilesRemoved = %v, want empty", d.FilesRemoved)
	}
	if d.SessionsDelta != 0 {
		t.Errorf("SessionsDelta = %d, want 0", d.SessionsDelta)
	}
}

func TestComputeCheckpointDiff_NilTokenUsageHandled(t *testing.T) {
	t.Parallel()

	a := &checkpoint.CheckpointSummary{CheckpointID: id.MustCheckpointID("aaaaaaaaaaaa")}
	b := &checkpoint.CheckpointSummary{
		CheckpointID: id.MustCheckpointID("bbbbbbbbbbbb"),
		TokenUsage:   &agent.TokenUsage{InputTokens: 10, OutputTokens: 20},
	}

	d := computeCheckpointDiff(a, b)

	if d.TokensDelta.Input != 10 || d.TokensDelta.Output != 20 || d.TokensDelta.Total != 30 {
		t.Errorf("token delta from nil-A: %+v", d.TokensDelta)
	}
}

func TestWriteCheckpointDiffText_RendersIdenticalSet(t *testing.T) {
	t.Parallel()

	a := &checkpoint.CheckpointSummary{
		CheckpointID: id.MustCheckpointID("aaaaaaaaaaaa"),
		FilesTouched: []string{"a"},
	}
	b := &checkpoint.CheckpointSummary{
		CheckpointID: id.MustCheckpointID("bbbbbbbbbbbb"),
		FilesTouched: []string{"a"},
	}
	d := computeCheckpointDiff(a, b)

	var buf bytes.Buffer
	if err := writeCheckpointDiffText(&buf, d); err != nil {
		t.Fatalf("writeCheckpointDiffText: %v", err)
	}
	if !strings.Contains(buf.String(), "identical set") {
		t.Errorf("expected 'identical set' phrase, got:\n%s", buf.String())
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
