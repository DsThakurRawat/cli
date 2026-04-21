package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/huh"
	dispatchpkg "github.com/entireio/cli/cmd/entire/cli/dispatch"
)

func TestNewDispatchWizardState_Defaults(t *testing.T) {
	t.Parallel()

	state := newDispatchWizardState()
	if state.modeChoice != dispatchWizardModeLocal {
		t.Fatalf("expected local mode default, got %q", state.modeChoice)
	}
	if state.scopeType != dispatchWizardScopeCurrentRepo {
		t.Fatalf("expected current repo scope default, got %q", state.scopeType)
	}
	if state.timeWindowPreset != "7d" {
		t.Fatalf("expected 7d default, got %q", state.timeWindowPreset)
	}
	if state.branchMode != dispatchWizardBranchDefault {
		t.Fatalf("expected default-branches mode default, got %q", state.branchMode)
	}
	if state.voicePreset != "neutral" {
		t.Fatalf("expected neutral voice preset, got %q", state.voicePreset)
	}
	if state.format != "text" {
		t.Fatalf("expected text format default, got %q", state.format)
	}
}

func TestDispatchWizardState_DefaultLocalBranchesPreselectCurrentBranch(t *testing.T) {
	t.Parallel()

	state := newDispatchWizardState()
	state.applyCurrentBranchDefault("feature/current")

	if got := strings.Join(state.selectedBranches, ","); got != "feature/current" {
		t.Fatalf("expected current branch to be preselected, got %q", got)
	}
}

func TestDispatchWizardState_ResolveOrgDefaultsToAllBranches(t *testing.T) {
	t.Parallel()

	state := newDispatchWizardState()
	state.modeChoice = dispatchWizardModeServer
	state.scopeType = dispatchWizardScopeOrganization
	state.selectedOrg = "entireio"

	opts, err := state.resolve(dispatchWizardChoices{
		orgOptions: []huh.Option[string]{
			huh.NewOption("entireio", "entireio"),
		},
	}, func() (string, error) { return "feature/preview", nil })
	if err != nil {
		t.Fatal(err)
	}
	if !opts.AllBranches {
		t.Fatal("expected org scope to default to all branches")
	}
	if opts.Branches != nil {
		t.Fatalf("expected nil branches, got %v", opts.Branches)
	}
}

func TestDispatchWizardState_ResolveSelectedBranches(t *testing.T) {
	t.Parallel()

	state := newDispatchWizardState()
	state.branchMode = dispatchWizardBranchSelected
	state.selectedBranches = []string{"main", "release"}

	opts, err := state.resolve(dispatchWizardChoices{
		currentRepoBranches: []huh.Option[string]{huh.NewOption("main", "main")},
	}, func() (string, error) { return "feature/preview", nil })
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(opts.Branches, ","); got != "main,release" {
		t.Fatalf("expected selected branches, got %q", got)
	}
}

func TestDispatchWizardChoices_BranchModesSuppressSelectedForMultiRepo(t *testing.T) {
	t.Parallel()

	choices := dispatchWizardChoices{
		currentRepoBranches: []huh.Option[string]{
			huh.NewOption("main", "main"),
		},
		repoOptions: []huh.Option[string]{
			huh.NewOption("entireio/repo-a", "entireio/repo-a"),
			huh.NewOption("entireio/repo-b", "entireio/repo-b"),
		},
	}
	state := newDispatchWizardState()
	state.modeChoice = dispatchWizardModeServer
	state.scopeType = dispatchWizardScopeSelectedRepos
	state.selectedRepos = []string{"entireio/repo-a", "entireio/repo-b"}

	values := optionValues(choices.branchModeOptions(state))
	if strings.Contains(strings.Join(values, ","), dispatchWizardBranchSelected) {
		t.Fatalf("did not expect selected branch mode for multi-repo scope, got %v", values)
	}
}

func TestDispatchWizardChoices_ScopeOptionsAdaptByMode(t *testing.T) {
	t.Parallel()

	choices := dispatchWizardChoices{
		repoOptions: []huh.Option[string]{
			huh.NewOption("entireio/cli", "entireio/cli"),
			huh.NewOption("entireio/entire.io", "entireio/entire.io"),
		},
		orgOptions: []huh.Option[string]{
			huh.NewOption("entireio", "entireio"),
		},
	}

	state := newDispatchWizardState()
	localScopeValues := optionValues(choices.scopeOptions(state))
	if got := strings.Join(localScopeValues, ","); got != dispatchWizardScopeCurrentRepo {
		t.Fatalf("unexpected local scope options: %v", localScopeValues)
	}

	state.modeChoice = dispatchWizardModeServer
	serverScopeValues := optionValues(choices.scopeOptions(state))
	if got := strings.Join(serverScopeValues, ","); got != dispatchWizardScopeSelectedRepos+","+dispatchWizardScopeOrganization {
		t.Fatalf("unexpected server scope options: %v", serverScopeValues)
	}
}

func TestDispatchWizardChoices_ServerRepoOptionsUseFullSlugLabels(t *testing.T) {
	t.Parallel()

	choices := dispatchWizardChoices{
		repoOptions: []huh.Option[string]{
			huh.NewOption("entireio/cli", "entireio/cli"),
			huh.NewOption("entireio/entire.io", "entireio/entire.io"),
		},
	}

	if got := strings.Join(optionKeys(choices.repoOptions), ","); got != "entireio/cli,entireio/entire.io" {
		t.Fatalf("expected repo options to use org/repo labels, got %q", got)
	}
}

func TestDispatchWizardState_ServerModeKeepsSelectedReposScope(t *testing.T) {
	t.Parallel()

	state := newDispatchWizardState()
	state.modeChoice = dispatchWizardModeServer
	state.scopeType = dispatchWizardScopeSelectedRepos
	state.selectedRepos = []string{"entireio/cli"}

	if got := state.effectiveScopeType(dispatchWizardChoices{
		repoOptions: []huh.Option[string]{
			huh.NewOption("entireio/cli", "entireio/cli"),
		},
	}); got != dispatchWizardScopeSelectedRepos {
		t.Fatalf("expected server mode to keep selected repos scope, got %q", got)
	}

	opts, err := state.resolve(dispatchWizardChoices{
		repoOptions: []huh.Option[string]{
			huh.NewOption("entireio/cli", "entireio/cli"),
		},
	}, func() (string, error) { return "feature/preview", nil })
	if err != nil {
		t.Fatalf("expected server mode to resolve selected repos, got %v", err)
	}
	if got := strings.Join(opts.RepoPaths, ","); got != "entireio/cli" {
		t.Fatalf("expected selected repo path to propagate, got %q", got)
	}
}

func TestDispatchWizardState_ShowsRepoPickerOnlyForSelectedRepos(t *testing.T) {
	t.Parallel()

	state := newDispatchWizardState()
	choices := dispatchWizardChoices{}
	if state.showRepoPicker(choices) {
		t.Fatal("did not expect repo picker in local mode")
	}

	state.modeChoice = dispatchWizardModeServer
	state.scopeType = dispatchWizardScopeSelectedRepos
	if !state.showRepoPicker(choices) {
		t.Fatal("expected repo picker for selected repos scope in server mode")
	}
}

func TestDispatchWizardState_ShowsOrganizationPickerOnlyForOrgScope(t *testing.T) {
	t.Parallel()

	state := newDispatchWizardState()
	if state.showOrganizationPicker(dispatchWizardChoices{}) {
		t.Fatal("did not expect organization picker in local mode")
	}

	state.modeChoice = dispatchWizardModeServer
	if state.showOrganizationPicker(dispatchWizardChoices{}) {
		t.Fatal("did not expect organization picker for current repo server scope")
	}

	state.scopeType = dispatchWizardScopeOrganization
	if !state.showOrganizationPicker(dispatchWizardChoices{
		orgOptions: []huh.Option[string]{
			huh.NewOption("entireio", "entireio"),
		},
	}) {
		t.Fatal("expected organization picker for organization scope")
	}
}

func TestDispatchWizardState_ShowsBranchPickerOnlyForSelectedBranchMode(t *testing.T) {
	t.Parallel()

	choices := dispatchWizardChoices{
		currentRepoBranches: []huh.Option[string]{
			huh.NewOption("main", "main"),
		},
		branchesByRepoSlug: map[string][]huh.Option[string]{
			"entireio/repo-a": {huh.NewOption("main", "main")},
		},
	}
	state := newDispatchWizardState()
	if !state.showSelectedBranchesPicker(choices) {
		t.Fatal("expected branch picker in local mode")
	}

	state.modeChoice = dispatchWizardModeServer
	if state.showSelectedBranchesPicker(choices) {
		t.Fatal("did not expect branch picker for default branch mode in server mode")
	}
	state.branchMode = dispatchWizardBranchSelected
	state.scopeType = dispatchWizardScopeSelectedRepos
	state.selectedRepos = []string{"entireio/repo-a"}
	if !state.showSelectedBranchesPicker(choices) {
		t.Fatal("expected branch picker for selected branch mode")
	}

	state.selectedRepos = []string{"entireio/repo-a", "entireio/repo-b"}
	if state.showSelectedBranchesPicker(choices) {
		t.Fatal("did not expect branch picker for multi-repo scope")
	}
}

func TestDispatchWizardState_ResolveVoicePresets(t *testing.T) {
	t.Parallel()

	state := newDispatchWizardState()
	state.voicePreset = "marvin"
	opts, err := state.resolve(dispatchWizardChoices{}, func() (string, error) { return "feature/preview", nil })
	if err != nil {
		t.Fatal(err)
	}
	if opts.Voice != "marvin" {
		t.Fatalf("expected marvin voice, got %q", opts.Voice)
	}

	state.voicePreset = "neutral"
	opts, err = state.resolve(dispatchWizardChoices{}, func() (string, error) { return "feature/preview", nil })
	if err != nil {
		t.Fatal(err)
	}
	if opts.Voice != "neutral" {
		t.Fatalf("expected neutral voice, got %q", opts.Voice)
	}
}

func TestBuildDispatchWizardSummary(t *testing.T) {
	t.Parallel()

	summary := buildDispatchWizardSummary(dispatchpkg.Options{
		Mode:        dispatchpkg.ModeLocal,
		RepoPaths:   []string{"/tmp/repo-a", "/tmp/repo-b"},
		Branches:    []string{"main", "release"},
		AllBranches: false,
	})
	if !strings.Contains(summary, "Mode: local") {
		t.Fatalf("expected local mode in summary, got %q", summary)
	}
	if !strings.Contains(summary, "Scope: repos:/tmp/repo-a, /tmp/repo-b") {
		t.Fatalf("expected repo scope in summary, got %q", summary)
	}
	if !strings.Contains(summary, "Branches: main, release") {
		t.Fatalf("expected branches in summary, got %q", summary)
	}
}

func TestBuildDispatchCommand(t *testing.T) {
	t.Parallel()

	command := buildDispatchCommand(dispatchpkg.Options{
		Mode:        dispatchpkg.ModeServer,
		Since:       "7d",
		Branches:    nil,
		Generate:    true,
		Voice:       "marvin",
		Format:      "markdown",
		RepoPaths:   []string{"entireio/cli"},
		AllBranches: false,
	})
	if !strings.Contains(command, "entire dispatch") {
		t.Fatalf("expected base command, got %q", command)
	}
	if !strings.Contains(command, "--generate") {
		t.Fatalf("expected generate flag, got %q", command)
	}
	if !strings.Contains(command, "--voice marvin") {
		t.Fatalf("expected preset voice flag, got %q", command)
	}
	if !strings.Contains(command, "--repos entireio/cli") {
		t.Fatalf("expected server repos flag, got %q", command)
	}
	if strings.Contains(command, "--local") {
		t.Fatalf("did not expect local flag, got %q", command)
	}
	if strings.Contains(command, "--branches") {
		t.Fatalf("did not expect branches flag for default-branch mode, got %q", command)
	}
	if strings.Contains(command, "--dry-run") {
		t.Fatalf("did not expect dry-run flag, got %q", command)
	}
}

func optionValues(options []huh.Option[string]) []string {
	values := make([]string, 0, len(options))
	for _, option := range options {
		values = append(values, option.Value)
	}
	return values
}

func optionKeys(options []huh.Option[string]) []string {
	keys := make([]string, 0, len(options))
	for _, option := range options {
		keys = append(keys, option.Key)
	}
	return keys
}

func TestWizardNowUTCPlaceholder(t *testing.T) {
	t.Parallel()

	oldNow := wizardNowUTC
	wizardNowUTC = func() time.Time { return time.Date(2026, 4, 17, 9, 30, 0, 0, time.UTC) }
	t.Cleanup(func() {
		wizardNowUTC = oldNow
	})

	if wizardNowUTC().Format("2006-01-02") != "2026-04-17" {
		t.Fatal("expected deterministic test now")
	}
}
