package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	apiurl "github.com/entireio/cli/cmd/entire/cli/api"
)

const maxResponseBytes = 1 << 20

type Client struct {
	httpClient *http.Client
	baseURL    string
}

type DeviceAuthStart struct {
	DeviceCode string `json:"deviceCode"`
	UserCode   string `json:"userCode"`
	BrowserURL string `json:"browserUrl"`
	ExpiresIn  int    `json:"expiresIn"`
}

type DeviceAuthPoll struct {
	Status string `json:"status"`
	Token  string `json:"token,omitempty"`
	Error  string `json:"error,omitempty"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	return &Client{
		httpClient: httpClient,
		baseURL:    apiurl.BaseURL(),
	}
}

func (c *Client) BaseURL() string {
	return c.baseURL
}

func (c *Client) StartDeviceAuth(ctx context.Context) (*DeviceAuthStart, error) {
	endpoint, err := apiurl.ResolveURLFromBase(c.baseURL, "/api/v1/cli/auth/start")
	if err != nil {
		return nil, fmt.Errorf("resolve device auth start URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create device auth start request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "entire-cli")

	resp, err := c.httpClient.Do(req) //nolint:gosec // base URL is constrained to Entire API or explicit local dev override
	if err != nil {
		return nil, fmt.Errorf("start device auth: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, readAPIError(resp, "start device auth")
	}

	var result DeviceAuthStart
	if err := decodeJSON(resp.Body, &result); err != nil {
		return nil, fmt.Errorf("decode device auth start response: %w", err)
	}

	return &result, nil
}

func (c *Client) PollDeviceAuth(ctx context.Context, deviceCode string) (*DeviceAuthPoll, error) {
	endpoint, err := apiurl.ResolveURLFromBase(c.baseURL, "/api/v1/cli/auth/poll?device_code="+url.QueryEscape(deviceCode))
	if err != nil {
		return nil, fmt.Errorf("resolve device auth poll URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create device auth poll request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "entire-cli")

	resp, err := c.httpClient.Do(req) //nolint:gosec // base URL is constrained to Entire API or explicit local dev override
	if err != nil {
		return nil, fmt.Errorf("poll device auth: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusGone {
		return &DeviceAuthPoll{Status: "expired"}, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, readAPIError(resp, "poll device auth")
	}

	var result DeviceAuthPoll
	if err := decodeJSON(resp.Body, &result); err != nil {
		return nil, fmt.Errorf("decode device auth poll response: %w", err)
	}

	return &result, nil
}

func readAPIError(resp *http.Response, action string) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("%s: status %d", action, resp.StatusCode)
	}

	var apiErr errorResponse
	if err := json.Unmarshal(body, &apiErr); err == nil && strings.TrimSpace(apiErr.Error) != "" {
		return fmt.Errorf("%s: %s", action, apiErr.Error)
	}

	text := strings.TrimSpace(string(body))
	if text != "" {
		return fmt.Errorf("%s: status %d: %s", action, resp.StatusCode, text)
	}

	return fmt.Errorf("%s: status %d", action, resp.StatusCode)
}

func decodeJSON(r io.Reader, dest any) error {
	body, err := io.ReadAll(io.LimitReader(r, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("read JSON response: %w", err)
	}

	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		return fmt.Errorf("decode JSON response: %w", err)
	}

	return nil
}
