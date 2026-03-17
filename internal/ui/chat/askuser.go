package chat

import (
	"strings"

	"github.com/dmora/crucible/internal/askuser"
	"github.com/dmora/crucible/internal/message"
	"github.com/dmora/crucible/internal/ui/styles"
)

// askUserRenderContext renders ask_user tool messages in the chat.
type askUserRenderContext struct{}

// NewAskUserToolMessageItem creates a new ask_user tool message item.
func NewAskUserToolMessageItem(
	sty *styles.Styles,
	toolCall message.ToolCall,
	result *message.ToolResult,
	canceled bool,
) ToolMessageItem {
	// Detect cancellation from result content (deterministic error string on reload).
	if !canceled && result != nil && result.Content == askuser.ErrCanceledMessage {
		canceled = true
	}
	return newBaseToolMessageItem(sty, toolCall, result, &askUserRenderContext{}, canceled)
}

// RenderTool implements the [ToolRenderer] interface.
func (a *askUserRenderContext) RenderTool(sty *styles.Styles, width int, opts *ToolRenderOpts) string {
	cappedWidth := cappedMessageWidth(width)
	name := "Ask User"

	if opts.IsPending() {
		return pendingTool(sty, name, opts.Anim)
	}

	header := toolHeader(sty, opts.Status, name, cappedWidth, opts.Compact)
	if opts.Compact {
		return header
	}

	// Handle early states (error, canceled, pending).
	if earlyState, ok := toolEarlyStateContent(sty, opts, cappedWidth); ok {
		return joinToolParts(header, earlyState)
	}

	if !opts.HasResult() || opts.Result.Content == "" {
		return header
	}

	// Parse the "Header: value1, value2" result format.
	bodyWidth := cappedWidth - toolBodyLeftPaddingTotal
	body := renderAskUserResult(sty, opts.Result.Content, bodyWidth)
	return joinToolParts(header, sty.Tool.Body.Render(body))
}

// renderAskUserResult parses the dual-format Result string and renders Q&A pairs.
func renderAskUserResult(sty *styles.Styles, content string, _ int) string {
	lines := strings.Split(content, "\n")
	var rendered []string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ": ", 2)
		if len(parts) == 2 {
			rendered = append(rendered, sty.Tool.ParamKey.Render(parts[0]+":")+
				" "+sty.Tool.ContentText.Render(parts[1]))
		} else {
			// Fallback: render raw line.
			rendered = append(rendered, line)
		}
	}

	if len(rendered) == 0 {
		return content
	}
	return strings.Join(rendered, "\n")
}
