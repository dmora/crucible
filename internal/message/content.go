package message

import (
	"encoding/base64"
	"fmt"
	"html"
	"slices"
	"strings"
	"time"
)

type MessageRole string

const (
	Assistant MessageRole = "assistant"
	User      MessageRole = "user"
	System    MessageRole = "system"
	Tool      MessageRole = "tool"
)

type FinishReason string

const (
	FinishReasonEndTurn          FinishReason = "end_turn"
	FinishReasonMaxTokens        FinishReason = "max_tokens"
	FinishReasonToolUse          FinishReason = "tool_use"
	FinishReasonCanceled         FinishReason = "canceled"
	FinishReasonError            FinishReason = "error"
	FinishReasonPermissionDenied FinishReason = "permission_denied"

	// Should never happen
	FinishReasonUnknown FinishReason = "unknown"
)

type ContentPart interface {
	isPart()
}

type ReasoningContent struct {
	Thinking         string `json:"thinking"`
	Signature        string `json:"signature"`
	ThoughtSignature string `json:"thought_signature"` // Used for google
	ToolID           string `json:"tool_id"`           // Used for openrouter google models
	StartedAt        int64  `json:"started_at,omitempty"`
	FinishedAt       int64  `json:"finished_at,omitempty"`
}

func (tc ReasoningContent) String() string {
	return tc.Thinking
}
func (ReasoningContent) isPart() {}

type TextContent struct {
	Text string `json:"text"`
}

func (tc TextContent) String() string {
	return tc.Text
}

func (TextContent) isPart() {}

type BinaryContent struct {
	Path     string
	MIMEType string
	Data     []byte
}

func (bc BinaryContent) String() string {
	return base64.StdEncoding.EncodeToString(bc.Data)
}

func (BinaryContent) isPart() {}

// ToolState tracks the backend execution lifecycle of a tool call.
// This is the domain model state — mutated only by the agent event loop,
// never by UI code.
type ToolState string

const (
	ToolStatePending ToolState = "pending" // created, awaiting execution
	// ToolStateRunning is reserved for context exhaustion tracking (#42).
	// Not yet set by production code.
	ToolStateRunning  ToolState = "running"  // executing
	ToolStateDone     ToolState = "done"     // FunctionResponse received
	ToolStateCanceled ToolState = "canceled" // turn canceled before FunctionResponse
)

// IsTerminal reports whether the tool has reached a final state.
func (s ToolState) IsTerminal() bool {
	return s == ToolStateDone || s == ToolStateCanceled
}

type ToolCall struct {
	ID    string    `json:"id"`
	Name  string    `json:"name"`
	Input string    `json:"input"`
	State ToolState `json:"state"`
}

func (ToolCall) isPart() {}

type ToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Name       string `json:"name"`
	Content    string `json:"content"`
	Data       string `json:"data"`
	MIMEType   string `json:"mime_type"`
	Metadata   string `json:"metadata"`
	IsError    bool   `json:"is_error"`
}

func (ToolResult) isPart() {}

// GroundingSource represents a web source from Gemini's Google Search grounding.
type GroundingSource struct {
	Title  string `json:"title"`
	URL    string `json:"url"`
	Domain string `json:"domain,omitempty"`
}

// GroundingSupport maps a response claim to the sources that back it.
type GroundingSupport struct {
	Text         string    `json:"text,omitempty"`
	ChunkIndices []int32   `json:"chunk_indices,omitempty"`
	Scores       []float32 `json:"scores,omitempty"`
}

// GroundingCitation holds attribution metadata for a quoted segment.
type GroundingCitation struct {
	StartIndex int32  `json:"start_index"`
	EndIndex   int32  `json:"end_index"`
	URI        string `json:"uri,omitempty"`
	Title      string `json:"title,omitempty"`
	License    string `json:"license,omitempty"`
}

// GroundingContent represents Google Search grounding metadata attached to an
// assistant response. It contains the search queries the model used and the
// web sources that informed the response.
type GroundingContent struct {
	Queries   []string            `json:"queries,omitempty"`
	Sources   []GroundingSource   `json:"sources,omitempty"`
	Supports  []GroundingSupport  `json:"supports,omitempty"`
	Citations []GroundingCitation `json:"citations,omitempty"`
}

func (GroundingContent) isPart() {}

type Finish struct {
	Reason           FinishReason `json:"reason"`
	Time             int64        `json:"time"`
	Message          string       `json:"message,omitempty"`
	Details          string       `json:"details,omitempty"`
	PromptTokens     int64        `json:"prompt_tokens,omitempty"`
	CandidatesTokens int64        `json:"candidates_tokens,omitempty"`
	TotalTokens      int64        `json:"total_tokens,omitempty"`
}

func (Finish) isPart() {}

type Message struct {
	ID               string
	Role             MessageRole
	SessionID        string
	Parts            []ContentPart
	Model            string
	Provider         string
	CreatedAt        int64
	UpdatedAt        int64
	IsSummaryMessage bool
}

func (m *Message) Content() TextContent {
	for _, part := range m.Parts {
		if c, ok := part.(TextContent); ok {
			return c
		}
	}
	return TextContent{}
}

func (m *Message) ReasoningContent() ReasoningContent {
	for _, part := range m.Parts {
		if c, ok := part.(ReasoningContent); ok {
			return c
		}
	}
	return ReasoningContent{}
}

func (m *Message) BinaryContent() []BinaryContent {
	binaryContents := make([]BinaryContent, 0)
	for _, part := range m.Parts {
		if c, ok := part.(BinaryContent); ok {
			binaryContents = append(binaryContents, c)
		}
	}
	return binaryContents
}

// Grounding returns the first GroundingContent part, or nil if none exists.
func (m *Message) Grounding() *GroundingContent {
	for _, part := range m.Parts {
		if g, ok := part.(GroundingContent); ok {
			return &g
		}
	}
	return nil
}

func (m *Message) ToolCalls() []ToolCall {
	toolCalls := make([]ToolCall, 0)
	for _, part := range m.Parts {
		if c, ok := part.(ToolCall); ok {
			toolCalls = append(toolCalls, c)
		}
	}
	return toolCalls
}

func (m *Message) ToolResults() []ToolResult {
	toolResults := make([]ToolResult, 0)
	for _, part := range m.Parts {
		if c, ok := part.(ToolResult); ok {
			toolResults = append(toolResults, c)
		}
	}
	return toolResults
}

func (m *Message) IsFinished() bool {
	for _, part := range m.Parts {
		if _, ok := part.(Finish); ok {
			return true
		}
	}
	return false
}

func (m *Message) FinishPart() *Finish {
	for _, part := range m.Parts {
		if c, ok := part.(Finish); ok {
			return &c
		}
	}
	return nil
}

func (m *Message) FinishReason() FinishReason {
	for _, part := range m.Parts {
		if c, ok := part.(Finish); ok {
			return c.Reason
		}
	}
	return ""
}

func (m *Message) IsThinking() bool {
	if m.ReasoningContent().Thinking != "" && m.Content().Text == "" && !m.IsFinished() {
		return true
	}
	return false
}

func (m *Message) AppendContent(delta string) {
	found := false
	for i, part := range m.Parts {
		if c, ok := part.(TextContent); ok {
			m.Parts[i] = TextContent{Text: c.Text + delta}
			found = true
		}
	}
	if !found {
		m.Parts = append(m.Parts, TextContent{Text: delta})
	}
}

func (m *Message) AppendReasoningContent(delta string) {
	found := false
	for i, part := range m.Parts {
		if c, ok := part.(ReasoningContent); ok {
			m.Parts[i] = ReasoningContent{
				Thinking:   c.Thinking + delta,
				Signature:  c.Signature,
				StartedAt:  c.StartedAt,
				FinishedAt: c.FinishedAt,
			}
			found = true
		}
	}
	if !found {
		m.Parts = append(m.Parts, ReasoningContent{
			Thinking:  delta,
			StartedAt: time.Now().Unix(),
		})
	}
}

func (m *Message) FinishThinking() {
	for i, part := range m.Parts {
		if c, ok := part.(ReasoningContent); ok {
			if c.FinishedAt == 0 {
				m.Parts[i] = ReasoningContent{
					Thinking:   c.Thinking,
					Signature:  c.Signature,
					StartedAt:  c.StartedAt,
					FinishedAt: time.Now().Unix(),
				}
			}
			return
		}
	}
}

func (m *Message) ThinkingDuration() time.Duration {
	reasoning := m.ReasoningContent()
	if reasoning.StartedAt == 0 {
		return 0
	}

	endTime := reasoning.FinishedAt
	if endTime == 0 {
		endTime = time.Now().Unix()
	}

	return time.Duration(endTime-reasoning.StartedAt) * time.Second
}

func (m *Message) FinishToolCall(toolCallID string) {
	for i, part := range m.Parts {
		if c, ok := part.(ToolCall); ok && c.ID == toolCallID {
			c.State = ToolStateDone
			m.Parts[i] = c
			return
		}
	}
}

// CancelPendingToolCalls transitions all non-terminal tool calls to ToolStateCanceled.
func (m *Message) CancelPendingToolCalls() {
	for i, part := range m.Parts {
		if c, ok := part.(ToolCall); ok && !c.State.IsTerminal() {
			c.State = ToolStateCanceled
			m.Parts[i] = c
		}
	}
}

func (m *Message) AddToolCall(tc ToolCall) {
	for i, part := range m.Parts {
		if c, ok := part.(ToolCall); ok {
			if c.ID == tc.ID {
				m.Parts[i] = tc
				return
			}
		}
	}
	m.Parts = append(m.Parts, tc)
}

// Clone returns a deep copy of the message with an independent Parts slice.
// This prevents race conditions when the message is modified concurrently.
func (m *Message) Clone() Message {
	clone := *m
	clone.Parts = make([]ContentPart, len(m.Parts))
	copy(clone.Parts, m.Parts)
	return clone
}

func (m *Message) AddFinish(reason FinishReason, message, details string) {
	// remove any existing finish part
	for i, part := range m.Parts {
		if _, ok := part.(Finish); ok {
			m.Parts = slices.Delete(m.Parts, i, i+1)
			break
		}
	}
	m.Parts = append(m.Parts, Finish{Reason: reason, Time: time.Now().Unix(), Message: message, Details: details})
}

// SetFinishTokens sets token usage on the finish part.
func (m *Message) SetFinishTokens(prompt, candidates, total int64) {
	for i, part := range m.Parts {
		if f, ok := part.(Finish); ok {
			f.PromptTokens = prompt
			f.CandidatesTokens = candidates
			f.TotalTokens = total
			m.Parts[i] = f
			return
		}
	}
}

func PromptWithTextAttachments(prompt string, attachments []Attachment) string {
	var sb strings.Builder
	sb.WriteString(prompt)
	addedAttachments := false
	for _, content := range attachments {
		if !content.IsText() {
			continue
		}
		if !addedAttachments {
			sb.WriteString("\n<system_info>The files below have been attached by the user, consider them in your response</system_info>\n")
			addedAttachments = true
		}
		if content.FilePath != "" {
			fmt.Fprintf(&sb, "<file path='%s'>\n", html.EscapeString(content.FilePath))
		} else {
			sb.WriteString("<file>\n")
		}
		sb.WriteString("\n")
		sb.Write(content.Content)
		sb.WriteString("\n</file>\n")
	}
	return sb.String()
}
