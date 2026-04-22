package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	dispatchpkg "github.com/entireio/cli/cmd/entire/cli/dispatch"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var runDispatch = dispatchpkg.Run
var renderDispatchMarkdown = dispatchpkg.RenderMarkdown
var dispatchTerminalMode = isTerminalWriter
var runInteractiveDispatch = defaultRunInteractiveDispatch
var renderTerminalMarkdown = defaultRenderTerminalMarkdown

func newDispatchCmd() *cobra.Command {
	var (
		flagLocal       bool
		flagSince       string
		flagUntil       string
		flagAllBranches bool
		flagRepos       []string
		flagOrgs        []string
		flagVoice       string
	)

	cmd := &cobra.Command{
		Use:    "dispatch",
		Short:  "Generate a dispatch summarizing recent agent work",
		Hidden: true,
		Long: `Generate a dispatch summarizing recent agent work.

Examples:
  entire dispatch
  entire dispatch --since 14d --all-branches
  entire dispatch --local
  entire dispatch --voice neutral`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var (
				opts dispatchpkg.Options
				err  error
			)

			if shouldRunDispatchWizard(cmd.Flags().NFlag(), isTerminalStdin(os.Stdin), isTerminalWriter(cmd.OutOrStdout())) {
				opts, err = runDispatchWizard(cmd)
			} else {
				opts, err = parseDispatchFlags(cmd, flagLocal, flagSince, flagUntil, flagAllBranches, flagRepos, flagOrgs, flagVoice)
			}
			if err != nil {
				if errors.Is(err, errDispatchCancelled) {
					return nil
				}
				return err
			}

			if err := runDispatchCommand(cmd.Context(), cmd.OutOrStdout(), opts); err != nil {
				if errors.Is(err, errDispatchCancelled) {
					return nil
				}
				return err
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&flagLocal, "local", false, "use local LLM tokens instead of server synthesis")
	cmd.Flags().StringVar(&flagSince, "since", "7d", "time window (Go duration, relative time, or ISO date)")
	cmd.Flags().StringVar(&flagUntil, "until", "", "window end time (defaults to now)")
	cmd.Flags().BoolVar(&flagAllBranches, "all-branches", false, "include all branches instead of the default branch scope")
	cmd.Flags().StringSliceVar(&flagRepos, "repos", nil, "server repo slugs (for example entireio/cli)")
	cmd.Flags().StringSliceVar(&flagOrgs, "org", nil, "enumerate checkpoints across one or more orgs")
	cmd.Flags().StringVar(&flagVoice, "voice", "", "voice preset name or literal description")

	return cmd
}

func runDispatchCommand(ctx context.Context, outW io.Writer, opts dispatchpkg.Options) error {
	if dispatchTerminalMode(outW) {
		markdown, err := runInteractiveDispatch(ctx, outW, opts)
		if err != nil {
			return err
		}
		rendered, err := renderTerminalMarkdown(outW, markdown)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprint(outW, rendered); err != nil {
			return fmt.Errorf("write dispatch output: %w", err)
		}
		return nil
	}

	result, err := runDispatch(ctx, opts)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprint(outW, renderDispatchMarkdown(result)); err != nil {
		return fmt.Errorf("write dispatch output: %w", err)
	}
	return nil
}

func isTerminalStdin(file *os.File) bool {
	return term.IsTerminal(int(file.Fd())) //nolint:gosec // G115: uintptr->int is safe for fd
}

func shouldRunDispatchWizard(flagCount int, stdinIsTerminal bool, stdoutIsTerminal bool) bool {
	return flagCount == 0 && stdinIsTerminal && stdoutIsTerminal
}

func parseDispatchFlags(
	cmd *cobra.Command,
	flagLocal bool,
	flagSince string,
	flagUntil string,
	flagAllBranches bool,
	flagRepos []string,
	flagOrgs []string,
	flagVoice string,
) (dispatchpkg.Options, error) {
	return resolveDispatchOptions(
		flagLocal,
		flagSince,
		flagUntil,
		flagAllBranches,
		flagRepos,
		flagOrgs,
		flagVoice,
		func() (string, error) {
			return GetCurrentBranch(cmd.Context())
		},
	)
}

func resolveDispatchOptions(
	flagLocal bool,
	flagSince string,
	flagUntil string,
	flagAllBranches bool,
	flagRepos []string,
	flagOrgs []string,
	flagVoice string,
	currentBranch func() (string, error),
) (dispatchpkg.Options, error) {
	flagRepos = normalizeDispatchScopeValues(flagRepos)
	flagOrgs = normalizeDispatchScopeValues(flagOrgs)
	if len(flagOrgs) > 0 && len(flagRepos) > 0 {
		return dispatchpkg.Options{}, errors.New("--org and --repos are mutually exclusive")
	}
	if flagLocal && len(flagRepos) > 0 {
		return dispatchpkg.Options{}, errors.New("--repos cannot be used with --local")
	}
	if flagLocal && len(flagOrgs) > 0 {
		return dispatchpkg.Options{}, errors.New("--org cannot be used with --local")
	}

	mode := dispatchpkg.ModeServer
	if flagLocal {
		mode = dispatchpkg.ModeLocal
	}

	var branches []string
	allBranches := flagAllBranches
	implicitCurrentBranch := false
	switch {
	case allBranches:
	case len(flagRepos) > 0, len(flagOrgs) > 0:
		branches = nil
	default:
		currentBranchName, branchErr := currentBranch()
		if branchErr != nil {
			return dispatchpkg.Options{}, branchErr
		}
		branches = []string{currentBranchName}
		implicitCurrentBranch = true
	}

	return dispatchpkg.Options{
		Mode:                  mode,
		RepoPaths:             append([]string(nil), flagRepos...),
		Orgs:                  append([]string(nil), flagOrgs...),
		Since:                 flagSince,
		Until:                 flagUntil,
		Branches:              branches,
		AllBranches:           allBranches,
		ImplicitCurrentBranch: implicitCurrentBranch,
		Voice:                 flagVoice,
	}, nil
}

func normalizeDispatchScopeValues(values []string) []string {
	normalized := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	return normalized
}
