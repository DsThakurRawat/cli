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
	"time"

	"github.com/charmbracelet/lipgloss"
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

	sty := newAuthListStyles(w)
	renderAuthListTable(w, sty, tokens, time.Now())
	return nil
}

// authListStyles holds the lipgloss styles for `entire auth list`. Mirrors the
// approach in activity_render.go: keep style construction tied to color
// detection, and render plain text when color is disabled.
type authListStyles struct {
	colorEnabled bool

	header  lipgloss.Style // bold + dim, used for column headers
	id      lipgloss.Style // dim, like commit hash
	name    lipgloss.Style // bold
	muted   lipgloss.Style // scope, dates
	dim     lipgloss.Style // "never", "-"
	warning lipgloss.Style // expires-soon
	expired lipgloss.Style // already expired
}

func newAuthListStyles(w io.Writer) authListStyles {
	useColor := shouldUseColor(w)
	s := authListStyles{colorEnabled: useColor}
	if !useColor {
		return s
	}
	s.header = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Bold(true)
	s.id = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	s.name = lipgloss.NewStyle().Bold(true)
	s.muted = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	s.dim = lipgloss.NewStyle().Faint(true)
	s.warning = lipgloss.NewStyle().Foreground(lipgloss.Color("3")) // yellow
	s.expired = lipgloss.NewStyle().Foreground(lipgloss.Color("1")) // red
	return s
}

func (s authListStyles) render(style lipgloss.Style, text string) string {
	if !s.colorEnabled {
		return text
	}
	return style.Render(text)
}

// renderAuthListTable prints a styled, column-aligned table of tokens. We
// compute column widths from plain text and pad with spaces using
// lipgloss.Width — tabwriter doesn't understand ANSI escapes so it can't be
// used here once cells are styled.
func renderAuthListTable(w io.Writer, sty authListStyles, tokens []api.Token, now time.Time) {
	headers := []string{"ID", "NAME", "SCOPE", "CREATED", "LAST USED", "EXPIRES"}

	type cell struct {
		styled string // rendered (may contain ANSI)
		plain  string // plain text used for width calculation
	}

	rows := make([][]cell, 0, len(tokens))
	for _, t := range tokens {
		idPlain := t.ID
		namePlain := fallback(t.Name, "-")
		scopePlain := fallback(t.Scope, "-")
		createdPlain := formatAuthDate(t.CreatedAt)
		lastUsedPlain := formatAuthLastUsed(t.LastUsedAt, now)
		expiresPlain := formatAuthDate(t.ExpiresAt)

		var nameStyled string
		if t.Name != "" {
			nameStyled = sty.render(sty.name, namePlain)
		} else {
			nameStyled = sty.render(sty.dim, namePlain)
		}
		var lastUsedStyled string
		if t.LastUsedAt == nil {
			lastUsedStyled = sty.render(sty.dim, lastUsedPlain)
		} else {
			lastUsedStyled = sty.render(sty.muted, lastUsedPlain)
		}
		var expiresStyled string
		switch expiresState(t.ExpiresAt, now) {
		case expiresStateExpired:
			expiresStyled = sty.render(sty.expired, expiresPlain)
		case expiresStateSoon:
			expiresStyled = sty.render(sty.warning, expiresPlain)
		case expiresStateNormal:
			expiresStyled = sty.render(sty.muted, expiresPlain)
		}

		rows = append(rows, []cell{
			{styled: sty.render(sty.id, idPlain), plain: idPlain},
			{styled: nameStyled, plain: namePlain},
			{styled: sty.render(sty.muted, scopePlain), plain: scopePlain},
			{styled: sty.render(sty.muted, createdPlain), plain: createdPlain},
			{styled: lastUsedStyled, plain: lastUsedPlain},
			{styled: expiresStyled, plain: expiresPlain},
		})
	}

	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = lipgloss.Width(h)
	}
	for _, row := range rows {
		for i, c := range row {
			if width := lipgloss.Width(c.plain); width > widths[i] {
				widths[i] = width
			}
		}
	}

	// Header row.
	for i, h := range headers {
		fmt.Fprint(w, sty.render(sty.header, h))
		if i < len(headers)-1 {
			fmt.Fprint(w, strings.Repeat(" ", widths[i]-lipgloss.Width(h)+2))
		}
	}
	fmt.Fprintln(w)

	// Body rows.
	for _, row := range rows {
		for i, c := range row {
			fmt.Fprint(w, c.styled)
			if i < len(row)-1 {
				fmt.Fprint(w, strings.Repeat(" ", widths[i]-lipgloss.Width(c.plain)+2))
			}
		}
		fmt.Fprintln(w)
	}
}

func lastUsedSortKey(t api.Token) string {
	if t.LastUsedAt == nil {
		return ""
	}
	return *t.LastUsedAt
}

// formatAuthDate renders an RFC3339 timestamp as YYYY-MM-DD. Uses local time so
// "today" / "yesterday" feel right; tokens are user-scoped so user TZ wins.
func formatAuthDate(s string) string {
	if s == "" {
		return "-"
	}
	if ts, err := time.Parse(time.RFC3339, s); err == nil {
		return ts.Local().Format("2006-01-02")
	}
	return s
}

// formatAuthLastUsed renders a relative "last used" timestamp. Mirrors the
// commit-list relative-date convention: today → "today", yesterday →
// "yesterday", recent → "Xh/Xd ago", older → absolute date. nil → "never".
func formatAuthLastUsed(s *string, now time.Time) string {
	if s == nil || *s == "" {
		return "never"
	}
	ts, err := time.Parse(time.RFC3339, *s)
	if err != nil {
		return *s
	}
	delta := now.Sub(ts)
	switch {
	case delta < 0:
		return ts.Local().Format("2006-01-02")
	case delta < time.Minute:
		return "just now"
	case delta < time.Hour:
		return fmt.Sprintf("%dm ago", int(delta.Minutes()))
	case delta < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(delta.Hours()))
	case delta < 48*time.Hour:
		return "yesterday"
	case delta < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(delta.Hours()/24))
	default:
		return ts.Local().Format("2006-01-02")
	}
}

type expiresStateValue int

const (
	expiresStateNormal expiresStateValue = iota
	expiresStateSoon
	expiresStateExpired
)

// expiresState classifies an RFC3339 expires-at relative to now: expired
// (already past), soon (within 7 days), normal (otherwise). Used to color the
// EXPIRES column so users can spot tokens worth rotating at a glance.
func expiresState(s string, now time.Time) expiresStateValue {
	if s == "" {
		return expiresStateNormal
	}
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return expiresStateNormal
	}
	delta := ts.Sub(now)
	switch {
	case delta <= 0:
		return expiresStateExpired
	case delta < 7*24*time.Hour:
		return expiresStateSoon
	default:
		return expiresStateNormal
	}
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
