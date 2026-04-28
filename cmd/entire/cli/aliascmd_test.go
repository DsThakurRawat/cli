package cli

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
)

func TestWarnDeprecatedAliasOnce_PrintsOnce(t *testing.T) {
	t.Parallel()

	// Use unique replacement strings so this test doesn't collide with other
	// tests that also exercise the once-per-process map.
	old := "test-once-old"
	repl := "test-once-new"
	resetDeprecationOnceForKey(old, repl)

	var buf bytes.Buffer
	cmd := newAliasTestCommand(&buf)

	warnDeprecatedAliasOnce(cmd, old, repl)
	warnDeprecatedAliasOnce(cmd, old, repl)
	warnDeprecatedAliasOnce(cmd, old, repl)

	count := strings.Count(buf.String(), "deprecated")
	if count != 1 {
		t.Fatalf("expected exactly 1 deprecation line, got %d. Output:\n%s", count, buf.String())
	}
}

func TestWarnDeprecatedAliasOnce_DifferentPairsBothPrint(t *testing.T) {
	t.Parallel()

	resetDeprecationOnceForKey("alpha", "beta")
	resetDeprecationOnceForKey("gamma", "delta")

	var buf bytes.Buffer
	cmd := newAliasTestCommand(&buf)

	warnDeprecatedAliasOnce(cmd, "alpha", "beta")
	warnDeprecatedAliasOnce(cmd, "gamma", "delta")

	out := buf.String()
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "beta") {
		t.Fatalf("missing first pair in output: %s", out)
	}
	if !strings.Contains(out, "gamma") || !strings.Contains(out, "delta") {
		t.Fatalf("missing second pair in output: %s", out)
	}
}

func TestFirstWord(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"":             "",
		"configure":    "configure",
		"reset":        "reset",
		"reset --hard": "reset",
		"command  arg": "command",
		"\tleading":    "",
	}
	for in, want := range cases {
		if got := firstWord(in); got != want {
			t.Errorf("firstWord(%q) = %q, want %q", in, got, want)
		}
	}
}

// resetDeprecationOnceForKey clears any cached *sync.Once for the given pair
// so a test can assert printing behavior independently of other tests sharing
// the package-level map.
func resetDeprecationOnceForKey(old, replacement string) {
	deprecationWarnOnce.Delete(old + "->" + replacement)
}

func newAliasTestCommand(out *bytes.Buffer) *cobra.Command {
	cmd := &cobra.Command{Use: "fake"}
	cmd.SetOut(out)
	cmd.SetErr(out)
	return cmd
}

// guard against accidental concurrent access to the package map during the
// test suite. This is exported only via the import below so the linter sees
// sync as used in non-test contexts when this file compiles in isolation.
var _ sync.Mutex
