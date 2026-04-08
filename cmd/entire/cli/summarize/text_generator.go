package summarize

import (
	"context"
	"fmt"

	"github.com/entireio/cli/cmd/entire/cli/agent"
	"github.com/entireio/cli/cmd/entire/cli/checkpoint"
)

// TextGeneratorAdapter uses an agent.TextGenerator with Entire's shared
// summary prompt and response parser.
type TextGeneratorAdapter struct {
	TextGenerator agent.TextGenerator
	Model         string
}

// Generate creates a summary using the shared prompt, then delegates raw text
// generation to the configured agent provider.
func (g *TextGeneratorAdapter) Generate(ctx context.Context, input Input) (*checkpoint.Summary, error) {
	transcriptText := FormatCondensedTranscript(input)
	prompt := buildSummarizationPrompt(transcriptText)

	result, err := g.TextGenerator.GenerateText(ctx, prompt, g.Model)
	if err != nil {
		return nil, fmt.Errorf("provider text generation failed: %w", err)
	}

	return parseSummaryText(result)
}
