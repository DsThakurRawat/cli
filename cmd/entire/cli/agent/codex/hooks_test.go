package codex

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInstallHooks_CreatesConfig(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)

	ag := &CodexAgent{}
	count, err := ag.InstallHooks(context.Background(), false, false)
	require.NoError(t, err)
	require.Equal(t, 3, count) // SessionStart, UserPromptSubmit, Stop

	// Verify hooks.json was created
	hooksPath := filepath.Join(tempDir, ".codex", HooksFileName)
	data, err := os.ReadFile(hooksPath)
	require.NoError(t, err)
	require.Contains(t, string(data), "entire hooks codex session-start")
	require.Contains(t, string(data), "entire hooks codex user-prompt-submit")
	require.Contains(t, string(data), "entire hooks codex stop")

	// Verify config.toml enables codex_hooks feature
	configPath := filepath.Join(tempDir, ".codex", configFileName)
	configData, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.Contains(t, string(configData), "codex_hooks = true")
	require.Contains(t, string(configData), "[features]")
}

func TestInstallHooks_Idempotent(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)

	ag := &CodexAgent{}

	// First install
	count1, err := ag.InstallHooks(context.Background(), false, false)
	require.NoError(t, err)
	require.Equal(t, 3, count1)

	// Second install should be idempotent
	count2, err := ag.InstallHooks(context.Background(), false, false)
	require.NoError(t, err)
	require.Equal(t, 0, count2)
}

func TestInstallHooks_LocalDev(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)

	ag := &CodexAgent{}
	count, err := ag.InstallHooks(context.Background(), true, false)
	require.NoError(t, err)
	require.Equal(t, 3, count)

	hooksPath := filepath.Join(tempDir, ".codex", HooksFileName)
	data, err := os.ReadFile(hooksPath)
	require.NoError(t, err)
	require.Contains(t, string(data), "go run ${CLAUDE_PROJECT_DIR}/cmd/entire/main.go hooks codex session-start")
}

func TestInstallHooks_Force(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)

	ag := &CodexAgent{}

	// Install first
	_, err := ag.InstallHooks(context.Background(), false, false)
	require.NoError(t, err)

	// Force reinstall removes old and adds new
	count, err := ag.InstallHooks(context.Background(), false, true)
	require.NoError(t, err)
	require.Equal(t, 3, count)
}

func TestUninstallHooks(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)

	ag := &CodexAgent{}

	// Install first
	_, err := ag.InstallHooks(context.Background(), false, false)
	require.NoError(t, err)

	// Uninstall
	err = ag.UninstallHooks(context.Background())
	require.NoError(t, err)

	// Verify hooks are gone
	require.False(t, ag.AreHooksInstalled(context.Background()))
}

func TestAreHooksInstalled_NoFile(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)

	ag := &CodexAgent{}
	require.False(t, ag.AreHooksInstalled(context.Background()))
}

func TestAreHooksInstalled_WithHooks(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)

	ag := &CodexAgent{}
	_, err := ag.InstallHooks(context.Background(), false, false)
	require.NoError(t, err)

	require.True(t, ag.AreHooksInstalled(context.Background()))
}

func TestInstallHooks_PreservesExistingConfig(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)

	ag := &CodexAgent{}

	// Write existing config with custom hooks
	codexDir := filepath.Join(tempDir, ".codex")
	require.NoError(t, os.MkdirAll(codexDir, 0o750))
	existingConfig := `{
		"hooks": {
			"PreToolUse": [
				{
					"matcher": "^Bash$",
					"hooks": [
						{"type": "command", "command": "my-custom-hook"}
					]
				}
			]
		}
	}`
	require.NoError(t, os.WriteFile(filepath.Join(codexDir, HooksFileName), []byte(existingConfig), 0o600))

	// Install Entire hooks
	_, err := ag.InstallHooks(context.Background(), false, false)
	require.NoError(t, err)

	// Verify custom hooks are preserved
	data, err := os.ReadFile(filepath.Join(codexDir, HooksFileName))
	require.NoError(t, err)
	require.Contains(t, string(data), "my-custom-hook")
	require.Contains(t, string(data), "entire hooks codex stop")
}
