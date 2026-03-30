package compact

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/entireio/cli/cmd/entire/cli/transcript"
)

// --- Gemini CLI format support ---
//
// Gemini transcripts are a single JSON object (not JSONL):
//
//	{"sessionId":"...","messages":[{"id":"...","timestamp":"...","type":"user"|"gemini"|"info","content":"...","toolCalls":[...],"thoughts":[...]}]}
//
// Key differences from other formats:
//   - Assistant messages use type "gemini" (not "assistant")
//   - System messages use type "info" and should be dropped
//   - Tool calls are at the message level in a "toolCalls" array
//   - A message can have both content text and toolCalls
//   - Timestamps are ISO strings (not millisecond integers)
//   - "thoughts" and "tokens" are metadata fields; tokens.input/output are preserved

// geminiDroppedTypes are Gemini message types that carry no transcript-relevant data.
var geminiDroppedTypes = map[string]bool{
	"info": true,
}

// isGeminiFormat checks whether content is a single JSON object with the
// Gemini session shape: top-level "sessionId" and "messages" keys, but NO "info"
// key (which distinguishes it from OpenCode format).
func isGeminiFormat(content []byte) bool {
	trimmed := bytes.TrimSpace(content)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return false
	}
	var probe struct {
		SessionID *json.RawMessage `json:"sessionId"`
		Messages  *json.RawMessage `json:"messages"`
		Info      *json.RawMessage `json:"info"`
	}
	if json.Unmarshal(trimmed, &probe) != nil {
		return false
	}
	return probe.SessionID != nil && probe.Messages != nil && probe.Info == nil
}

// geminiMessage mirrors the Gemini message structure for unmarshaling.
type geminiMessage struct {
	ID        string           `json:"id"`
	Timestamp string           `json:"timestamp"`
	Type      string           `json:"type"`
	Content   string           `json:"content"`
	ToolCalls []geminiToolCall `json:"toolCalls"`
	Thoughts  json.RawMessage  `json:"thoughts"` // parsed only to detect; always dropped
	Tokens    *geminiTokens    `json:"tokens"`
	Model     string           `json:"model"` // dropped
}

// geminiTokens holds token usage from a Gemini message.
type geminiTokens struct {
	Input  int `json:"input"`
	Output int `json:"output"`
}

// geminiToolCall represents a single tool invocation within a Gemini message.
type geminiToolCall struct {
	ID     string                 `json:"id"`
	Name   string                 `json:"name"`
	Args   map[string]interface{} `json:"args"`
	Result []geminiToolResult     `json:"result"`
	Status string                 `json:"status"`
}

// geminiToolResult represents a tool result entry from the Gemini format.
type geminiToolResult struct {
	FunctionResponse struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Response struct {
			Output string `json:"output"`
		} `json:"response"`
	} `json:"functionResponse"`
}

// compactGemini converts a full Gemini session JSON into transcript lines.
func compactGemini(content []byte, opts MetadataFields) ([]byte, error) {
	var session struct {
		Messages []geminiMessage `json:"messages"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(content), &session); err != nil {
		return nil, fmt.Errorf("parsing gemini session: %w", err)
	}

	base := newTranscriptLine(opts)
	var result []byte

	for _, msg := range session.Messages {
		if geminiDroppedTypes[msg.Type] {
			continue
		}

		ts, _ := json.Marshal(msg.Timestamp) //nolint:errcheck,errchkjson // string timestamp never fails

		switch msg.Type {
		case transcript.TypeUser:
			emitGeminiUser(&result, base, msg, ts)
		case "gemini":
			emitGeminiAssistant(&result, base, msg, ts)
		}
	}

	return result, nil
}

// emitGeminiUser produces a single user line. Gemini user messages have
// plain string content.
func emitGeminiUser(result *[]byte, base transcriptLine, msg geminiMessage, ts json.RawMessage) {
	if msg.Content == "" {
		return
	}

	b, err := json.Marshal([]userTextBlock{{Text: msg.Content}})
	if err != nil {
		return
	}

	line := base
	line.Type = transcript.TypeUser
	line.TS = ts
	line.Content = b
	appendLine(result, line)
}

// emitGeminiAssistant produces a single assistant line. The content array
// contains text blocks (from content field) and tool_use blocks (from toolCalls).
func emitGeminiAssistant(result *[]byte, base transcriptLine, msg geminiMessage, ts json.RawMessage) {
	contentBlocks := make([]map[string]json.RawMessage, 0, 1+len(msg.ToolCalls))

	// Add text block if content is non-empty.
	if msg.Content != "" {
		tb, _ := json.Marshal(transcript.ContentTypeText) //nolint:errcheck,errchkjson // string never fails
		text, _ := json.Marshal(msg.Content)              //nolint:errcheck,errchkjson // string never fails
		contentBlocks = append(contentBlocks, map[string]json.RawMessage{
			"type": tb,
			"text": text,
		})
	}

	// Add tool_use blocks from toolCalls.
	for _, tc := range msg.ToolCalls {
		tb, _ := json.Marshal(transcript.ContentTypeToolUse) //nolint:errcheck,errchkjson // string never fails
		id, _ := json.Marshal(tc.ID)                         //nolint:errcheck,errchkjson // string never fails
		name, _ := json.Marshal(tc.Name)                     //nolint:errcheck,errchkjson // string never fails
		input, _ := json.Marshal(tc.Args)                    //nolint:errcheck,errchkjson // map marshal is best-effort

		toolBlock := map[string]json.RawMessage{
			"type":   tb,
			"id":     id,
			"name":   name,
			"input":  input,
			"result": geminiToolResultCompact(tc),
		}
		contentBlocks = append(contentBlocks, toolBlock)
	}

	if len(contentBlocks) == 0 {
		return
	}

	contentJSON, err := json.Marshal(contentBlocks)
	if err != nil {
		return
	}

	line := base
	line.Type = transcript.TypeAssistant
	line.TS = ts
	line.ID = msg.ID
	line.Content = contentJSON
	if msg.Tokens != nil {
		line.InputTokens = msg.Tokens.Input
		line.OutputTokens = msg.Tokens.Output
	}
	appendLine(result, line)
}

// geminiToolResultCompact builds the compact {"output":"...","status":"..."}
// object from a Gemini tool call.
func geminiToolResultCompact(tc geminiToolCall) json.RawMessage {
	output := ""
	if len(tc.Result) > 0 {
		output = tc.Result[0].FunctionResponse.Response.Output
	}

	r := toolResultJSON{
		Output: output,
		Status: tc.Status,
	}
	b, err := json.Marshal(r)
	if err != nil {
		return nil
	}
	return b
}
