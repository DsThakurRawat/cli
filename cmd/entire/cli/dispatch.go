package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	dispatchpkg "github.com/entireio/cli/cmd/entire/cli/dispatch"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const dispatchWizardGenerateDefault = true

func newDispatchCmd() *cobra.Command {
	var (
		flagLocal    bool
		flagSince    string
		flagUntil    string
		flagBranches string
		flagRepos    []string
		flagOrg      string
		flagGenerate bool
		flagVoice    string
		flagFormat   string
	)

	cmd := &cobra.Command{
		Use:   "dispatch",
		Short: "Generate a dispatch summarizing recent agent work",
		Long: `Generate a dispatch summarizing recent agent work.

Examples:
  entire dispatch
  entire dispatch --since 14d --branches main,release
  entire dispatch --local --repos ~/Projects/cli
  entire dispatch --format markdown
  entire dispatch --generate --voice neutral`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var (
				opts dispatchpkg.Options
				err  error
			)

			if shouldRunDispatchWizard(cmd.Flags().NFlag(), isTerminalStdin(os.Stdin), isTerminalWriter(cmd.OutOrStdout())) {
				opts, err = runDispatchWizard(cmd)
			} else {
				opts, err = parseDispatchFlags(cmd, flagLocal, flagSince, flagUntil, flagBranches, flagRepos, flagOrg, flagGenerate, flagVoice, flagFormat)
			}
			if err != nil {
				if errors.Is(err, errDispatchCancelled) {
					return nil
				}
				return err
			}

			result, err := dispatchpkg.Run(cmd.Context(), opts)
			if err != nil {
				return err
			}

			rendered, err := dispatchpkg.Render(opts.Format, result)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), rendered)
			return nil
		},
	}

	cmd.Flags().BoolVar(&flagLocal, "local", false, "use local LLM tokens instead of server synthesis")
	cmd.Flags().StringVar(&flagSince, "since", "7d", "time window (Go duration, relative time, or ISO date)")
	cmd.Flags().StringVar(&flagUntil, "until", "", "window end time (defaults to now)")
	cmd.Flags().StringVar(&flagBranches, "branches", "", "comma-separated branch names, or 'all'")
	cmd.Flags().StringSliceVar(&flagRepos, "repos", nil, "server repo slugs (for example entireio/cli)")
	cmd.Flags().StringVar(&flagOrg, "org", "", "enumerate checkpoints across an org")
	cmd.Flags().BoolVar(&flagGenerate, "generate", false, "synthesize prose from dispatch bullets")
	cmd.Flags().StringVar(&flagVoice, "voice", "", "voice preset name, file path, or literal description")
	cmd.Flags().StringVar(&flagFormat, "format", "text", "output format: text | markdown | json")

	return cmd
}

func isTerminalStdin(file *os.File) bool {
	return term.IsTerminal(int(file.Fd()))
}

func shouldRunDispatchWizard(flagCount int, stdinIsTerminal bool, stdoutIsTerminal bool) bool {
	return flagCount == 0 && stdinIsTerminal && stdoutIsTerminal
}

func parseDispatchFlags(
	cmd *cobra.Command,
	flagLocal bool,
	flagSince string,
	flagUntil string,
	flagBranches string,
	flagRepos []string,
	flagOrg string,
	flagGenerate bool,
	flagVoice string,
	flagFormat string,
) (dispatchpkg.Options, error) {
	return resolveDispatchOptions(
		flagLocal,
		flagSince,
		flagUntil,
		flagBranches,
		flagRepos,
		flagOrg,
		flagGenerate,
		flagVoice,
		flagFormat,
		func() (string, error) {
			return GetCurrentBranch(cmd.Context())
		},
	)
}

func resolveDispatchOptions(
	flagLocal bool,
	flagSince string,
	flagUntil string,
	flagBranches string,
	flagRepos []string,
	flagOrg string,
	flagGenerate bool,
	flagVoice string,
	flagFormat string,
	currentBranch func() (string, error),
) (dispatchpkg.Options, error) {
	if flagOrg != "" && len(flagRepos) > 0 {
		return dispatchpkg.Options{}, errors.New("--org and --repos are mutually exclusive")
	}
	if flagLocal && len(flagRepos) > 0 {
		return dispatchpkg.Options{}, errors.New("--repos cannot be used with --local")
	}

	format := strings.ToLower(strings.TrimSpace(flagFormat))
	switch format {
	case "", "text":
		format = "text"
	case "markdown", "json":
	default:
		return dispatchpkg.Options{}, fmt.Errorf("unsupported format %q", flagFormat)
	}

	mode := dispatchpkg.ModeServer
	if flagLocal {
		mode = dispatchpkg.ModeLocal
	}

	branches, allBranches, err := dispatchpkg.ParseBranches(flagBranches, "")
	if err != nil {
		return dispatchpkg.Options{}, err
	}
	implicitCurrentBranch := false
	if strings.TrimSpace(flagBranches) == "" && !allBranches {
		if len(flagRepos) > 0 {
			branches = nil
		} else if strings.TrimSpace(flagOrg) != "" {
			branches = nil
			allBranches = true
		} else {
			currentBranchName, branchErr := currentBranch()
			if branchErr != nil {
				return dispatchpkg.Options{}, branchErr
			}
			branches = []string{currentBranchName}
			implicitCurrentBranch = true
		}
	}

	return dispatchpkg.Options{
		Mode:                  mode,
		RepoPaths:             append([]string(nil), flagRepos...),
		Org:                   flagOrg,
		Since:                 flagSince,
		Until:                 flagUntil,
		Branches:              branches,
		AllBranches:           allBranches,
		ImplicitCurrentBranch: implicitCurrentBranch,
		Generate:              flagGenerate,
		Voice:                 flagVoice,
		Format:                format,
	}, nil
}
