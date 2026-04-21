package dispatch

import (
	"context"
	"strings"
	"testing"
)

func TestRun_ServerAllowsRepos(t *testing.T) {
	t.Parallel()

	_, err := Run(context.Background(), Options{
		Mode:      ModeServer,
		RepoPaths: []string{"entireio/cli"},
	})
	if err == nil {
		t.Fatal("expected login error")
	}
	if strings.Contains(err.Error(), "--repos") {
		t.Fatalf("did not expect repos validation error: %v", err)
	}
	if !strings.Contains(err.Error(), "dispatch requires login") {
		t.Fatalf("unexpected error: %v", err)
	}
}
