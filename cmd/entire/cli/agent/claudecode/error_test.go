package claudecode

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestClaudeError_ErrorIncludesKindAndMessage(t *testing.T) {
	t.Parallel()
	e := &ClaudeError{Kind: ClaudeErrorAuth, Message: "Invalid API key"}
	s := e.Error()
	if !strings.Contains(s, "auth") {
		t.Errorf("Error() = %q; want to contain kind 'auth'", s)
	}
	if !strings.Contains(s, "Invalid API key") {
		t.Errorf("Error() = %q; want to contain message", s)
	}
}

func TestClaudeError_UnwrapReturnsCause(t *testing.T) {
	t.Parallel()
	cause := errors.New("underlying")
	e := &ClaudeError{Kind: ClaudeErrorUnknown, Cause: cause}
	if got := errors.Unwrap(e); !errors.Is(got, cause) {
		t.Errorf("Unwrap() = %v; want %v", got, cause)
	}
}

func TestClaudeError_UnwrapNilCause(t *testing.T) {
	t.Parallel()
	e := &ClaudeError{Kind: ClaudeErrorAuth}
	if got := errors.Unwrap(e); got != nil {
		t.Errorf("Unwrap() = %v; want nil", got)
	}
}

func TestClaudeError_ErrorEmptyMessageFallback(t *testing.T) {
	t.Parallel()
	e := &ClaudeError{Kind: ClaudeErrorRateLimit}
	s := e.Error()
	want := "claude CLI error (kind=rate_limit)"
	if s != want {
		t.Errorf("Error() = %q; want %q", s, want)
	}
}

func TestClaudeError_ErrorsAsIntegration(t *testing.T) {
	t.Parallel()
	cause := errors.New("timeout")
	wrapped := fmt.Errorf("operation failed: %w", &ClaudeError{
		Kind:    ClaudeErrorCLIMissing,
		Message: "claude not found",
		Cause:   cause,
	})

	var ce *ClaudeError
	if !errors.As(wrapped, &ce) {
		t.Fatal("errors.As did not find *ClaudeError in wrapped chain")
	}
	if ce.Kind != ClaudeErrorCLIMissing {
		t.Errorf("Kind = %q; want %q", ce.Kind, ClaudeErrorCLIMissing)
	}
	if !errors.Is(ce, cause) {
		t.Error("errors.Is did not find cause through ClaudeError.Unwrap()")
	}
}

// --- Envelope classifier tests ---

func TestClassifyEnvelopeError_Auth401(t *testing.T) {
	t.Parallel()
	status := 401
	got := classifyEnvelopeError("Authentication required", &status, 0)
	if got.Kind != ClaudeErrorAuth {
		t.Errorf("Kind = %v; want %v", got.Kind, ClaudeErrorAuth)
	}
	if got.Message != "Authentication required" {
		t.Errorf("Message = %q; want envelope text", got.Message)
	}
	if got.APIStatus != 401 {
		t.Errorf("APIStatus = %d; want 401", got.APIStatus)
	}
}

func TestClassifyEnvelopeError_Auth403(t *testing.T) {
	t.Parallel()
	status := 403
	got := classifyEnvelopeError("forbidden", &status, 0)
	if got.Kind != ClaudeErrorAuth {
		t.Errorf("Kind = %v; want %v", got.Kind, ClaudeErrorAuth)
	}
}

func TestClassifyEnvelopeError_RateLimit429(t *testing.T) {
	t.Parallel()
	status := 429
	got := classifyEnvelopeError("Too many requests", &status, 0)
	if got.Kind != ClaudeErrorRateLimit {
		t.Errorf("Kind = %v; want %v", got.Kind, ClaudeErrorRateLimit)
	}
}

func TestClassifyEnvelopeError_Config404(t *testing.T) {
	t.Parallel()
	status := 404
	got := classifyEnvelopeError("model not found", &status, 0)
	if got.Kind != ClaudeErrorConfig {
		t.Errorf("Kind = %v; want %v", got.Kind, ClaudeErrorConfig)
	}
}

func TestClassifyEnvelopeError_Config400(t *testing.T) {
	t.Parallel()
	status := 400
	got := classifyEnvelopeError("invalid_request_error", &status, 0)
	if got.Kind != ClaudeErrorConfig {
		t.Errorf("Kind = %v; want %v", got.Kind, ClaudeErrorConfig)
	}
}

func TestClassifyEnvelopeError_UnknownNoStatus(t *testing.T) {
	t.Parallel()
	got := classifyEnvelopeError("something blew up", nil, 0)
	if got.Kind != ClaudeErrorUnknown {
		t.Errorf("Kind = %v; want %v", got.Kind, ClaudeErrorUnknown)
	}
	if got.Message != "something blew up" {
		t.Errorf("Message = %q; want envelope text", got.Message)
	}
}

func TestClassifyEnvelopeError_Unknown5xx(t *testing.T) {
	t.Parallel()
	status := 503
	got := classifyEnvelopeError("upstream error", &status, 0)
	if got.Kind != ClaudeErrorUnknown {
		t.Errorf("Kind = %v; want %v", got.Kind, ClaudeErrorUnknown)
	}
}

func TestClassifyEnvelopeError_ExitCodePropagated(t *testing.T) {
	t.Parallel()
	status := 500
	got := classifyEnvelopeError("internal error", &status, 2)
	if got.ExitCode != 2 {
		t.Errorf("ExitCode = %d; want 2", got.ExitCode)
	}
}

// --- Stderr classifier tests ---

func TestClassifyStderrError_AuthFromInvalidKey(t *testing.T) {
	t.Parallel()
	got := classifyStderrError("error: Invalid API key", 1)
	if got.Kind != ClaudeErrorAuth {
		t.Errorf("Kind = %v; want %v", got.Kind, ClaudeErrorAuth)
	}
	if got.ExitCode != 1 {
		t.Errorf("ExitCode = %d; want 1", got.ExitCode)
	}
}

func TestClassifyStderrError_AuthFromNotLoggedIn(t *testing.T) {
	t.Parallel()
	got := classifyStderrError("Please run claude login first; you are not logged in", 1)
	if got.Kind != ClaudeErrorAuth {
		t.Errorf("Kind = %v; want %v", got.Kind, ClaudeErrorAuth)
	}
}

func TestClassifyStderrError_AuthCaseInsensitive(t *testing.T) {
	t.Parallel()
	got := classifyStderrError("INVALID API KEY", 1)
	if got.Kind != ClaudeErrorAuth {
		t.Errorf("Kind = %v; want %v for case-insensitive match", got.Kind, ClaudeErrorAuth)
	}
}

func TestClassifyStderrError_UnknownPreservesMessage(t *testing.T) {
	t.Parallel()
	got := classifyStderrError("segfault", 134)
	if got.Kind != ClaudeErrorUnknown {
		t.Errorf("Kind = %v; want %v", got.Kind, ClaudeErrorUnknown)
	}
	if got.Message != "segfault" {
		t.Errorf("Message = %q; want stderr text", got.Message)
	}
	if got.ExitCode != 134 {
		t.Errorf("ExitCode = %d; want 134", got.ExitCode)
	}
}

func TestClassifyStderrError_TruncatesLongStderr(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 800)
	got := classifyStderrError(long, 1)
	if len(got.Message) > 500 {
		t.Errorf("len(Message) = %d; want <= 500 (truncated)", len(got.Message))
	}
}
