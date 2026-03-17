package agent

import (
	"encoding/json"
	"testing"
	"time"

	adkmodel "google.golang.org/adk/model"
	"google.golang.org/genai"

	"github.com/dmora/crucible/internal/agent/tools"
	"github.com/dmora/crucible/internal/csync"
	"github.com/dmora/crucible/internal/message"
	"github.com/dmora/crucible/internal/pubsub"
	"github.com/dmora/crucible/internal/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Diff Logic Tests ---

func TestComputeTodoDiff_FirstCreate(t *testing.T) {
	newTodos := []session.Todo{
		{Content: "Draft API spec", Status: session.TodoStatusPending},
		{Content: "Build endpoint", Status: session.TodoStatusPending},
	}
	meta := computeTodoDiff(nil, newTodos)
	assert.True(t, meta.IsNew)
	assert.Equal(t, 0, meta.Completed)
	assert.Equal(t, 2, meta.Total)
	assert.Empty(t, meta.JustCompleted)
	assert.Empty(t, meta.JustStarted)
	assert.Len(t, meta.Todos, 2)
}

func TestComputeTodoDiff_MarkCompleted(t *testing.T) {
	old := []session.Todo{
		{Content: "Draft API spec", Status: session.TodoStatusPending},
		{Content: "Build endpoint", Status: session.TodoStatusPending},
	}
	new := []session.Todo{
		{Content: "Draft API spec", Status: session.TodoStatusCompleted},
		{Content: "Build endpoint", Status: session.TodoStatusPending},
	}
	meta := computeTodoDiff(old, new)
	assert.False(t, meta.IsNew)
	assert.Equal(t, 1, meta.Completed)
	assert.Equal(t, 2, meta.Total)
	assert.Equal(t, []string{"Draft API spec"}, meta.JustCompleted)
	assert.Empty(t, meta.JustStarted)
}

func TestComputeTodoDiff_StartTask(t *testing.T) {
	old := []session.Todo{
		{Content: "Draft API spec", Status: session.TodoStatusPending},
		{Content: "Build endpoint", Status: session.TodoStatusPending},
	}
	new := []session.Todo{
		{Content: "Draft API spec", Status: session.TodoStatusInProgress},
		{Content: "Build endpoint", Status: session.TodoStatusPending},
	}
	meta := computeTodoDiff(old, new)
	assert.Equal(t, "Draft API spec", meta.JustStarted)
	assert.Empty(t, meta.JustCompleted)
}

func TestComputeTodoDiff_CompleteAndStart(t *testing.T) {
	old := []session.Todo{
		{Content: "Draft API spec", Status: session.TodoStatusInProgress},
		{Content: "Build endpoint", Status: session.TodoStatusPending},
	}
	new := []session.Todo{
		{Content: "Draft API spec", Status: session.TodoStatusCompleted},
		{Content: "Build endpoint", Status: session.TodoStatusInProgress},
	}
	meta := computeTodoDiff(old, new)
	assert.Equal(t, []string{"Draft API spec"}, meta.JustCompleted)
	assert.Equal(t, "Build endpoint", meta.JustStarted)
	assert.Equal(t, 1, meta.Completed)
}

func TestComputeTodoDiff_AllCompleted(t *testing.T) {
	old := []session.Todo{
		{Content: "Draft", Status: session.TodoStatusInProgress},
		{Content: "Build", Status: session.TodoStatusPending},
	}
	new := []session.Todo{
		{Content: "Draft", Status: session.TodoStatusCompleted},
		{Content: "Build", Status: session.TodoStatusCompleted},
	}
	meta := computeTodoDiff(old, new)
	assert.Equal(t, 2, meta.Completed)
	assert.Equal(t, 2, meta.Total)
}

func TestComputeTodoDiff_NoChange(t *testing.T) {
	todos := []session.Todo{
		{Content: "Draft", Status: session.TodoStatusPending},
		{Content: "Build", Status: session.TodoStatusInProgress},
	}
	meta := computeTodoDiff(todos, todos)
	assert.Empty(t, meta.JustCompleted)
	assert.Empty(t, meta.JustStarted)
}

// --- Validation Tests ---

func TestValidateTodosInput_Valid(t *testing.T) {
	items := []todosInputItem{
		{Content: "Draft spec", Status: "pending"},
		{Content: "Build it", Status: "in_progress", ActiveForm: "Building it"},
	}
	todos, err := validateTodosInput(items)
	require.NoError(t, err)
	assert.Len(t, todos, 2)
	assert.Equal(t, session.TodoStatusPending, todos[0].Status)
	assert.Equal(t, session.TodoStatusInProgress, todos[1].Status)
	assert.Equal(t, "Building it", todos[1].ActiveForm)
}

func TestValidateTodosInput_InvalidStatus(t *testing.T) {
	items := []todosInputItem{
		{Content: "Draft spec", Status: "done"},
	}
	_, err := validateTodosInput(items)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid status")
}

func TestValidateTodosInput_EmptyContent(t *testing.T) {
	items := []todosInputItem{
		{Content: "  ", Status: "pending"},
	}
	_, err := validateTodosInput(items)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "content cannot be empty")
}

func TestValidateTodosInput_EmptyList(t *testing.T) {
	_, err := validateTodosInput(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be empty")
}

// --- Tool Handler Tests ---

func TestNewTodosTool_Creates(t *testing.T) {
	env := testEnv(t)
	tt, err := newTodosTool(env.sessions, "sess-1")
	require.NoError(t, err)
	assert.NotNil(t, tt)
}

func TestValidateTodosInput_MultipleInProgress(t *testing.T) {
	items := []todosInputItem{
		{Content: "Task A", Status: "in_progress", ActiveForm: "Doing A"},
		{Content: "Task B", Status: "in_progress", ActiveForm: "Doing B"},
	}
	_, err := validateTodosInput(items)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "at most one item can be in_progress")
}

func TestValidateTodosInput_InProgressMissingActiveFormFallsBackToContent(t *testing.T) {
	items := []todosInputItem{
		{Content: "Task A", Status: "in_progress"},
	}
	todos, err := validateTodosInput(items)
	assert.NoError(t, err)
	assert.Equal(t, "Task A", todos[0].ActiveForm)
}

func TestValidateTodosInput_PendingWithoutActiveFormOK(t *testing.T) {
	items := []todosInputItem{
		{Content: "Task A", Status: "pending"},
	}
	todos, err := validateTodosInput(items)
	require.NoError(t, err)
	assert.Len(t, todos, 1)
}

// --- Bridge Tests ---

func TestBridgeTodos_Persistence(t *testing.T) {
	env := testEnv(t)
	sess, err := env.sessions.Create(t.Context(), "test")
	require.NoError(t, err)

	meta := tools.TodosResponseMetadata{
		Todos: []session.Todo{
			{Content: "Draft spec", Status: session.TodoStatusCompleted},
			{Content: "Build it", Status: session.TodoStatusPending},
		},
		Completed: 1,
		Total:     2,
	}
	metaJSON, err := json.Marshal(meta)
	require.NoError(t, err)

	broker := pubsub.NewBroker[message.Message]()
	metrics := csync.NewMap[string, *TurnMetrics]()
	lastTodoUpdate := csync.NewMap[string, time.Time]()
	msg := newInMemoryMessage(sess.ID, message.Assistant, nil, "", "")
	var result AgentResult
	ep := newEventProcessor(broker, metrics, &msg, &result, env.sessions, lastTodoUpdate, nil)

	ep.bridgeTodos(string(metaJSON))

	// Verify session was updated.
	updated, err := env.sessions.Get(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Len(t, updated.Todos, 2)
	assert.Equal(t, session.TodoStatusCompleted, updated.Todos[0].Status)

	// Verify staleness map was updated.
	ts, ok := lastTodoUpdate.Get(sess.ID)
	assert.True(t, ok)
	assert.WithinDuration(t, time.Now(), ts, 2*time.Second)
}

func TestBridgeTodos_EmptyMetadata(t *testing.T) {
	broker := pubsub.NewBroker[message.Message]()
	metrics := csync.NewMap[string, *TurnMetrics]()
	msg := newInMemoryMessage("sess-1", message.Assistant, nil, "", "")
	var result AgentResult
	ep := newEventProcessor(broker, metrics, &msg, &result, nil, nil, nil)

	// Should not panic.
	ep.bridgeTodos("")
}

// --- Staleness Tests ---

func TestStaleTodosReminder_NoTodos(t *testing.T) {
	env := testEnv(t)
	agent := testSessionAgent(env, "prompt").(*sessionAgent)
	sess := session.Session{ID: "sess-1", Todos: nil}
	assert.Empty(t, agent.staleTodosReminder(sess))
}

func TestStaleTodosReminder_FreshTodos(t *testing.T) {
	env := testEnv(t)
	agent := testSessionAgent(env, "prompt").(*sessionAgent)
	agent.lastTodoUpdate.Set("sess-1", time.Now())
	sess := session.Session{
		ID:    "sess-1",
		Todos: []session.Todo{{Content: "task", Status: session.TodoStatusPending}},
	}
	assert.Empty(t, agent.staleTodosReminder(sess))
}

func TestStaleTodosReminder_StaleTodos(t *testing.T) {
	env := testEnv(t)
	agent := testSessionAgent(env, "prompt").(*sessionAgent)
	agent.lastTodoUpdate.Set("sess-1", time.Now().Add(-10*time.Minute))
	sess := session.Session{
		ID:    "sess-1",
		Todos: []session.Todo{{Content: "task", Status: session.TodoStatusPending}},
	}
	reminder := agent.staleTodosReminder(sess)
	assert.Contains(t, reminder, "<steering_reminder>")
	assert.Contains(t, reminder, "stale")
}

func TestStaleTodosReminder_FirstCheckSeedsTimer(t *testing.T) {
	env := testEnv(t)
	agent := testSessionAgent(env, "prompt").(*sessionAgent)
	sess := session.Session{
		ID:    "sess-1",
		Todos: []session.Todo{{Content: "task", Status: session.TodoStatusPending}},
	}
	// First call seeds the timer and returns empty.
	assert.Empty(t, agent.staleTodosReminder(sess))
	// Timer was seeded.
	_, ok := agent.lastTodoUpdate.Get("sess-1")
	assert.True(t, ok)
}

func TestStaleTodosReminder_ResetOnUpdate(t *testing.T) {
	env := testEnv(t)
	agent := testSessionAgent(env, "prompt").(*sessionAgent)
	// Stale initially.
	agent.lastTodoUpdate.Set("sess-1", time.Now().Add(-10*time.Minute))
	sess := session.Session{
		ID:    "sess-1",
		Todos: []session.Todo{{Content: "task", Status: session.TodoStatusPending}},
	}
	assert.NotEmpty(t, agent.staleTodosReminder(sess))

	// Bridge updates the map (simulating what bridgeTodos does).
	agent.lastTodoUpdate.Set("sess-1", time.Now())
	assert.Empty(t, agent.staleTodosReminder(sess))
}

func TestStaleTodosReminder_CrossSessionIsolation(t *testing.T) {
	env := testEnv(t)
	agent := testSessionAgent(env, "prompt").(*sessionAgent)

	// Session A: stale todos.
	agent.lastTodoUpdate.Set("sess-A", time.Now().Add(-10*time.Minute))
	sessA := session.Session{
		ID:    "sess-A",
		Todos: []session.Todo{{Content: "task A", Status: session.TodoStatusPending}},
	}

	// Session B: fresh todos.
	agent.lastTodoUpdate.Set("sess-B", time.Now())
	sessB := session.Session{
		ID:    "sess-B",
		Todos: []session.Todo{{Content: "task B", Status: session.TodoStatusInProgress}},
	}

	// Only A should get the reminder.
	assert.NotEmpty(t, agent.staleTodosReminder(sessA))
	assert.Empty(t, agent.staleTodosReminder(sessB))
}

func TestStaleTodosReminder_AllCompleted(t *testing.T) {
	env := testEnv(t)
	agent := testSessionAgent(env, "prompt").(*sessionAgent)
	agent.lastTodoUpdate.Set("sess-1", time.Now().Add(-10*time.Minute))
	sess := session.Session{
		ID:    "sess-1",
		Todos: []session.Todo{{Content: "task", Status: session.TodoStatusCompleted}},
	}
	// No reminder when all todos are completed, even if stale.
	assert.Empty(t, agent.staleTodosReminder(sess))
}

// --- Replay Test ---

func TestEventsToMessages_MetadataPreserved(t *testing.T) {
	meta := tools.TodosResponseMetadata{
		IsNew:     true,
		Todos:     []session.Todo{{Content: "task", Status: session.TodoStatusPending}},
		Completed: 0,
		Total:     1,
	}
	metaJSON, err := json.Marshal(meta)
	require.NoError(t, err)

	events := testEvents{
		{
			ID:     "evt-1",
			Author: "agent",
			LLMResponse: adkmodel.LLMResponse{
				Content: &genai.Content{Parts: []*genai.Part{
					{FunctionCall: &genai.FunctionCall{ID: "fc-1", Name: tools.TodosToolName}},
				}},
			},
		},
		{
			ID:     "evt-2",
			Author: "agent",
			LLMResponse: adkmodel.LLMResponse{
				Content: &genai.Content{Parts: []*genai.Part{
					{FunctionResponse: &genai.FunctionResponse{
						ID:       "fc-1",
						Name:     tools.TodosToolName,
						Response: map[string]any{"result": "Updated todos: 0/1 completed", "metadata": string(metaJSON)},
					}},
				}},
			},
		},
	}

	msgs := eventsToMessages(events, "sess-1")
	// Find the tool result message.
	var toolResult *message.ToolResult
	for _, m := range msgs {
		for _, p := range m.Parts {
			if tr, ok := p.(message.ToolResult); ok && tr.Name == tools.TodosToolName {
				toolResult = &tr
				break
			}
		}
	}
	require.NotNil(t, toolResult, "should find a todos tool result")
	assert.Equal(t, string(metaJSON), toolResult.Metadata)

	// Verify the metadata round-trips.
	var decoded tools.TodosResponseMetadata
	require.NoError(t, json.Unmarshal([]byte(toolResult.Metadata), &decoded))
	assert.True(t, decoded.IsNew)
	assert.Equal(t, 1, decoded.Total)
}

// --- extractFunctionResponseMetadata Tests ---

func TestExtractFunctionResponseMetadata_StringValue(t *testing.T) {
	resp := &genai.FunctionResponse{
		Response: map[string]any{"metadata": `{"is_new":true}`},
	}
	assert.Equal(t, `{"is_new":true}`, extractFunctionResponseMetadata(resp))
}

func TestExtractFunctionResponseMetadata_NonStringValue(t *testing.T) {
	resp := &genai.FunctionResponse{
		Response: map[string]any{"metadata": map[string]any{"is_new": true}},
	}
	result := extractFunctionResponseMetadata(resp)
	assert.Contains(t, result, "is_new")
}

func TestExtractFunctionResponseMetadata_NoMetadata(t *testing.T) {
	resp := &genai.FunctionResponse{
		Response: map[string]any{"result": "ok"},
	}
	assert.Empty(t, extractFunctionResponseMetadata(resp))
}

func TestExtractFunctionResponseMetadata_NilResponse(t *testing.T) {
	assert.Empty(t, extractFunctionResponseMetadata(nil))
	assert.Empty(t, extractFunctionResponseMetadata(&genai.FunctionResponse{}))
}

// --- Wiring Test: Run appends staleness reminder ---

func TestRun_AppendsStalenessReminder(t *testing.T) {
	env := testEnv(t)
	agent := testSessionAgent(env, "base prompt").(*sessionAgent)

	// Create a session with incomplete todos.
	sess, err := env.sessions.Create(t.Context(), "test-stale")
	require.NoError(t, err)
	sess.Todos = []session.Todo{{Content: "pending task", Status: session.TodoStatusPending}}
	_, err = env.sessions.Save(t.Context(), sess)
	require.NoError(t, err)

	// Seed a stale timestamp.
	agent.lastTodoUpdate.Set(sess.ID, time.Now().Add(-10*time.Minute))

	// We can't easily run the full agent loop without a real LLM, but we can
	// verify the staleness check works by calling staleTodosReminder directly
	// with the session we just created.
	reloaded, err := env.sessions.Get(t.Context(), sess.ID)
	require.NoError(t, err)
	reminder := agent.staleTodosReminder(reloaded)
	assert.Contains(t, reminder, "<steering_reminder>")
	assert.Contains(t, reminder, "stale")
}
