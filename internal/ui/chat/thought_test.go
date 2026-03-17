package chat

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dmora/crucible/internal/agent/tools"
	"github.com/dmora/crucible/internal/message"
)

func TestThoughtRenderTool_DefaultCollapsed(t *testing.T) {
	sty := newTestStyles()
	input, _ := json.Marshal(tools.ThoughtParams{
		Reasoning: "Task requires draft then build because the change is non-trivial and touches multiple files. " +
			"First we need to understand the existing architecture and how the components interact with each other. " +
			"Then we should design an approach that minimizes risk while still delivering the feature. " +
			"After that we implement the changes incrementally and verify each step. " +
			"Finally we run the full test suite to confirm nothing is broken by the modifications.",
	})
	tc := message.ToolCall{
		Name:  "thought",
		State: message.ToolStateDone,
		Input: string(input),
	}
	result := &message.ToolResult{Content: ""}
	item := NewThoughtToolMessageItem(sty, tc, result, false)
	output := item.Render(80)

	if !strings.Contains(output, "Thought") {
		t.Errorf("expected 'Thought' label in collapsed output, got: %s", output)
	}
	// Should NOT contain text beyond the 4-line preview.
	if strings.Contains(output, "nothing is broken") {
		t.Errorf("expected reasoning to be truncated after 4 lines in collapsed output, got: %s", output)
	}
	// Should contain ellipsis indicating truncation.
	if !strings.Contains(output, "…") {
		t.Errorf("expected ellipsis in truncated collapsed output, got: %s", output)
	}
}

func TestThoughtRenderTool_Expanded(t *testing.T) {
	sty := newTestStyles()
	input, _ := json.Marshal(tools.ThoughtParams{
		Reasoning:  "Task requires draft then build because the change is non-trivial.",
		NextAction: "dispatch to draft",
	})
	tc := message.ToolCall{
		Name:  "thought",
		State: message.ToolStateDone,
		Input: string(input),
	}
	result := &message.ToolResult{Content: ""}
	item := NewThoughtToolMessageItem(sty, tc, result, false)
	// Cast to *baseToolMessageItem to access ToggleExpanded.
	base := item.(*baseToolMessageItem)
	base.ToggleExpanded()
	output := item.Render(80)

	if !strings.Contains(output, "non-trivial") {
		t.Errorf("expected full reasoning in expanded output, got: %s", output)
	}
	if !strings.Contains(output, "Next:") {
		t.Errorf("expected 'Next:' label in expanded output, got: %s", output)
	}
	if !strings.Contains(output, "dispatch to draft") {
		t.Errorf("expected next_action text in expanded output, got: %s", output)
	}
}

func TestThoughtRenderTool_Revised(t *testing.T) {
	sty := newTestStyles()
	input, _ := json.Marshal(tools.ThoughtParams{
		Reasoning:  "Revising after station returned unexpected error.",
		IsRevision: true,
	})
	tc := message.ToolCall{
		Name:  "thought",
		State: message.ToolStateDone,
		Input: string(input),
	}
	result := &message.ToolResult{Content: ""}
	item := NewThoughtToolMessageItem(sty, tc, result, false)
	output := item.Render(80)

	if !strings.Contains(output, "Revised") {
		t.Errorf("expected 'Revised' indicator in output, got: %s", output)
	}
}
