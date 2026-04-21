package dispatch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestCloudClient_CreateDispatch_Happy(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/users/me/dispatches" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["repos"] == nil {
			t.Fatalf("bad body: %v", body)
		}
		repos, ok := body["repos"].([]any)
		if !ok || len(repos) != 1 || repos[0] != "entireio/cli" {
			t.Fatalf("bad repos payload: %v", body["repos"])
		}
		if _, ok := body["wait"]; ok {
			t.Fatalf("did not expect wait in request body: %v", body)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"window":{"normalized_since":"2026-04-09T00:00:00Z","normalized_until":"2026-04-16T00:00:00Z"},"covered_repos":["entireio/cli"],"repos":[],"totals":{"checkpoints":0,"used_checkpoint_count":0,"branches":0,"files_touched":0},"warnings":{"access_denied_count":0,"pending_count":0,"failed_count":0,"unknown_count":0,"uncategorized_count":0},"generated_text":"hi"}`))
	}))
	defer srv.Close()

	client := NewCloudClient(CloudConfig{BaseURL: srv.URL, Token: "t"})
	got, err := client.CreateDispatch(ctx, CreateDispatchRequest{
		Repos:    []string{"entireio/cli"},
		Since:    "2026-04-09T00:00:00Z",
		Until:    "2026-04-16T00:00:00Z",
		Branches: []string{"all"},
		Generate: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.GeneratedText != "hi" {
		t.Fatalf("bad generated text: %q", got.GeneratedText)
	}
}

func TestCloudClient_CreateDispatch_Unauthorized(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	client := NewCloudClient(CloudConfig{BaseURL: srv.URL, Token: ""})
	_, err := client.CreateDispatch(ctx, CreateDispatchRequest{Repos: []string{"x/y"}})
	if err == nil || !strings.Contains(err.Error(), "entire login") {
		t.Fatalf("expected auth error, got %v", err)
	}
}

func TestCloudClient_FetchBatchAnalyses_PaginatesBy200(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/users/me/checkpoints/analyses/batch" {
			http.NotFound(w, r)
			return
		}
		requests++

		var body struct {
			RepoFullName  string   `json:"repoFullName"`
			CheckpointIDs []string `json:"checkpointIds"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.RepoFullName != "entireio/cli" {
			t.Fatalf("unexpected repo: %q", body.RepoFullName)
		}

		analyses := make(map[string]AnalysisStatus, len(body.CheckpointIDs))
		for _, id := range body.CheckpointIDs {
			analyses[id] = AnalysisStatus{
				Status:  "complete",
				Summary: "summary " + id,
				Labels:  []string{"ci"},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"analyses": analyses}); err != nil {
			t.Fatal(err)
		}
	}))
	defer srv.Close()

	ids := make([]string, 0, 201)
	for i := range 201 {
		ids = append(ids, "cp-"+strconv.Itoa(i))
	}

	client := NewCloudClient(CloudConfig{BaseURL: srv.URL, Token: "t"})
	got, err := client.FetchBatchAnalyses(ctx, "entireio/cli", ids)
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("expected 2 requests, got %d", requests)
	}
	if len(got) != len(ids) {
		t.Fatalf("expected %d analyses, got %d", len(ids), len(got))
	}
	if got["cp-200"].Summary != "summary cp-200" {
		t.Fatalf("unexpected analysis payload: %+v", got["cp-200"])
	}
}
