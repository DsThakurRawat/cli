package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/entireio/cli/cmd/entire/cli/testutil"
	"github.com/spf13/cobra"
)

func TestSessionCurrent_NoSessionsPrintsHint(t *testing.T) {
	// t.Chdir cannot coexist with t.Parallel; this test mutates process CWD.
	dir := t.TempDir()
	testutil.InitRepo(t, dir)
	t.Chdir(dir)

	cmd := newSessionCurrentCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetContext(context.Background())
	cmd.SetArgs(nil)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\nstderr: %s", err, stderr.String())
	}

	if !strings.Contains(stdout.String(), "No active session") {
		t.Errorf("expected 'No active session' hint, got: %q", stdout.String())
	}
}

func TestSessionCurrent_NotARepoErrors(t *testing.T) {
	// CWD-mutating; cannot run in parallel.
	dir := t.TempDir() // not initialized as git repo
	t.Chdir(dir)

	cmd := newSessionCurrentCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetContext(context.Background())
	cmd.SilenceErrors = true
	cmd.SetArgs(nil)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when not in a git repository, got nil")
	}
}

// Compile-time check that newSessionCurrentCmd returns a *cobra.Command (a
// trivial sanity guard so an accidental return-type change is caught here
// rather than at the wiring site in sessions.go).
var _ *cobra.Command = newSessionCurrentCmd()
