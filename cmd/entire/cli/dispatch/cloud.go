package dispatch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/entireio/cli/cmd/entire/cli/api"
)

const cloudTimeout = 30 * time.Second

type CloudConfig struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
	Timeout time.Duration
}

type CloudClient struct {
	baseURL string
	token   string
	http    *http.Client
}

func NewCloudClient(cfg CloudConfig) *CloudClient {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = api.BaseURL()
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = cloudTimeout
	}

	httpClient := cfg.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	} else if httpClient.Timeout == 0 {
		httpClient.Timeout = timeout
	}

	return &CloudClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   cfg.Token,
		http:    httpClient,
	}
}

type CreateDispatchRequest struct {
	Repo     string   `json:"repo,omitempty"`
	Repos    []string `json:"repos,omitempty"`
	Org      string   `json:"org,omitempty"`
	Since    string   `json:"since"`
	Until    string   `json:"until"`
	Branches any      `json:"branches"`
	Generate bool     `json:"generate"`
	Voice    string   `json:"voice,omitempty"`
}

type CreateDispatchResponse struct {
	Window        APIWindow   `json:"window"`
	CoveredRepos  []string    `json:"covered_repos,omitempty"`
	Repos         []APIRepo   `json:"repos,omitempty"`
	Totals        APITotals   `json:"totals"`
	Warnings      APIWarnings `json:"warnings"`
	GeneratedText string      `json:"generated_text,omitempty"`
}

type APIWindow struct {
	NormalizedSince          string `json:"normalized_since"`
	NormalizedUntil          string `json:"normalized_until"`
	FirstCheckpointCreatedAt string `json:"first_checkpoint_created_at,omitempty"`
	LastCheckpointCreatedAt  string `json:"last_checkpoint_created_at,omitempty"`
}

type APIRepo struct {
	FullName string       `json:"full_name"`
	Sections []APISection `json:"sections"`
}

type APISection struct {
	Label   string      `json:"label"`
	Bullets []APIBullet `json:"bullets"`
}

type APIBullet struct {
	CheckpointID string   `json:"checkpoint_id"`
	Text         string   `json:"text"`
	Source       string   `json:"source"`
	Branch       string   `json:"branch"`
	CreatedAt    string   `json:"created_at"`
	Labels       []string `json:"labels"`
}

type APITotals struct {
	Checkpoints         int `json:"checkpoints"`
	UsedCheckpointCount int `json:"used_checkpoint_count"`
	Branches            int `json:"branches"`
	FilesTouched        int `json:"files_touched"`
}

type APIWarnings struct {
	AccessDeniedCount  int `json:"access_denied_count"`
	PendingCount       int `json:"pending_count"`
	FailedCount        int `json:"failed_count"`
	UnknownCount       int `json:"unknown_count"`
	UncategorizedCount int `json:"uncategorized_count"`
}

type AnalysisStatus struct {
	Status  string   `json:"status"`
	Summary string   `json:"summary,omitempty"`
	Labels  []string `json:"labels,omitempty"`
}

type OrgCheckpoint struct {
	ID           string `json:"id"`
	RepoFullName string `json:"repo_full_name"`
	Branch       string `json:"branch,omitempty"`
	CreatedAt    string `json:"created_at"`
}

func (c *CloudClient) CreateDispatch(ctx context.Context, reqBody CreateDispatchRequest) (*CreateDispatchResponse, error) {
	var out CreateDispatchResponse
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/users/me/dispatches", reqBody, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *CloudClient) FetchBatchAnalyses(ctx context.Context, repoFullName string, ids []string) (map[string]AnalysisStatus, error) {
	if len(ids) == 0 {
		return map[string]AnalysisStatus{}, nil
	}

	out := make(map[string]AnalysisStatus, len(ids))
	for _, chunk := range chunkIDs(ids, 200) {
		var payload struct {
			Analyses map[string]AnalysisStatus `json:"analyses"`
		}
		err := c.doJSON(ctx, http.MethodPost, "/api/v1/users/me/checkpoints/analyses/batch", map[string]any{
			"repoFullName":  repoFullName,
			"checkpointIds": chunk,
		}, &payload)
		if err != nil {
			return nil, err
		}
		for id, status := range payload.Analyses {
			out[id] = status
		}
	}

	return out, nil
}

func (c *CloudClient) EnumerateOrgCheckpoints(ctx context.Context, org, since string) ([]OrgCheckpoint, error) {
	cursor := ""
	var out []OrgCheckpoint

	for {
		path := fmt.Sprintf("/api/v1/orgs/%s/checkpoints?since=%s", org, since)
		if cursor != "" {
			path += "&cursor=" + cursor
		}

		var payload struct {
			Checkpoints []OrgCheckpoint `json:"checkpoints"`
			Cursor      string          `json:"cursor"`
		}
		if err := c.doJSON(ctx, http.MethodGet, path, nil, &payload); err != nil {
			return nil, err
		}
		out = append(out, payload.Checkpoints...)
		if payload.Cursor == "" {
			return out, nil
		}
		cursor = payload.Cursor
	}
}

func (c *CloudClient) doJSON(ctx context.Context, method, path string, reqBody, out any) error {
	var body io.Reader
	if reqBody != nil {
		data, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return errors.New("dispatch requires login — run `entire login`")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		trimmed := strings.TrimSpace(string(body))
		if trimmed == "" {
			return fmt.Errorf("%s %s: unexpected status %d", method, path, resp.StatusCode)
		}
		return fmt.Errorf("%s %s: unexpected status %d: %s", method, path, resp.StatusCode, trimmed)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func chunkIDs(ids []string, size int) [][]string {
	if size <= 0 || len(ids) == 0 {
		return nil
	}

	chunks := make([][]string, 0, (len(ids)+size-1)/size)
	for start := 0; start < len(ids); start += size {
		end := start + size
		if end > len(ids) {
			end = len(ids)
		}
		chunks = append(chunks, ids[start:end])
	}
	return chunks
}

func cloudBaseURL() string {
	return api.BaseURL()
}
