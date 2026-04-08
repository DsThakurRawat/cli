package summarize

import (
	"context"
	"strings"
	"testing"

	"github.com/entireio/cli/cmd/entire/cli/agent"
	"github.com/entireio/cli/cmd/entire/cli/agent/types"
)

type mockTextGenerator struct {
	prompt string
	model  string
	result string
}

func (m *mockTextGenerator) Name() types.AgentName                        { return "mock" }
func (m *mockTextGenerator) Type() types.AgentType                        { return "Mock" }
func (m *mockTextGenerator) Description() string                          { return "mock" }
func (m *mockTextGenerator) IsPreview() bool                              { return false }
func (m *mockTextGenerator) DetectPresence(context.Context) (bool, error) { return false, nil }
func (m *mockTextGenerator) ProtectedDirs() []string                      { return nil }
func (m *mockTextGenerator) ReadTranscript(string) ([]byte, error)        { return nil, nil }
func (m *mockTextGenerator) ChunkTranscript(context.Context, []byte, int) ([][]byte, error) {
	return nil, nil
}
func (m *mockTextGenerator) ReassembleTranscript([][]byte) ([]byte, error) { return nil, nil }
func (m *mockTextGenerator) GetSessionID(*agent.HookInput) string          { return "" }
func (m *mockTextGenerator) GetSessionDir(string) (string, error)          { return "", nil }
func (m *mockTextGenerator) ResolveSessionFile(string, string) string      { return "" }
func (m *mockTextGenerator) ReadSession(*agent.HookInput) (*agent.AgentSession, error) {
	return nil, nil //nolint:nilnil // test stub
}
func (m *mockTextGenerator) WriteSession(context.Context, *agent.AgentSession) error { return nil }
func (m *mockTextGenerator) FormatResumeCommand(string) string                       { return "" }
func (m *mockTextGenerator) GenerateText(_ context.Context, prompt string, model string) (string, error) {
	m.prompt = prompt
	m.model = model
	return m.result, nil
}

func TestTextGeneratorAdapter_Generate(t *testing.T) {
	t.Parallel()

	mock := &mockTextGenerator{
		result: "```json\n{\"intent\":\"Intent\",\"outcome\":\"Outcome\",\"learnings\":{\"repo\":[],\"code\":[],\"workflow\":[]},\"friction\":[],\"open_items\":[]}\n```",
	}

	generator := &TextGeneratorAdapter{
		TextGenerator: mock,
		Model:         "test-model",
	}

	summary, err := generator.Generate(context.Background(), Input{
		Transcript: []Entry{
			{Type: EntryTypeUser, Content: "Fix the bug"},
			{Type: EntryTypeAssistant, Content: "I fixed it"},
		},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if summary.Intent != "Intent" {
		t.Fatalf("summary.Intent = %q, want %q", summary.Intent, "Intent")
	}
	if mock.model != "test-model" {
		t.Fatalf("GenerateText model = %q, want %q", mock.model, "test-model")
	}
	if !strings.Contains(mock.prompt, "Fix the bug") {
		t.Fatalf("prompt did not include condensed transcript: %q", mock.prompt)
	}
}
