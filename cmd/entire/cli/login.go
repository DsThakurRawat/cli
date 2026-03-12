package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"time"

	"github.com/entireio/cli/cmd/entire/cli/auth"
	"github.com/spf13/cobra"
)

const deviceAuthPollInterval = time.Second

var browserOpener = openBrowser

func newLoginCmd() *cobra.Command {
	var printBrowserURL bool

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in to Entire",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLogin(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), printBrowserURL)
		},
	}

	cmd.Flags().BoolVar(&printBrowserURL, "print-browser-url", false, "Print the approval URL instead of opening a browser")

	return cmd
}

func runLogin(ctx context.Context, outW, errW io.Writer, printBrowserURL bool) error {
	client := auth.NewClient(nil)

	start, err := client.StartDeviceAuth(ctx)
	if err != nil {
		return fmt.Errorf("start login: %w", err)
	}

	fmt.Fprintf(outW, "Device code: %s\n", start.UserCode)
	fmt.Fprintf(outW, "Approval URL: %s\n", start.BrowserURL)

	if printBrowserURL {
		fmt.Fprintln(outW, "Open the approval URL in your browser to continue.")
	} else {
		if err := browserOpener(ctx, start.BrowserURL); err != nil {
			fmt.Fprintf(errW, "Warning: failed to open browser automatically: %v\n", err)
			fmt.Fprintln(outW, "Open the approval URL in your browser to continue.")
		} else {
			fmt.Fprintln(outW, "Opened your browser for approval.")
		}
	}

	fmt.Fprintln(outW, "Waiting for approval...")

	token, err := waitForApproval(ctx, client, start.DeviceCode, start.ExpiresIn)
	if err != nil {
		return fmt.Errorf("complete login: %w", err)
	}

	store, err := auth.NewStore()
	if err != nil {
		return fmt.Errorf("create auth store: %w", err)
	}

	if err := store.SaveToken(client.BaseURL(), token); err != nil {
		return fmt.Errorf("save auth token: %w", err)
	}

	fmt.Fprintf(outW, "Login complete. Saved token to %s\n", store.FilePath())
	return nil
}

func waitForApproval(ctx context.Context, client *auth.Client, deviceCode string, expiresIn int) (string, error) {
	deadline := time.Now().Add(time.Duration(expiresIn) * time.Second)

	for {
		if time.Now().After(deadline) {
			return "", errors.New("device authorization expired")
		}

		result, err := client.PollDeviceAuth(ctx, deviceCode)
		if err != nil {
			return "", fmt.Errorf("poll approval status: %w", err)
		}

		switch result.Status {
		case "pending":
			// continue below
		case "complete":
			if result.Token == "" {
				return "", errors.New("device authorization completed without a token")
			}
			return result.Token, nil
		case "expired":
			return "", errors.New("device authorization expired")
		case "denied":
			if result.Error != "" {
				return "", fmt.Errorf("device authorization denied: %s", result.Error)
			}
			return "", errors.New("device authorization denied")
		default:
			return "", fmt.Errorf("unexpected device authorization status: %s", result.Status)
		}

		select {
		case <-ctx.Done():
			return "", fmt.Errorf("wait for approval: %w", ctx.Err())
		case <-time.After(deviceAuthPollInterval):
		}
	}
}

func openBrowser(ctx context.Context, browserURL string) error {
	var command string
	var args []string

	switch runtime.GOOS {
	case "darwin":
		command = "open"
		args = []string{browserURL}
	case "linux":
		command = "xdg-open"
		args = []string{browserURL}
	default:
		return fmt.Errorf("unsupported platform %s", runtime.GOOS)
	}

	cmd := exec.CommandContext(ctx, command, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start browser command %q: %w", command, err)
	}

	if err := cmd.Process.Release(); err != nil {
		return fmt.Errorf("release browser process: %w", err)
	}

	return nil
}
