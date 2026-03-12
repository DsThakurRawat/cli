package apiurl

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

const (
	// DefaultBaseURL is the production Entire API origin.
	DefaultBaseURL = "https://entire.io"

	// BaseURLEnvVar overrides the Entire API origin for local development.
	BaseURLEnvVar = "ENTIRE_API_BASE_URL"
)

// BaseURL returns the effective Entire API base URL.
// ENTIRE_API_BASE_URL takes precedence over the production default.
func BaseURL() string {
	if raw := strings.TrimSpace(os.Getenv(BaseURLEnvVar)); raw != "" {
		return normalizeBaseURL(raw)
	}

	return DefaultBaseURL
}

// ResolveURL joins an API-relative path against the effective base URL.
func ResolveURL(path string) (string, error) {
	return ResolveURLFromBase(BaseURL(), path)
}

// ResolveURLFromBase joins an API-relative path against an explicit base URL.
func ResolveURLFromBase(baseURL, path string) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse base URL: %w", err)
	}

	rel, err := url.Parse(path)
	if err != nil {
		return "", fmt.Errorf("parse path: %w", err)
	}

	return base.ResolveReference(rel).String(), nil
}

func normalizeBaseURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}
