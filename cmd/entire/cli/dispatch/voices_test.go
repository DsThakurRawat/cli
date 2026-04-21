package dispatch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveVoice_PresetMatch(t *testing.T) {
	t.Parallel()

	got := ResolveVoice("marvin")
	if !got.IsPreset {
		t.Fatalf("expected preset, got %+v", got)
	}
	if got.Name != "marvin" {
		t.Fatalf("expected name=marvin, got %q", got.Name)
	}
	if got.Text == "" {
		t.Fatal("expected non-empty preset text")
	}
}

func TestResolveVoice_CaseInsensitive(t *testing.T) {
	t.Parallel()

	got := ResolveVoice("MARVIN")
	if !got.IsPreset {
		t.Fatalf("expected preset for uppercase input, got %+v", got)
	}
	if got.Name != "marvin" {
		t.Fatalf("expected normalized name=marvin, got %q", got.Name)
	}
}

func TestResolveVoice_FilePathFallback(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "my-voice.md")
	want := "my custom voice directive"
	if err := os.WriteFile(path, []byte(want), 0o600); err != nil {
		t.Fatal(err)
	}

	got := ResolveVoice(path)
	if got.IsPreset {
		t.Fatalf("expected non-preset for file path, got %+v", got)
	}
	if got.Text != want {
		t.Fatalf("expected file content %q, got %q", want, got.Text)
	}
}

func TestResolveVoice_LiteralStringFallback(t *testing.T) {
	t.Parallel()

	got := ResolveVoice("sardonic AI named Gary")
	if got.IsPreset {
		t.Fatal("expected literal, not preset")
	}
	if got.Text != "sardonic AI named Gary" {
		t.Fatalf("expected passthrough, got %q", got.Text)
	}
}

func TestResolveVoice_EmptyDefaultsToNeutral(t *testing.T) {
	t.Parallel()

	got := ResolveVoice("")
	if !got.IsPreset || got.Name != "neutral" {
		t.Fatalf("expected neutral default, got %+v", got)
	}
}

func TestListPresetNames(t *testing.T) {
	t.Parallel()

	got := ListPresetNames()
	if len(got) != 2 || got[0] != "neutral" || got[1] != "marvin" {
		t.Fatalf("unexpected presets: %v", got)
	}
}
