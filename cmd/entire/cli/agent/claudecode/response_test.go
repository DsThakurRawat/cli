package claudecode

import (
	"strings"
	"testing"
)

func TestParseGenerateTextResponse_IsErrorEnvelope(t *testing.T) {
	t.Parallel()
	stdout := `{"type":"result","subtype":"success","is_error":true,"api_error_status":404,"result":"model not found"}`
	result, env, err := parseGenerateTextResponse([]byte(stdout))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "model not found" {
		t.Errorf("result = %q; want %q", result, "model not found")
	}
	if env == nil {
		t.Fatal("envelope = nil; want non-nil")
	}
	if !env.IsError {
		t.Error("IsError = false; want true")
	}
	if env.APIErrorStatus == nil || *env.APIErrorStatus != 404 {
		t.Errorf("APIErrorStatus = %v; want *404", env.APIErrorStatus)
	}
}

func TestParseGenerateTextResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		stdout  string
		want    string
		wantErr string
	}{
		{
			name:   "legacy object result",
			stdout: `{"result":"hello"}`,
			want:   "hello",
		},
		{
			name:   "legacy object empty result",
			stdout: `{"result":""}`,
			want:   "",
		},
		{
			name:   "array result",
			stdout: `[{"type":"system"},{"type":"result","result":"hello"}]`,
			want:   "hello",
		},
		{
			name:   "array empty result",
			stdout: `[{"type":"system"},{"type":"result","result":""}]`,
			want:   "",
		},
		{
			name:    "missing result item",
			stdout:  `[{"type":"system"},{"type":"assistant","message":"working"}]`,
			wantErr: "missing result item",
		},
		{
			name:    "invalid json",
			stdout:  `not json`,
			wantErr: "unsupported Claude CLI JSON response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, _, err := parseGenerateTextResponse([]byte(tt.stdout))
			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("expected error")
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("parseGenerateTextResponse() = %q, want %q", got, tt.want)
			}
		})
	}
}
