package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestParseDispatchFlags_OrgDefaultsToAllBranches(t *testing.T) {
	t.Parallel()

	opts, err := parseDispatchFlags(
		&cobra.Command{},
		false,
		"7d",
		"",
		"",
		nil,
		"entireio",
		false,
		"",
		"text",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !opts.AllBranches {
		t.Fatal("expected org mode without --branches to default to all branches")
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
		"",
		[]string{"entireio/cli", "entireio/entire.io"},
		"",
		false,
		"",
		"text",
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

func TestParseDispatchFlags_LocalRejectsRepos(t *testing.T) {
	t.Parallel()

	_, err := parseDispatchFlags(
		&cobra.Command{},
		true,
		"7d",
		"",
		"",
		[]string{"entireio/cli"},
		"",
		false,
		"",
		"text",
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "--repos cannot be used with --local" {
		t.Fatalf("unexpected error: %v", err)
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

func TestDispatchWizardGenerateDefault(t *testing.T) {
	t.Parallel()

	if !dispatchWizardGenerateDefault {
		t.Fatal("expected dispatch wizard to default generate prose to true")
	}
}

func TestNewDispatchCmd_DoesNotExposeDryRunFlag(t *testing.T) {
	t.Parallel()

	cmd := newDispatchCmd()
	if cmd.Flags().Lookup("dry-run") != nil {
		t.Fatal("expected dispatch command not to expose dry-run")
	}
}

func TestNewDispatchCmd_DoesNotExposeWaitFlag(t *testing.T) {
	t.Parallel()

	cmd := newDispatchCmd()
	if cmd.Flags().Lookup("wait") != nil {
		t.Fatal("expected dispatch command not to expose wait")
	}
}
