package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupDispatch is a test helper that appends a dispatch and immediately
// completes it with the given verdict.
func setupDispatch(sessionID, station string, verdict DispatchVerdict) {
	idx := AppendDispatch(sessionID, station)
	CompleteDispatch(sessionID, idx, verdict, "")
}

// --- 8 validated scenarios from issue #75 ---

// Scenario 1: Happy path — draft→build→review→verify→ship all succeed.
func TestCheckRoute_HappyPath(t *testing.T) {
	clearDispatchLogs(t)
	sid := "route-happy"

	setupDispatch(sid, "draft", VerdictDone)
	setupDispatch(sid, "build", VerdictDone)
	setupDispatch(sid, "review", VerdictDone)
	setupDispatch(sid, "verify", VerdictDone)

	// Each station in the default pipeline should pass.
	denial, err := checkRoute(sid, "build", []string{"draft"}, []string{"review"})
	require.NoError(t, err)
	assert.Empty(t, denial, "build should pass: draft done, review done after build")

	denial, err = checkRoute(sid, "review", []string{"build"}, nil)
	require.NoError(t, err)
	assert.Empty(t, denial)

	denial, err = checkRoute(sid, "verify", []string{"review"}, nil)
	require.NoError(t, err)
	assert.Empty(t, denial)

	denial, err = checkRoute(sid, "ship", []string{"verify"}, nil)
	require.NoError(t, err)
	assert.Empty(t, denial)
}

// Scenario 2: Verify finds bugs → build→review→verify→ship works.
func TestCheckRoute_VerifyFindsBugs_RebuildCycle(t *testing.T) {
	clearDispatchLogs(t)
	sid := "route-rebuild"

	// Initial flow: draft → build → review → verify (finds bugs)
	setupDispatch(sid, "draft", VerdictDone)
	setupDispatch(sid, "build", VerdictDone)
	setupDispatch(sid, "review", VerdictDone)
	setupDispatch(sid, "verify", VerdictDone) // found bugs, supervisor re-dispatches

	// Rebuild: review satisfies afterDone for build
	denial, err := checkRoute(sid, "build", []string{"draft"}, []string{"review"})
	require.NoError(t, err)
	assert.Empty(t, denial, "build should pass: review ran after last build")

	// Second build succeeds
	setupDispatch(sid, "build", VerdictDone)

	// Review after second build
	denial, err = checkRoute(sid, "review", []string{"build"}, nil)
	require.NoError(t, err)
	assert.Empty(t, denial)

	setupDispatch(sid, "review", VerdictDone)

	// Verify after second review
	denial, err = checkRoute(sid, "verify", []string{"review"}, nil)
	require.NoError(t, err)
	assert.Empty(t, denial)

	setupDispatch(sid, "verify", VerdictDone)

	// Ship after verify
	denial, err = checkRoute(sid, "ship", []string{"verify"}, nil)
	require.NoError(t, err)
	assert.Empty(t, denial)
}

// Scenario 3: Build fails → retry is allowed (afterDone only on VerdictDone).
func TestCheckRoute_BuildFails_RetryAllowed(t *testing.T) {
	clearDispatchLogs(t)
	sid := "route-retry"

	setupDispatch(sid, "draft", VerdictDone)
	setupDispatch(sid, "build", VerdictFailed) // build fails

	// Retry build — should pass because afterDone only triggers on VerdictDone.
	denial, err := checkRoute(sid, "build", []string{"draft"}, []string{"review"})
	require.NoError(t, err)
	assert.Empty(t, denial, "build retry should be allowed after failure")
}

// Scenario 4: Build→build chain is blocked.
func TestCheckRoute_BuildBuildChain_Blocked(t *testing.T) {
	clearDispatchLogs(t)
	sid := "route-blocked"

	setupDispatch(sid, "draft", VerdictDone)
	setupDispatch(sid, "build", VerdictDone) // build succeeds

	// Second build without review — should be denied.
	denial, err := checkRoute(sid, "build", []string{"draft"}, []string{"review"})
	require.NoError(t, err)
	assert.Contains(t, denial, "DENIED")
	assert.Contains(t, denial, "review")
}

// Scenario 5: Draft↔inspect loop — no constraints, always free.
func TestCheckRoute_DraftInspectLoop_Free(t *testing.T) {
	clearDispatchLogs(t)
	sid := "route-free"

	// Draft and inspect have no constraints.
	denial, err := checkRoute(sid, "draft", nil, nil)
	require.NoError(t, err)
	assert.Empty(t, denial)

	setupDispatch(sid, "draft", VerdictDone)

	denial, err = checkRoute(sid, "inspect", nil, nil)
	require.NoError(t, err)
	assert.Empty(t, denial)

	setupDispatch(sid, "inspect", VerdictDone)

	// Dispatch draft again — still free.
	denial, err = checkRoute(sid, "draft", nil, nil)
	require.NoError(t, err)
	assert.Empty(t, denial)
}

// Scenario 6: Skip draft, go straight to build — blocked.
func TestCheckRoute_SkipDraft_BuildBlocked(t *testing.T) {
	clearDispatchLogs(t)
	sid := "route-skip-draft"

	// No dispatches at all — build requires draft.
	denial, err := checkRoute(sid, "build", []string{"draft"}, []string{"review"})
	require.NoError(t, err)
	assert.Contains(t, denial, "DENIED")
	assert.Contains(t, denial, "draft")
}

// Scenario 7: Ship without verify — blocked.
func TestCheckRoute_ShipWithoutVerify_Blocked(t *testing.T) {
	clearDispatchLogs(t)
	sid := "route-ship-no-verify"

	setupDispatch(sid, "draft", VerdictDone)
	setupDispatch(sid, "build", VerdictDone)
	setupDispatch(sid, "review", VerdictDone)
	// No verify — ship requires verify.

	denial, err := checkRoute(sid, "ship", []string{"verify"}, nil)
	require.NoError(t, err)
	assert.Contains(t, denial, "DENIED")
	assert.Contains(t, denial, "verify")
}

// Scenario 8: User removes afterDone from build — build→build allowed.
func TestCheckRoute_NoAfterDone_BuildBuildAllowed(t *testing.T) {
	clearDispatchLogs(t)
	sid := "route-no-afterdone"

	setupDispatch(sid, "draft", VerdictDone)
	setupDispatch(sid, "build", VerdictDone)

	// With empty afterDone (user override), build→build is allowed.
	denial, err := checkRoute(sid, "build", []string{"draft"}, nil)
	require.NoError(t, err)
	assert.Empty(t, denial, "build→build should be allowed when afterDone is empty")
}

// --- Additional edge case tests ---

func TestCheckRoute_CanceledDispatch_DoesNotTriggerAfterDone(t *testing.T) {
	clearDispatchLogs(t)
	sid := "route-canceled"

	setupDispatch(sid, "draft", VerdictDone)
	setupDispatch(sid, "build", VerdictCanceled) // canceled, not done

	// afterDone should not apply because build never completed with VerdictDone.
	denial, err := checkRoute(sid, "build", []string{"draft"}, []string{"review"})
	require.NoError(t, err)
	assert.Empty(t, denial, "canceled builds should not trigger afterDone constraint")
}

// A required station's only dispatch is VerdictFailed — requires is not satisfied.
func TestCheckRoute_RequiresFailedOnly_Blocked(t *testing.T) {
	clearDispatchLogs(t)
	sid := "route-req-failed"

	setupDispatch(sid, "draft", VerdictFailed) // draft ran but failed

	denial, err := checkRoute(sid, "build", []string{"draft"}, []string{"review"})
	require.NoError(t, err)
	assert.Contains(t, denial, "DENIED")
	assert.Contains(t, denial, "draft")
}

func TestCheckRoute_RequiresMultipleStations(t *testing.T) {
	clearDispatchLogs(t)
	sid := "route-multi-req"

	setupDispatch(sid, "review", VerdictDone)
	// verify not done yet

	// Ship requires both review and verify.
	denial, err := checkRoute(sid, "ship", []string{"review", "verify"}, nil)
	require.NoError(t, err)
	assert.Contains(t, denial, "DENIED")
	assert.Contains(t, denial, "verify")

	// Now complete verify.
	setupDispatch(sid, "verify", VerdictDone)

	denial, err = checkRoute(sid, "ship", []string{"review", "verify"}, nil)
	require.NoError(t, err)
	assert.Empty(t, denial)
}

// --- Artifact enforcement tests ---

// newArtifactToolContext creates a mockToolContext with optional version marker.
func newArtifactToolContext(t *testing.T, setVersionMarker bool) *mockToolContext {
	t.Helper()
	tctx := newMockToolContext()
	if setVersionMarker {
		_ = tctx.State().Set("app:artifact_registry_version", "1")
	}
	return tctx
}

func TestCheckArtifact_NoRequirements(t *testing.T) {
	tctx := newArtifactToolContext(t, true)
	denial, err := checkArtifact(tctx, "build", nil, false, "sess-no-req")
	require.NoError(t, err)
	assert.Empty(t, denial)
}

func TestCheckArtifact_DisableEnforcement(t *testing.T) {
	tctx := newArtifactToolContext(t, true)
	denial, err := checkArtifact(tctx, "build", []string{"plan"}, true, "sess-disabled")
	require.NoError(t, err)
	assert.Empty(t, denial)
}

func TestCheckArtifact_PresentInRegistry(t *testing.T) {
	tctx := newArtifactToolContext(t, true)
	setActiveArtifact(tctx, "plan", "/tmp/spec.md", 1)

	denial, err := checkArtifact(tctx, "build", []string{"plan"}, false, "sess-present")
	require.NoError(t, err)
	assert.Empty(t, denial)
}

func TestCheckArtifact_MissingArtifact_CurrentSession(t *testing.T) {
	clearDispatchLogs(t)
	tctx := newArtifactToolContext(t, true) // version marker set = current session

	denial, err := checkArtifact(tctx, "build", []string{"plan"}, false, "sess-missing")
	require.NoError(t, err)
	assert.Contains(t, denial, "DENIED")
	assert.Contains(t, denial, "plan")
}

func TestCheckArtifact_LegacyFallback(t *testing.T) {
	clearDispatchLogs(t)
	sid := "sess-legacy"
	tctx := newArtifactToolContext(t, false) // no version marker = legacy session

	// Plan completed with VerdictDone in dispatch log.
	setupDispatch(sid, "plan", VerdictDone)

	denial, err := checkArtifact(tctx, "build", []string{"plan"}, false, sid)
	require.NoError(t, err)
	assert.Empty(t, denial, "legacy session should fall back to dispatch log")
}

func TestCheckArtifact_CurrentSession_NoFallback(t *testing.T) {
	clearDispatchLogs(t)
	sid := "sess-current-no-fallback"
	tctx := newArtifactToolContext(t, true) // version marker set

	// Plan completed but produced no artifact file (registry empty).
	setupDispatch(sid, "plan", VerdictDone)

	denial, err := checkArtifact(tctx, "build", []string{"plan"}, false, sid)
	require.NoError(t, err)
	assert.Contains(t, denial, "DENIED", "current session must not fall back to dispatch log")
}

func TestCheckArtifact_MultipleRequired_OneMissing(t *testing.T) {
	tctx := newArtifactToolContext(t, true)
	setActiveArtifact(tctx, "plan", "/tmp/spec.md", 1)
	// "design" artifact missing.

	denial, err := checkArtifact(tctx, "build", []string{"plan", "design"}, false, "sess-multi")
	require.NoError(t, err)
	assert.Contains(t, denial, "DENIED")
	assert.Contains(t, denial, "design")
}

func TestCheckArtifact_SkipFlag(t *testing.T) {
	tctx := newArtifactToolContext(t, true)
	_ = tctx.State().Set("app:skip_artifact_check", "true")

	denial, err := checkArtifact(tctx, "build", []string{"plan"}, false, "sess-skip")
	require.NoError(t, err)
	assert.Empty(t, denial, "skip flag should bypass enforcement")
}

func TestCheckArtifact_LegacySessionFirstDispatchAfterUpgrade(t *testing.T) {
	clearDispatchLogs(t)
	sid := "sess-legacy-upgrade"
	tctx := newArtifactToolContext(t, false) // no version marker — legacy session

	// Plan completed in legacy session (no registry entry, but dispatch log has it).
	setupDispatch(sid, "plan", VerdictDone)

	// Build dispatches — should pass via legacy fallback.
	denial, err := checkArtifact(tctx, "build", []string{"plan"}, false, sid)
	require.NoError(t, err)
	assert.Empty(t, denial, "legacy session should use dispatch log fallback even after upgrade")
}
