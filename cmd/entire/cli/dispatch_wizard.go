package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	dispatchpkg "github.com/entireio/cli/cmd/entire/cli/dispatch"
	"github.com/entireio/cli/cmd/entire/cli/paths"
	searchpkg "github.com/entireio/cli/cmd/entire/cli/search"
	"github.com/go-git/go-git/v6"
	"github.com/spf13/cobra"
)

var errDispatchCancelled = errors.New("dispatch cancelled")
var wizardNowUTC = func() time.Time { return time.Now().UTC() }

const (
	dispatchWizardModeLocal  = "local"
	dispatchWizardModeServer = "server"

	dispatchWizardScopeCurrentRepo   = "current_repo"
	dispatchWizardScopeSelectedRepos = "selected_repos"
	dispatchWizardScopeOrganization  = "organization"

	dispatchWizardBranchDefault  = "default"
	dispatchWizardBranchCurrent  = "current"
	dispatchWizardBranchAll      = "all"
	dispatchWizardBranchSelected = "selected"
)

type dispatchWizardState struct {
	modeChoice       string
	scopeType        string
	timeWindowPreset string
	branchMode       string
	selectedRepos    []string
	selectedOrg      string
	selectedBranches []string
	voicePreset      string
	format           string
	confirmRun       bool
}

type dispatchWizardChoices struct {
	currentRepo         string
	currentRepoBranches []huh.Option[string]
	repoOptions         []huh.Option[string]
	orgOptions          []huh.Option[string]
	branchesByRepoSlug  map[string][]huh.Option[string]
}

func newDispatchWizardState() dispatchWizardState {
	return dispatchWizardState{
		modeChoice:       dispatchWizardModeLocal,
		scopeType:        dispatchWizardScopeCurrentRepo,
		timeWindowPreset: "7d",
		branchMode:       dispatchWizardBranchDefault,
		voicePreset:      "neutral",
		format:           "text",
	}
}

func (s dispatchWizardState) isLocal() bool {
	return s.modeChoice != dispatchWizardModeServer
}

func (s dispatchWizardState) sinceValue() string {
	return s.timeWindowPreset
}

func (s dispatchWizardState) voiceValue() string {
	if strings.TrimSpace(s.voicePreset) == "marvin" {
		return "marvin"
	}
	return "neutral"
}

func (s dispatchWizardState) effectiveScopeType(choices dispatchWizardChoices) string {
	if s.isLocal() {
		return dispatchWizardScopeCurrentRepo
	}

	if s.scopeType == dispatchWizardScopeOrganization && len(choices.orgOptions) > 0 {
		return dispatchWizardScopeOrganization
	}
	if len(choices.repoOptions) > 0 {
		return dispatchWizardScopeSelectedRepos
	}
	if len(choices.orgOptions) > 0 {
		return dispatchWizardScopeOrganization
	}
	return dispatchWizardScopeSelectedRepos
}

func (s dispatchWizardState) allowsSelectedBranches(choices dispatchWizardChoices) bool {
	if s.isLocal() {
		return len(choices.currentRepoBranches) > 0
	}
	if s.effectiveScopeType(choices) == dispatchWizardScopeSelectedRepos {
		return len(s.selectedRepos) == 1
	}
	return false
}

func (s dispatchWizardState) effectiveBranchMode(choices dispatchWizardChoices) string {
	if s.isLocal() {
		return dispatchWizardBranchSelected
	}

	switch s.branchMode {
	case dispatchWizardBranchDefault, dispatchWizardBranchAll, dispatchWizardBranchSelected:
	default:
		return dispatchWizardBranchDefault
	}
	if s.branchMode == dispatchWizardBranchSelected && !s.allowsSelectedBranches(choices) {
		return dispatchWizardBranchDefault
	}
	return s.branchMode
}

func (s dispatchWizardState) selectedRepoPaths(choices dispatchWizardChoices) []string {
	if s.isLocal() || s.effectiveScopeType(choices) != dispatchWizardScopeSelectedRepos {
		return nil
	}
	return append([]string(nil), s.selectedRepos...)
}

func (s dispatchWizardState) showRepoPicker(choices dispatchWizardChoices) bool {
	return !s.isLocal() && s.effectiveScopeType(choices) == dispatchWizardScopeSelectedRepos
}

func (s dispatchWizardState) showScopePicker(choices dispatchWizardChoices) bool {
	return !s.isLocal() && len(choices.scopeOptions(s)) > 1
}

func (s dispatchWizardState) showOrganizationPicker(choices dispatchWizardChoices) bool {
	return !s.isLocal() && s.effectiveScopeType(choices) == dispatchWizardScopeOrganization
}

func (s dispatchWizardState) showSelectedBranchesPicker(choices dispatchWizardChoices) bool {
	return s.isLocal() || s.effectiveBranchMode(choices) == dispatchWizardBranchSelected
}

func (s dispatchWizardState) showBranchModePicker() bool {
	return !s.isLocal()
}

func (s *dispatchWizardState) applyCurrentBranchDefault(branch string) {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return
	}
	if len(s.selectedBranches) == 0 {
		s.selectedBranches = []string{branch}
		return
	}
	for _, selected := range s.selectedBranches {
		if selected == branch {
			return
		}
	}
	s.selectedBranches = append([]string{branch}, s.selectedBranches...)
}

func (s dispatchWizardState) orgValue(choices dispatchWizardChoices) string {
	if s.isLocal() || s.effectiveScopeType(choices) != dispatchWizardScopeOrganization {
		return ""
	}
	if value := strings.TrimSpace(s.selectedOrg); value != "" {
		return value
	}
	if len(choices.orgOptions) > 0 {
		return choices.orgOptions[0].Value
	}
	return ""
}

func (s dispatchWizardState) branchesFlag(choices dispatchWizardChoices) string {
	switch s.effectiveBranchMode(choices) {
	case dispatchWizardBranchDefault:
		return ""
	case dispatchWizardBranchAll:
		return "all"
	case dispatchWizardBranchSelected:
		return strings.Join(s.selectedBranches, ",")
	}
	return ""
}

func (s dispatchWizardState) resolve(choices dispatchWizardChoices, currentBranch func() (string, error)) (dispatchpkg.Options, error) {
	return resolveDispatchOptions(
		s.isLocal(),
		s.sinceValue(),
		"",
		s.branchesFlag(choices),
		s.selectedRepoPaths(choices),
		s.orgValue(choices),
		dispatchWizardGenerateDefault,
		s.voiceValue(),
		s.format,
		currentBranch,
	)
}

func (c dispatchWizardChoices) scopeOptions(state dispatchWizardState) []huh.Option[string] {
	if state.isLocal() {
		return []huh.Option[string]{huh.NewOption("Current repo", dispatchWizardScopeCurrentRepo)}
	}
	options := make([]huh.Option[string], 0, 2)
	if len(c.repoOptions) > 0 {
		options = append(options, huh.NewOption("Selected repos", dispatchWizardScopeSelectedRepos))
	}
	if len(c.orgOptions) > 0 {
		options = append(options, huh.NewOption("Organization", dispatchWizardScopeOrganization))
	}
	return options
}

func (c dispatchWizardChoices) branchModeOptions(state dispatchWizardState) []huh.Option[string] {
	if state.isLocal() {
		return nil
	}
	options := []huh.Option[string]{
		huh.NewOption("Default branches", dispatchWizardBranchDefault),
		huh.NewOption("All branches", dispatchWizardBranchAll),
	}
	if state.allowsSelectedBranches(c) {
		options = append(options, huh.NewOption("Selected branches", dispatchWizardBranchSelected))
	}
	return options
}

func (c dispatchWizardChoices) branchOptions(state dispatchWizardState) []huh.Option[string] {
	if state.isLocal() {
		return append([]huh.Option[string](nil), c.currentRepoBranches...)
	}

	if state.effectiveScopeType(c) == dispatchWizardScopeSelectedRepos && len(state.selectedRepos) == 1 {
		return append([]huh.Option[string](nil), c.branchesByRepoSlug[state.selectedRepos[0]]...)
	}
	return nil
}

func buildDispatchWizardSummary(opts dispatchpkg.Options) string {
	scope := "current repo"
	switch {
	case strings.TrimSpace(opts.Org) != "":
		scope = "org:" + strings.TrimSpace(opts.Org)
	case len(opts.RepoPaths) > 0:
		scope = "repos:" + strings.Join(opts.RepoPaths, ", ")
	}

	branches := "current branch"
	if opts.AllBranches {
		branches = "all"
	} else if len(opts.Branches) > 0 {
		branches = strings.Join(opts.Branches, ", ")
	}

	mode := "server"
	if opts.Mode == dispatchpkg.ModeLocal {
		mode = "local"
	}

	return strings.Join([]string{
		"Mode: " + mode,
		"Scope: " + scope,
		"Branches: " + branches,
	}, "\n")
}

func buildDispatchCommand(opts dispatchpkg.Options) string {
	branchesValue := ""
	if opts.AllBranches {
		branchesValue = "all"
	} else if len(opts.Branches) > 0 {
		branchesValue = strings.Join(opts.Branches, ",")
	}

	return strings.Join(compactStrings([]string{
		"entire dispatch",
		mapBoolToFlag(opts.Mode == dispatchpkg.ModeLocal, "--local"),
		renderStringFlag("--since", strings.TrimSpace(opts.Since)),
		renderStringFlag("--branches", branchesValue),
		renderStringFlag("--repos", strings.Join(opts.RepoPaths, ",")),
		renderStringFlag("--org", strings.TrimSpace(opts.Org)),
		mapBoolToFlag(opts.Generate, "--generate"),
		renderStringFlag("--voice", strings.TrimSpace(opts.Voice)),
		renderStringFlag("--format", strings.TrimSpace(opts.Format)),
	}), " ")
}

func compactStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func mapBoolToFlag(enabled bool, flag string) string {
	if enabled {
		return flag
	}
	return ""
}

func renderStringFlag(name string, value string) string {
	if value == "" {
		return ""
	}
	return name + " " + quoteShellValue(value)
}

func quoteShellValue(value string) string {
	if value == "" {
		return `""`
	}
	if strings.ContainsAny(value, " ,:\t") {
		return fmt.Sprintf("%q", value)
	}
	return value
}

func runDispatchWizard(cmd *cobra.Command) (dispatchpkg.Options, error) {
	choices, err := discoverDispatchWizardChoices(cmd.Context())
	if err != nil {
		return dispatchpkg.Options{}, err
	}

	state := newDispatchWizardState()
	if len(choices.repoOptions) > 0 {
		state.selectedRepos = []string{choices.repoOptions[0].Value}
	}
	if len(choices.orgOptions) > 0 {
		state.selectedOrg = choices.orgOptions[0].Value
	}

	currentBranch := func() (string, error) {
		return GetCurrentBranch(cmd.Context())
	}
	if branch, branchErr := currentBranch(); branchErr == nil {
		state.applyCurrentBranchDefault(branch)
	}

	form := NewAccessibleForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Dispatch mode").
				Options(
					huh.NewOption("Local", dispatchWizardModeLocal),
					huh.NewOption("Server", dispatchWizardModeServer),
				).
				Value(&state.modeChoice),
		).Title("Mode").Description("Choose where the dispatch should run."),
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Scope").
				OptionsFunc(func() []huh.Option[string] {
					return choices.scopeOptions(state)
				}, &state).
				Value(&state.scopeType),
		).Title("Scope").Description("Choose which server-side scope to dispatch.").
			WithHideFunc(func() bool {
				return !state.showScopePicker(choices)
			}),
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Repos").
				Description("Select repos to include.").
				Filterable(true).
				OptionsFunc(func() []huh.Option[string] {
					return append([]huh.Option[string](nil), choices.repoOptions...)
				}, nil).
				Value(&state.selectedRepos).
				Validate(func(value []string) error {
					if state.effectiveScopeType(choices) == dispatchWizardScopeSelectedRepos && len(value) == 0 {
						return errors.New("select at least one repo")
					}
					return nil
				}),
		).Title("Repos").Description("Choose which repos to include.").
			WithHideFunc(func() bool {
				return !state.showRepoPicker(choices)
			}),
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Organization").
				Description("Select the organization to dispatch.").
				OptionsFunc(func() []huh.Option[string] {
					if len(choices.orgOptions) == 0 {
						return []huh.Option[string]{huh.NewOption("No organizations discovered", "")}
					}
					return append([]huh.Option[string](nil), choices.orgOptions...)
				}, nil).
				Value(&state.selectedOrg).
				Validate(func(value string) error {
					if state.effectiveScopeType(choices) == dispatchWizardScopeOrganization && strings.TrimSpace(value) == "" {
						return errors.New("select an organization")
					}
					return nil
				}),
		).Title("Organization").Description("Choose which organization to include.").
			WithHideFunc(func() bool {
				return !state.showOrganizationPicker(choices)
			}),
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Time window").
				Options(
					huh.NewOption("1 day", "1d"),
					huh.NewOption("7 days", "7d"),
					huh.NewOption("14 days", "14d"),
					huh.NewOption("30 days", "30d"),
				).
				Value(&state.timeWindowPreset),
		).Title("Window").Description("Choose the time window."),
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Branch mode").
				OptionsFunc(func() []huh.Option[string] {
					return choices.branchModeOptions(state)
				}, &state).
				Value(&state.branchMode),
		).Title("Branch mode").Description("Choose how server dispatch should interpret branches.").
			WithHideFunc(func() bool {
				return !state.showBranchModePicker()
			}),
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Branches").
				DescriptionFunc(func() string {
					if state.isLocal() {
						return "Select branches from the current repo."
					}
					return "Select branch overrides to apply across the selected repo."
				}, &state).
				Filterable(true).
				OptionsFunc(func() []huh.Option[string] {
					return choices.branchOptions(state)
				}, &state).
				Value(&state.selectedBranches).
				Validate(func(value []string) error {
					if state.showSelectedBranchesPicker(choices) && len(value) == 0 {
						return errors.New("select at least one branch")
					}
					return nil
				}),
		).Title("Branches").Description("Choose which branches to include.").
			WithHideFunc(func() bool {
				return !state.showSelectedBranchesPicker(choices)
			}),
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Voice").
				Options(
					huh.NewOption("Neutral", "neutral"),
					huh.NewOption("Marvin", "marvin"),
				).
				Value(&state.voicePreset),
			huh.NewSelect[string]().
				Title("Format").
				Options(
					huh.NewOption("Text", "text"),
					huh.NewOption("Markdown", "markdown"),
					huh.NewOption("JSON", "json"),
				).
				Value(&state.format),
		).Title("Output").Description("Choose output and rendering settings."),
		huh.NewGroup(
			huh.NewNote().
				Title("Resolved options").
				DescriptionFunc(func() string {
					opts, resolveErr := state.resolve(choices, currentBranch)
					if resolveErr != nil {
						return "Validation error: " + resolveErr.Error()
					}
					return buildDispatchWizardSummary(opts)
				}, &state),
			huh.NewNote().
				Title("Command").
				DescriptionFunc(func() string {
					opts, resolveErr := state.resolve(choices, currentBranch)
					if resolveErr != nil {
						return "Validation error: " + resolveErr.Error()
					}
					return buildDispatchCommand(opts)
				}, &state),
			huh.NewConfirm().
				Title("Run dispatch?").
				Affirmative("Run").
				Negative("Cancel").
				Value(&state.confirmRun),
		).Title("Confirm").Description("Review the resolved command and run it."),
	)

	if err := form.Run(); err != nil {
		if handled := handleFormCancellation(cmd.OutOrStdout(), "dispatch", err); handled == nil {
			return dispatchpkg.Options{}, errDispatchCancelled
		}
		return dispatchpkg.Options{}, err
	}
	if !state.confirmRun {
		fmt.Fprintln(cmd.OutOrStdout(), "dispatch cancelled.")
		return dispatchpkg.Options{}, errDispatchCancelled
	}

	return state.resolve(choices, currentBranch)
}

func discoverDispatchWizardChoices(ctx context.Context) (dispatchWizardChoices, error) {
	currentRepo, err := paths.WorktreeRoot(ctx)
	if err != nil {
		return dispatchWizardChoices{}, fmt.Errorf("not in a git repository: %w", err)
	}

	repoRoots := discoverLocalRepoRoots(ctx, currentRepo)
	repoOptions := make([]huh.Option[string], 0, len(repoRoots))
	branchesByRepoSlug := make(map[string][]huh.Option[string], len(repoRoots))
	currentRepoBranches := discoverBranchOptions(ctx, currentRepo)
	orgSet := make(map[string]struct{})
	seenRepoSlugs := make(map[string]struct{}, len(repoRoots))
	for _, repoRoot := range repoRoots {
		repoSlug := discoverRepoSlug(repoRoot)
		if repoSlug == "" {
			continue
		}
		if _, ok := seenRepoSlugs[repoSlug]; ok {
			continue
		}
		seenRepoSlugs[repoSlug] = struct{}{}
		repoOptions = append(repoOptions, huh.NewOption(repoSlug, repoSlug))
		branchesByRepoSlug[repoSlug] = discoverBranchOptions(ctx, repoRoot)
		if org := discoverRepoOrg(repoRoot); org != "" {
			orgSet[org] = struct{}{}
		}
	}

	orgNames := make([]string, 0, len(orgSet))
	for org := range orgSet {
		orgNames = append(orgNames, org)
	}
	sort.Strings(orgNames)

	orgOptions := make([]huh.Option[string], 0, len(orgNames))
	for _, org := range orgNames {
		orgOptions = append(orgOptions, huh.NewOption(org, org))
	}

	return dispatchWizardChoices{
		currentRepo:         currentRepo,
		currentRepoBranches: currentRepoBranches,
		repoOptions:         repoOptions,
		orgOptions:          orgOptions,
		branchesByRepoSlug:  branchesByRepoSlug,
	}, nil
}

func discoverLocalRepoRoots(ctx context.Context, currentRepo string) []string {
	rootSet := map[string]struct{}{currentRepo: {}}
	parent := filepath.Dir(currentRepo)

	entries, err := os.ReadDir(parent)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			candidate := filepath.Join(parent, entry.Name())
			repoRoot, resolveErr := resolveGitTopLevel(ctx, candidate)
			if resolveErr != nil {
				continue
			}
			rootSet[repoRoot] = struct{}{}
		}
	}

	repoRoots := make([]string, 0, len(rootSet))
	for repoRoot := range rootSet {
		repoRoots = append(repoRoots, repoRoot)
	}
	sort.Slice(repoRoots, func(i, j int) bool {
		if repoRoots[i] == currentRepo {
			return true
		}
		if repoRoots[j] == currentRepo {
			return false
		}
		return filepath.Base(repoRoots[i]) < filepath.Base(repoRoots[j])
	})
	return repoRoots
}

func resolveGitTopLevel(ctx context.Context, path string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func discoverBranchOptions(ctx context.Context, repoRoot string) []huh.Option[string] {
	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	branches := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			branches = append(branches, line)
		}
	}
	sort.Strings(branches)

	options := make([]huh.Option[string], 0, len(branches))
	for _, branch := range branches {
		options = append(options, huh.NewOption(branch, branch))
	}
	return options
}

func discoverRepoSlug(repoRoot string) string {
	repo, err := git.PlainOpenWithOptions(repoRoot, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return ""
	}
	remote, err := repo.Remote("origin")
	if err != nil || len(remote.Config().URLs) == 0 {
		return ""
	}
	owner, repoName, err := searchpkg.ParseGitHubRemote(remote.Config().URLs[0])
	if err != nil {
		return ""
	}
	return owner + "/" + repoName
}

func discoverRepoOrg(repoRoot string) string {
	repoSlug := discoverRepoSlug(repoRoot)
	if repoSlug == "" {
		return ""
	}
	owner, _, found := strings.Cut(repoSlug, "/")
	if !found {
		return ""
	}
	return owner
}
