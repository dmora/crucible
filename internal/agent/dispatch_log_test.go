package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	adksession "google.golang.org/adk/session"
)

// clearDispatchLogs resets the global dispatchLogs map for test isolation.
func clearDispatchLogs(t *testing.T) {
	t.Helper()
	for k := range dispatchLogs.Copy() {
		dispatchLogs.Del(k)
	}
}

func TestAppendDispatch(t *testing.T) {
	clearDispatchLogs(t)

	idx0 := AppendDispatch("sess1", "draft")
	idx1 := AppendDispatch("sess1", "inspect")
	idx2 := AppendDispatch("sess1", "draft") // same station, second invocation

	assert.Equal(t, 0, idx0)
	assert.Equal(t, 1, idx1)
	assert.Equal(t, 2, idx2)

	log := GetDispatchLog("sess1")
	require.Len(t, log, 3)

	assert.Equal(t, "draft", log[0].Station)
	assert.Equal(t, VerdictRunning, log[0].Verdict)
	assert.Equal(t, 0, log[0].Seq)

	assert.Equal(t, "inspect", log[1].Station)
	assert.Equal(t, 1, log[1].Seq)

	assert.Equal(t, "draft", log[2].Station)
	assert.Equal(t, 2, log[2].Seq)
}

func TestCompleteDispatch(t *testing.T) {
	clearDispatchLogs(t)

	idx := AppendDispatch("sess1", "build")
	time.Sleep(5 * time.Millisecond) // ensure non-zero duration
	CompleteDispatch("sess1", idx, VerdictDone)

	log := GetDispatchLog("sess1")
	require.Len(t, log, 1)
	assert.Equal(t, VerdictDone, log[0].Verdict)
	assert.Greater(t, log[0].Duration, time.Duration(0))
}

func TestCompleteDispatchVerdicts(t *testing.T) {
	clearDispatchLogs(t)

	idxDone := AppendDispatch("sess1", "draft")
	idxFailed := AppendDispatch("sess1", "inspect")
	idxCanceled := AppendDispatch("sess1", "build")

	CompleteDispatch("sess1", idxDone, VerdictDone)
	CompleteDispatch("sess1", idxFailed, VerdictFailed)
	CompleteDispatch("sess1", idxCanceled, VerdictCanceled)

	log := GetDispatchLog("sess1")
	require.Len(t, log, 3)
	assert.Equal(t, VerdictDone, log[0].Verdict)
	assert.Equal(t, VerdictFailed, log[1].Verdict)
	assert.Equal(t, VerdictCanceled, log[2].Verdict)
}

func TestCompleteDispatchInvalidIndex(t *testing.T) {
	clearDispatchLogs(t)

	AppendDispatch("sess1", "draft")

	// Out-of-bounds index should not panic.
	CompleteDispatch("sess1", -1, VerdictDone)
	CompleteDispatch("sess1", 99, VerdictDone)

	// Non-existent session should not panic.
	CompleteDispatch("nonexistent", 0, VerdictDone)

	log := GetDispatchLog("sess1")
	require.Len(t, log, 1)
	assert.Equal(t, VerdictRunning, log[0].Verdict)
}

func TestGetDispatchLogReturnsNilForUnknownSession(t *testing.T) {
	clearDispatchLogs(t)
	assert.Nil(t, GetDispatchLog("unknown"))
}

func TestGetDispatchLogReturnsCopy(t *testing.T) {
	clearDispatchLogs(t)

	AppendDispatch("sess1", "draft")
	log1 := GetDispatchLog("sess1")
	log1[0].Verdict = VerdictFailed // mutate the copy

	log2 := GetDispatchLog("sess1")
	assert.Equal(t, VerdictRunning, log2[0].Verdict) // original unchanged
}

func TestSetDispatchLog(t *testing.T) {
	clearDispatchLogs(t)

	entries := []DispatchEntry{
		{Station: "draft", Verdict: VerdictDone, Seq: 0},
		{Station: "inspect", Verdict: VerdictFailed, Seq: 1},
	}
	SetDispatchLog("sess1", entries)

	log := GetDispatchLog("sess1")
	require.Len(t, log, 2)
	assert.Equal(t, "draft", log[0].Station)
	assert.Equal(t, "inspect", log[1].Station)

	// nextSeq should be set correctly for future appends.
	idx := AppendDispatch("sess1", "build")
	assert.Equal(t, 2, idx)
	log = GetDispatchLog("sess1")
	assert.Equal(t, 2, log[2].Seq)
}

func TestPurgeSessionDispatchLog(t *testing.T) {
	clearDispatchLogs(t)

	AppendDispatch("sess1", "draft")
	AppendDispatch("sess2", "build")

	purgeSessionDispatchLog("sess1")

	assert.Nil(t, GetDispatchLog("sess1"))
	assert.Len(t, GetDispatchLog("sess2"), 1)
}

func TestPurgeSessionProcessStatesAlsoPurgesDispatchLog(t *testing.T) {
	clearProcessStates(t)
	clearDispatchLogs(t)

	updateProcessState("sess1:draft", ProcessInfo{SessionID: "sess1", Station: "draft", State: ProcessStateRunning})
	AppendDispatch("sess1", "draft")

	PurgeSessionProcessStates("sess1")

	assert.Empty(t, GetProcessStates())
	assert.Nil(t, GetDispatchLog("sess1"))
}

func TestCompleteDispatchSnapshotsContextUsage(t *testing.T) {
	clearDispatchLogs(t)
	clearProcessStates(t)

	// Set up process info with context usage.
	updateProcessState("sess1:build", ProcessInfo{
		SessionID:   "sess1",
		Station:     "build",
		State:       ProcessStateRunning,
		ContextUsed: 42000,
		ContextSize: 200000,
	})

	idx := AppendDispatch("sess1", "build")
	CompleteDispatch("sess1", idx, VerdictDone)

	log := GetDispatchLog("sess1")
	require.Len(t, log, 1)
	assert.Equal(t, 42000, log[0].ContextUsed)
	assert.Equal(t, 200000, log[0].ContextSize)
}

func TestCompleteDispatchNoProcessState(t *testing.T) {
	clearDispatchLogs(t)
	clearProcessStates(t)

	// No process state — context fields should remain zero.
	idx := AppendDispatch("sess1", "draft")
	CompleteDispatch("sess1", idx, VerdictDone)

	log := GetDispatchLog("sess1")
	require.Len(t, log, 1)
	assert.Equal(t, 0, log[0].ContextUsed)
	assert.Equal(t, 0, log[0].ContextSize)
}

func TestDurableDispatchRoundTrip(t *testing.T) {
	original := []durableDispatchEntry{
		{Station: "draft", Verdict: int(VerdictDone), StartedAt: 1700000000, DurationMs: 134000, Seq: 0, ContextUsed: 42000, ContextSize: 200000},
		{Station: "inspect", Verdict: int(VerdictFailed), StartedAt: 1700000200, DurationMs: 62000, Seq: 1},
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var restored []durableDispatchEntry
	require.NoError(t, json.Unmarshal(data, &restored))
	assert.Equal(t, original, restored)
}

func TestHydrateDispatchLog(t *testing.T) {
	clearDispatchLogs(t)

	// Create ADK session with persisted dispatch log (including context usage).
	durable := []durableDispatchEntry{
		{Station: "draft", Verdict: int(VerdictDone), StartedAt: 1700000000, DurationMs: 134000, Seq: 0, ContextUsed: 42000, ContextSize: 200000},
		{Station: "inspect", Verdict: int(VerdictRunning), StartedAt: 1700000200, DurationMs: 0, Seq: 1},
	}
	data, err := json.Marshal(durable)
	require.NoError(t, err)

	svc := adksession.InMemoryService()
	resp, err := svc.Create(context.Background(), &adksession.CreateRequest{
		AppName: "crucible",
		UserID:  "user",
		State: map[string]any{
			dispatchLogStateKey: string(data),
		},
	})
	require.NoError(t, err)

	sessionID := resp.Session.ID()
	hydrateDispatchLog(resp.Session, sessionID)

	log := GetDispatchLog(sessionID)
	require.Len(t, log, 2)

	assert.Equal(t, "draft", log[0].Station)
	assert.Equal(t, VerdictDone, log[0].Verdict)
	assert.Equal(t, 134*time.Second, log[0].Duration)
	assert.Equal(t, 42000, log[0].ContextUsed)
	assert.Equal(t, 200000, log[0].ContextSize)

	// Running entry from dead session should become Canceled.
	assert.Equal(t, "inspect", log[1].Station)
	assert.Equal(t, VerdictCanceled, log[1].Verdict)
}

func TestHydrateDispatchLogNilSession(t *testing.T) {
	clearDispatchLogs(t)

	// Should not panic.
	hydrateDispatchLog(nil, "sess1")
	assert.Nil(t, GetDispatchLog("sess1"))
}
