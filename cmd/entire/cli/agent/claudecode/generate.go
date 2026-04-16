package claudecode

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// GenerateText sends a prompt to the Claude CLI and returns the raw text response.
// Implements the agent.TextGenerator interface.
// The model parameter hints which model to use (e.g., "haiku", "sonnet").
// If empty, defaults to "haiku" for fast, cheap generation.
func (c *ClaudeCodeAgent) GenerateText(ctx context.Context, prompt string, model string) (string, error) {
	claudePath := "claude"
	if model == "" {
		model = "haiku"
	}

	commandRunner := c.CommandRunner
	if commandRunner == nil {
		commandRunner = exec.CommandContext
	}

	cmd := commandRunner(ctx, claudePath,
		"--print", "--output-format", "json",
		"--model", model, "--setting-sources", "")

	// Isolate from the user's git repo to prevent recursive hook triggers
	// and index pollution (same approach as summarize/claude.go).
	cmd.Dir = os.TempDir()
	cmd.Env = stripGitEnv(os.Environ())
	cmd.Stdin = strings.NewReader(prompt)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", context.DeadlineExceeded
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			return "", context.Canceled
		}
		if isExecNotFound(err) {
			return "", &ClaudeError{Kind: ClaudeErrorCLIMissing, Cause: err}
		}
		// Non-zero exit: try to parse stdout for a structured error envelope,
		// then fall back to stderr classification.
		if _, env, parseErr := parseGenerateTextResponse(stdout.Bytes()); parseErr == nil && env != nil && env.IsError {
			result := ""
			if env.Result != nil {
				result = *env.Result
			}
			exitCode := 0
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				exitCode = exitErr.ExitCode()
			}
			return "", classifyEnvelopeError(result, env.APIErrorStatus, exitCode)
		}
		exitCode := -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
		return "", classifyStderrError(stderr.String(), exitCode)
	}

	// Exit 0: parse the response and check for is_error (Claude CLI returns
	// most operational errors as exit 0 with is_error:true in the envelope).
	result, env, err := parseGenerateTextResponse(stdout.Bytes())
	if err != nil {
		return "", &ClaudeError{Kind: ClaudeErrorUnknown, Message: fmt.Sprintf("failed to parse claude CLI response: %v", err), Cause: err}
	}
	if env != nil && env.IsError {
		return "", classifyEnvelopeError(result, env.APIErrorStatus, 0)
	}

	return result, nil
}

// isExecNotFound returns true if err indicates the subprocess binary could not be found.
// Covers both PATH-based lookups (*exec.Error / exec.ErrNotFound) and absolute-path
// failures (*fs.PathError wrapping os.ErrNotExist on macOS/Linux).
func isExecNotFound(err error) bool {
	var execErr *exec.Error
	if errors.As(err, &execErr) {
		return true
	}
	return errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist)
}

// stripGitEnv returns a copy of env with all GIT_* variables removed.
// This prevents a subprocess from discovering or modifying the parent's git repo.
// Duplicated from summarize/claude.go — simple filter not worth extracting to shared package.
func stripGitEnv(env []string) []string {
	filtered := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, "GIT_") {
			filtered = append(filtered, e)
		}
	}
	return filtered
}
