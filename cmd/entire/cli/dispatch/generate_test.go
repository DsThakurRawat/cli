package dispatch

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestGenerateLocalDispatch_UsesVoiceAndBullets(t *testing.T) {
	mock := &stubTextGenerator{text: "generated dispatch"}
	oldFactory := dispatchTextGeneratorFactory
	dispatchTextGeneratorFactory = func() (dispatchTextGenerator, error) { return mock, nil }
	t.Cleanup(func() { dispatchTextGeneratorFactory = oldFactory })

	dispatch := &Dispatch{
		Repos: []RepoGroup{{
			FullName: "entireio/cli",
			Sections: []Section{{
				Label: "CI",
				Bullets: []Bullet{{
					Text: "Fixed tests.",
				}},
			}},
		}},
	}

	got, err := generateLocalDispatch(context.Background(), dispatch, "marvin")
	if err != nil {
		t.Fatal(err)
	}
	if got != "generated dispatch" {
		t.Fatalf("unexpected text: %q", got)
	}
	if !strings.Contains(mock.prompt, "<voice_guidance>") {
		t.Fatalf("missing voice guidance in prompt: %s", mock.prompt)
	}
	if !strings.Contains(mock.prompt, "Fixed tests.") {
		t.Fatalf("missing bullet in prompt: %s", mock.prompt)
	}
}

func TestGenerateLocalDispatch_PropagatesGeneratorError(t *testing.T) {
	oldFactory := dispatchTextGeneratorFactory
	dispatchTextGeneratorFactory = func() (dispatchTextGenerator, error) {
		return &stubTextGenerator{err: errors.New("boom")}, nil
	}
	t.Cleanup(func() { dispatchTextGeneratorFactory = oldFactory })

	_, err := generateLocalDispatch(context.Background(), &Dispatch{}, "")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected generator error, got %v", err)
	}
}

type stubTextGenerator struct {
	prompt string
	text   string
	err    error
}

func (s *stubTextGenerator) GenerateText(_ context.Context, prompt string, _ string) (string, error) {
	s.prompt = prompt
	if s.err != nil {
		return "", s.err
	}
	return s.text, nil
}
