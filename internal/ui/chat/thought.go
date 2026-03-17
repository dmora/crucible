package chat

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/dmora/crucible/internal/agent/tools"
	"github.com/dmora/crucible/internal/message"
	"github.com/dmora/crucible/internal/ui/styles"
)

// thoughtRenderContext renders thought tool messages in the chat.
type thoughtRenderContext struct{}

// NewThoughtToolMessageItem creates a new thought tool message item.
func NewThoughtToolMessageItem(
	sty *styles.Styles,
	toolCall message.ToolCall,
	result *message.ToolResult,
	canceled bool,
) ToolMessageItem {
	return newBaseToolMessageItem(sty, toolCall, result, &thoughtRenderContext{}, canceled)
}

// RenderTool implements the [ToolRenderer] interface.
func (r *thoughtRenderContext) RenderTool(sty *styles.Styles, width int, opts *ToolRenderOpts) string {
	cappedWidth := cappedMessageWidth(width)

	if opts.IsPending() {
		return pendingTool(sty, "Thought", opts.Anim)
	}

	// Parse input to determine label and content.
	var params tools.ThoughtParams
	if opts.ToolCall.Input != "" {
		_ = json.Unmarshal([]byte(opts.ToolCall.Input), &params)
	}

	label := "Thought"
	if params.IsRevision {
		label = "Revised"
	}

	// Build custom header (no toolIcon — wrong semantics for a thought card).
	header := sty.Chat.Message.ThinkingBox.Render(
		sty.Subtle.Render("◇ " + label),
	)

	if opts.Compact {
		return header
	}

	// Handle early states (error, canceled).
	if earlyState, ok := toolEarlyStateContent(sty, opts, cappedWidth); ok {
		return joinToolParts(header, earlyState)
	}

	if params.Reasoning == "" {
		return header
	}

	bodyWidth := cappedWidth - toolBodyLeftPaddingTotal

	// Collapsed (default): show wrapped preview (up to 4 lines).
	if !opts.ExpandedContent {
		wrapped := ansi.Wordwrap(params.Reasoning, bodyWidth-4, "")
		lines := strings.SplitN(wrapped, "\n", 5)
		if len(lines) > 4 {
			lines[3] += "…"
			lines = lines[:4]
		}
		preview := sty.Muted.Render("  " + strings.Join(lines, "\n  "))
		return fmt.Sprintf("%s\n%s", header, preview)
	}

	// Expanded: full reasoning in ThinkingBox style.
	reasoning := ansi.Wordwrap(params.Reasoning, bodyWidth-2, "")
	body := sty.Chat.Message.ThinkingBox.Width(cappedWidth).Render(reasoning)

	if params.NextAction != "" {
		nextLine := sty.Tool.ParamKey.Render("Next:") + " " + sty.Tool.ContentText.Render(params.NextAction)
		body += "\n" + sty.Tool.Body.Render(nextLine)
	}

	return fmt.Sprintf("%s\n%s", header, body)
}
