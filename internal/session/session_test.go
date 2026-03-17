package session_test

import (
	"sync"
	"testing"

	"github.com/dmora/crucible/internal/db"
	"github.com/dmora/crucible/internal/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestService(t *testing.T) session.Service {
	t.Helper()
	conn, err := db.Connect(t.Context(), t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	return session.NewService(db.New(conn), conn)
}

// TestUpdateTodosAndUsageConcurrent proves that UpdateTodos and UpdateUsage
// can run concurrently on the same session without data loss. This is the
// regression test for the race condition where saveSessionUsage's full-row
// Save() used to clobber todos written by bridgeTodos.
func TestUpdateTodosAndUsageConcurrent(t *testing.T) {
	svc := newTestService(t)
	sess, err := svc.Create(t.Context(), "concurrent-test")
	require.NoError(t, err)

	todos := []session.Todo{
		{Content: "Draft spec", Status: session.TodoStatusCompleted},
		{Content: "Build feature", Status: session.TodoStatusInProgress, ActiveForm: "Building feature"},
		{Content: "Write tests", Status: session.TodoStatusPending},
	}

	var wg sync.WaitGroup
	wg.Add(2)

	// Writer 1: update todos.
	go func() {
		defer wg.Done()
		err := svc.UpdateTodos(t.Context(), sess.ID, todos)
		assert.NoError(t, err)
	}()

	// Writer 2: update usage.
	go func() {
		defer wg.Done()
		err := svc.UpdateUsage(t.Context(), sess.ID, 5000, 1200, 6200, 0, 0.05)
		assert.NoError(t, err)
	}()

	wg.Wait()

	// Both writes should be visible — neither clobbered the other.
	final, err := svc.Get(t.Context(), sess.ID)
	require.NoError(t, err)

	assert.Len(t, final.Todos, 3, "todos should survive concurrent usage update")
	assert.Equal(t, session.TodoStatusInProgress, final.Todos[1].Status)
	assert.Equal(t, int64(5000), final.PromptTokens, "prompt tokens should survive concurrent todos update")
	assert.Equal(t, int64(1200), final.CompletionTokens)
	assert.InDelta(t, 0.05, final.Cost, 0.001)
}

// TestUpdateTodosIsolation verifies UpdateTodos only touches the todos column.
func TestUpdateTodosIsolation(t *testing.T) {
	svc := newTestService(t)
	sess, err := svc.Create(t.Context(), "isolation-test")
	require.NoError(t, err)

	// Set some usage first.
	require.NoError(t, svc.UpdateUsage(t.Context(), sess.ID, 1000, 500, 1500, 0, 0.10))

	// Now update todos.
	todos := []session.Todo{{Content: "task", Status: session.TodoStatusPending}}
	require.NoError(t, svc.UpdateTodos(t.Context(), sess.ID, todos))

	// Usage should be untouched.
	final, err := svc.Get(t.Context(), sess.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(1000), final.PromptTokens)
	assert.Equal(t, int64(500), final.CompletionTokens)
	assert.InDelta(t, 0.10, final.Cost, 0.001)
	assert.Len(t, final.Todos, 1)
}

// TestUpdateUsageIsolation verifies UpdateUsage only touches usage columns.
func TestUpdateUsageIsolation(t *testing.T) {
	svc := newTestService(t)
	sess, err := svc.Create(t.Context(), "isolation-test")
	require.NoError(t, err)

	// Set some todos first.
	todos := []session.Todo{
		{Content: "task A", Status: session.TodoStatusCompleted},
		{Content: "task B", Status: session.TodoStatusPending},
	}
	require.NoError(t, svc.UpdateTodos(t.Context(), sess.ID, todos))

	// Now update usage.
	require.NoError(t, svc.UpdateUsage(t.Context(), sess.ID, 2000, 800, 2800, 0, 0.03))

	// Todos should be untouched.
	final, err := svc.Get(t.Context(), sess.ID)
	require.NoError(t, err)
	assert.Len(t, final.Todos, 2)
	assert.Equal(t, session.TodoStatusCompleted, final.Todos[0].Status)
	assert.Equal(t, int64(2000), final.PromptTokens)
}

// TestUpdateUsageCostAccumulates verifies cost is additive across calls.
func TestUpdateUsageCostAccumulates(t *testing.T) {
	svc := newTestService(t)
	sess, err := svc.Create(t.Context(), "cost-test")
	require.NoError(t, err)

	require.NoError(t, svc.UpdateUsage(t.Context(), sess.ID, 100, 50, 150, 0, 0.01))
	require.NoError(t, svc.UpdateUsage(t.Context(), sess.ID, 200, 100, 300, 0, 0.02))

	final, err := svc.Get(t.Context(), sess.ID)
	require.NoError(t, err)
	// Tokens overwrite, cost and total_tokens/station_tokens accumulate.
	assert.Equal(t, int64(200), final.PromptTokens)
	assert.Equal(t, int64(100), final.CompletionTokens)
	assert.Equal(t, int64(450), final.TotalTokens) // 150 + 300
	assert.InDelta(t, 0.03, final.Cost, 0.001)
}
