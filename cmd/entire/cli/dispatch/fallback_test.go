package dispatch

import (
	"testing"
	"time"
)

func TestApplyFallbackChain_CloudComplete(t *testing.T) {
	t.Parallel()

	candidates := []candidate{{
		CheckpointID: "cp1",
		RepoFullName: "entireio/cli",
		Branch:       "main",
		CreatedAt:    time.Unix(1, 0).UTC(),
	}}
	analyses := map[string]AnalysisStatus{
		"cp1": {Status: "complete", Summary: "summary", Labels: []string{"CI"}},
	}

	got := applyFallbackChain(candidates, analyses)
	if len(got.Used) != 1 {
		t.Fatalf("expected one bullet, got %d", len(got.Used))
	}
	if got.Used[0].Bullet.Source != "cloud_analysis" {
		t.Fatalf("unexpected source: %+v", got.Used[0].Bullet)
	}
	if got.Used[0].Bullet.Text != "summary" {
		t.Fatalf("unexpected text: %+v", got.Used[0].Bullet)
	}
}

func TestApplyFallbackChain_PendingSkips(t *testing.T) {
	t.Parallel()

	got := applyFallbackChain([]candidate{{CheckpointID: "cp1"}}, map[string]AnalysisStatus{
		"cp1": {Status: "pending"},
	})
	if len(got.Used) != 0 {
		t.Fatalf("expected no bullets, got %d", len(got.Used))
	}
	if got.Warnings.PendingCount != 1 {
		t.Fatalf("unexpected warnings: %+v", got.Warnings)
	}
}

func TestApplyFallbackChain_UnknownFallsBackToLocalSummary(t *testing.T) {
	t.Parallel()

	got := applyFallbackChain([]candidate{{
		CheckpointID:      "cp1",
		LocalSummaryTitle: "local summary",
		RepoFullName:      "entireio/cli",
		Branch:            "main",
		CreatedAt:         time.Unix(1, 0).UTC(),
	}}, map[string]AnalysisStatus{
		"cp1": {Status: "unknown"},
	})
	if len(got.Used) != 1 || got.Used[0].Bullet.Source != "local_summary" {
		t.Fatalf("unexpected used bullets: %+v", got.Used)
	}
	if got.Warnings.UnknownCount != 1 {
		t.Fatalf("unexpected warnings: %+v", got.Warnings)
	}
}

func TestApplyFallbackChain_FailedFallsBackToCommitMessage(t *testing.T) {
	t.Parallel()

	got := applyFallbackChain([]candidate{{
		CheckpointID:  "cp1",
		CommitSubject: "ship the thing",
		RepoFullName:  "entireio/cli",
		Branch:        "main",
		CreatedAt:     time.Unix(1, 0).UTC(),
	}}, map[string]AnalysisStatus{
		"cp1": {Status: "failed"},
	})
	if len(got.Used) != 1 || got.Used[0].Bullet.Source != "commit_message" {
		t.Fatalf("unexpected used bullets: %+v", got.Used)
	}
	if got.Warnings.FailedCount != 1 {
		t.Fatalf("unexpected warnings: %+v", got.Warnings)
	}
}

func TestApplyFallbackChain_NotVisibleCountsAccessDenied(t *testing.T) {
	t.Parallel()

	got := applyFallbackChain([]candidate{{
		CheckpointID:  "cp1",
		CommitSubject: "ship the thing",
		RepoFullName:  "entireio/cli",
		Branch:        "main",
		CreatedAt:     time.Unix(1, 0).UTC(),
	}}, map[string]AnalysisStatus{
		"cp1": {Status: "not_visible"},
	})
	if got.Warnings.AccessDeniedCount != 1 {
		t.Fatalf("unexpected warnings: %+v", got.Warnings)
	}
}

func TestApplyFallbackChain_UncategorizedWhenNoFallback(t *testing.T) {
	t.Parallel()

	got := applyFallbackChain([]candidate{{CheckpointID: "cp1"}}, map[string]AnalysisStatus{
		"cp1": {Status: "unknown"},
	})
	if len(got.Used) != 0 {
		t.Fatalf("expected no bullets, got %d", len(got.Used))
	}
	if got.Warnings.UnknownCount != 1 || got.Warnings.UncategorizedCount != 1 {
		t.Fatalf("unexpected warnings: %+v", got.Warnings)
	}
}
