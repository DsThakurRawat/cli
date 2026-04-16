//go:build integration

package integration

import (
	"os/exec"
	"testing"

	"github.com/entireio/cli/cmd/entire/cli/testutil"
)

// TestShadow_ResetToKnownCheckpointReconciles verifies that after condensation,
// resetting to the condensed commit causes the reconcile path to fire:
// both BaseCommit and AttributionBaseCommit are updated to HEAD.
//
// The reconcile path in migrateShadowBranchIfNeeded triggers when:
//   - state.LastCheckpointID is set (from condensation)
//   - HEAD has an Entire-Checkpoint trailer matching LastCheckpointID
//   - state.BaseCommit != HEAD (so migration is needed)
//
// SaveStep (via SimulateStop) calls migrateAndPersistIfNeeded which checks for
// reconcile, and importantly does NOT clear LastCheckpointID (unlike InitializeSession).
//
// Flow:
//  1. Agent creates checkpoints, user commits with hooks (condensation sets LastCheckpointID)
//  2. A plain commit advances HEAD (simulates agent-driven pull/commit)
//  3. SimulateStop with file changes triggers SaveStep -> migration (BaseCommit advances,
//     LastCheckpointID preserved)
//  4. git reset --hard back to the condensed commit
//  5. SimulateStop with file changes triggers SaveStep -> reconcile (LastCheckpointID matches
//     HEAD trailer)
//  6. Assert: BaseCommit == condensed commit, AttributionBaseCommit == condensed commit
//  7. Assert: old shadow branch at the pulled-commit base still exists (rewind data preserved)
func TestShadow_ResetToKnownCheckpointReconciles(t *testing.T) {
	t.Parallel()
	env := NewTestEnv(t)
	defer env.Cleanup()

	// ========================================
	// Phase 1: Setup repo + entire + session
	// ========================================
	env.InitRepo()

	env.WriteFile("README.md", "# Test Repository")
	env.GitAdd("README.md")
	env.GitCommit("Initial commit")

	env.GitCheckoutNewBranch("feature/reset-reconcile")
	env.InitEntire()

	// ========================================
	// Phase 2: Agent works — create file, checkpoint, user commits with hooks
	// ========================================
	t.Log("Phase 2: Agent creates checkpoint and user commits with hooks")

	session := env.NewSession()
	if err := env.SimulateUserPromptSubmitWithPrompt(session.ID, "Create function A"); err != nil {
		t.Fatalf("SimulateUserPromptSubmitWithPrompt failed: %v", err)
	}

	fileAContent := "package main\n\nfunc A() {}\n"
	env.WriteFile("a.go", fileAContent)

	session.CreateTranscript(
		"Create function A",
		[]FileChange{{Path: "a.go", Content: fileAContent}},
	)
	if err := env.SimulateStop(session.ID, session.TranscriptPath); err != nil {
		t.Fatalf("SimulateStop (checkpoint 1) failed: %v", err)
	}

	// User commits with shadow hooks (triggers condensation)
	env.GitAdd("a.go")
	env.GitCommitWithShadowHooks("Add function A", "a.go")

	condensedCommitHash := env.GetHeadHash()
	t.Logf("Condensed commit hash: %s", condensedCommitHash[:7])

	// Verify condensation happened: LastCheckpointID should be set
	state, err := env.GetSessionState(session.ID)
	if err != nil {
		t.Fatalf("GetSessionState failed: %v", err)
	}
	if state == nil {
		t.Fatal("Session state should exist after condensation")
	}
	if state.LastCheckpointID.IsEmpty() {
		t.Fatal("LastCheckpointID should be set after condensation")
	}
	lastCheckpointID := state.LastCheckpointID
	t.Logf("LastCheckpointID after condensation: %s", lastCheckpointID)

	// ========================================
	// Phase 3: Advance HEAD with a plain commit (simulates agent-driven pull)
	// Session is IDLE so BaseCommit is NOT updated by post-commit hook.
	// ========================================
	t.Log("Phase 3: Advance HEAD with plain commit (simulates agent-driven pull)")

	fileBContent := "package main\n\nfunc B() {}\n"
	env.WriteFile("b.go", fileBContent)
	env.GitAdd("b.go")
	env.GitCommit("Add function B")

	pulledCommitHash := env.GetHeadHash()
	t.Logf("Pulled commit hash: %s", pulledCommitHash[:7])

	// Verify BaseCommit was NOT updated (session is IDLE)
	state, err = env.GetSessionState(session.ID)
	if err != nil {
		t.Fatalf("GetSessionState after pull failed: %v", err)
	}
	if state.BaseCommit != condensedCommitHash {
		t.Fatalf("BaseCommit should still be %s (IDLE session should not be updated by post-commit), got %s",
			condensedCommitHash[:7], state.BaseCommit[:7])
	}

	// ========================================
	// Phase 4: SimulateStop with file changes triggers migration via SaveStep.
	// SaveStep calls migrateAndPersistIfNeeded which detects HEAD changed,
	// migrates BaseCommit to pulledCommitHash, but does NOT clear LastCheckpointID.
	// ========================================
	t.Log("Phase 4: SimulateStop with file changes triggers migration (preserves LastCheckpointID)")

	// Write an uncommitted file so SimulateStop detects file changes and calls SaveStep
	fileCContent := "package main\n\nfunc C() {}\n"
	env.WriteFile("c.go", fileCContent)

	session.TranscriptBuilder = NewTranscriptBuilder()
	session.CreateTranscript(
		"Create function C after pull",
		[]FileChange{{Path: "c.go", Content: fileCContent}},
	)
	if err := env.SimulateStop(session.ID, session.TranscriptPath); err != nil {
		t.Fatalf("SimulateStop (after pull) failed: %v", err)
	}

	// Verify migration happened: BaseCommit should now be pulledCommitHash
	state, err = env.GetSessionState(session.ID)
	if err != nil {
		t.Fatalf("GetSessionState after migration failed: %v", err)
	}
	if state.BaseCommit != pulledCommitHash {
		t.Fatalf("BaseCommit should be %s (pulled HEAD) after migration, got %s",
			pulledCommitHash[:7], state.BaseCommit[:7])
	}
	t.Logf("BaseCommit after migration: %s", state.BaseCommit[:7])

	// Verify LastCheckpointID was NOT cleared (SaveStep does not clear it)
	if state.LastCheckpointID.IsEmpty() {
		t.Fatal("LastCheckpointID should still be set after SaveStep migration (only InitializeSession clears it)")
	}
	if state.LastCheckpointID != lastCheckpointID {
		t.Fatalf("LastCheckpointID should still be %s, got %s",
			lastCheckpointID, state.LastCheckpointID)
	}
	t.Logf("LastCheckpointID preserved after migration: %s", state.LastCheckpointID)

	// Record shadow branch at pulled commit (created by the checkpoint in this phase)
	shadowBranchAtPulled := env.GetShadowBranchNameForCommit(pulledCommitHash)
	if !env.BranchExists(shadowBranchAtPulled) {
		t.Fatalf("Shadow branch %s should exist after checkpoint", shadowBranchAtPulled)
	}
	t.Logf("Shadow branch at pulled commit: %s", shadowBranchAtPulled)

	// ========================================
	// Phase 5: git reset --hard back to the condensed commit
	// ========================================
	t.Log("Phase 5: git reset --hard to condensed commit")

	cmd := exec.Command("git", "reset", "--hard", condensedCommitHash)
	cmd.Dir = env.RepoDir
	cmd.Env = testutil.GitIsolatedEnv()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git reset --hard failed: %v\nOutput: %s", err, output)
	}

	currentHead := env.GetHeadHash()
	if currentHead != condensedCommitHash {
		t.Fatalf("HEAD should be %s after reset, got %s", condensedCommitHash[:7], currentHead[:7])
	}

	// ========================================
	// Phase 6: SimulateStop with file changes triggers reconcile via SaveStep.
	// SaveStep -> migrateAndPersistIfNeeded -> migrateShadowBranchIfNeeded detects:
	//   - BaseCommit(pulledCommitHash) != HEAD(condensedCommitHash)
	//   - LastCheckpointID is set
	//   - HEAD commit has Entire-Checkpoint trailer matching LastCheckpointID
	//   -> RECONCILE: sets both BaseCommit and AttributionBaseCommit to HEAD
	// ========================================
	t.Log("Phase 6: SimulateStop triggers reconcile")

	// Write an uncommitted file so SimulateStop detects changes and calls SaveStep
	fileDContent := "package main\n\nfunc D() {}\n"
	env.WriteFile("d.go", fileDContent)

	session.TranscriptBuilder = NewTranscriptBuilder()
	session.CreateTranscript(
		"Create function D after reset",
		[]FileChange{{Path: "d.go", Content: fileDContent}},
	)
	if err := env.SimulateStop(session.ID, session.TranscriptPath); err != nil {
		t.Fatalf("SimulateStop (after reset) failed: %v", err)
	}

	// ========================================
	// Phase 7: Verify reconcile results
	// ========================================
	t.Log("Phase 7: Verifying reconcile results")

	state, err = env.GetSessionState(session.ID)
	if err != nil {
		t.Fatalf("GetSessionState after reconcile failed: %v", err)
	}
	if state == nil {
		t.Fatal("Session state should exist after reconcile")
	}

	// BaseCommit should be updated to the condensed commit (reconcile path)
	if state.BaseCommit != condensedCommitHash {
		t.Errorf("BaseCommit should be %s (condensed commit) after reconcile, got %s",
			condensedCommitHash[:7], state.BaseCommit[:7])
	} else {
		t.Logf("BaseCommit correctly reconciled to: %s", state.BaseCommit[:7])
	}

	// AttributionBaseCommit should also be updated (reconcile resets both)
	if state.AttributionBaseCommit != condensedCommitHash {
		t.Errorf("AttributionBaseCommit should be %s (condensed commit) after reconcile, got %s",
			condensedCommitHash[:7], state.AttributionBaseCommit[:7])
	} else {
		t.Logf("AttributionBaseCommit correctly reconciled to: %s", state.AttributionBaseCommit[:7])
	}

	// Old shadow branch at the pulled commit should still exist because the reconcile
	// path intentionally does NOT rename or delete old shadow branches (preserves rewind data)
	if env.BranchExists(shadowBranchAtPulled) {
		t.Logf("Old shadow branch %s still exists (rewind data preserved)", shadowBranchAtPulled)
	} else {
		t.Errorf("Old shadow branch %s should still exist after reconcile (rewind data preservation)", shadowBranchAtPulled)
	}

	t.Log("Reset-to-known-checkpoint reconcile test completed successfully!")
}

// TestShadow_ResetToUnrelatedCommitMigratesOnly verifies that after condensation,
// resetting to a commit that has NO Entire-Checkpoint trailer causes only the
// migrate path (not reconcile): BaseCommit updates but AttributionBaseCommit
// stays pinned at its previous value.
//
// Flow:
//  1. Agent creates checkpoints, user commits with hooks (condensation)
//  2. Plain commit advances HEAD (simulates pull)
//  3. SimulateStop with file changes triggers migration (preserves LastCheckpointID)
//  4. git reset --hard to the initial commit (before any agent activity, no trailer)
//  5. SimulateStop with file changes triggers migration only (no reconcile — initial
//     commit has no Entire-Checkpoint trailer)
//  6. BaseCommit == reset target, AttributionBaseCommit != BaseCommit (diverged)
func TestShadow_ResetToUnrelatedCommitMigratesOnly(t *testing.T) {
	t.Parallel()
	env := NewTestEnv(t)
	defer env.Cleanup()

	// ========================================
	// Phase 1: Setup repo + entire + session
	// ========================================
	env.InitRepo()

	env.WriteFile("README.md", "# Test Repository")
	env.GitAdd("README.md")
	env.GitCommit("Initial commit")

	initialCommitHash := env.GetHeadHash()
	t.Logf("Initial commit (no trailer): %s", initialCommitHash[:7])

	env.GitCheckoutNewBranch("feature/reset-migrate")
	env.InitEntire()

	// ========================================
	// Phase 2: Agent works + commits with hooks (condensation)
	// ========================================
	t.Log("Phase 2: Agent creates checkpoint and user commits with hooks")

	session := env.NewSession()
	if err := env.SimulateUserPromptSubmitWithPrompt(session.ID, "Create function X"); err != nil {
		t.Fatalf("SimulateUserPromptSubmitWithPrompt failed: %v", err)
	}

	fileXContent := "package main\n\nfunc X() {}\n"
	env.WriteFile("x.go", fileXContent)

	session.CreateTranscript(
		"Create function X",
		[]FileChange{{Path: "x.go", Content: fileXContent}},
	)
	if err := env.SimulateStop(session.ID, session.TranscriptPath); err != nil {
		t.Fatalf("SimulateStop (checkpoint 1) failed: %v", err)
	}

	// User commits with shadow hooks (triggers condensation)
	env.GitAdd("x.go")
	env.GitCommitWithShadowHooks("Add function X", "x.go")

	condensedCommitHash := env.GetHeadHash()
	t.Logf("Condensed commit: %s", condensedCommitHash[:7])

	// Verify condensation happened
	state, err := env.GetSessionState(session.ID)
	if err != nil {
		t.Fatalf("GetSessionState failed: %v", err)
	}
	if state == nil {
		t.Fatal("Session state should exist after condensation")
	}
	if state.LastCheckpointID.IsEmpty() {
		t.Fatal("LastCheckpointID should be set after condensation")
	}

	// ========================================
	// Phase 3: Create another commit to advance HEAD (simulates pull)
	// Session is IDLE so BaseCommit stays at condensedCommitHash.
	// ========================================
	t.Log("Phase 3: Advance HEAD with plain commit")

	fileYContent := "package main\n\nfunc Y() {}\n"
	env.WriteFile("y.go", fileYContent)
	env.GitAdd("y.go")
	env.GitCommit("Add function Y")

	pulledCommitHash := env.GetHeadHash()
	t.Logf("Pulled commit: %s", pulledCommitHash[:7])

	// ========================================
	// Phase 4: SimulateStop with file changes triggers migration.
	// BaseCommit advances from condensedCommitHash to pulledCommitHash.
	// LastCheckpointID preserved (SaveStep does not clear it).
	// AttributionBaseCommit stays pinned (migration path does NOT update it).
	// ========================================
	t.Log("Phase 4: SimulateStop with file changes triggers migration")

	// Write uncommitted file so SimulateStop detects changes
	fileZContent := "package main\n\nfunc Z() {}\n"
	env.WriteFile("z.go", fileZContent)

	session.TranscriptBuilder = NewTranscriptBuilder()
	session.CreateTranscript(
		"Create function Z after pull",
		[]FileChange{{Path: "z.go", Content: fileZContent}},
	)
	if err := env.SimulateStop(session.ID, session.TranscriptPath); err != nil {
		t.Fatalf("SimulateStop (after pull) failed: %v", err)
	}

	state, err = env.GetSessionState(session.ID)
	if err != nil {
		t.Fatalf("GetSessionState after migration failed: %v", err)
	}
	if state.BaseCommit != pulledCommitHash {
		t.Fatalf("BaseCommit should be %s after migration, got %s",
			pulledCommitHash[:7], state.BaseCommit[:7])
	}
	t.Logf("BaseCommit after migration: %s", state.BaseCommit[:7])

	// Record AttributionBaseCommit for later comparison
	attributionAfterMigration := state.AttributionBaseCommit
	t.Logf("AttributionBaseCommit after migration: %s", attributionAfterMigration[:7])

	// ========================================
	// Phase 5: git reset --hard to initial commit (no Entire-Checkpoint trailer)
	// ========================================
	t.Log("Phase 5: git reset --hard to initial commit (no Entire trailer)")

	cmd := exec.Command("git", "reset", "--hard", initialCommitHash)
	cmd.Dir = env.RepoDir
	cmd.Env = testutil.GitIsolatedEnv()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git reset --hard failed: %v\nOutput: %s", err, output)
	}

	currentHead := env.GetHeadHash()
	if currentHead != initialCommitHash {
		t.Fatalf("HEAD should be %s after reset, got %s", initialCommitHash[:7], currentHead[:7])
	}

	// ========================================
	// Phase 6: SimulateStop with file changes triggers migration only (no reconcile).
	// HEAD's initial commit has no Entire-Checkpoint trailer, so the reconcile
	// check fails and the normal migrate path fires instead.
	// ========================================
	t.Log("Phase 6: SimulateStop triggers migration only (no reconcile)")

	// Write uncommitted file so SimulateStop detects changes
	fileWContent := "package main\n\nfunc W() {}\n"
	env.WriteFile("w.go", fileWContent)

	session.TranscriptBuilder = NewTranscriptBuilder()
	session.CreateTranscript(
		"Create function W after reset",
		[]FileChange{{Path: "w.go", Content: fileWContent}},
	)
	if err := env.SimulateStop(session.ID, session.TranscriptPath); err != nil {
		t.Fatalf("SimulateStop (after reset to unrelated) failed: %v", err)
	}

	// ========================================
	// Phase 7: Verify migrate-only results
	// ========================================
	t.Log("Phase 7: Verifying migrate-only results")

	state, err = env.GetSessionState(session.ID)
	if err != nil {
		t.Fatalf("GetSessionState after reset-migration failed: %v", err)
	}
	if state == nil {
		t.Fatal("Session state should exist after migration")
	}

	// BaseCommit should be updated to the reset target
	if state.BaseCommit != initialCommitHash {
		t.Errorf("BaseCommit should be %s (reset target) after migration, got %s",
			initialCommitHash[:7], state.BaseCommit[:7])
	} else {
		t.Logf("BaseCommit correctly migrated to: %s", state.BaseCommit[:7])
	}

	// AttributionBaseCommit should NOT be updated to BaseCommit (diverged).
	// The migrate path does NOT update AttributionBaseCommit, so it stays pinned.
	if state.AttributionBaseCommit == state.BaseCommit {
		t.Errorf("AttributionBaseCommit should NOT equal BaseCommit after migrate-only path; "+
			"both are %s", state.BaseCommit[:7])
	} else {
		t.Logf("AttributionBaseCommit (%s) diverged from BaseCommit (%s) as expected",
			state.AttributionBaseCommit[:7], state.BaseCommit[:7])
	}

	t.Log("Reset-to-unrelated-commit migrate-only test completed successfully!")
}
