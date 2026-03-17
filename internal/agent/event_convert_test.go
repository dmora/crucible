package agent

import (
	"iter"
	"testing"
	"time"

	adkmodel "google.golang.org/adk/model"
	adksession "google.golang.org/adk/session"
	"google.golang.org/genai"

	"github.com/dmora/crucible/internal/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testEvents wraps a slice of *adksession.Event to implement adksession.Events.
type testEvents []*adksession.Event

func (e testEvents) All() iter.Seq[*adksession.Event] {
	return func(yield func(*adksession.Event) bool) {
		for _, ev := range e {
			if !yield(ev) {
				return
			}
		}
	}
}

func (e testEvents) Len() int                   { return len(e) }
func (e testEvents) At(i int) *adksession.Event { return e[i] }

func newEvent(id, author string, parts []*genai.Part) *adksession.Event {
	return &adksession.Event{
		LLMResponse: adkmodel.LLMResponse{
			Content: &genai.Content{Parts: parts},
		},
		ID:        id,
		Timestamp: time.Unix(1000, 0),
		Author:    author,
	}
}

func newEventWithFinish(id, author string, parts []*genai.Part, finish genai.FinishReason) *adksession.Event {
	ev := newEvent(id, author, parts)
	ev.FinishReason = finish
	return ev
}

func TestEventsToMessages_Empty(t *testing.T) {
	msgs := eventsToMessages(testEvents{}, "sess-1")
	assert.Empty(t, msgs)
}

func TestEventsToMessages_UserMessage(t *testing.T) {
	events := testEvents{
		newEvent("e1", "user", []*genai.Part{{Text: "Hello"}}),
	}
	msgs := eventsToMessages(events, "sess-1")

	require.Len(t, msgs, 1)
	assert.Equal(t, message.User, msgs[0].Role)
	assert.Equal(t, "e1", msgs[0].ID)
	assert.Equal(t, "sess-1", msgs[0].SessionID)
	assert.Equal(t, "Hello", msgs[0].Content().Text)
	assert.True(t, msgs[0].IsFinished())
}

func TestEventsToMessages_AssistantText(t *testing.T) {
	events := testEvents{
		newEventWithFinish("e1", "agent", []*genai.Part{{Text: "world"}}, "STOP"),
	}
	msgs := eventsToMessages(events, "sess-1")

	require.Len(t, msgs, 1)
	assert.Equal(t, message.Assistant, msgs[0].Role)
	assert.Equal(t, "world", msgs[0].Content().Text)
	assert.True(t, msgs[0].IsFinished())
}

func TestEventsToMessages_ThinkingContent(t *testing.T) {
	events := testEvents{
		newEventWithFinish("e1", "agent", []*genai.Part{
			{Text: "reasoning here", Thought: true},
			{Text: "visible reply"},
		}, "STOP"),
	}
	msgs := eventsToMessages(events, "sess-1")

	require.Len(t, msgs, 1)
	msg := msgs[0]
	assert.Equal(t, message.Assistant, msg.Role)
	assert.Equal(t, "visible reply", msg.Content().Text)

	// Check reasoning content is present.
	var hasReasoning bool
	for _, p := range msg.Parts {
		if rc, ok := p.(message.ReasoningContent); ok {
			hasReasoning = true
			assert.Equal(t, "reasoning here", rc.Thinking)
		}
	}
	assert.True(t, hasReasoning, "should contain reasoning content")
}

func TestEventsToMessages_FunctionCallAndResponse(t *testing.T) {
	events := testEvents{
		// Agent makes a function call.
		newEvent("e1", "agent", []*genai.Part{
			{FunctionCall: &genai.FunctionCall{ID: "fc-1", Name: "read_file"}},
		}),
		// Function response comes back.
		newEvent("e2", "agent", []*genai.Part{
			{FunctionResponse: &genai.FunctionResponse{
				ID:       "fc-1",
				Name:     "read_file",
				Response: map[string]any{"result": "file contents here"},
			}},
		}),
		// Agent produces final text.
		newEventWithFinish("e3", "agent", []*genai.Part{{Text: "Here are the contents"}}, "STOP"),
	}
	msgs := eventsToMessages(events, "sess-1")

	// Expected: assistant (with tool call), tool result, assistant (with text)
	require.Len(t, msgs, 3)

	// First: assistant with tool call.
	assert.Equal(t, message.Assistant, msgs[0].Role)
	var hasToolCall bool
	for _, p := range msgs[0].Parts {
		if tc, ok := p.(message.ToolCall); ok {
			hasToolCall = true
			assert.Equal(t, "read_file", tc.Name)
			assert.Equal(t, "fc-1", tc.ID)
		}
	}
	assert.True(t, hasToolCall, "first message should have tool call")

	// Second: tool result.
	assert.Equal(t, message.Tool, msgs[1].Role)
	assert.Equal(t, "e2-tool", msgs[1].ID)
	require.Len(t, msgs[1].Parts, 2) // ToolResult + FinishPart
	tr, ok := msgs[1].Parts[0].(message.ToolResult)
	require.True(t, ok)
	assert.Equal(t, "fc-1", tr.ToolCallID)
	assert.Equal(t, "file contents here", tr.Content)

	// Third: assistant with text.
	assert.Equal(t, message.Assistant, msgs[2].Role)
	assert.Equal(t, "Here are the contents", msgs[2].Content().Text)
}

func TestEventsToMessages_MultiTurnConversation(t *testing.T) {
	events := testEvents{
		newEvent("e1", "user", []*genai.Part{{Text: "question 1"}}),
		newEventWithFinish("e2", "agent", []*genai.Part{{Text: "answer 1"}}, "STOP"),
		newEvent("e3", "user", []*genai.Part{{Text: "question 2"}}),
		newEventWithFinish("e4", "agent", []*genai.Part{{Text: "answer 2"}}, "STOP"),
	}
	msgs := eventsToMessages(events, "sess-1")

	require.Len(t, msgs, 4)
	assert.Equal(t, message.User, msgs[0].Role)
	assert.Equal(t, "question 1", msgs[0].Content().Text)
	assert.Equal(t, message.Assistant, msgs[1].Role)
	assert.Equal(t, "answer 1", msgs[1].Content().Text)
	assert.Equal(t, message.User, msgs[2].Role)
	assert.Equal(t, "question 2", msgs[2].Content().Text)
	assert.Equal(t, message.Assistant, msgs[3].Role)
	assert.Equal(t, "answer 2", msgs[3].Content().Text)
}

func TestEventsToMessages_NilContentSkipped(t *testing.T) {
	events := testEvents{
		{
			LLMResponse: adkmodel.LLMResponse{Content: nil},
			ID:          "e-nil",
			Timestamp:   time.Unix(1000, 0),
			Author:      "agent",
		},
		newEvent("e1", "user", []*genai.Part{{Text: "hi"}}),
	}
	msgs := eventsToMessages(events, "sess-1")

	require.Len(t, msgs, 1)
	assert.Equal(t, "hi", msgs[0].Content().Text)
}

func TestEventsToMessages_AssistantFlushedAtEnd(t *testing.T) {
	// Assistant event without finish reason — should still be flushed at end.
	events := testEvents{
		newEvent("e1", "agent", []*genai.Part{{Text: "incomplete response"}}),
	}
	msgs := eventsToMessages(events, "sess-1")

	require.Len(t, msgs, 1)
	assert.Equal(t, message.Assistant, msgs[0].Role)
	assert.Equal(t, "incomplete response", msgs[0].Content().Text)
}

func TestEventsToUserPrompts(t *testing.T) {
	events := testEvents{
		newEvent("e1", "user", []*genai.Part{{Text: "prompt 1"}}),
		newEventWithFinish("e2", "agent", []*genai.Part{{Text: "reply"}}, "STOP"),
		newEvent("e3", "user", []*genai.Part{{Text: "prompt 2"}}),
		// Empty user event — should be excluded.
		newEvent("e4", "user", []*genai.Part{{Text: ""}}),
	}
	msgs := eventsToUserPrompts(events, "sess-1")

	require.Len(t, msgs, 2)
	assert.Equal(t, "prompt 1", msgs[0].Content().Text)
	assert.Equal(t, "prompt 2", msgs[1].Content().Text)
}

func TestEventsToMessages_ThoughtToolRoundTrip(t *testing.T) {
	events := testEvents{
		// Agent calls thought tool.
		newEvent("e1", "agent", []*genai.Part{
			{FunctionCall: &genai.FunctionCall{
				ID:   "fc-thought-1",
				Name: "thought",
				Args: map[string]any{
					"reasoning":   "Task requires draft then build. Draft first because...",
					"next_action": "dispatch to draft",
				},
			}},
		}),
		// Thought tool responds with {"acknowledged": true} — no "result" or "error" key.
		newEvent("e2", "agent", []*genai.Part{
			{FunctionResponse: &genai.FunctionResponse{
				ID:       "fc-thought-1",
				Name:     "thought",
				Response: map[string]any{"acknowledged": true},
			}},
		}),
		// Agent produces terse status text, then dispatches.
		newEventWithFinish("e3", "agent", []*genai.Part{
			{Text: "Dispatching to draft."},
		}, "STOP"),
	}

	msgs := eventsToMessages(events, "sess-1")
	require.Len(t, msgs, 3)

	// First: assistant with thought tool call. Input contains reasoning.
	assert.Equal(t, message.Assistant, msgs[0].Role)
	toolCalls := msgs[0].ToolCalls()
	require.Len(t, toolCalls, 1)
	assert.Equal(t, "thought", toolCalls[0].Name)
	assert.Contains(t, toolCalls[0].Input, "reasoning")

	// Second: tool result with empty content (acknowledged has no "result"/"error" key).
	assert.Equal(t, message.Tool, msgs[1].Role)
	tr, ok := msgs[1].Parts[0].(message.ToolResult)
	require.True(t, ok)
	assert.Equal(t, "fc-thought-1", tr.ToolCallID)
	assert.Equal(t, "", tr.Content)
	assert.False(t, tr.IsError)

	// Third: assistant text.
	assert.Equal(t, message.Assistant, msgs[2].Role)
	assert.Equal(t, "Dispatching to draft.", msgs[2].Content().Text)
}

func TestEventsToMessages_FunctionResponseThenText(t *testing.T) {
	// Regression test: an event with FunctionResponse followed by Text parts
	// must not panic. flushAssistant nils currentAssistant after FunctionResponse,
	// so ensureAssistant must re-create it for subsequent Text parts.
	events := testEvents{
		// Agent calls a tool.
		newEvent("e1", "agent", []*genai.Part{
			{FunctionCall: &genai.FunctionCall{ID: "fc-1", Name: "thought"}},
		}),
		// Single event with FunctionResponse + Text (mixed parts).
		newEventWithFinish("e2", "agent", []*genai.Part{
			{FunctionResponse: &genai.FunctionResponse{
				ID:       "fc-1",
				Name:     "thought",
				Response: map[string]any{"acknowledged": true},
			}},
			{Text: "Status update after thought."},
		}, "STOP"),
	}

	// Must not panic.
	msgs := eventsToMessages(events, "sess-1")

	// Expected: assistant (tool call), tool result, assistant (text).
	require.Len(t, msgs, 3)
	assert.Equal(t, message.Tool, msgs[1].Role)
	assert.Equal(t, message.Assistant, msgs[2].Role)
	assert.Equal(t, "Status update after thought.", msgs[2].Content().Text)
}

func TestEventsToMessages_UserShellEvent(t *testing.T) {
	shellXML := `<user_shell command="git status" exit_code="0">
On branch main
nothing to commit
</user_shell>`
	events := testEvents{
		newEvent("e1", "user", []*genai.Part{{Text: shellXML}}),
	}
	msgs := eventsToMessages(events, "sess-1")

	// Should produce assistant (ToolCall) + tool (ToolResult) messages.
	require.Len(t, msgs, 2)

	// Assistant message with bash tool call.
	assert.Equal(t, message.Assistant, msgs[0].Role)
	toolCalls := msgs[0].ToolCalls()
	require.Len(t, toolCalls, 1)
	assert.Equal(t, "bash", toolCalls[0].Name)
	assert.Equal(t, message.ToolStateDone, toolCalls[0].State)
	assert.Contains(t, toolCalls[0].Input, "git status")

	// Tool result message.
	assert.Equal(t, message.Tool, msgs[1].Role)
	tr, ok := msgs[1].Parts[0].(message.ToolResult)
	require.True(t, ok)
	assert.Equal(t, "bash", tr.Name)
	assert.Equal(t, "On branch main\nnothing to commit", tr.Content)
	assert.False(t, tr.IsError)
}

func TestEventsToMessages_UserShellEventError(t *testing.T) {
	shellXML := `<user_shell command="false" exit_code="1">

</user_shell>`
	events := testEvents{
		newEvent("e1", "user", []*genai.Part{{Text: shellXML}}),
	}
	msgs := eventsToMessages(events, "sess-1")

	require.Len(t, msgs, 2)
	tr, ok := msgs[1].Parts[0].(message.ToolResult)
	require.True(t, ok)
	assert.True(t, tr.IsError, "exit_code=1 should produce IsError=true")
}

func TestEventsToMessages_UserShellWithTextParts(t *testing.T) {
	shellXML := `<user_shell command="ls" exit_code="0">
file.txt
</user_shell>`
	events := testEvents{
		newEvent("e1", "user", []*genai.Part{
			{Text: "Hello supervisor"},
			{Text: shellXML},
		}),
	}
	msgs := eventsToMessages(events, "sess-1")

	// Should produce: user message + assistant tool call + tool result.
	require.Len(t, msgs, 3)
	assert.Equal(t, message.User, msgs[0].Role)
	assert.Equal(t, "Hello supervisor", msgs[0].Content().Text)
	assert.Equal(t, message.Assistant, msgs[1].Role)
	assert.Equal(t, message.Tool, msgs[2].Role)
}

func TestEventsToMessages_RegularUserUnchanged(t *testing.T) {
	// Regular user text should not be affected by shell event handling.
	events := testEvents{
		newEvent("e1", "user", []*genai.Part{{Text: "just a normal message"}}),
	}
	msgs := eventsToMessages(events, "sess-1")

	require.Len(t, msgs, 1)
	assert.Equal(t, message.User, msgs[0].Role)
	assert.Equal(t, "just a normal message", msgs[0].Content().Text)
}

func TestParseUserShellEvent(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantOK  bool
		wantCmd string
		wantEC  int
	}{
		{
			name:    "valid shell event",
			text:    "<user_shell command=\"ls -la\" exit_code=\"0\">\nfile.txt\n</user_shell>",
			wantOK:  true,
			wantCmd: "ls -la",
			wantEC:  0,
		},
		{
			name:    "error exit code",
			text:    "<user_shell command=\"false\" exit_code=\"1\">\nerror\n</user_shell>",
			wantOK:  true,
			wantCmd: "false",
			wantEC:  1,
		},
		{
			name:   "regular text",
			text:   "Hello world",
			wantOK: false,
		},
		{
			name:   "partial match",
			text:   "prefix <user_shell command=\"x\" exit_code=\"0\">\nout\n</user_shell>",
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, ok := parseUserShellEvent(tt.text)
			assert.Equal(t, tt.wantOK, ok)
			if ok {
				assert.Equal(t, tt.wantCmd, parsed.Command)
				assert.Equal(t, tt.wantEC, parsed.ExitCode)
			}
		})
	}
}

func TestExtractFunctionResponseContent(t *testing.T) {
	tests := []struct {
		name        string
		resp        *genai.FunctionResponse
		expected    string
		expectError bool
	}{
		{"nil response", nil, "", false},
		{"nil map", &genai.FunctionResponse{Response: nil}, "", false},
		{"result key", &genai.FunctionResponse{Response: map[string]any{"result": "ok"}}, "ok", false},
		{"error key", &genai.FunctionResponse{Response: map[string]any{"error": "fail"}}, "fail", true},
		{"neither key", &genai.FunctionResponse{Response: map[string]any{"other": "val"}}, "", false},
		{"acknowledged key (thought tool)", &genai.FunctionResponse{Response: map[string]any{"acknowledged": true}}, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content, isError := extractFunctionResponseContent(tt.resp)
			assert.Equal(t, tt.expected, content)
			assert.Equal(t, tt.expectError, isError)
		})
	}
}
