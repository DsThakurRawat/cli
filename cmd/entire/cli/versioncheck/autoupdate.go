package versioncheck

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"

	"github.com/charmbracelet/huh"

	"github.com/entireio/cli/cmd/entire/cli/interactive"
	"github.com/entireio/cli/cmd/entire/cli/logging"
)

// envKillSwitch disables the interactive update prompt regardless of TTY.
const envKillSwitch = "ENTIRE_NO_AUTO_UPDATE"

// Test seams.
var (
	runInstaller  = realRunInstaller
	confirmUpdate = realConfirmUpdate
)

// MaybeAutoUpdate offers an interactive upgrade after the standard
// "version available" notification has been printed. Silent on every
// failure path — it must never interrupt the CLI. Returns true iff the
// installer command ran and failed, so the caller can invalidate the
// version-check cache and re-prompt on the next invocation.
func MaybeAutoUpdate(ctx context.Context, w io.Writer, currentVersion string) (installerFailed bool) {
	if os.Getenv(envKillSwitch) != "" {
		return false
	}
	if !interactive.CanPromptInteractively() {
		return false
	}

	confirmed, err := confirmUpdate()
	if err != nil {
		logging.Debug(ctx, "auto-update: prompt failed", "error", err.Error())
		return false
	}
	if !confirmed {
		return false
	}

	cmdStr := updateCommand(currentVersion)
	fmt.Fprintf(w, "\nUpdating Entire CLI: %s\n", cmdStr)
	if err := runInstaller(ctx, cmdStr); err != nil {
		fmt.Fprintf(w, "Update failed: %v\n", err)
		return true
	}
	fmt.Fprintln(w, "Update complete. Re-run entire to use the new version.")
	return false
}

func realConfirmUpdate() (bool, error) {
	// Pre-select "Yes" so pressing Enter accepts — matches the (Y/n) UX.
	confirmed := true
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Install the new version now?").
				Affirmative("Yes").
				Negative("No").
				Value(&confirmed),
		),
	).WithTheme(huh.ThemeDracula())
	if os.Getenv("ACCESSIBLE") != "" {
		form = form.WithAccessible(true)
	}
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) || errors.Is(err, huh.ErrTimeout) {
			return false, nil
		}
		return false, fmt.Errorf("confirm form: %w", err)
	}
	return confirmed, nil
}

// realRunInstaller shells out to the installer command, streaming stdin/stdout/stderr
// so password prompts and progress output reach the user.
func realRunInstaller(ctx context.Context, cmdStr string) error {
	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		c = exec.CommandContext(ctx, "cmd", "/C", cmdStr)
	} else {
		c = exec.CommandContext(ctx, "sh", "-c", cmdStr)
	}
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("installer exited: %w", err)
	}
	return nil
}
