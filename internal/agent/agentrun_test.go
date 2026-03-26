package agent

import (
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dmora/crucible/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestNewStationProcessManagerEnv(t *testing.T) {
	tests := []struct {
		name    string
		backend string
		env     map[string]string
		wantEnv map[string]string
	}{
		{
			name:    "claude nil env",
			backend: "claude",
			env:     nil,
			wantEnv: nil,
		},
		{
			name:    "claude empty env",
			backend: "claude",
			env:     map[string]string{},
			wantEnv: map[string]string{},
		},
		{
			name:    "claude env vars passed through",
			backend: "claude",
			env:     map[string]string{"FOO": "bar", "BAZ": "qux"},
			wantEnv: map[string]string{"FOO": "bar", "BAZ": "qux"},
		},
		{
			name:    "opencode nil env gets permission injected",
			backend: "opencode",
			env:     nil,
			wantEnv: map[string]string{"OPENCODE_PERMISSION": `{"external_directory":"allow"}`},
		},
		{
			name:    "opencode empty env gets permission injected",
			backend: "opencode",
			env:     map[string]string{},
			wantEnv: map[string]string{"OPENCODE_PERMISSION": `{"external_directory":"allow"}`},
		},
		{
			name:    "opencode custom permission preserved",
			backend: "opencode",
			env:     map[string]string{"OPENCODE_PERMISSION": `{"custom":"value"}`},
			wantEnv: map[string]string{"OPENCODE_PERMISSION": `{"custom":"value"}`},
		},
		{
			name:    "opencode extra env preserved with permission",
			backend: "opencode",
			env:     map[string]string{"FOO": "bar"},
			wantEnv: map[string]string{"FOO": "bar", "OPENCODE_PERMISSION": `{"external_directory":"allow"}`},
		},
		{
			name:    "opencode-acp nil env gets permission injected",
			backend: "opencode-acp",
			env:     nil,
			wantEnv: map[string]string{"OPENCODE_PERMISSION": `{"external_directory":"allow"}`},
		},
		{
			name:    "opencode-acp custom permission preserved",
			backend: "opencode-acp",
			env:     map[string]string{"OPENCODE_PERMISSION": `{"custom":"value"}`},
			wantEnv: map[string]string{"OPENCODE_PERMISSION": `{"custom":"value"}`},
		},
		{
			name:    "opencode-acp extra env preserved with permission",
			backend: "opencode-acp",
			env:     map[string]string{"FOO": "bar"},
			wantEnv: map[string]string{"FOO": "bar", "OPENCODE_PERMISSION": `{"external_directory":"allow"}`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origEnv := tt.env
			pm := newStationProcessManager("test-station", "/tmp", config.StationConfig{
				Backend: tt.backend,
				Env:     tt.env,
			}, 0)
			assert.Equal(t, tt.wantEnv, pm.env)
			// Verify the original config map was not mutated.
			if origEnv != nil {
				assert.Equal(t, origEnv, tt.env, "caller's env map should not be mutated")
			}
		})
	}
}

func TestNewStationToolDescriptionIncludesIsolationFooter(t *testing.T) {
	pm := newStationProcessManager("test-station", "/tmp", config.StationConfig{}, 0)

	var hold, abort atomic.Bool
	tl, err := newStationTool(pm, "sess-1", "Run tests for the project", nil, &hold, nil, &abort)
	assert.NoError(t, err)

	desc := tl.Description()
	assert.Contains(t, desc, "Run tests for the project", "original description preserved")
	assert.Contains(t, desc, stationIsolationFooter, "isolation footer appended")
	assert.True(t, strings.HasSuffix(desc, stationIsolationFooter),
		"footer should be the suffix of the description")
}

func TestBuildTask(t *testing.T) {
	tests := []struct {
		name      string
		backend   string
		skill     string
		task      string
		firstTurn bool
		want      string
	}{
		{
			name:      "no skill, first turn",
			backend:   "claude",
			skill:     "",
			task:      "implement the login page",
			firstTurn: true,
			want:      "implement the login page",
		},
		{
			name:      "no skill, not first turn",
			backend:   "claude",
			skill:     "",
			task:      "implement the login page",
			firstTurn: false,
			want:      "implement the login page",
		},
		{
			name:      "claude skill, first turn",
			backend:   "claude",
			skill:     "feature-dev:feature-dev",
			task:      "implement the login page",
			firstTurn: true,
			want:      "Load your feature-dev:feature-dev skill and then: implement the login page",
		},
		{
			name:      "claude skill, not first turn",
			backend:   "claude",
			skill:     "feature-dev:feature-dev",
			task:      "implement the login page",
			firstTurn: false,
			want:      "implement the login page",
		},
		{
			name:      "codex skill, first turn",
			backend:   "codex",
			skill:     "$rigorous-pr-review",
			task:      "review the auth module",
			firstTurn: true,
			want:      "lets perform a $rigorous-pr-review: review the auth module",
		},
		{
			name:      "codex skill, not first turn",
			backend:   "codex",
			skill:     "$rigorous-pr-review",
			task:      "review the auth module",
			firstTurn: false,
			want:      "review the auth module",
		},
		{
			name:      "opencode skill, first turn",
			backend:   "opencode",
			skill:     "review-plan",
			task:      "check the plan",
			firstTurn: true,
			want:      "/review-plan check the plan",
		},
		{
			name:      "opencode skill, not first turn",
			backend:   "opencode",
			skill:     "review-plan",
			task:      "check the plan",
			firstTurn: false,
			want:      "check the plan",
		},
		{
			name:      "opencode-acp skill, first turn",
			backend:   "opencode-acp",
			skill:     "review-plan",
			task:      "check the plan",
			firstTurn: true,
			want:      "/review-plan check the plan",
		},
		{
			name:      "opencode-acp skill, not first turn",
			backend:   "opencode-acp",
			skill:     "review-plan",
			task:      "check the plan",
			firstTurn: false,
			want:      "check the plan",
		},
		{
			name:      "claude namespaced skill, first turn",
			backend:   "claude",
			skill:     "claude-code-quality:review-plan",
			task:      "check the plan",
			firstTurn: true,
			want:      "Load your claude-code-quality:review-plan skill and then: check the plan",
		},
		{
			name:      "empty task with skill, first turn",
			backend:   "claude",
			skill:     "feature-dev:feature-dev",
			task:      "",
			firstTurn: true,
			want:      "Load your feature-dev:feature-dev skill and then: ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tb := &TaskBuilder{backend: tt.backend, skill: tt.skill}
			got := tb.Build(stationInput{Task: tt.task}, tt.firstTurn, "")
			assert.Equal(t, tt.want, got)
		})
	}
}
