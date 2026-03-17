package agent

import (
	"strings"
	"testing"
)

func TestIsSpawnPerTurn(t *testing.T) {
	tests := []struct {
		backend string
		want    bool
	}{
		{"codex", true},
		{"opencode", true},
		{"opencode-acp", true},
		{"claude", false},
		{"", false},
	}
	for _, tt := range tests {
		tb := &TaskBuilder{backend: tt.backend}
		if got := tb.IsSpawnPerTurn(); got != tt.want {
			t.Errorf("IsSpawnPerTurn(%q) = %v, want %v", tt.backend, got, tt.want)
		}
	}
}

func TestBuildWithContinuationBody(t *testing.T) {
	// Verify that Build(continuationBody, true) correctly wraps a
	// <continuation_context>... string with skill prefix and station suffix.
	tb := &TaskBuilder{
		station: "build",
		skill:   "feature-dev:feature-dev",
		backend: "claude",
	}
	body := "<continuation_context>\n<mission>do the thing</mission>\n</continuation_context>"
	got := tb.Build(body, true)

	if !strings.HasPrefix(got, "Load your feature-dev:feature-dev skill and then:") {
		t.Errorf("expected skill prefix, got %q", got[:min(60, len(got))])
	}
	if !strings.Contains(got, "<continuation_context>") {
		t.Error("continuation body should be preserved")
	}
}
