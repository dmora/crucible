package chat

import (
	"fmt"
	"log/slog"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/dmora/crucible/internal/message"
	"github.com/dmora/crucible/internal/ui/anim"
	"github.com/dmora/crucible/internal/ui/common"
	"github.com/dmora/crucible/internal/ui/styles"
)

// assistantMessageTruncateFormat is the text shown when an assistant message is
// truncated.
const assistantMessageTruncateFormat = "… (%d lines hidden) [click or space to expand]"

// maxCollapsedThinkingHeight defines the maximum height of the thinking
const maxCollapsedThinkingHeight = 10

// AssistantMessageItem represents an assistant message in the chat UI.
//
// This item includes thinking, and the content but does not include the tool calls.
type AssistantMessageItem struct {
	*highlightableMessageItem
	*cachedMessageItem
	*focusableMessageItem

	message           *message.Message
	sty               *styles.Styles
	anim              anim.SpinnerBackend
	thinkingExpanded  bool
	thinkingBoxHeight int // Tracks the rendered thinking box height for click detection.
	revealLen         int // Typing reveal: how many runes of content to show.
	lastRenderedLen   int // Last revealLen that was actually rendered (avoids redundant cache clears).
}

// NewAssistantMessageItem creates a new AssistantMessageItem.
func NewAssistantMessageItem(sty *styles.Styles, message *message.Message) MessageItem {
	a := &AssistantMessageItem{
		highlightableMessageItem: defaultHighlighter(sty),
		cachedMessageItem:        &cachedMessageItem{},
		focusableMessageItem:     &focusableMessageItem{},
		message:                  message,
		sty:                      sty,
	}

	settings := anim.Settings{
		ID:          a.ID(),
		Size:        25,
		GradColorA:  sty.Primary,
		GradColorB:  sty.Secondary,
		GradColorC:  sty.Tertiary,
		LabelColor:  sty.FgBase,
		CycleColors: true,
	}
	backend, err := anim.NewSpinner(sty.SpinnerPreset, settings)
	if err != nil {
		slog.Warn("invalid spinner preset, falling back to industrial", "preset", sty.SpinnerPreset, "err", err)
		backend = anim.New(settings)
	}
	a.anim = backend
	return a
}

// StartAnimation starts the assistant message animation if it should be spinning.
func (a *AssistantMessageItem) StartAnimation() tea.Cmd {
	if !a.isSpinning() {
		return nil
	}
	return a.anim.Start()
}

// Animate progresses the assistant message animation if it should be spinning.
func (a *AssistantMessageItem) Animate(msg anim.StepMsg) tea.Cmd {
	if !a.isSpinning() {
		return nil
	}
	// Advance typing reveal cursor toward full content.
	total := a.totalContentRunes()
	if a.revealLen < total {
		pending := total - a.revealLen
		a.revealLen += max(1, pending/3)
		// Only invalidate cache when enough new runes are revealed to change
		// the rendered markdown (avoids redundant glamour calls between frames).
		if a.revealLen-a.lastRenderedLen > 20 || a.revealLen >= total {
			a.clearCache()
		}
	}
	return a.anim.Animate(msg)
}

// ID implements MessageItem.
func (a *AssistantMessageItem) ID() string {
	return a.message.ID
}

// RawRender implements [MessageItem].
func (a *AssistantMessageItem) RawRender(width int) string {
	cappedWidth := cappedMessageWidth(width)

	var spinner string
	if a.isSpinning() {
		spinner = a.renderSpinning()
	}

	content, height, ok := a.getCachedRender(cappedWidth)
	if !ok {
		content = a.renderMessageContent(cappedWidth)
		height = lipgloss.Height(content)
		// cache the rendered content
		a.setCachedRender(content, cappedWidth, height)
	}

	highlightedContent := a.renderHighlighted(content, cappedWidth, height)
	if spinner != "" {
		if highlightedContent == "" {
			return spinner
		}
		// Find the last line with actual content (skip trailing empty lines
		// that markdown renderers or spacers may add).
		lines := strings.Split(highlightedContent, "\n")
		lastIdx := len(lines) - 1
		for lastIdx > 0 && strings.TrimSpace(ansi.Strip(lines[lastIdx])) == "" {
			lastIdx--
		}
		// Classic spinners render on a fixed new line below content.
		if !a.anim.FollowsText() {
			return strings.Join(lines[:lastIdx+1], "\n") + "\n" + spinner
		}
		// Industrial spinners chase the trailing edge of streamed text.
		// Measure the real content width (strip markdown trailing-space padding).
		stripped := strings.TrimRight(ansi.Strip(lines[lastIdx]), " ")
		contentWidth := lipgloss.Width(stripped)
		remaining := cappedWidth - contentWidth
		if remaining >= 3 {
			trimmed := ansi.Truncate(lines[lastIdx], contentWidth, "")
			lines[lastIdx] = trimmed + ansi.Truncate(spinner, remaining, "")
			return strings.Join(lines[:lastIdx+1], "\n")
		}
		// Line is full — put spinner on the next line.
		return strings.Join(lines[:lastIdx+1], "\n") + "\n" + spinner
	}

	return highlightedContent
}

// Render implements MessageItem.
func (a *AssistantMessageItem) Render(width int) string {
	// XXX: Here, we're manually applying the focused/blurred styles because
	// using lipgloss.Render can degrade performance for long messages due to
	// it's wrapping logic.
	// We already know that the content is wrapped to the correct width in
	// RawRender, so we can just apply the styles directly to each line.
	focused := a.sty.Chat.Message.AssistantFocused.Render()
	blurred := a.sty.Chat.Message.AssistantBlurred.Render()
	rendered := a.RawRender(width)
	lines := strings.Split(rendered, "\n")
	for i, line := range lines {
		if a.focused {
			lines[i] = focused + line
		} else {
			lines[i] = blurred + line
		}
	}
	return strings.Join(lines, "\n")
}

// renderMessageContent renders the message content including thinking, main content, and finish reason.
func (a *AssistantMessageItem) renderMessageContent(width int) string {
	var messageParts []string
	thinking := strings.TrimSpace(a.message.ReasoningContent().Thinking)
	content := strings.TrimSpace(a.message.Content().Text)

	// Apply typing reveal: only show up to revealLen runes while streaming.
	if a.isSpinning() {
		thinking, content = a.applyTypingReveal(thinking, content)
	}
	a.lastRenderedLen = a.revealLen

	// if the message has reasoning content add that first
	if thinking != "" {
		messageParts = append(messageParts, a.renderThinking(thinking, width))
	}

	// then add the main content
	if content != "" {
		// add a spacer between thinking and content
		if thinking != "" {
			messageParts = append(messageParts, "")
		}
		messageParts = append(messageParts, a.renderMarkdown(content, width))
	}

	// finally add any finish reason info
	if a.message.IsFinished() {
		switch a.message.FinishReason() {
		case message.FinishReasonCanceled:
			messageParts = append(messageParts, a.sty.Base.Italic(true).Render("Canceled"))
		case message.FinishReasonError:
			messageParts = append(messageParts, a.renderError(width))
		}
	}

	return strings.Join(messageParts, "\n")
}

// totalContentRunes returns the total rune count of thinking + text content.
func (a *AssistantMessageItem) totalContentRunes() int {
	n := len([]rune(strings.TrimSpace(a.message.ReasoningContent().Thinking)))
	n += len([]rune(strings.TrimSpace(a.message.Content().Text)))
	return n
}

// applyTypingReveal truncates thinking and content to revealLen total runes.
func (a *AssistantMessageItem) applyTypingReveal(thinking, content string) (string, string) {
	thinkingRunes := []rune(thinking)
	contentRunes := []rune(content)
	remaining := a.revealLen
	if remaining < len(thinkingRunes) {
		return string(thinkingRunes[:remaining]), ""
	}
	remaining -= len(thinkingRunes)
	if remaining < len(contentRunes) {
		return thinking, string(contentRunes[:remaining])
	}
	return thinking, content
}

// renderThinking renders the thinking/reasoning content with footer.
func (a *AssistantMessageItem) renderThinking(thinking string, width int) string {
	// Strip literal \n\n from Gemini thought summaries (API artifact).
	thinking = strings.ReplaceAll(thinking, `\n\n`, "")
	renderer := common.PlainMarkdownRenderer(a.sty, width)
	rendered, err := renderer.Render(thinking)
	if err != nil {
		rendered = thinking
	}
	rendered = strings.TrimSpace(rendered)

	lines := strings.Split(rendered, "\n")
	totalLines := len(lines)

	isTruncated := totalLines > maxCollapsedThinkingHeight
	if !a.thinkingExpanded && isTruncated {
		lines = lines[totalLines-maxCollapsedThinkingHeight:]
		hint := a.sty.Chat.Message.ThinkingTruncationHint.Render(
			fmt.Sprintf(assistantMessageTruncateFormat, totalLines-maxCollapsedThinkingHeight),
		)
		lines = append([]string{hint, ""}, lines...)
	}

	thinkingStyle := a.sty.Chat.Message.ThinkingBox.Width(width)
	result := thinkingStyle.Render(strings.Join(lines, "\n"))
	a.thinkingBoxHeight = lipgloss.Height(result)

	var footer string
	// if thinking is done add the thought for footer
	if !a.message.IsThinking() || len(a.message.ToolCalls()) > 0 {
		duration := a.message.ThinkingDuration()
		if duration.String() != "0s" {
			footer = a.sty.Chat.Message.ThinkingFooterTitle.Render("Thought for ") +
				a.sty.Chat.Message.ThinkingFooterDuration.Render(duration.String())
		}
	}

	if footer != "" {
		result += "\n\n" + footer
	}

	return result
}

// renderMarkdown renders content as markdown.
func (a *AssistantMessageItem) renderMarkdown(content string, width int) string {
	renderer := common.MarkdownRenderer(a.sty, width)
	result, err := renderer.Render(content)
	if err != nil {
		return content
	}
	return strings.TrimSuffix(result, "\n")
}

func (a *AssistantMessageItem) renderSpinning() string {
	if a.message.IsSummaryMessage {
		a.anim.SetLabel("COMPACTING")
	}
	return a.anim.Render()
}

// renderError renders an error message.
func (a *AssistantMessageItem) renderError(width int) string {
	finishPart := a.message.FinishPart()
	errTag := a.sty.Chat.Message.ErrorTag.Render("ERROR")
	truncated := ansi.Truncate(finishPart.Message, width-2-lipgloss.Width(errTag), "...")
	title := fmt.Sprintf("%s %s", errTag, a.sty.Chat.Message.ErrorTitle.Render(truncated))
	details := a.sty.Chat.Message.ErrorDetails.Width(width - 2).Render(finishPart.Details)
	return fmt.Sprintf("%s\n\n%s", title, details)
}

// isSpinning returns true if the assistant message is still generating.
// Once tool calls begin, the spinner belongs on the tool cards, not here.
func (a *AssistantMessageItem) isSpinning() bool {
	return !a.message.IsFinished() && len(a.message.ToolCalls()) == 0
}

// SetMessage is used to update the underlying message.
func (a *AssistantMessageItem) SetMessage(message *message.Message) tea.Cmd {
	wasSpinning := a.isSpinning()
	a.message = message
	a.clearCache()
	// When finished, reveal all content immediately — no typing animation for the tail.
	if message.IsFinished() {
		a.revealLen = a.totalContentRunes()
	}
	if !wasSpinning && a.isSpinning() {
		return a.StartAnimation()
	}
	return nil
}

// ToggleExpanded toggles the expanded state of the thinking box.
func (a *AssistantMessageItem) ToggleExpanded() {
	a.thinkingExpanded = !a.thinkingExpanded
	a.clearCache()
}

// HandleMouseClick implements MouseClickable.
func (a *AssistantMessageItem) HandleMouseClick(btn ansi.MouseButton, x, y int) bool {
	if btn != ansi.MouseLeft {
		return false
	}
	// check if the click is within the thinking box
	if a.thinkingBoxHeight > 0 && y < a.thinkingBoxHeight {
		a.ToggleExpanded()
		return true
	}
	return false
}

// HandleKeyEvent implements KeyEventHandler.
func (a *AssistantMessageItem) HandleKeyEvent(key tea.KeyMsg) (bool, tea.Cmd) {
	if k := key.String(); k == "c" || k == "y" {
		text := a.message.Content().Text
		return true, common.CopyToClipboard(text, "Message copied to clipboard")
	}
	return false, nil
}
