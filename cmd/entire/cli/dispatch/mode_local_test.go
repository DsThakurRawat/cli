package dispatch

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entireio/cli/cmd/entire/cli/agent"
	"github.com/entireio/cli/cmd/entire/cli/checkpoint"
	checkpointid "github.com/entireio/cli/cmd/entire/cli/checkpoint/id"
	"github.com/entireio/cli/cmd/entire/cli/testutil"
	"github.com/entireio/cli/cmd/entire/cli/trailers"
	"github.com/entireio/cli/redact"
	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing/object"
)

func TestLocalMode_EnumeratesCheckpoints(t *testing.T) {
	dir := t.TempDir()
	stubGeneratedLocalDispatch(t)
	testutil.InitRepo(t, dir)
	testutil.WriteFile(t, dir, "a.txt", "x")
	testutil.GitAdd(t, dir, "a.txt")
	testutil.GitCommit(t, dir, "initial")
	addOriginRemote(t, dir)

	createdAt := time.Now().UTC()
	seedCommittedCheckpoint(t, dir, seededCheckpoint{
		id:           testCheckpointID,
		branch:       "main",
		createdAt:    createdAt,
		filesTouched: []string{"a.txt"},
		outcome:      testLocalFallbackText,
	})

	oldNow := nowUTC
	nowUTC = func() time.Time { return createdAt.Add(2 * time.Hour) }
	t.Cleanup(func() {
		nowUTC = oldNow
	})

	t.Chdir(dir)

	got, err := Run(context.Background(), Options{
		Mode:     ModeLocal,
		Since:    "7d",
		Branches: []string{"main"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Repos) != 1 {
		t.Fatalf("expected 1 repo group, got %d", len(got.Repos))
	}
	if got.Repos[0].FullName != testRepoFullName {
		t.Fatalf("unexpected repo group: %+v", got.Repos[0])
	}
	if got.Repos[0].Sections[0].Bullets[0].Text != testLocalFallbackText {
		t.Fatalf("unexpected bullet: %+v", got.Repos[0].Sections[0].Bullets[0])
	}
	if len(got.CoveredRepos) != 1 || got.CoveredRepos[0] != testRepoFullName {
		t.Fatalf("unexpected covered repos: %v", got.CoveredRepos)
	}
}

func TestLocalMode_UsesUntilWindow(t *testing.T) {
	dir := t.TempDir()
	stubGeneratedLocalDispatch(t)
	testutil.InitRepo(t, dir)
	testutil.WriteFile(t, dir, "a.txt", "x")
	testutil.GitAdd(t, dir, "a.txt")
	testutil.GitCommit(t, dir, "initial")
	addOriginRemote(t, dir)

	now := time.Now().UTC()
	seedCommittedCheckpoint(t, dir, seededCheckpoint{
		id:           testCheckpointID,
		branch:       "main",
		createdAt:    now,
		filesTouched: []string{"a.txt"},
		outcome:      testLocalFallbackText,
	})

	oldNow := nowUTC
	nowUTC = func() time.Time { return now }
	t.Cleanup(func() {
		nowUTC = oldNow
	})

	t.Chdir(dir)

	got, err := Run(context.Background(), Options{
		Mode:     ModeLocal,
		Since:    "7d",
		Until:    now.Add(-time.Hour).Format(time.RFC3339),
		Branches: []string{"main"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Repos) != 0 {
		t.Fatalf("expected no repo groups, got %d", len(got.Repos))
	}
}

func TestLocalMode_RejectsOrgScope(t *testing.T) {
	_, err := Run(context.Background(), Options{
		Mode: ModeLocal,
		Orgs: []string{"entireio"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "--org cannot be used with --local" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLocalMode_FallsBackToCommitSubjectWhenSummaryMissing(t *testing.T) {
	dir := t.TempDir()
	stubGeneratedLocalDispatch(t)
	testutil.InitRepo(t, dir)
	testutil.WriteFile(t, dir, "a.txt", "x")
	testutil.GitAdd(t, dir, "a.txt")
	testutil.GitCommit(t, dir, "initial")
	addOriginRemote(t, dir)

	now := time.Now().UTC()
	cpID := testCheckpointID
	testutil.WriteFile(t, dir, "plans.md", "ship it")
	testutil.GitAdd(t, dir, "plans.md")
	commitWithMessage(t, dir, trailers.FormatCheckpoint("ship the thing", mustCheckpointID(t, cpID)))
	seedCommittedCheckpoint(t, dir, seededCheckpoint{
		id:           cpID,
		branch:       "main",
		createdAt:    now,
		filesTouched: []string{"plans.md"},
		outcome:      "",
	})

	oldNow := nowUTC
	nowUTC = func() time.Time { return now }
	t.Cleanup(func() {
		nowUTC = oldNow
	})

	t.Chdir(dir)

	got, err := Run(context.Background(), Options{
		Mode:     ModeLocal,
		Since:    "7d",
		Branches: []string{"main"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Repos) != 1 {
		t.Fatalf("expected one repo group, got %+v", got.Repos)
	}
	if got.Repos[0].Sections[0].Bullets[0].Text != "ship the thing" {
		t.Fatalf("unexpected bullet: %+v", got.Repos[0].Sections[0].Bullets[0])
	}
}

func TestLocalMode_GenerateProducesInlineText(t *testing.T) {
	dir := t.TempDir()
	testutil.InitRepo(t, dir)
	testutil.WriteFile(t, dir, "a.txt", "x")
	testutil.GitAdd(t, dir, "a.txt")
	testutil.GitCommit(t, dir, "initial")
	addOriginRemote(t, dir)

	createdAt := time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC)
	seedCommittedCheckpoint(t, dir, seededCheckpoint{
		id:           testCheckpointID,
		branch:       "main",
		createdAt:    createdAt,
		filesTouched: []string{"a.txt"},
		outcome:      testLocalFallbackText,
	})

	oldNow := nowUTC
	oldFactory := dispatchTextGeneratorFactory
	nowUTC = func() time.Time { return createdAt.Add(2 * time.Hour) }
	mock := &stubTextGenerator{text: "generated inline dispatch"}
	dispatchTextGeneratorFactory = func() (dispatchTextGenerator, error) {
		return mock, nil
	}
	t.Cleanup(func() {
		nowUTC = oldNow
		dispatchTextGeneratorFactory = oldFactory
	})

	t.Chdir(dir)

	got, err := Run(context.Background(), Options{
		Mode:     ModeLocal,
		Since:    "7d",
		Branches: []string{"main"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.GeneratedText != "generated inline dispatch" {
		t.Fatalf("expected generated text, got %q", got.GeneratedText)
	}
}

func TestLocalMode_FailsWhenGeneratedMarkdownIsEmpty(t *testing.T) {
	dir := t.TempDir()
	testutil.InitRepo(t, dir)
	testutil.WriteFile(t, dir, "a.txt", "x")
	testutil.GitAdd(t, dir, "a.txt")
	testutil.GitCommit(t, dir, "initial")
	addOriginRemote(t, dir)

	createdAt := time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC)
	seedCommittedCheckpoint(t, dir, seededCheckpoint{
		id:           testCheckpointID,
		branch:       "main",
		createdAt:    createdAt,
		filesTouched: []string{"a.txt"},
		outcome:      testLocalFallbackText,
	})

	oldNow := nowUTC
	oldFactory := dispatchTextGeneratorFactory
	nowUTC = func() time.Time { return createdAt.Add(2 * time.Hour) }
	dispatchTextGeneratorFactory = func() (dispatchTextGenerator, error) {
		return &stubTextGenerator{text: "  \n\t "}, nil
	}
	t.Cleanup(func() {
		nowUTC = oldNow
		dispatchTextGeneratorFactory = oldFactory
	})

	t.Chdir(dir)

	_, err := Run(context.Background(), Options{
		Mode:     ModeLocal,
		Since:    "7d",
		Branches: []string{"main"},
	})
	if err == nil {
		t.Fatal("expected error when local generation returns empty markdown")
	}
	if err.Error() != "dispatch generation returned no markdown" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLocalMode_ImplicitCurrentBranchUsesHEADReachability(t *testing.T) {
	dir := t.TempDir()
	stubGeneratedLocalDispatch(t)
	testutil.InitRepo(t, dir)
	testutil.WriteFile(t, dir, "a.txt", "x")
	testutil.GitAdd(t, dir, "a.txt")
	testutil.GitCommit(t, dir, "initial")
	addOriginRemote(t, dir)

	cpID := testCheckpointID
	testutil.GitCheckoutNewBranch(t, dir, "entire-dispatch")
	testutil.WriteFile(t, dir, "plans.md", "dispatch plan")
	testutil.GitAdd(t, dir, "plans.md")
	commitWithMessage(t, dir, trailers.FormatCheckpoint("plan commit", mustCheckpointID(t, cpID)))

	repo, err := git.PlainOpenWithOptions(dir, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		t.Fatal(err)
	}
	store := checkpoint.NewGitStore(repo)
	parsedID, err := checkpointid.NewCheckpointID(cpID)
	if err != nil {
		t.Fatal(err)
	}
	err = store.WriteCommitted(context.Background(), checkpoint.WriteCommittedOptions{
		CheckpointID:     parsedID,
		SessionID:        "session-1",
		Strategy:         "manual-commit",
		Branch:           "entire-dispatch",
		Transcript:       redact.AlreadyRedacted([]byte("{\"type\":\"user\"}\n")),
		Prompts:          []string{"summarize recent work"},
		FilesTouched:     []string{"plans.md"},
		CheckpointsCount: 1,
		Agent:            agent.AgentTypeClaudeCode,
		Summary: &checkpoint.Summary{
			Outcome: testLocalFallbackText,
		},
		AuthorName:  "Test User",
		AuthorEmail: "test@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}

	testutil.GitCheckoutNewBranch(t, dir, "entire-dispatch-codex")

	oldNow := nowUTC
	nowUTC = func() time.Time { return time.Now().UTC() }
	t.Cleanup(func() {
		nowUTC = oldNow
	})

	t.Chdir(dir)

	got, err := Run(context.Background(), Options{
		Mode:                  ModeLocal,
		Since:                 "7d",
		Branches:              []string{"entire-dispatch-codex"},
		ImplicitCurrentBranch: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Repos) != 1 || got.Repos[0].Sections[0].Bullets[0].Text != testLocalFallbackText {
		t.Fatalf("unexpected dispatch payload: %+v", got)
	}
}

func TestLocalMode_ExplicitBranchesRemainExact(t *testing.T) {
	dir := t.TempDir()
	stubGeneratedLocalDispatch(t)
	testutil.InitRepo(t, dir)
	testutil.WriteFile(t, dir, "a.txt", "x")
	testutil.GitAdd(t, dir, "a.txt")
	testutil.GitCommit(t, dir, "initial")
	addOriginRemote(t, dir)

	cpID := testCheckpointID
	testutil.GitCheckoutNewBranch(t, dir, "entire-dispatch")
	testutil.WriteFile(t, dir, "plans.md", "dispatch plan")
	testutil.GitAdd(t, dir, "plans.md")
	commitWithMessage(t, dir, trailers.FormatCheckpoint("plan commit", mustCheckpointID(t, cpID)))
	repo, err := git.PlainOpenWithOptions(dir, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		t.Fatal(err)
	}
	store := checkpoint.NewGitStore(repo)
	parsedID, err := checkpointid.NewCheckpointID(cpID)
	if err != nil {
		t.Fatal(err)
	}
	err = store.WriteCommitted(context.Background(), checkpoint.WriteCommittedOptions{
		CheckpointID:     parsedID,
		SessionID:        "session-1",
		Strategy:         "manual-commit",
		Branch:           "entire-dispatch",
		Transcript:       redact.AlreadyRedacted([]byte("{\"type\":\"user\"}\n")),
		Prompts:          []string{"summarize recent work"},
		FilesTouched:     []string{"plans.md"},
		CheckpointsCount: 1,
		Agent:            agent.AgentTypeClaudeCode,
		Summary: &checkpoint.Summary{
			Outcome: testLocalFallbackText,
		},
		AuthorName:  "Test User",
		AuthorEmail: "test@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}

	testutil.GitCheckoutNewBranch(t, dir, "entire-dispatch-codex")

	oldNow := nowUTC
	nowUTC = func() time.Time { return time.Now().UTC() }
	t.Cleanup(func() {
		nowUTC = oldNow
	})

	t.Chdir(dir)

	got, err := Run(context.Background(), Options{
		Mode:     ModeLocal,
		Since:    "7d",
		Branches: []string{"entire-dispatch-codex"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Repos) != 0 {
		t.Fatalf("expected 0 repo groups with explicit branch filter, got %d", len(got.Repos))
	}
}

func TestLocalMode_ImplicitCurrentBranchUsesCheckpointBranchWithoutTrailerReachability(t *testing.T) {
	dir := t.TempDir()
	stubGeneratedLocalDispatch(t)
	testutil.InitRepo(t, dir)
	testutil.WriteFile(t, dir, "a.txt", "x")
	testutil.GitAdd(t, dir, "a.txt")
	testutil.GitCommit(t, dir, "initial")
	addOriginRemote(t, dir)

	testutil.GitCheckoutNewBranch(t, dir, "entire-dispatch-codex")

	createdAt := time.Now().UTC()
	seedCommittedCheckpoint(t, dir, seededCheckpoint{
		id:           testCheckpointID,
		branch:       "entire-dispatch-codex",
		createdAt:    createdAt,
		filesTouched: []string{"a.txt"},
		outcome:      testLocalFallbackText,
	})

	oldNow := nowUTC
	nowUTC = func() time.Time { return createdAt.Add(2 * time.Hour) }
	t.Cleanup(func() {
		nowUTC = oldNow
	})

	t.Chdir(dir)

	got, err := Run(context.Background(), Options{
		Mode:                  ModeLocal,
		Since:                 "7d",
		Branches:              []string{"entire-dispatch-codex"},
		ImplicitCurrentBranch: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Repos) != 1 || got.Repos[0].Sections[0].Bullets[0].Text != testLocalFallbackText {
		t.Fatalf("unexpected dispatch payload: %+v", got)
	}
}

func TestReachableCheckpointIDsOnHEAD_LimitsLogToWindowAndCheckpointTrailers(t *testing.T) {
	tmpDir := t.TempDir()
	argsFile := filepath.Join(tmpDir, "git-args.txt")
	gitPath := filepath.Join(tmpDir, "git")

	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > \"$TEST_GIT_ARGS_FILE\"\n" +
		"printf 'subject\\n\\nEntire-Checkpoint: " + testCheckpointID + "\\000'\n"
	if err := os.WriteFile(gitPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", tmpDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TEST_GIT_ARGS_FILE", argsFile)

	since := time.Date(2026, 4, 1, 12, 30, 0, 0, time.UTC)
	reachable, err := reachableCheckpointIDsOnHEAD(context.Background(), "/tmp/repo", since)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reachable[testCheckpointID]; !ok {
		t.Fatalf("expected checkpoint %s to be reachable, got %v", testCheckpointID, reachable)
	}

	argsBytes, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	args := string(argsBytes)
	if !strings.Contains(args, "--grep") || !strings.Contains(args, "Entire-Checkpoint:") {
		t.Fatalf("expected git log to filter checkpoint trailers, got args %q", args)
	}
	if !strings.Contains(args, "--since=2026-04-01T12:30:00Z") {
		t.Fatalf("expected git log to bound history by since window, got args %q", args)
	}
}

func mustCheckpointID(t *testing.T, value string) checkpointid.CheckpointID {
	t.Helper()

	cpID, err := checkpointid.NewCheckpointID(value)
	if err != nil {
		t.Fatal(err)
	}
	return cpID
}

func commitWithMessage(t *testing.T, repoDir, message string) {
	t.Helper()

	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		t.Fatal(err)
	}

	worktree, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}

	_, err = worktree.Commit(message, &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Test User",
			Email: "test@example.com",
			When:  time.Now(),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

type seededCheckpoint struct {
	id           string
	branch       string
	createdAt    time.Time
	filesTouched []string
	outcome      string
}

func stubGeneratedLocalDispatch(t *testing.T) {
	t.Helper()

	oldFactory := dispatchTextGeneratorFactory
	dispatchTextGeneratorFactory = func() (dispatchTextGenerator, error) {
		return &stubTextGenerator{text: "generated dispatch"}, nil
	}
	t.Cleanup(func() {
		dispatchTextGeneratorFactory = oldFactory
	})
}

func seedCommittedCheckpoint(t *testing.T, repoDir string, cp seededCheckpoint) {
	t.Helper()

	repo, err := git.PlainOpenWithOptions(repoDir, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		t.Fatal(err)
	}

	store := checkpoint.NewGitStore(repo)
	cpID, err := checkpointid.NewCheckpointID(cp.id)
	if err != nil {
		t.Fatal(err)
	}

	err = store.WriteCommitted(context.Background(), checkpoint.WriteCommittedOptions{
		CheckpointID:     cpID,
		SessionID:        "session-1",
		Strategy:         "manual-commit",
		Branch:           cp.branch,
		Transcript:       redact.AlreadyRedacted([]byte("{\"type\":\"user\"}\n")),
		Prompts:          []string{"summarize recent work"},
		FilesTouched:     cp.filesTouched,
		CheckpointsCount: 1,
		Agent:            agent.AgentTypeClaudeCode,
		Summary: &checkpoint.Summary{
			Outcome: cp.outcome,
		},
		AuthorName:  "Test User",
		AuthorEmail: "test@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func addOriginRemote(t *testing.T, repoDir string) {
	t.Helper()

	repo, err := git.PlainOpenWithOptions(repoDir, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{testRepoRemoteURL},
	})
	if err != nil {
		t.Fatal(err)
	}
}
