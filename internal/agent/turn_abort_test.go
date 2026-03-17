package agent

import (
	"sync/atomic"
	"testing"

	adkmodel "google.golang.org/adk/model"
	adksession "google.golang.org/adk/session"
	"google.golang.org/genai"

	"github.com/dmora/crucible/internal/askuser"
	"github.com/dmora/crucible/internal/csync"
	"github.com/dmora/crucible/internal/message"
	"github.com/dmora/crucible/internal/pubsub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- handleFunctionResponse abort tests ---

func TestHandleFunctionResponse_ReturnsTrue_WhenAbortSet(t *testing.T) {
	broker := pubsub.NewBroker[message.Message]()
	metrics := csync.NewMap[string, *TurnMetrics]()
	var result AgentResult
	abort := new(atomic.Bool)

	msg := newInMemoryMessage("sess-1", message.Assistant, nil, "", "")
	msg.AddToolCall(message.ToolCall{ID: "fc-1", Name: "build", State: message.ToolStatePending})
	ep := newEventProcessor(broker, metrics, &msg, &result, nil, nil, abort)

	// Simulate: tool sets abort flag before ADK yields the FunctionResponse.
	abort.Store(true)

	frEvent := &adksession.Event{
		LLMResponse: adkmodel.LLMResponse{
			Content: &genai.Content{Parts: []*genai.Part{
				{FunctionResponse: &genai.FunctionResponse{
					ID:       "fc-1",
					Name:     "build",
					Response: map[string]any{"result": "DENIED: ..."},
				}},
			}},
		},
	}
	stopped := ep.process(frEvent)
	assert.True(t, stopped, "process() should return true when turnAbort is set")
}

func TestHandleFunctionResponse_ReturnsFalse_WhenAbortNotSet(t *testing.T) {
	broker := pubsub.NewBroker[message.Message]()
	metrics := csync.NewMap[string, *TurnMetrics]()
	var result AgentResult
	abort := new(atomic.Bool)

	msg := newInMemoryMessage("sess-1", message.Assistant, nil, "", "")
	msg.AddToolCall(message.ToolCall{ID: "fc-1", Name: "build", State: message.ToolStatePending})
	ep := newEventProcessor(broker, metrics, &msg, &result, nil, nil, abort)

	frEvent := &adksession.Event{
		LLMResponse: adkmodel.LLMResponse{
			Content: &genai.Content{Parts: []*genai.Part{
				{FunctionResponse: &genai.FunctionResponse{
					ID:       "fc-1",
					Name:     "build",
					Response: map[string]any{"result": "success"},
				}},
			}},
		},
	}
	stopped := ep.process(frEvent)
	assert.False(t, stopped, "process() should return false when turnAbort is not set")
}

func TestHandleFunctionResponse_ReturnsFalse_WhenAbortNil(t *testing.T) {
	broker := pubsub.NewBroker[message.Message]()
	metrics := csync.NewMap[string, *TurnMetrics]()
	var result AgentResult

	msg := newInMemoryMessage("sess-1", message.Assistant, nil, "", "")
	msg.AddToolCall(message.ToolCall{ID: "fc-1", Name: "build", State: message.ToolStatePending})
	ep := newEventProcessor(broker, metrics, &msg, &result, nil, nil, nil)

	frEvent := &adksession.Event{
		LLMResponse: adkmodel.LLMResponse{
			Content: &genai.Content{Parts: []*genai.Part{
				{FunctionResponse: &genai.FunctionResponse{
					ID:       "fc-1",
					Name:     "build",
					Response: map[string]any{"result": "ok"},
				}},
			}},
		},
	}
	stopped := ep.process(frEvent)
	assert.False(t, stopped, "process() should return false when turnAbort is nil")
}

// --- isAbortResponse tests ---

func TestIsAbortResponse_AbortTrue(t *testing.T) {
	resp := &genai.FunctionResponse{
		Response: map[string]any{
			"result": "DENIED: ...",
			"_abort": true,
		},
	}
	assert.True(t, isAbortResponse(resp))
}

func TestIsAbortResponse_ErrCanceledMessage(t *testing.T) {
	resp := &genai.FunctionResponse{
		Response: map[string]any{
			"error": askuser.ErrCanceledMessage,
		},
	}
	assert.True(t, isAbortResponse(resp))
}

func TestIsAbortResponse_NormalResult(t *testing.T) {
	resp := &genai.FunctionResponse{
		Response: map[string]any{
			"result": "spec complete",
		},
	}
	assert.False(t, isAbortResponse(resp))
}

func TestIsAbortResponse_NilResponse(t *testing.T) {
	assert.False(t, isAbortResponse(nil))
	assert.False(t, isAbortResponse(&genai.FunctionResponse{}))
}

func TestIsAbortResponse_AbortFalse(t *testing.T) {
	resp := &genai.FunctionResponse{
		Response: map[string]any{
			"_abort": false,
			"result": "ok",
		},
	}
	assert.False(t, isAbortResponse(resp))
}

// --- eventsToMessages abort replay tests ---

func TestEventsToMessages_AbortSetsFinishReasonCanceled(t *testing.T) {
	events := testEvents{
		// Agent makes a function call.
		newEvent("e1", "agent", []*genai.Part{
			{FunctionCall: &genai.FunctionCall{ID: "fc-1", Name: "build"}},
		}),
		// Function response with _abort marker.
		newEvent("e2", "agent", []*genai.Part{
			{FunctionResponse: &genai.FunctionResponse{
				ID:       "fc-1",
				Name:     "build",
				Response: map[string]any{"result": "DENIED: ...", "_abort": true},
			}},
		}),
	}
	msgs := eventsToMessages(events, "sess-1")

	// Expected: assistant (with tool call + FinishReasonCanceled), tool result.
	require.Len(t, msgs, 2)

	// First: assistant with tool call and canceled finish.
	assert.Equal(t, message.Assistant, msgs[0].Role)
	assert.Equal(t, message.FinishReasonCanceled, msgs[0].FinishReason())

	// Second: tool result.
	assert.Equal(t, message.Tool, msgs[1].Role)
}

func TestEventsToMessages_NormalResult_HasFinishReasonEndTurn(t *testing.T) {
	events := testEvents{
		// Agent makes a function call.
		newEvent("e1", "agent", []*genai.Part{
			{FunctionCall: &genai.FunctionCall{ID: "fc-1", Name: "build"}},
		}),
		// Normal function response (no abort).
		newEvent("e2", "agent", []*genai.Part{
			{FunctionResponse: &genai.FunctionResponse{
				ID:       "fc-1",
				Name:     "build",
				Response: map[string]any{"result": "spec complete"},
			}},
		}),
		// Agent produces final text.
		newEventWithFinish("e3", "agent", []*genai.Part{{Text: "Done"}}, "STOP"),
	}
	msgs := eventsToMessages(events, "sess-1")

	require.Len(t, msgs, 3)
	// Final assistant message has STOP finish reason.
	assert.Equal(t, message.FinishReasonEndTurn, msgs[2].FinishReason())
}

func TestEventsToMessages_AbortTruncatesTrailingText(t *testing.T) {
	// Regression: if an ADK event contains [FunctionResponse(abort), Text("trailing")],
	// the Text part must NOT produce an assistant message with ghost text.
	events := testEvents{
		// Agent calls tool.
		newEvent("e1", "agent", []*genai.Part{
			{FunctionCall: &genai.FunctionCall{ID: "fc-1", Name: "build"}},
		}),
		// Mixed event: FunctionResponse with abort + trailing Text.
		newEvent("e2", "agent", []*genai.Part{
			{FunctionResponse: &genai.FunctionResponse{
				ID:       "fc-1",
				Name:     "build",
				Response: map[string]any{"result": "DENIED", "_abort": true},
			}},
			{Text: "Let me try a different approach..."},
		}),
	}
	msgs := eventsToMessages(events, "sess-1")

	// Should have: assistant (tool call, canceled), tool result.
	// Should NOT have an assistant message with "Let me try a different approach..."
	require.Len(t, msgs, 2)
	assert.Equal(t, message.Assistant, msgs[0].Role)
	assert.Equal(t, message.FinishReasonCanceled, msgs[0].FinishReason())
	assert.Equal(t, message.Tool, msgs[1].Role)

	// Verify no message contains the trailing text.
	for _, m := range msgs {
		assert.NotContains(t, m.Content().Text, "Let me try a different approach")
	}
}

func TestEventsToMessages_AbortStopsSubsequentEvents(t *testing.T) {
	// After an abort-marked FunctionResponse, subsequent events should NOT
	// be processed — replay stops at the same point the live path did.
	events := testEvents{
		// Agent calls tool.
		newEvent("e1", "agent", []*genai.Part{
			{FunctionCall: &genai.FunctionCall{ID: "fc-1", Name: "build"}},
		}),
		// FunctionResponse with abort.
		newEvent("e2", "agent", []*genai.Part{
			{FunctionResponse: &genai.FunctionResponse{
				ID:       "fc-1",
				Name:     "build",
				Response: map[string]any{"result": "DENIED", "_abort": true},
			}},
		}),
		// This event should NOT be processed.
		newEventWithFinish("e3", "agent", []*genai.Part{
			{Text: "Ghost text from post-abort LLM call"},
		}, "STOP"),
	}
	msgs := eventsToMessages(events, "sess-1")

	// Only 2 messages: assistant (canceled) + tool result.
	require.Len(t, msgs, 2)
	for _, m := range msgs {
		assert.NotContains(t, m.Content().Text, "Ghost text")
	}
}

func TestEventsToMessages_AskUserAbortDetected(t *testing.T) {
	// ask_user cancel: the FunctionResponse has the deterministic error string.
	events := testEvents{
		newEvent("e1", "agent", []*genai.Part{
			{FunctionCall: &genai.FunctionCall{ID: "fc-1", Name: "ask_user"}},
		}),
		newEvent("e2", "agent", []*genai.Part{
			{FunctionResponse: &genai.FunctionResponse{
				ID:       "fc-1",
				Name:     "ask_user",
				Response: map[string]any{"error": askuser.ErrCanceledMessage},
			}},
		}),
	}
	msgs := eventsToMessages(events, "sess-1")

	require.Len(t, msgs, 2)
	assert.Equal(t, message.FinishReasonCanceled, msgs[0].FinishReason())
}

// --- cancelQueuedMessages tests ---

func TestCancelQueuedMessages_PublishesCanceledAssistant(t *testing.T) {
	env := testEnv(t)
	agent := testSessionAgent(env, "test prompt").(*sessionAgent)

	sessionID := "sess-cancel-q"
	ctx := t.Context()

	// Subscribe to broker events to capture published messages.
	sub := env.messageBroker.Subscribe(ctx)

	// Queue two calls with published user messages, one without.
	csync.AppendSlice(agent.messageQueue, sessionID, SessionAgentCall{
		SessionID:      sessionID,
		Prompt:         "first",
		PublishedMsgID: "msg-published-1",
	})
	csync.AppendSlice(agent.messageQueue, sessionID, SessionAgentCall{
		SessionID:      sessionID,
		Prompt:         "second",
		PublishedMsgID: "msg-published-2",
	})
	csync.AppendSlice(agent.messageQueue, sessionID, SessionAgentCall{
		SessionID: sessionID,
		Prompt:    "third",
		// No PublishedMsgID — user message was never published.
	})

	agent.cancelQueuedMessages(sessionID)

	// Drain the 2 expected published messages from the subscription channel.
	var published []message.Message
	for range 2 {
		ev := <-sub
		published = append(published, ev.Payload)
	}

	// Should have published 2 canceled assistant messages (not 3 — the one
	// without PublishedMsgID should be skipped).
	require.Len(t, published, 2)
	for _, msg := range published {
		assert.Equal(t, message.Assistant, msg.Role)
		assert.Equal(t, message.FinishReasonCanceled, msg.FinishReason())
		assert.Equal(t, sessionID, msg.SessionID)
	}

	// Queue should be empty.
	assert.Equal(t, 0, agent.QueuedPrompts(sessionID))
}

func TestCancelQueuedMessages_EmptyQueue(t *testing.T) {
	env := testEnv(t)
	agent := testSessionAgent(env, "test prompt").(*sessionAgent)

	// Should not panic on empty queue.
	agent.cancelQueuedMessages("sess-no-queue")
	assert.Equal(t, 0, agent.QueuedPrompts("sess-no-queue"))
}
