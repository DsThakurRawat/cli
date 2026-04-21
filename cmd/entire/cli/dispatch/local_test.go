package dispatch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	testutil.InitRepo(t, dir)
	testutil.WriteFile(t, dir, "a.txt", "x")
	testutil.GitAdd(t, dir, "a.txt")
	testutil.GitCommit(t, dir, "initial")
	addOriginRemote(t, dir, "https://github.com/entireio/cli.git")

	createdAt := time.Now().UTC()
	seedCommittedCheckpoint(t, dir, seededCheckpoint{
		id:           "a1b2c3d4e5f6",
		branch:       "main",
		createdAt:    createdAt,
		filesTouched: []string{"a.txt"},
		outcome:      "local fallback summary",
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/users/me/checkpoints/analyses/batch" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"analyses": map[string]AnalysisStatus{
				"a1b2c3d4e5f6": {
					Status:  "complete",
					Summary: "remote summary",
					Labels:  []string{"CI & Tooling"},
				},
			},
		}); err != nil {
			t.Fatal(err)
		}
	}))
	defer srv.Close()

	oldLookup := lookupCurrentToken
	oldNow := nowUTC
	lookupCurrentToken = func() (string, error) { return "test-token", nil }
	nowUTC = func() time.Time { return createdAt.Add(2 * time.Hour) }
	t.Cleanup(func() {
		lookupCurrentToken = oldLookup
		nowUTC = oldNow
	})

	t.Setenv("ENTIRE_API_BASE_URL", srv.URL)
	t.Chdir(dir)

	got, err := Run(context.Background(), Options{
		Mode:     ModeLocal,
		Since:    "7d",
		Branches: []string{"main"},
		Format:   "json",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Totals.Checkpoints != 1 {
		t.Fatalf("expected 1 candidate, got %d", got.Totals.Checkpoints)
	}
	if got.Totals.UsedCheckpointCount != 1 {
		t.Fatalf("expected 1 used checkpoint, got %d", got.Totals.UsedCheckpointCount)
	}
	if len(got.Repos) != 1 {
		t.Fatalf("expected 1 repo group, got %d", len(got.Repos))
	}
	if got.Repos[0].FullName != "entireio/cli" {
		t.Fatalf("unexpected repo group: %+v", got.Repos[0])
	}
	if got.Repos[0].Sections[0].Bullets[0].Text != "remote summary" {
		t.Fatalf("unexpected bullet: %+v", got.Repos[0].Sections[0].Bullets[0])
	}
	if len(got.CoveredRepos) != 1 || got.CoveredRepos[0] != "entireio/cli" {
		t.Fatalf("unexpected covered repos: %v", got.CoveredRepos)
	}
}

func TestLocalMode_UsesUntilWindow(t *testing.T) {
	dir := t.TempDir()
	testutil.InitRepo(t, dir)
	testutil.WriteFile(t, dir, "a.txt", "x")
	testutil.GitAdd(t, dir, "a.txt")
	testutil.GitCommit(t, dir, "initial")
	addOriginRemote(t, dir, "https://github.com/entireio/cli.git")

	now := time.Now().UTC()
	seedCommittedCheckpoint(t, dir, seededCheckpoint{
		id:           "a1b2c3d4e5f6",
		branch:       "main",
		createdAt:    now,
		filesTouched: []string{"a.txt"},
		outcome:      "local fallback summary",
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected request to %s", r.URL.Path)
	}))
	defer srv.Close()

	oldLookup := lookupCurrentToken
	oldNow := nowUTC
	lookupCurrentToken = func() (string, error) { return "test-token", nil }
	nowUTC = func() time.Time { return now }
	t.Cleanup(func() {
		lookupCurrentToken = oldLookup
		nowUTC = oldNow
	})

	t.Setenv("ENTIRE_API_BASE_URL", srv.URL)
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
	if got.Totals.Checkpoints != 0 {
		t.Fatalf("expected 0 candidates, got %d", got.Totals.Checkpoints)
	}
	if len(got.Repos) != 0 {
		t.Fatalf("expected no repo groups, got %d", len(got.Repos))
	}
}

func TestLocalMode_OrgEnumeratesFromCloud(t *testing.T) {
	dir := t.TempDir()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/orgs/entireio/checkpoints":
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(map[string]any{
				"checkpoints": []map[string]any{{
					"id":             "a1b2c3d4e5f6",
					"repo_full_name": "entireio/cli",
					"branch":         "main",
					"created_at":     "2026-04-16T12:00:00Z",
				}},
			}); err != nil {
				t.Fatal(err)
			}
		case "/api/v1/users/me/checkpoints/analyses/batch":
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(map[string]any{
				"analyses": map[string]AnalysisStatus{
					"a1b2c3d4e5f6": {
						Status:  "complete",
						Summary: "org summary",
						Labels:  []string{"Dispatch"},
					},
				},
			}); err != nil {
				t.Fatal(err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	oldLookup := lookupCurrentToken
	oldNow := nowUTC
	lookupCurrentToken = func() (string, error) { return "test-token", nil }
	nowUTC = func() time.Time { return time.Date(2026, 4, 16, 14, 0, 0, 0, time.UTC) }
	t.Cleanup(func() {
		lookupCurrentToken = oldLookup
		nowUTC = oldNow
	})

	t.Setenv("ENTIRE_API_BASE_URL", srv.URL)
	t.Chdir(dir)

	got, err := Run(context.Background(), Options{
		Mode:        ModeLocal,
		Org:         "entireio",
		Since:       "7d",
		Branches:    []string{"main"},
		AllBranches: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Totals.Checkpoints != 1 {
		t.Fatalf("expected 1 candidate, got %d", got.Totals.Checkpoints)
	}
	if len(got.Repos) != 1 || got.Repos[0].FullName != "entireio/cli" {
		t.Fatalf("unexpected repo groups: %+v", got.Repos)
	}
	if got.Repos[0].Sections[0].Bullets[0].Text != "org summary" {
		t.Fatalf("unexpected bullet: %+v", got.Repos[0].Sections[0].Bullets[0])
	}
}

func TestLocalMode_SkipsPendingAnalysesWithoutWaiting(t *testing.T) {
	dir := t.TempDir()
	testutil.InitRepo(t, dir)
	testutil.WriteFile(t, dir, "a.txt", "x")
	testutil.GitAdd(t, dir, "a.txt")
	testutil.GitCommit(t, dir, "initial")
	addOriginRemote(t, dir, "https://github.com/entireio/cli.git")

	now := time.Now().UTC()
	seedCommittedCheckpoint(t, dir, seededCheckpoint{
		id:           "a1b2c3d4e5f6",
		branch:       "main",
		createdAt:    now,
		filesTouched: []string{"a.txt"},
		outcome:      "local fallback summary",
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/users/me/checkpoints/analyses/batch" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"analyses": map[string]AnalysisStatus{
				"a1b2c3d4e5f6": {Status: "pending"},
			},
		}); err != nil {
			t.Fatal(err)
		}
	}))
	defer srv.Close()

	oldLookup := lookupCurrentToken
	oldNow := nowUTC
	lookupCurrentToken = func() (string, error) { return "test-token", nil }
	nowUTC = func() time.Time { return now }
	t.Cleanup(func() {
		lookupCurrentToken = oldLookup
		nowUTC = oldNow
	})

	t.Setenv("ENTIRE_API_BASE_URL", srv.URL)
	t.Chdir(dir)

	got, err := Run(context.Background(), Options{
		Mode:     ModeLocal,
		Since:    "7d",
		Branches: []string{"main"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Repos) != 0 {
		t.Fatalf("expected no repos when pending analyses are skipped, got %+v", got.Repos)
	}
	if got.Warnings.PendingCount != 1 {
		t.Fatalf("expected pending warning count, got %+v", got.Warnings)
	}
}

func TestLocalMode_GenerateProducesInlineText(t *testing.T) {
	dir := t.TempDir()
	testutil.InitRepo(t, dir)
	testutil.WriteFile(t, dir, "a.txt", "x")
	testutil.GitAdd(t, dir, "a.txt")
	testutil.GitCommit(t, dir, "initial")
	addOriginRemote(t, dir, "https://github.com/entireio/cli.git")

	createdAt := time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC)
	seedCommittedCheckpoint(t, dir, seededCheckpoint{
		id:           "a1b2c3d4e5f6",
		branch:       "main",
		createdAt:    createdAt,
		filesTouched: []string{"a.txt"},
		outcome:      "local fallback summary",
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/users/me/checkpoints/analyses/batch" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"analyses": map[string]AnalysisStatus{
				"a1b2c3d4e5f6": {
					Status:  "complete",
					Summary: "remote summary",
				},
			},
		}); err != nil {
			t.Fatal(err)
		}
	}))
	defer srv.Close()

	oldLookup := lookupCurrentToken
	oldNow := nowUTC
	oldFactory := dispatchTextGeneratorFactory
	lookupCurrentToken = func() (string, error) { return "test-token", nil }
	nowUTC = func() time.Time { return createdAt.Add(2 * time.Hour) }
	mock := &stubTextGenerator{text: "generated inline dispatch"}
	dispatchTextGeneratorFactory = func() (dispatchTextGenerator, error) {
		return mock, nil
	}
	t.Cleanup(func() {
		lookupCurrentToken = oldLookup
		nowUTC = oldNow
		dispatchTextGeneratorFactory = oldFactory
	})

	t.Setenv("ENTIRE_API_BASE_URL", srv.URL)
	t.Chdir(dir)

	got, err := Run(context.Background(), Options{
		Mode:     ModeLocal,
		Since:    "7d",
		Branches: []string{"main"},
		Generate: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.RequestedGenerate {
		t.Fatal("expected requested generate flag")
	}
	if !got.Generated {
		t.Fatal("expected generated=true")
	}
	if got.GeneratedText != "generated inline dispatch" {
		t.Fatalf("expected generated text, got %q", got.GeneratedText)
	}
}

func TestLocalMode_ImplicitCurrentBranchUsesHEADReachability(t *testing.T) {
	dir := t.TempDir()
	testutil.InitRepo(t, dir)
	testutil.WriteFile(t, dir, "a.txt", "x")
	testutil.GitAdd(t, dir, "a.txt")
	testutil.GitCommit(t, dir, "initial")
	addOriginRemote(t, dir, "https://github.com/entireio/cli.git")

	cpID := "a1b2c3d4e5f6"
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
			Outcome: "local fallback summary",
		},
		AuthorName:  "Test User",
		AuthorEmail: "test@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}

	testutil.GitCheckoutNewBranch(t, dir, "entire-dispatch-codex")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/users/me/checkpoints/analyses/batch" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"analyses": map[string]AnalysisStatus{
				cpID: {
					Status:  "complete",
					Summary: "remote summary",
					Labels:  []string{"Dispatch"},
				},
			},
		}); err != nil {
			t.Fatal(err)
		}
	}))
	defer srv.Close()

	oldLookup := lookupCurrentToken
	oldNow := nowUTC
	lookupCurrentToken = func() (string, error) { return "test-token", nil }
	nowUTC = func() time.Time { return time.Now().UTC() }
	t.Cleanup(func() {
		lookupCurrentToken = oldLookup
		nowUTC = oldNow
	})

	t.Setenv("ENTIRE_API_BASE_URL", srv.URL)
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
	if got.Totals.Checkpoints != 1 {
		t.Fatalf("expected 1 candidate, got %d", got.Totals.Checkpoints)
	}
	if len(got.Repos) != 1 || got.Repos[0].Sections[0].Bullets[0].Text != "remote summary" {
		t.Fatalf("unexpected dispatch payload: %+v", got)
	}
}

func TestLocalMode_ExplicitBranchesRemainExact(t *testing.T) {
	dir := t.TempDir()
	testutil.InitRepo(t, dir)
	testutil.WriteFile(t, dir, "a.txt", "x")
	testutil.GitAdd(t, dir, "a.txt")
	testutil.GitCommit(t, dir, "initial")
	addOriginRemote(t, dir, "https://github.com/entireio/cli.git")

	cpID := "a1b2c3d4e5f6"
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
			Outcome: "local fallback summary",
		},
		AuthorName:  "Test User",
		AuthorEmail: "test@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}

	testutil.GitCheckoutNewBranch(t, dir, "entire-dispatch-codex")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected request to %s", r.URL.Path)
	}))
	defer srv.Close()

	oldLookup := lookupCurrentToken
	oldNow := nowUTC
	lookupCurrentToken = func() (string, error) { return "test-token", nil }
	nowUTC = func() time.Time { return time.Now().UTC() }
	t.Cleanup(func() {
		lookupCurrentToken = oldLookup
		nowUTC = oldNow
	})

	t.Setenv("ENTIRE_API_BASE_URL", srv.URL)
	t.Chdir(dir)

	got, err := Run(context.Background(), Options{
		Mode:     ModeLocal,
		Since:    "7d",
		Branches: []string{"entire-dispatch-codex"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Totals.Checkpoints != 0 {
		t.Fatalf("expected 0 candidates with explicit branch filter, got %d", got.Totals.Checkpoints)
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

func addOriginRemote(t *testing.T, repoDir, remoteURL string) {
	t.Helper()

	repo, err := git.PlainOpenWithOptions(repoDir, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{remoteURL},
	})
	if err != nil {
		t.Fatal(err)
	}
}
