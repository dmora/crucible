package chat

import (
	"strings"
	"testing"

	"github.com/dmora/crucible/internal/askuser"
	"github.com/dmora/crucible/internal/message"
	"github.com/dmora/crucible/internal/ui/styles"
)

func newTestStyles() *styles.Styles {
	s := styles.NewStyles("", false)
	return &s
}

func TestAskUserRenderTool_Pending(t *testing.T) {
	sty := newTestStyles()
	tc := message.ToolCall{Name: "ask_user"}
	item := NewAskUserToolMessageItem(sty, tc, nil, false)
	output := item.Render(80)
	if !strings.Contains(output, "Ask User") {
		t.Errorf("expected 'Ask User' in pending output, got: %s", output)
	}
}

func TestAskUserRenderTool_WithResult(t *testing.T) {
	sty := newTestStyles()
	tc := message.ToolCall{Name: "ask_user", State: message.ToolStateDone}
	result := &message.ToolResult{Content: "Approach: Minimal fix"}
	item := NewAskUserToolMessageItem(sty, tc, result, false)
	output := item.Render(80)
	if !strings.Contains(output, "Approach") {
		t.Errorf("expected 'Approach' in output, got: %s", output)
	}
}

func TestAskUserRenderTool_Canceled(t *testing.T) {
	sty := newTestStyles()
	tc := message.ToolCall{Name: "ask_user", State: message.ToolStateDone}
	result := &message.ToolResult{Content: askuser.ErrCanceledMessage}
	item := NewAskUserToolMessageItem(sty, tc, result, false)
	output := item.Render(80)
	if !strings.Contains(output, "canceled") && !strings.Contains(output, "Canceled") {
		t.Errorf("expected cancel indicator in output, got: %s", output)
	}
}

func TestRenderAskUserResult(t *testing.T) {
	sty := newTestStyles()
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"single answer", "Approach: Minimal fix", "Approach:"},
		{"multi answer", "Approach: Minimal fix\nScope: Module-only", "Scope:"},
		{"no colon fallback", "raw text without colon", "raw text without colon"},
		{"empty content", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := renderAskUserResult(sty, tt.content, 60)
			if tt.want != "" && !strings.Contains(result, tt.want) {
				t.Errorf("expected %q in result, got: %s", tt.want, result)
			}
		})
	}
}
