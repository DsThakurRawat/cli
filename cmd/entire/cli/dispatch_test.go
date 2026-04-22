package cli

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	dispatchpkg "github.com/entireio/cli/cmd/entire/cli/dispatch"
	"github.com/spf13/cobra"
)

func TestParseDispatchFlags_OrgDefaultsToAllBranches(t *testing.T) {
	t.Parallel()

	opts, err := parseDispatchFlags(
		&cobra.Command{},
		false,
		"7d",
		"",
		false,
		nil,
		[]string{"entireio"},
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	if opts.AllBranches {
		t.Fatal("did not expect org mode without --all-branches to default to all branches")
	}
	if opts.Branches != nil {
		t.Fatalf("expected nil branches, got %v", opts.Branches)
	}
}

func TestParseDispatchFlags_ServerReposAreAllowed(t *testing.T) {
	t.Parallel()

	opts, err := parseDispatchFlags(
		&cobra.Command{},
		false,
		"7d",
		"",
		false,
		[]string{"entireio/cli", "entireio/entire.io"},
		nil,
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(opts.RepoPaths); got != 2 {
		t.Fatalf("expected 2 repo slugs, got %d", got)
	}
	if opts.Mode != 0 {
		t.Fatalf("expected server mode, got %v", opts.Mode)
	}
	if opts.Branches != nil {
		t.Fatalf("expected nil branches for server repo default-branch mode, got %v", opts.Branches)
	}
	if opts.AllBranches {
		t.Fatal("did not expect all branches for server repo default-branch mode")
	}
}

func TestParseDispatchFlags_NormalizesRepoScopeValues(t *testing.T) {
	t.Parallel()

	opts, err := parseDispatchFlags(
		&cobra.Command{},
		false,
		"7d",
		"",
		false,
		[]string{" entireio/cli ", "", "entireio/cli", " otherco/service ", "   "},
		nil,
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(opts.RepoPaths, ","); got != "entireio/cli,otherco/service" {
		t.Fatalf("expected normalized repo scope, got %q", got)
	}
	if opts.Branches != nil {
		t.Fatalf("expected nil branches for repo scope, got %v", opts.Branches)
	}
}

func TestParseDispatchFlags_LocalRejectsRepos(t *testing.T) {
	t.Parallel()

	_, err := parseDispatchFlags(
		&cobra.Command{},
		true,
		"7d",
		"",
		false,
		[]string{"entireio/cli"},
		nil,
		"",
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "--repos cannot be used with --local" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseDispatchFlags_LocalRejectsOrg(t *testing.T) {
	t.Parallel()

	_, err := parseDispatchFlags(
		&cobra.Command{},
		true,
		"7d",
		"",
		false,
		nil,
		[]string{"entireio"},
		"",
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "--org cannot be used with --local" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseDispatchFlags_MultipleOrgsAreAllowed(t *testing.T) {
	t.Parallel()

	opts, err := parseDispatchFlags(
		&cobra.Command{},
		false,
		"7d",
		"",
		false,
		nil,
		[]string{"entireio", "otherco"},
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(opts.Orgs, ","); got != "entireio,otherco" {
		t.Fatalf("expected multi-org scope to propagate, got %q", got)
	}
	if opts.Branches != nil {
		t.Fatalf("expected nil branches for org scope, got %v", opts.Branches)
	}
}

func TestParseDispatchFlags_AllBranchesFlag(t *testing.T) {
	t.Parallel()

	opts, err := parseDispatchFlags(
		&cobra.Command{},
		false,
		"7d",
		"",
		true,
		nil,
		nil,
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !opts.AllBranches {
		t.Fatal("expected all branches to propagate")
	}
	if opts.Branches != nil {
		t.Fatalf("expected nil branches, got %v", opts.Branches)
	}
}

func TestShouldRunDispatchWizard(t *testing.T) {
	t.Parallel()

	if !shouldRunDispatchWizard(0, true, true) {
		t.Fatal("expected wizard to run when stdin and stdout are terminals with no flags")
	}
	if shouldRunDispatchWizard(0, false, true) {
		t.Fatal("expected wizard not to run when stdin is piped")
	}
	if shouldRunDispatchWizard(1, true, true) {
		t.Fatal("expected wizard not to run when flags are provided")
	}
}

func TestNewDispatchCmd_NonTerminalPrintsPlainMarkdown(t *testing.T) {
	oldRunDispatch := runDispatch
	oldTerminalMode := dispatchTerminalMode
	oldMarkdown := renderDispatchMarkdown
	runDispatch = func(_ context.Context, _ dispatchpkg.Options) (*dispatchpkg.Dispatch, error) {
		return &dispatchpkg.Dispatch{GeneratedText: "generated dispatch"}, nil
	}
	dispatchTerminalMode = func(_ io.Writer) bool { return false }
	renderDispatchMarkdown = func(dispatch *dispatchpkg.Dispatch) string {
		if dispatch.GeneratedText != "generated dispatch" {
			t.Fatalf("unexpected dispatch: %+v", dispatch)
		}
		return testDispatchGeneratedMarkdown
	}
	t.Cleanup(func() {
		runDispatch = oldRunDispatch
		dispatchTerminalMode = oldTerminalMode
		renderDispatchMarkdown = oldMarkdown
	})

	cmd := newDispatchCmd()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--repos", "entireio/cli"})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := stdout.String(); got != testDispatchGeneratedMarkdown {
		t.Fatalf("unexpected stdout: %q", got)
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("unexpected stderr: %q", got)
	}
}

func TestNewDispatchCmd_TerminalUsesInteractiveRenderer(t *testing.T) {
	oldTerminalMode := dispatchTerminalMode
	oldInteractive := runInteractiveDispatch
	oldGlow := renderTerminalMarkdown
	dispatchTerminalMode = func(_ io.Writer) bool { return true }
	runInteractiveDispatch = func(_ context.Context, _ io.Writer, _ dispatchpkg.Options) (string, error) {
		return testDispatchGeneratedMarkdown, nil
	}
	renderTerminalMarkdown = func(_ io.Writer, markdown string) (string, error) {
		if markdown != testDispatchGeneratedMarkdown {
			t.Fatalf("unexpected markdown: %q", markdown)
		}
		return "glow output\n", nil
	}
	t.Cleanup(func() {
		dispatchTerminalMode = oldTerminalMode
		runInteractiveDispatch = oldInteractive
		renderTerminalMarkdown = oldGlow
	})

	cmd := newDispatchCmd()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--repos", "entireio/cli"})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := stdout.String(); got != "glow output\n" {
		t.Fatalf("unexpected stdout: %q", got)
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("unexpected stderr: %q", got)
	}
}
