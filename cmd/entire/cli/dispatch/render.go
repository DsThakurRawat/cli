package dispatch

import (
	"encoding/json"
	"fmt"
	"strings"
)

func RenderText(dispatch *Dispatch) string {
	if dispatch == nil {
		return ""
	}
	if dispatch.Generated && strings.TrimSpace(dispatch.GeneratedText) != "" {
		return strings.TrimSpace(dispatch.GeneratedText) + "\n"
	}

	var b strings.Builder
	b.WriteString("entire dispatch\n\n")
	for _, repo := range dispatch.Repos {
		b.WriteString(repo.FullName)
		b.WriteString("\n")
		for _, section := range repo.Sections {
			b.WriteString("  ")
			b.WriteString(section.Label)
			b.WriteString("\n")
			for _, bullet := range section.Bullets {
				b.WriteString("    • ")
				b.WriteString(bullet.Text)
				b.WriteString("\n")
			}
		}
		b.WriteString("\n")
	}

	fmt.Fprintf(&b, "%d checkpoints · %d used · %d branches · %d files touched\n",
		dispatch.Totals.Checkpoints,
		dispatch.Totals.UsedCheckpointCount,
		dispatch.Totals.Branches,
		dispatch.Totals.FilesTouched,
	)
	appendWarningsText(&b, dispatch.Warnings)
	return strings.TrimSpace(b.String()) + "\n"
}

func RenderMarkdown(dispatch *Dispatch) string {
	if dispatch == nil {
		return ""
	}
	if dispatch.Generated && strings.TrimSpace(dispatch.GeneratedText) != "" {
		return strings.TrimSpace(dispatch.GeneratedText) + "\n"
	}

	var b strings.Builder
	b.WriteString("# entire dispatch\n\n")
	for _, repo := range dispatch.Repos {
		b.WriteString("## ")
		b.WriteString(repo.FullName)
		b.WriteString("\n\n")
		for _, section := range repo.Sections {
			b.WriteString("### ")
			b.WriteString(section.Label)
			b.WriteString("\n\n")
			for _, bullet := range section.Bullets {
				b.WriteString("- ")
				b.WriteString(bullet.Text)
				b.WriteString("\n")
			}
			b.WriteString("\n")
		}
	}

	fmt.Fprintf(&b, "%d checkpoints · %d used · %d branches · %d files touched\n",
		dispatch.Totals.Checkpoints,
		dispatch.Totals.UsedCheckpointCount,
		dispatch.Totals.Branches,
		dispatch.Totals.FilesTouched,
	)
	appendWarningsMarkdown(&b, dispatch.Warnings)
	return strings.TrimSpace(b.String()) + "\n"
}

func RenderJSON(dispatch *Dispatch) string {
	if dispatch == nil {
		return "{}\n"
	}

	data, err := json.MarshalIndent(dispatch, "", "  ")
	if err != nil {
		return "{}\n"
	}
	return string(data) + "\n"
}

func Render(format string, dispatch *Dispatch) (string, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "text":
		return RenderText(dispatch), nil
	case "markdown":
		return RenderMarkdown(dispatch), nil
	case "json":
		return RenderJSON(dispatch), nil
	default:
		return "", fmt.Errorf("unsupported format %q", format)
	}
}

func appendWarningsText(b *strings.Builder, warnings Warnings) {
	lines := warningLines(warnings)
	if len(lines) == 0 {
		return
	}
	b.WriteString("Warnings:\n")
	for _, line := range lines {
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteString("\n")
	}
}

func appendWarningsMarkdown(b *strings.Builder, warnings Warnings) {
	lines := warningLines(warnings)
	if len(lines) == 0 {
		return
	}
	b.WriteString("\n### Warnings\n\n")
	for _, line := range lines {
		line = strings.TrimSpace(strings.TrimLeft(line, "⚠⏳✕ℹ "))
		b.WriteString("- ")
		b.WriteString(line)
		b.WriteString("\n")
	}
}

func warningLines(warnings Warnings) []string {
	lines := make([]string, 0, 5)
	if warnings.AccessDeniedCount > 0 {
		lines = append(lines, fmt.Sprintf("⚠ %d checkpoints you no longer have access to", warnings.AccessDeniedCount))
	}
	if warnings.PendingCount > 0 {
		lines = append(lines, fmt.Sprintf("⏳ %d checkpoints still being analyzed (retry in a minute)", warnings.PendingCount))
	}
	if warnings.FailedCount > 0 {
		lines = append(lines, fmt.Sprintf("✕ %d checkpoints failed analysis on the server", warnings.FailedCount))
	}
	if warnings.UnknownCount > 0 {
		lines = append(lines, fmt.Sprintf("ℹ %d checkpoints not known to the server", warnings.UnknownCount))
	}
	if warnings.UncategorizedCount > 0 {
		lines = append(lines, fmt.Sprintf("%d uncategorized checkpoints skipped", warnings.UncategorizedCount))
	}
	return lines
}
