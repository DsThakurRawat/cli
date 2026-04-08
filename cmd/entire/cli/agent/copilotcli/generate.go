package copilotcli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

var copilotCommandRunner = exec.CommandContext

// GenerateText sends a prompt to the Copilot CLI and returns the raw text response.
func (c *CopilotCLIAgent) GenerateText(ctx context.Context, prompt string, model string) (string, error) {
	args := []string{"-p", prompt, "--allow-all-tools"}
	if model != "" {
		args = append(args, "--model", model)
	}

	cmd := copilotCommandRunner(ctx, "copilot", args...)
	cmd.Dir = os.TempDir()
	cmd.Env = stripGitEnv(os.Environ())

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) {
			return "", fmt.Errorf("copilot CLI not found: %w", err)
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", fmt.Errorf("copilot CLI failed (exit %d): %s", exitErr.ExitCode(), stderr.String())
		}
		return "", fmt.Errorf("failed to run copilot CLI: %w", err)
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
