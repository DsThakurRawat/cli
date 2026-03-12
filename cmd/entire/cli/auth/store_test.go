package auth

import (
	"os"
	"path/filepath"
	"testing"

	apiurl "github.com/entireio/cli/cmd/entire/cli/api"
)

func TestStoreSaveToken_PreservesOtherBaseURLs(t *testing.T) {
	t.Parallel()

	store := NewStoreForPath(filepath.Join(t.TempDir(), "auth.json"))

	if err := store.SaveToken("https://entire.io", "prod-token"); err != nil {
		t.Fatalf("SaveToken(prod) error = %v", err)
	}

	if err := store.SaveToken("http://localhost:8787", "local-token"); err != nil {
		t.Fatalf("SaveToken(local) error = %v", err)
	}

	state, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got := state.Tokens["https://entire.io"].Value; got != "prod-token" {
		t.Fatalf("prod token = %q, want %q", got, "prod-token")
	}

	if got := state.Tokens["http://localhost:8787"].Value; got != "local-token" {
		t.Fatalf("local token = %q, want %q", got, "local-token")
	}
}

func TestStoreSaveToken_WritesPrivateFile(t *testing.T) {
	t.Parallel()

	filePath := filepath.Join(t.TempDir(), "auth.json")
	store := NewStoreForPath(filePath)

	if err := store.SaveToken("https://entire.io", "prod-token"); err != nil {
		t.Fatalf("SaveToken() error = %v", err)
	}

	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}

	if mode := info.Mode() & 0o777; mode != 0o600 {
		t.Fatalf("auth file mode = %o, want 600", mode)
	}
}

func TestLookupToken(t *testing.T) {
	t.Setenv(apiurl.BaseURLEnvVar, "http://localhost:8787")

	state := &File{
		Tokens: map[string]Token{
			"http://localhost:8787": {Value: "local-token"},
		},
	}

	if got := LookupToken(state); got != "local-token" {
		t.Fatalf("LookupToken() = %q, want %q", got, "local-token")
	}
}
