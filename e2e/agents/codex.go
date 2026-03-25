package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

func init() {
	if env := os.Getenv("E2E_AGENT"); env != "" && env != "codex" {
		return
	}
	Register(&Codex{})
	RegisterGate("codex", 2)
}

// Codex implements the E2E Agent interface for OpenAI's Codex CLI.
type Codex struct{}

func (c *Codex) Name() string               { return "codex" }
func (c *Codex) Binary() string             { return "codex" }
func (c *Codex) EntireAgent() string        { return "codex" }
func (c *Codex) PromptPattern() string      { return `>` }
func (c *Codex) TimeoutMultiplier() float64 { return 1.5 }

func (c *Codex) Bootstrap() error {
	// On CI, ensure OPENAI_API_KEY is available.
	if os.Getenv("CI") == "" {
		return nil
	}
	if os.Getenv("OPENAI_API_KEY") == "" {
		return nil
	}
	return nil
}

func (c *Codex) IsTransientError(out Output, err error) bool {
	if err == nil {
		return false
	}
	combined := out.Stdout + out.Stderr
	for _, p := range []string{"overloaded", "rate limit", "rate_limit", "503", "529", "ECONNRESET", "ETIMEDOUT"} {
		if strings.Contains(combined, p) {
			return true
		}
	}
	return false
}

func (c *Codex) RunPrompt(ctx context.Context, dir string, prompt string, opts ...Option) (Output, error) {
	cfg := &runConfig{}
	for _, o := range opts {
		o(cfg)
	}

	args := []string{
		"exec",
		"--dangerously-bypass-approvals-and-sandbox",
		"--skip-git-repo-check",
	}
	if cfg.Model != "" {
		args = append(args, "-m", cfg.Model)
	}
	args = append(args, prompt)

	displayArgs := []string{
		"exec",
		"--dangerously-bypass-approvals-and-sandbox",
		"--skip-git-repo-check",
	}
	if cfg.Model != "" {
		displayArgs = append(displayArgs, "-m", cfg.Model)
	}
	displayArgs = append(displayArgs, fmt.Sprintf("%q", prompt))

	env := filterEnv(os.Environ(), "ENTIRE_TEST_TTY")

	cmd := exec.CommandContext(ctx, c.Binary(), args...)
	cmd.Dir = dir
	cmd.Stdin = nil
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 5 * time.Second

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		exitErr := &exec.ExitError{}
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	return Output{
		Command:  c.Binary() + " " + strings.Join(displayArgs, " "),
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}, err
}

func (c *Codex) StartSession(ctx context.Context, dir string) (Session, error) {
	name := fmt.Sprintf("codex-test-%d", time.Now().UnixNano())

	env := filterEnv(os.Environ(), "ENTIRE_TEST_TTY")

	s, err := NewTmuxSession(name, dir, nil, "env")
	if err != nil {
		return nil, err
	}

	// Set filtered env and launch codex interactively
	for _, e := range env {
		if strings.HasPrefix(e, "OPENAI_API_KEY=") || strings.HasPrefix(e, "CODEX_HOME=") {
			_ = s.Send("export " + e)
		}
	}

	_ = s.Send("codex --dangerously-bypass-approvals-and-sandbox")

	// Wait for the interactive prompt
	if _, err := s.WaitFor(c.PromptPattern(), 30*time.Second); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("waiting for codex prompt: %w", err)
	}

	return s, nil
}
