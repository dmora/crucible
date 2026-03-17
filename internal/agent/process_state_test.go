package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/dmora/crucible/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	adksession "google.golang.org/adk/session"
)

func TestSessionProcessStateFiltering(t *testing.T) {
	// Clear global state.
	clearProcessStates(t)

	updateProcessState("sessA:draft", ProcessInfo{SessionID: "sessA", Station: "draft", State: ProcessStateRunning})
	updateProcessState("sessA:build", ProcessInfo{SessionID: "sessA", Station: "build", State: ProcessStateStopped})
	updateProcessState("sessB:draft", ProcessInfo{SessionID: "sessB", Station: "draft", State: ProcessStateRunning})
	updateProcessState("sessC:review", ProcessInfo{SessionID: "sessC", Station: "review", State: ProcessStateError})

	all := GetProcessStates()
	assert.Len(t, all, 4)

	// Filter for sessB.
	filtered := make(map[string]ProcessInfo)
	for k, info := range all {
		if info.SessionID == "sessB" {
			filtered[k] = info
		}
	}
	assert.Len(t, filtered, 1)
	assert.Equal(t, "draft", filtered["sessB:draft"].Station)
}

func TestHydrateSessionProcessStates(t *testing.T) {
	clearProcessStates(t)

	// Create an ADK session with durable state for two stations.
	svc := adksession.InMemoryService()
	resp, err := svc.Create(context.Background(), &adksession.CreateRequest{
		AppName: "crucible",
		UserID:  "user",
		State: map[string]any{
			"station:draft:state": mustMarshal(t, stationDurableState{
				Station:     "draft",
				Backend:     "claude",
				Model:       "claude-sonnet-4-5-20250514",
				ResumeID:    "resume-abc",
				ContextUsed: 42000,
				ContextSize: 200000,
				StartedAt:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).Unix(),
			}),
			"station:build:state": mustMarshal(t, stationDurableState{
				Station: "build",
				Backend: "claude",
				Model:   "claude-sonnet-4-5-20250514",
			}),
		},
	})
	require.NoError(t, err)

	stations := map[string]config.StationConfig{
		"draft": {Backend: "claude"},
		"build": {Backend: "claude"},
	}

	sessionID := resp.Session.ID()
	err = HydrateSessionProcessStates(resp.Session, sessionID, stations)
	require.NoError(t, err)

	states := GetProcessStates()
	assert.Len(t, states, 2)

	draft := states[sessionID+":draft"]
	assert.Equal(t, ProcessStateStopped, draft.State)
	assert.Equal(t, "claude", draft.Backend)
	assert.Equal(t, "claude-sonnet-4-5-20250514", draft.Model)
	assert.Equal(t, "resume-abc", draft.ResumeID)
	assert.Equal(t, 42000, draft.ContextUsed)
	assert.Equal(t, 200000, draft.ContextSize)
	assert.Equal(t, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).Unix(), draft.StartedAt.Unix())

	build := states[sessionID+":build"]
	assert.Equal(t, ProcessStateStopped, build.State)
	assert.Equal(t, "claude-sonnet-4-5-20250514", build.Model)
}

func TestHydrateSkipsLiveProcess(t *testing.T) {
	clearProcessStates(t)

	// Pre-populate a running entry.
	updateProcessState("sess1:draft", ProcessInfo{
		SessionID: "sess1",
		Station:   "draft",
		State:     ProcessStateRunning,
		Model:     "live-model",
	})

	svc := adksession.InMemoryService()
	resp, err := svc.Create(context.Background(), &adksession.CreateRequest{
		AppName:   "crucible",
		UserID:    "user",
		SessionID: "sess1",
		State: map[string]any{
			"station:draft:state": mustMarshal(t, stationDurableState{
				Station: "draft",
				Backend: "claude",
				Model:   "old-model",
			}),
		},
	})
	require.NoError(t, err)

	err = HydrateSessionProcessStates(resp.Session, "sess1", map[string]config.StationConfig{
		"draft": {Backend: "claude"},
	})
	require.NoError(t, err)

	info, ok := processStates.Get("sess1:draft")
	require.True(t, ok)
	assert.Equal(t, ProcessStateRunning, info.State)
	assert.Equal(t, "live-model", info.Model) // not overwritten
}

func TestHydrateNilSession(t *testing.T) {
	clearProcessStates(t)

	err := HydrateSessionProcessStates(nil, "sess1", map[string]config.StationConfig{
		"draft": {Backend: "claude"},
	})
	assert.NoError(t, err)
	assert.Empty(t, GetProcessStates())
}

func TestPurgeSessionProcessStates(t *testing.T) {
	clearProcessStates(t)

	updateProcessState("sessA:draft", ProcessInfo{SessionID: "sessA", Station: "draft", State: ProcessStateRunning})
	updateProcessState("sessA:build", ProcessInfo{SessionID: "sessA", Station: "build", State: ProcessStateStopped})
	updateProcessState("sessB:draft", ProcessInfo{SessionID: "sessB", Station: "draft", State: ProcessStateRunning})

	PurgeSessionProcessStates("sessA")

	states := GetProcessStates()
	assert.Len(t, states, 1)
	assert.Contains(t, states, "sessB:draft")
}

func TestStationDurableStateRoundTrip(t *testing.T) {
	original := stationDurableState{
		Station:     "build",
		Backend:     "claude",
		Model:       "claude-sonnet-4-5-20250514",
		ResumeID:    "resume-xyz",
		ContextUsed: 150000,
		ContextSize: 200000,
		StartedAt:   time.Now().Unix(),
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var restored stationDurableState
	require.NoError(t, json.Unmarshal(data, &restored))

	assert.Equal(t, original, restored)
}

// clearProcessStates resets the global processStates map for test isolation.
func clearProcessStates(t *testing.T) {
	t.Helper()
	for k := range processStates.Copy() {
		processStates.Del(k)
	}
}

// mustMarshal marshals v to a JSON string, failing the test on error.
func mustMarshal(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	return string(data)
}
