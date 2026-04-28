package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/entireio/cli/cmd/entire/cli/agent"
	"github.com/entireio/cli/cmd/entire/cli/checkpoint"
	"github.com/entireio/cli/cmd/entire/cli/checkpoint/id"
	"github.com/entireio/cli/cmd/entire/cli/testutil"
	"github.com/entireio/cli/redact"
	"github.com/go-git/go-git/v6"
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
	if d.CheckpointsDelta != 1 {
		t.Errorf("CheckpointsDelta = %d, want 1", d.CheckpointsDelta)
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
	if d.CheckpointsDelta != 0 {
		t.Errorf("CheckpointsDelta = %d, want 0", d.CheckpointsDelta)
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
	if !strings.Contains(buf.String(), "Checkpoints:") {
		t.Errorf("expected checkpoint count label, got:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "Sessions:") {
		t.Errorf("did not expect mislabeled sessions count, got:\n%s", buf.String())
	}
}

func TestWriteCheckpointDiffJSON_UsesCheckpointCountFields(t *testing.T) {
	t.Parallel()

	a := &checkpoint.CheckpointSummary{
		CheckpointID:     id.MustCheckpointID("aaaaaaaaaaaa"),
		CheckpointsCount: 1,
	}
	b := &checkpoint.CheckpointSummary{
		CheckpointID:     id.MustCheckpointID("bbbbbbbbbbbb"),
		CheckpointsCount: 3,
	}
	d := computeCheckpointDiff(a, b)

	var buf bytes.Buffer
	if err := writeCheckpointDiffJSON(&buf, d); err != nil {
		t.Fatalf("writeCheckpointDiffJSON: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal JSON: %v", err)
	}
	if got := payload["a_checkpoints"]; got != float64(1) {
		t.Errorf("a_checkpoints = %v, want 1", got)
	}
	if got := payload["b_checkpoints"]; got != float64(3) {
		t.Errorf("b_checkpoints = %v, want 3", got)
	}
	if got := payload["checkpoints_delta"]; got != float64(2) {
		t.Errorf("checkpoints_delta = %v, want 2", got)
	}
	if _, ok := payload["a_sessions"]; ok {
		t.Errorf("did not expect stale a_sessions field in JSON: %s", buf.String())
	}
}

func TestReadCheckpointDiffSummary_UsesV2WhenPreferred(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	testutil.InitRepo(t, dir)
	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}

	ctx := context.Background()
	checkpointID := id.MustCheckpointID("cccccccccccc")
	v1Store := checkpoint.NewGitStore(repo)
	v2Store := checkpoint.NewV2GitStore(repo, "")
	if err := v2Store.WriteCommitted(ctx, checkpoint.WriteCommittedOptions{
		CheckpointID:     checkpointID,
		SessionID:        "session-v2",
		Strategy:         "manual-commit",
		Transcript:       redact.AlreadyRedacted([]byte(`{"message":"v2"}` + "\n")),
		Prompts:          []string{"use v2"},
		FilesTouched:     []string{"v2.go"},
		CheckpointsCount: 7,
		AuthorName:       "Test User",
		AuthorEmail:      "test@example.com",
	}); err != nil {
		t.Fatalf("write v2 checkpoint: %v", err)
	}

	got, err := readCheckpointDiffSummary(ctx, checkpointID, v1Store, v2Store, true)
	if err != nil {
		t.Fatalf("readCheckpointDiffSummary: %v", err)
	}
	if got.CheckpointsCount != 7 {
		t.Fatalf("CheckpointsCount = %d, want 7", got.CheckpointsCount)
	}
	if !equalStringSlices(got.FilesTouched, []string{"v2.go"}) {
		t.Fatalf("FilesTouched = %v, want [v2.go]", got.FilesTouched)
	}
}

func TestCheckpointDiffCommand_InvalidIDErrorsBeforeRepositoryLookup(t *testing.T) {
	t.Parallel()

	cmd := newCheckpointDiffCmd()
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"not-an-id", "bbbbbbbbbbbb"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected invalid ID error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid checkpoint A") {
		t.Fatalf("error = %v, want invalid checkpoint A", err)
	}
}

func TestCheckpointDiffCommand_MissingCheckpointErrors(t *testing.T) {
	// CWD-mutating; cannot run in parallel.
	dir := t.TempDir()
	testutil.InitRepo(t, dir)
	t.Chdir(dir)

	cmd := newCheckpointDiffCmd()
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"aaaaaaaaaaaa", "bbbbbbbbbbbb"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected missing checkpoint error, got nil")
	}
	if !strings.Contains(err.Error(), "read checkpoint aaaaaaaaaaaa") {
		t.Fatalf("error = %v, want read checkpoint context", err)
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
