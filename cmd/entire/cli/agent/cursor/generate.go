package cursor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

var cursorCommandRunner = exec.CommandContext

// GenerateText sends a prompt to the Cursor agent CLI and returns the raw text response.
func (c *CursorAgent) GenerateText(ctx context.Context, prompt string, model string) (string, error) {
	args := []string{"-p", prompt, "--force", "--trust", "--workspace", os.TempDir()}
	if model != "" {
		args = append(args, "--model", model)
	}

	cmd := cursorCommandRunner(ctx, "agent", args...)
	cmd.Dir = os.TempDir()
	cmd.Env = stripGitEnv(os.Environ())

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) {
			return "", fmt.Errorf("cursor CLI not found: %w", err)
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", fmt.Errorf("cursor CLI failed (exit %d): %s", exitErr.ExitCode(), stderr.String())
		}
		return "", fmt.Errorf("failed to run cursor CLI: %w", err)
	}

	return strings.TrimSpace(stdout.String()), nil
}

func stripGitEnv(env []string) []string {
	filtered := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, "GIT_") {
			filtered = append(filtered, e)
		}
	}
	return filtered
}
