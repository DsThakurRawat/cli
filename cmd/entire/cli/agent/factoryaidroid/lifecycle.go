package factoryaidroid

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/entireio/cli/cmd/entire/cli/agent"
	"github.com/entireio/cli/cmd/entire/cli/logging"
	"github.com/entireio/cli/cmd/entire/cli/textutil"
	"github.com/entireio/cli/cmd/entire/cli/transcript"
)

// Compile-time interface assertions.
var (
	_ agent.TranscriptAnalyzer     = (*FactoryAIDroidAgent)(nil)
	_ agent.TranscriptPreparer     = (*FactoryAIDroidAgent)(nil)
	_ agent.TokenCalculator        = (*FactoryAIDroidAgent)(nil)
	_ agent.SubagentAwareExtractor = (*FactoryAIDroidAgent)(nil)
)

// HookNames returns the hook verbs Factory AI Droid supports.
func (f *FactoryAIDroidAgent) HookNames() []string {
	return f.GetHookNames()
}

// ParseHookEvent translates a Factory AI Droid hook into a normalized lifecycle Event.
// Returns nil if the hook has no lifecycle significance.
func (f *FactoryAIDroidAgent) ParseHookEvent(hookName string, stdin io.Reader) (*agent.Event, error) {
	switch hookName {
	case HookNameSessionStart:
		return f.parseSessionStart(stdin)
	case HookNameUserPromptSubmit:
		return f.parseTurnStart(stdin)
	case HookNameStop:
		return f.parseTurnEnd(stdin)
	case HookNameSessionEnd:
		return f.parseSessionEnd(stdin)
	case HookNamePreToolUse:
		return f.parseSubagentStart(stdin)
	case HookNamePostToolUse:
		return f.parseSubagentEnd(stdin)
	case HookNamePreCompact:
		return f.parseCompaction(stdin)
	case HookNameSubagentStop, HookNameNotification:
		// Acknowledged hooks with no lifecycle action
		return nil, nil //nolint:nilnil // nil event = no lifecycle action
	default:
		return nil, nil //nolint:nilnil // Unknown hooks have no lifecycle action
	}
}

// --- TranscriptAnalyzer ---

// GetTranscriptPosition returns the current line count of the JSONL transcript.
func (f *FactoryAIDroidAgent) GetTranscriptPosition(path string) (int, error) {
	_, pos, err := transcript.ParseFromFileAtLine(path, 0)
	if err != nil {
		return 0, err //nolint:wrapcheck // caller adds context
	}
	return pos, nil
}

// ExtractModifiedFilesFromOffset extracts files modified since a given line offset.
func (f *FactoryAIDroidAgent) ExtractModifiedFilesFromOffset(path string, startOffset int) ([]string, int, error) {
	lines, currentPos, err := transcript.ParseFromFileAtLine(path, startOffset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to parse transcript: %w", err)
	}
	files := ExtractModifiedFiles(lines)
	return files, currentPos, nil
}

// ExtractPrompts extracts user prompts from the transcript starting at the given line offset.
func (f *FactoryAIDroidAgent) ExtractPrompts(sessionRef string, fromOffset int) ([]string, error) {
	lines, _, err := transcript.ParseFromFileAtLine(sessionRef, fromOffset)
	if err != nil {
		return nil, fmt.Errorf("failed to parse transcript: %w", err)
	}

	var prompts []string
	for i := range lines {
		if lines[i].Type != transcript.TypeUser {
			continue
		}
		content := transcript.ExtractUserContent(lines[i].Message)
		if content != "" {
			prompts = append(prompts, textutil.StripIDEContextTags(content))
		}
	}
	return prompts, nil
}

// ExtractSummary extracts the last assistant message as a session summary.
func (f *FactoryAIDroidAgent) ExtractSummary(sessionRef string) (string, error) {
	data, err := os.ReadFile(sessionRef) //nolint:gosec // Path comes from agent hook input
	if err != nil {
		return "", fmt.Errorf("failed to read transcript: %w", err)
	}

	lines, parseErr := transcript.ParseFromBytes(data)
	if parseErr != nil {
		return "", fmt.Errorf("failed to parse transcript: %w", parseErr)
	}

	for i := len(lines) - 1; i >= 0; i-- {
		if lines[i].Type != transcript.TypeAssistant {
			continue
		}
		var msg transcript.AssistantMessage
		if err := json.Unmarshal(lines[i].Message, &msg); err != nil {
			continue
		}
		for _, block := range msg.Content {
			if block.Type == transcript.ContentTypeText && block.Text != "" {
				return block.Text, nil
			}
		}
	}
	return "", nil
}

// --- TranscriptPreparer ---

// PrepareTranscript waits for Factory Droid's async transcript flush to complete.
func (f *FactoryAIDroidAgent) PrepareTranscript(sessionRef string) error {
	waitForTranscriptFlush(sessionRef, time.Now())
	return nil
}

// --- TokenCalculator ---

// CalculateTokenUsage computes token usage from the transcript starting at the given line offset.
func (f *FactoryAIDroidAgent) CalculateTokenUsage(sessionRef string, fromOffset int) (*agent.TokenUsage, error) {
	return CalculateTotalTokenUsageFromTranscript(sessionRef, fromOffset, "")
}

// --- SubagentAwareExtractor ---

// ExtractAllModifiedFiles extracts files modified by both the main agent and any spawned subagents.
func (f *FactoryAIDroidAgent) ExtractAllModifiedFiles(sessionRef string, fromOffset int, subagentsDir string) ([]string, error) {
	return ExtractAllModifiedFilesFromTranscript(sessionRef, fromOffset, subagentsDir)
}

// CalculateTotalTokenUsage computes token usage including all spawned subagents.
func (f *FactoryAIDroidAgent) CalculateTotalTokenUsage(sessionRef string, fromOffset int, subagentsDir string) (*agent.TokenUsage, error) {
	return CalculateTotalTokenUsageFromTranscript(sessionRef, fromOffset, subagentsDir)
}

// --- Internal hook parsing functions ---

func (f *FactoryAIDroidAgent) parseSessionStart(stdin io.Reader) (*agent.Event, error) {
	raw, err := agent.ReadAndParseHookInput[sessionInfoRaw](stdin)
	if err != nil {
		return nil, err
	}
	return &agent.Event{
		Type:       agent.SessionStart,
		SessionID:  raw.SessionID,
		SessionRef: raw.TranscriptPath,
		Timestamp:  time.Now(),
	}, nil
}

func (f *FactoryAIDroidAgent) parseTurnStart(stdin io.Reader) (*agent.Event, error) {
	raw, err := agent.ReadAndParseHookInput[userPromptSubmitRaw](stdin)
	if err != nil {
		return nil, err
	}
	return &agent.Event{
		Type:       agent.TurnStart,
		SessionID:  raw.SessionID,
		SessionRef: raw.TranscriptPath,
		Prompt:     raw.Prompt,
		Timestamp:  time.Now(),
	}, nil
}

func (f *FactoryAIDroidAgent) parseTurnEnd(stdin io.Reader) (*agent.Event, error) {
	raw, err := agent.ReadAndParseHookInput[sessionInfoRaw](stdin)
	if err != nil {
		return nil, err
	}
	return &agent.Event{
		Type:       agent.TurnEnd,
		SessionID:  raw.SessionID,
		SessionRef: raw.TranscriptPath,
		Timestamp:  time.Now(),
	}, nil
}

func (f *FactoryAIDroidAgent) parseSessionEnd(stdin io.Reader) (*agent.Event, error) {
	raw, err := agent.ReadAndParseHookInput[sessionInfoRaw](stdin)
	if err != nil {
		return nil, err
	}
	return &agent.Event{
		Type:       agent.SessionEnd,
		SessionID:  raw.SessionID,
		SessionRef: raw.TranscriptPath,
		Timestamp:  time.Now(),
	}, nil
}

func (f *FactoryAIDroidAgent) parseSubagentStart(stdin io.Reader) (*agent.Event, error) {
	raw, err := agent.ReadAndParseHookInput[taskHookInputRaw](stdin)
	if err != nil {
		return nil, err
	}
	return &agent.Event{
		Type:       agent.SubagentStart,
		SessionID:  raw.SessionID,
		SessionRef: raw.TranscriptPath,
		ToolUseID:  raw.ToolUseID,
		ToolInput:  raw.ToolInput,
		Timestamp:  time.Now(),
	}, nil
}

func (f *FactoryAIDroidAgent) parseSubagentEnd(stdin io.Reader) (*agent.Event, error) {
	raw, err := agent.ReadAndParseHookInput[postToolHookInputRaw](stdin)
	if err != nil {
		return nil, err
	}
	event := &agent.Event{
		Type:       agent.SubagentEnd,
		SessionID:  raw.SessionID,
		SessionRef: raw.TranscriptPath,
		ToolUseID:  raw.ToolUseID,
		ToolInput:  raw.ToolInput,
		Timestamp:  time.Now(),
	}
	if raw.ToolResponse.AgentID != "" {
		event.SubagentID = raw.ToolResponse.AgentID
	}
	return event, nil
}

func (f *FactoryAIDroidAgent) parseCompaction(stdin io.Reader) (*agent.Event, error) {
	raw, err := agent.ReadAndParseHookInput[sessionInfoRaw](stdin)
	if err != nil {
		return nil, err
	}
	return &agent.Event{
		Type:       agent.Compaction,
		SessionID:  raw.SessionID,
		SessionRef: raw.TranscriptPath,
		Timestamp:  time.Now(),
	}, nil
}

// --- Transcript flush sentinel ---

const stopHookSentinel = "hooks factoryai-droid stop"

func waitForTranscriptFlush(transcriptPath string, hookStartTime time.Time) {
	const (
		maxWait      = 3 * time.Second
		pollInterval = 50 * time.Millisecond
		tailBytes    = 4096
		maxSkew      = 2 * time.Second
	)

	logCtx := logging.WithComponent(context.Background(), "agent.factoryaidroid")
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		if checkStopSentinel(transcriptPath, tailBytes, hookStartTime, maxSkew) {
			logging.Debug(logCtx, "transcript flush sentinel found",
				slog.Duration("wait", time.Since(hookStartTime)),
			)
			return
		}
		time.Sleep(pollInterval)
	}
	logging.Warn(logCtx, "transcript flush sentinel not found within timeout, proceeding",
		slog.Duration("timeout", maxWait),
	)
}

func checkStopSentinel(path string, tailBytes int64, hookStartTime time.Time, maxSkew time.Duration) bool {
	file, err := os.Open(path) //nolint:gosec // path comes from agent hook input
	if err != nil {
		return false
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return false
	}
	offset := info.Size() - tailBytes
	if offset < 0 {
		offset = 0
	}
	buf := make([]byte, info.Size()-offset)
	if _, err := file.ReadAt(buf, offset); err != nil {
		return false
	}

	lines := strings.Split(string(buf), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, stopHookSentinel) {
			continue
		}

		var entry struct {
			Timestamp string `json:"timestamp"`
		}
		if json.Unmarshal([]byte(line), &entry) != nil || entry.Timestamp == "" {
			continue
		}
		ts, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
		if err != nil {
			ts, err = time.Parse(time.RFC3339, entry.Timestamp)
			if err != nil {
				continue
			}
		}
		lowerBound := hookStartTime.Add(-maxSkew)
		upperBound := hookStartTime.Add(maxSkew)
		if ts.After(lowerBound) && ts.Before(upperBound) {
			return true
		}
	}
	return false
}
