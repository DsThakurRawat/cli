package cli

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entireio/cli/cmd/entire/cli/logging"
	"github.com/entireio/cli/cmd/entire/cli/testutil"
)

func TestWriteDoctorBundle_ContainsExpectedEntries(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	testutil.InitRepo(t, dir)

	// Write a fixture log file under .entire/logs/.
	logsDir := filepath.Join(dir, logging.LogsDir)
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(logsDir, "entire.log"), []byte("hello\n"), 0o600); err != nil {
		t.Fatalf("write log: %v", err)
	}

	// Write a project settings file.
	entireDir := filepath.Join(dir, ".entire")
	if err := os.WriteFile(filepath.Join(entireDir, "settings.json"), []byte(`{"enabled":true}`), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	out := filepath.Join(dir, "bundle.zip")
	if err := writeDoctorBundle(context.Background(), dir, out); err != nil {
		t.Fatalf("writeDoctorBundle: %v", err)
	}

	zr, err := zip.OpenReader(out)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer zr.Close()

	got := make(map[string]bool, len(zr.File))
	for _, f := range zr.File {
		got[f.Name] = true
	}

	required := []string{
		"logs/entire.log",
		"settings/settings.json",
		"git-status.txt",
		"git-log.txt",
		"git-remote.txt",
		"version.txt",
	}
	for _, name := range required {
		if !got[name] {
			t.Errorf("missing entry %q in bundle. Have: %v", name, mapKeys(got))
		}
	}
}

func TestWriteDoctorBundle_OmitsAbsentLogsDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	testutil.InitRepo(t, dir)

	out := filepath.Join(dir, "bundle.zip")
	if err := writeDoctorBundle(context.Background(), dir, out); err != nil {
		t.Fatalf("writeDoctorBundle: %v", err)
	}

	zr, err := zip.OpenReader(out)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer zr.Close()

	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "logs/") {
			t.Errorf("expected no logs/ entries when dir absent, found %q", f.Name)
		}
	}
}

func mapKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
