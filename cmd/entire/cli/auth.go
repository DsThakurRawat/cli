package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/entireio/cli/cmd/entire/cli/api"
	"github.com/entireio/cli/cmd/entire/cli/auth"
	"github.com/spf13/cobra"
)

// authStatusStore reads the current bearer token from the OS keyring.
type authStatusStore interface {
	GetToken(baseURL string) (string, error)
}

// authTokenLister lists API tokens for the authenticated user.
type authTokenLister func(ctx context.Context, token string) ([]api.Token, error)

// authTokenRevoker revokes a single API token by id.
type authTokenRevoker func(ctx context.Context, callerToken, id string) error

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage authentication and API tokens",
		Long:  "Authentication subcommands. Includes login, logout, status, listing tokens, and revoking tokens.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newLoginCmd())
	cmd.AddCommand(newLogoutCmd())
	cmd.AddCommand(newAuthStatusCmd())
	cmd.AddCommand(newAuthListCmd())
	cmd.AddCommand(newAuthRevokeCmd())
	return cmd
}

// --- status -----------------------------------------------------------------

func newAuthStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show authentication status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAuthStatus(cmd.Context(), cmd.OutOrStdout(),
				auth.NewStore(), defaultListTokens, api.BaseURL())
		},
	}
}

func defaultListTokens(ctx context.Context, token string) ([]api.Token, error) {
	tokens, err := api.NewClient(token).ListTokens(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	return tokens, nil
}

func runAuthStatus(ctx context.Context, w io.Writer, store authStatusStore, list authTokenLister, baseURL string) error {
	token, err := store.GetToken(baseURL)
	if err != nil {
		return fmt.Errorf("read keychain: %w", err)
	}
	if token == "" {
		fmt.Fprintf(w, "Not logged in to %s\n", baseURL)
		fmt.Fprintln(w, "Run 'entire login' to authenticate.")
		return nil
	}

	tokens, err := list(ctx, token)
	if err != nil {
		if api.IsHTTPErrorStatus(err, http.StatusUnauthorized) {
			fmt.Fprintf(w, "Token in keychain for %s is no longer valid.\n", baseURL)
			fmt.Fprintln(w, "Run 'entire login' to re-authenticate.")
			return nil
		}
		return fmt.Errorf("validate token: %w", err)
	}

	fmt.Fprintf(w, "Logged in to %s\n", baseURL)
	fmt.Fprintln(w, "  Token: stored in OS keychain")
	fmt.Fprintf(w, "  Active tokens on this account: %d\n", len(tokens))
	return nil
}

// --- list -------------------------------------------------------------------

func newAuthListCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List active API tokens for the authenticated user",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAuthList(cmd.Context(), cmd.OutOrStdout(),
				auth.NewStore(), defaultListTokens, api.BaseURL(), jsonOut)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print tokens as JSON")
	return cmd
}

func runAuthList(ctx context.Context, w io.Writer, store authStatusStore, list authTokenLister, baseURL string, jsonOut bool) error {
	token, err := store.GetToken(baseURL)
	if err != nil {
		return fmt.Errorf("read keychain: %w", err)
	}
	if token == "" {
		return fmt.Errorf("not logged in to %s; run 'entire login' first", baseURL)
	}

	tokens, err := list(ctx, token)
	if err != nil {
		return err
	}

	if jsonOut {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(tokens); err != nil {
			return fmt.Errorf("encode JSON: %w", err)
		}
		return nil
	}

	if len(tokens) == 0 {
		fmt.Fprintln(w, "No active tokens.")
		return nil
	}

	// Stable order: most recently used first, then created.
	sort.Slice(tokens, func(i, j int) bool {
		li := lastUsedSortKey(tokens[i])
		lj := lastUsedSortKey(tokens[j])
		if li != lj {
			return li > lj
		}
		return tokens[i].CreatedAt > tokens[j].CreatedAt
	})

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tSCOPE\tCREATED\tLAST USED\tEXPIRES")
	for _, t := range tokens {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			t.ID,
			fallback(t.Name, "-"),
			fallback(t.Scope, "-"),
			formatDate(t.CreatedAt),
			formatLastUsed(t.LastUsedAt),
			formatDate(t.ExpiresAt),
		)
	}
	return tw.Flush() //nolint:wrapcheck // tabwriter flush error is rare and self-explanatory
}

func lastUsedSortKey(t api.Token) string {
	if t.LastUsedAt == nil {
		return ""
	}
	return *t.LastUsedAt
}

func formatLastUsed(s *string) string {
	if s == nil || *s == "" {
		return "never"
	}
	return formatDate(*s)
}

func formatDate(s string) string {
	if s == "" {
		return "-"
	}
	if ts, err := time.Parse(time.RFC3339, s); err == nil {
		return ts.UTC().Format("2006-01-02 15:04")
	}
	return s
}

func fallback(s, alt string) string {
	if strings.TrimSpace(s) == "" {
		return alt
	}
	return s
}

// --- revoke -----------------------------------------------------------------

func newAuthRevokeCmd() *cobra.Command {
	var revokeCurrent bool
	cmd := &cobra.Command{
		Use:   "revoke [id]",
		Short: "Revoke an API token by id",
		Long:  "Revoke a specific API token. Use --current to revoke the token used by this CLI (equivalent to 'entire logout').",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := ""
			if len(args) == 1 {
				id = args[0]
			}
			if id == "" && !revokeCurrent {
				return cmd.Help()
			}
			if id != "" && revokeCurrent {
				return errors.New("cannot use both <id> and --current")
			}
			return runAuthRevoke(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(),
				auth.NewStore(), defaultRevokeTokenByID, defaultRevokeCurrentToken,
				api.BaseURL(), id, revokeCurrent)
		},
	}
	cmd.Flags().BoolVar(&revokeCurrent, "current", false, "Revoke the token used by this CLI and remove the local copy")
	return cmd
}

func defaultRevokeTokenByID(ctx context.Context, callerToken, id string) error {
	if err := api.NewClient(callerToken).RevokeToken(ctx, id); err != nil {
		return fmt.Errorf("revoke token: %w", err)
	}
	return nil
}

func runAuthRevoke(
	ctx context.Context,
	outW, errW io.Writer,
	store logoutTokenStore,
	revokeByID authTokenRevoker,
	revokeCurrent logoutRevokeFunc,
	baseURL, id string,
	current bool,
) error {
	token, err := store.GetToken(baseURL)
	if err != nil {
		return fmt.Errorf("read keychain: %w", err)
	}
	if token == "" {
		return fmt.Errorf("not logged in to %s; run 'entire login' first", baseURL)
	}

	if current {
		// Revoking our own token is just logout — reuse that path so behavior
		// stays identical (best-effort revoke + local delete).
		return runLogout(ctx, outW, errW, store, revokeCurrent, baseURL)
	}

	if err := revokeByID(ctx, token, id); err != nil {
		return err
	}
	fmt.Fprintf(outW, "Revoked token %s.\n", id)
	return nil
}
