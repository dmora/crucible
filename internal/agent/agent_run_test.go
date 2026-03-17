package agent

import (
	"testing"

	adkmodel "google.golang.org/adk/model"
	adksession "google.golang.org/adk/session"
	"google.golang.org/genai"

	"github.com/dmora/crucible/internal/csync"
	"github.com/dmora/crucible/internal/message"
	"github.com/dmora/crucible/internal/pubsub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEventProcessor_FunctionResponseTriggersNewMessage verifies that after a
// FunctionResponse event, eventHasFunctionResponse returns true, allowing Run()
// to create a new assistant message so post-tool text renders separately.
func TestEventProcessor_FunctionResponseTriggersNewMessage(t *testing.T) {
	broker := pubsub.NewBroker[message.Message]()
	metrics := csync.NewMap[string, *TurnMetrics]()
	var result AgentResult

	assistantMsg := newInMemoryMessage("sess-1", message.Assistant, []message.ContentPart{}, "model", "provider")
	ep := newEventProcessor(broker, metrics, &assistantMsg, &result, nil, nil, nil)

	// 1. Feed a FunctionCall event — should add a pending tool call.
	fcEvent := &adksession.Event{
		LLMResponse: adkmodel.LLMResponse{
			Partial: true,
			Content: &genai.Content{Parts: []*genai.Part{
				{FunctionCall: &genai.FunctionCall{ID: "fc-1", Name: "draft"}},
			}},
		},
	}
	stopped := ep.process(fcEvent)
	assert.False(t, stopped)

	toolCalls := assistantMsg.ToolCalls()
	require.Len(t, toolCalls, 1)
	assert.Equal(t, "fc-1", toolCalls[0].ID)
	assert.Equal(t, message.ToolStatePending, toolCalls[0].State)

	// 2. Feed a FunctionResponse event — should mark tool done and publish tool result.
	frEvent := &adksession.Event{
		LLMResponse: adkmodel.LLMResponse{
			Content: &genai.Content{Parts: []*genai.Part{
				{FunctionResponse: &genai.FunctionResponse{
					ID:       "fc-1",
					Name:     "draft",
					Response: map[string]any{"result": "spec complete"},
				}},
			}},
		},
	}
	stopped = ep.process(frEvent)
	assert.False(t, stopped)

	// Tool call should now be done.
	toolCalls = assistantMsg.ToolCalls()
	require.Len(t, toolCalls, 1)
	assert.Equal(t, message.ToolStateDone, toolCalls[0].State)

	// 3. Verify eventHasFunctionResponse returns true for the response event.
	assert.True(t, eventHasFunctionResponse(frEvent))

	// 4. Simulate Run()'s split: create a new assistant message after FunctionResponse.
	oldMsg := assistantMsg
	assistantMsg = newInMemoryMessage("sess-1", message.Assistant, []message.ContentPart{}, "model", "provider")
	ep.msg = &assistantMsg

	// 5. Feed a text event — should go to the NEW message, not the old one.
	textEvent := &adksession.Event{
		LLMResponse: adkmodel.LLMResponse{
			Partial: true,
			Content: &genai.Content{Parts: []*genai.Part{
				{Text: "Spec is ready for review."},
			}},
		},
	}
	stopped = ep.process(textEvent)
	assert.False(t, stopped)

	// New message has text, old message does not.
	assert.Equal(t, "Spec is ready for review.", assistantMsg.Content().Text)
	assert.Equal(t, "", oldMsg.Content().Text)
	// Old message still has the tool call.
	assert.Len(t, oldMsg.ToolCalls(), 1)
}

// TestEventHasFunctionResponse tests the eventHasFunctionResponse helper.
func TestEventHasFunctionResponse(t *testing.T) {
	tests := []struct {
		name   string
		event  *adksession.Event
		expect bool
	}{
		{"nil event", nil, false},
		{"nil content", &adksession.Event{}, false},
		{"text only", &adksession.Event{
			LLMResponse: adkmodel.LLMResponse{
				Content: &genai.Content{Parts: []*genai.Part{{Text: "hello"}}},
			},
		}, false},
		{"function call only", &adksession.Event{
			LLMResponse: adkmodel.LLMResponse{
				Content: &genai.Content{Parts: []*genai.Part{
					{FunctionCall: &genai.FunctionCall{ID: "fc-1", Name: "tool"}},
				}},
			},
		}, false},
		{"function response", &adksession.Event{
			LLMResponse: adkmodel.LLMResponse{
				Content: &genai.Content{Parts: []*genai.Part{
					{FunctionResponse: &genai.FunctionResponse{ID: "fc-1", Name: "tool"}},
				}},
			},
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expect, eventHasFunctionResponse(tt.event))
		})
	}
}

// TestRun_QueueBehavior verifies that when a session is busy, messages get
// queued and the queue can be inspected and cleared.
func TestRun_QueueBehavior(t *testing.T) {
	env := testEnv(t)
	agent := testSessionAgent(env, "test prompt").(*sessionAgent)

	sessionID := "sess-queue"

	// Simulate a busy session by claiming ownership.
	agent.activeRequests.SetNX(sessionID, &TaskHandle{ID: "test", Cancel: func() {}})
	assert.True(t, agent.IsSessionBusy(sessionID))

	// Queue two calls via the message queue (simulating what Run would do).
	call1 := SessionAgentCall{SessionID: sessionID, Prompt: "first"}
	call2 := SessionAgentCall{SessionID: sessionID, Prompt: "second"}
	csync.AppendSlice(agent.messageQueue, sessionID, call1)
	csync.AppendSlice(agent.messageQueue, sessionID, call2)

	// Verify queue state.
	assert.Equal(t, 2, agent.QueuedPrompts(sessionID))
	prompts := agent.QueuedPromptsList(sessionID)
	require.Len(t, prompts, 2)
	assert.Equal(t, "first", prompts[0])
	assert.Equal(t, "second", prompts[1])

	// Clear queue.
	agent.ClearQueue(sessionID)
	assert.Equal(t, 0, agent.QueuedPrompts(sessionID))

	// Cleanup.
	agent.activeRequests.Del(sessionID)
	assert.False(t, agent.IsSessionBusy(sessionID))
}

// TestRun_ValidationErrors tests that Run returns errors for invalid inputs.
func TestRun_ValidationErrors(t *testing.T) {
	env := testEnv(t)
	agent := testSessionAgent(env, "test prompt")

	t.Run("empty prompt", func(t *testing.T) {
		_, err := agent.Run(t.Context(), SessionAgentCall{SessionID: "sess-1", Prompt: ""})
		assert.ErrorIs(t, err, ErrEmptyPrompt)
	})

	t.Run("missing session ID", func(t *testing.T) {
		_, err := agent.Run(t.Context(), SessionAgentCall{SessionID: "", Prompt: "hello"})
		assert.ErrorIs(t, err, ErrSessionMissing)
	})
}
