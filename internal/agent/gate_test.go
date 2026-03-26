package agent

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/dmora/crucible/internal/permission"
	"github.com/dmora/crucible/internal/pubsub"
	"github.com/stretchr/testify/require"
)

// mockPermissionService implements permission.Service for gate tests.
// Only Request and SkipRequests are exercised; other methods panic.
type mockPermissionService struct {
	grantResult bool
	err         error
	lastRequest permission.CreatePermissionRequest
	callCount   int
}

func (m *mockPermissionService) Request(_ context.Context, opts permission.CreatePermissionRequest) (bool, error) {
	m.callCount++
	m.lastRequest = opts
	return m.grantResult, m.err
}

func (m *mockPermissionService) SkipRequests() bool { return false }

func (m *mockPermissionService) Subscribe(_ context.Context) <-chan pubsub.Event[permission.PermissionRequest] {
	panic("not implemented")
}

func (m *mockPermissionService) SubscribeNotifications(_ context.Context) <-chan pubsub.Event[permission.PermissionNotification] {
	panic("not implemented")
}

func (m *mockPermissionService) GrantPersistent(_ permission.PermissionRequest) {
	panic("not implemented")
}

func (m *mockPermissionService) Grant(_ permission.PermissionRequest) {
	panic("not implemented")
}

func (m *mockPermissionService) Deny(_ permission.PermissionRequest) {
	panic("not implemented")
}

func (m *mockPermissionService) AutoApproveSession(_ string) {
	panic("not implemented")
}

func (m *mockPermissionService) SetSkipRequests(_ bool) {
	panic("not implemented")
}

func TestCheckGate_NotGated(t *testing.T) {
	t.Parallel()
	mock := &mockPermissionService{grantResult: true}
	hold := &atomic.Bool{}

	approved, err := checkGate(context.Background(), permission.CreatePermissionRequest{}, false, hold, mock)

	require.NoError(t, err)
	require.True(t, approved)
	require.Equal(t, 0, mock.callCount, "Request should not be called when not gated")
}

func TestCheckGate_Gated_Approved(t *testing.T) {
	t.Parallel()
	mock := &mockPermissionService{grantResult: true}

	approved, err := checkGate(context.Background(), permission.CreatePermissionRequest{
		ToolName: "gate:build",
	}, true, nil, mock)

	require.NoError(t, err)
	require.True(t, approved)
	require.Equal(t, 1, mock.callCount)
}

func TestCheckGate_Gated_Denied(t *testing.T) {
	t.Parallel()
	mock := &mockPermissionService{grantResult: false}

	approved, err := checkGate(context.Background(), permission.CreatePermissionRequest{
		ToolName: "gate:build",
	}, true, nil, mock)

	require.NoError(t, err)
	require.False(t, approved)
	require.Equal(t, 1, mock.callCount)
}

func TestCheckGate_HoldFlag_Persists(t *testing.T) {
	t.Parallel()
	mock := &mockPermissionService{grantResult: true}
	hold := &atomic.Bool{}
	hold.Store(true)

	// First call: hold is set, triggers gate, hold persists.
	approved, err := checkGate(context.Background(), permission.CreatePermissionRequest{}, false, hold, mock)
	require.NoError(t, err)
	require.True(t, approved)
	require.Equal(t, 1, mock.callCount)
	require.True(t, hold.Load(), "hold flag should persist — only operator toggle clears it")

	// Second call: hold still active, triggers gate again.
	approved, err = checkGate(context.Background(), permission.CreatePermissionRequest{}, false, hold, mock)
	require.NoError(t, err)
	require.True(t, approved)
	require.Equal(t, 2, mock.callCount, "Request should be called again because hold persists")
}

func TestCheckGate_HoldFlag_PlusGate(t *testing.T) {
	t.Parallel()
	mock := &mockPermissionService{grantResult: true}
	hold := &atomic.Bool{}
	hold.Store(true)

	// Both gated and hold set — only one Request call, hold persists.
	approved, err := checkGate(context.Background(), permission.CreatePermissionRequest{}, true, hold, mock)

	require.NoError(t, err)
	require.True(t, approved)
	require.Equal(t, 1, mock.callCount, "should call Request exactly once")
	require.True(t, hold.Load(), "hold flag should persist — only operator toggle clears it")
}

func TestCheckGate_NilPerms(t *testing.T) {
	t.Parallel()

	// Gated but no permission service — no enforcement.
	approved, err := checkGate(context.Background(), permission.CreatePermissionRequest{}, true, nil, nil)

	require.NoError(t, err)
	require.True(t, approved)
}

func TestCheckGate_ContextCanceled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	mock := &mockPermissionService{err: ctx.Err()}

	approved, err := checkGate(ctx, permission.CreatePermissionRequest{}, true, nil, mock)

	require.ErrorIs(t, err, context.Canceled)
	require.False(t, approved)
}

func TestCheckGate_HoldFlag_Denied_Persists(t *testing.T) {
	t.Parallel()
	mock := &mockPermissionService{grantResult: false}
	hold := &atomic.Bool{}
	hold.Store(true)

	// Operator denies — hold must still be active.
	approved, err := checkGate(context.Background(), permission.CreatePermissionRequest{}, false, hold, mock)
	require.NoError(t, err)
	require.False(t, approved)
	require.True(t, hold.Load(), "hold flag should persist after denial")
	require.Equal(t, 1, mock.callCount)
}

func TestCheckGate_HoldFlag_Error_Persists(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	mock := &mockPermissionService{err: ctx.Err()}
	hold := &atomic.Bool{}
	hold.Store(true)

	// Context error — hold must still be active.
	approved, err := checkGate(ctx, permission.CreatePermissionRequest{}, false, hold, mock)
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, approved)
	require.True(t, hold.Load(), "hold flag should persist after error")
}

func TestCheckGate_HoldFlag_ClearHold_Transition(t *testing.T) {
	t.Parallel()
	mock := &mockPermissionService{grantResult: true}
	hold := &atomic.Bool{}
	hold.Store(true)

	// Hold active — gate fires.
	approved, err := checkGate(context.Background(), permission.CreatePermissionRequest{}, false, hold, mock)
	require.NoError(t, err)
	require.True(t, approved)
	require.Equal(t, 1, mock.callCount)

	// Simulate operator Ctrl+H toggle-off.
	hold.Store(false)

	// Hold cleared, not gated — no Request call.
	approved, err = checkGate(context.Background(), permission.CreatePermissionRequest{}, false, hold, mock)
	require.NoError(t, err)
	require.True(t, approved)
	require.Equal(t, 1, mock.callCount, "Request should not be called after hold cleared")
}

func TestCheckGate_HoldFlag_SetsForcePrompt(t *testing.T) {
	t.Parallel()
	mock := &mockPermissionService{grantResult: true}
	hold := &atomic.Bool{}
	hold.Store(true)

	// Hold active — ForcePrompt must be set on the request passed to permission service.
	_, _ = checkGate(context.Background(), permission.CreatePermissionRequest{
		ToolName: "gate:build",
	}, false, hold, mock)

	require.True(t, mock.lastRequest.ForcePrompt, "hold should set ForcePrompt on the permission request")
}

func TestCheckGate_NoHold_NoForcePrompt(t *testing.T) {
	t.Parallel()
	mock := &mockPermissionService{grantResult: true}

	// Gated but no hold — ForcePrompt should be false.
	_, _ = checkGate(context.Background(), permission.CreatePermissionRequest{
		ToolName: "gate:build",
	}, true, nil, mock)

	require.False(t, mock.lastRequest.ForcePrompt, "gated without hold should not set ForcePrompt")
}

func TestCheckGate_RequestFields(t *testing.T) {
	t.Parallel()
	mock := &mockPermissionService{grantResult: true}

	req := permission.CreatePermissionRequest{
		SessionID:   "sess-123",
		ToolCallID:  "call-456",
		ToolName:    "gate:review",
		Description: `Station "review" requests approval to execute`,
		Action:      "execute",
		Params:      map[string]any{"task": "review the code"},
		Path:        "/tmp/project",
	}

	approved, err := checkGate(context.Background(), req, true, nil, mock)

	require.NoError(t, err)
	require.True(t, approved)
	require.Equal(t, "gate:review", mock.lastRequest.ToolName)
	require.Equal(t, "execute", mock.lastRequest.Action)
	require.Equal(t, "sess-123", mock.lastRequest.SessionID)
	require.Equal(t, "/tmp/project", mock.lastRequest.Path)
}

func TestBuildGateParams_TaskOnly(t *testing.T) {
	t.Parallel()
	got := buildGateParams(stationInput{Task: "review"})
	require.Equal(t, "review", got["task"])
	require.Equal(t, "", got["task_description"])
}

func TestBuildGateParams_EmptySlices(t *testing.T) {
	t.Parallel()
	got := buildGateParams(stationInput{
		Task:         "review",
		ContextHints: []string{},
	})
	require.Equal(t, "review", got["task"],
		"task should always be present")
	require.Nil(t, got["context_hints"],
		"empty slices should not appear in gate params")
}

func TestBuildGateParams_AllFields(t *testing.T) {
	t.Parallel()
	input := stationInput{
		Task:            "build auth",
		TaskDescription: "Add JWT auth to the API",
		ContextHints:    []string{"see plans/auth.md"},
		Constraints:     []string{"no new deps"},
		SuccessCriteria: []string{"all tests pass"},
	}
	got := buildGateParams(input)

	require.Equal(t, "build auth", got["task"])
	require.Equal(t, "Add JWT auth to the API", got["task_description"])
	require.Equal(t, []string{"see plans/auth.md"}, got["context_hints"])
	require.Equal(t, []string{"no new deps"}, got["constraints"])
	require.Equal(t, []string{"all tests pass"}, got["success_criteria"])
}
