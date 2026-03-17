package agent

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"google.golang.org/genai"

	adkagent "google.golang.org/adk/agent"
	adkmodel "google.golang.org/adk/model"
	"google.golang.org/adk/plugin"
	adksession "google.golang.org/adk/session"
	"google.golang.org/adk/tool"
)

// newLoggingPlugin creates an ADK plugin that logs all agent lifecycle events
// through Crucible's slog system. At DEBUG level, full content is logged
// with no truncation — the actual text sent to and received from Gemini.
func newLoggingPlugin() *plugin.Plugin {
	p := &slogPlugin{}
	plug, _ := plugin.New(plugin.Config{
		Name:                  "crucible_logging",
		OnUserMessageCallback: p.onUserMessage,
		BeforeRunCallback:     p.beforeRun,
		AfterRunCallback:      p.afterRun,
		OnEventCallback:       p.onEvent,
		BeforeModelCallback:   p.beforeModel,
		AfterModelCallback:    p.afterModel,
		OnModelErrorCallback:  p.onModelError,
		BeforeToolCallback:    p.beforeTool,
		AfterToolCallback:     p.afterTool,
		OnToolErrorCallback:   p.onToolError,
	})
	return plug
}

type slogPlugin struct{}

func (p *slogPlugin) onUserMessage(ctx adkagent.InvocationContext, msg *genai.Content) (*genai.Content, error) {
	slog.Debug("ADK user message",
		"invocation_id", ctx.InvocationID(),
		"session_id", ctx.Session().ID(),
		"agent", pluginAgentName(ctx),
		"content", logFormatContent(msg),
	)
	return nil, nil
}

func (p *slogPlugin) beforeRun(ctx adkagent.InvocationContext) (*genai.Content, error) {
	slog.Debug("ADK invocation starting",
		"invocation_id", ctx.InvocationID(),
		"agent", pluginAgentName(ctx),
	)
	return nil, nil
}

func (p *slogPlugin) afterRun(_ adkagent.InvocationContext) {
	slog.Debug("ADK invocation completed")
}

func (p *slogPlugin) onEvent(_ adkagent.InvocationContext, event *adksession.Event) (*adksession.Event, error) {
	attrs := []any{
		"event_id", event.ID,
		"author", event.Author,
		"final", event.IsFinalResponse(),
	}
	if event.Branch != "" {
		attrs = append(attrs, "branch", event.Branch)
	}
	if event.Content != nil {
		attrs = append(attrs, "content", logFormatContent(event.Content))
	}
	if event.Actions.TransferToAgent != "" {
		attrs = append(attrs, "transfer_to", event.Actions.TransferToAgent)
	}
	if event.FinishReason != "" {
		attrs = append(attrs, "finish_reason", event.FinishReason)
	}
	slog.Debug("ADK event", attrs...)
	return nil, nil
}

func (p *slogPlugin) beforeModel(ctx adkagent.CallbackContext, req *adkmodel.LLMRequest) (*adkmodel.LLMResponse, error) {
	slog.Debug("ADK LLM request", "agent", ctx.AgentName(), "model", req.Model)
	logSystemInstruction(req)
	logToolDeclarations(req)
	logConversationContents(req)
	return nil, nil
}

func (p *slogPlugin) afterModel(ctx adkagent.CallbackContext, resp *adkmodel.LLMResponse, err error) (*adkmodel.LLMResponse, error) {
	if err != nil {
		slog.Error("ADK LLM response error", "agent", ctx.AgentName(), "error", err)
		return nil, nil
	}
	if resp == nil {
		return nil, nil
	}
	attrs := []any{"agent", ctx.AgentName()}
	if resp.ErrorCode != "" {
		attrs = append(attrs, "error_code", resp.ErrorCode, "error_message", resp.ErrorMessage)
	}
	if resp.Content != nil {
		attrs = append(attrs, "content", logFormatContent(resp.Content))
	}
	if resp.UsageMetadata != nil {
		attrs = append(attrs,
			"input_tokens", resp.UsageMetadata.PromptTokenCount,
			"output_tokens", resp.UsageMetadata.CandidatesTokenCount,
		)
	}
	slog.Debug("ADK LLM response", attrs...)
	return nil, nil
}

func (p *slogPlugin) onModelError(ctx adkagent.CallbackContext, _ *adkmodel.LLMRequest, err error) (*adkmodel.LLMResponse, error) {
	slog.Error("ADK LLM error", "agent", ctx.AgentName(), "error", err)
	return nil, nil
}

func (p *slogPlugin) beforeTool(ctx tool.Context, t tool.Tool, args map[string]any) (map[string]any, error) {
	loggedArgs := args
	if t.Name() == thoughtToolName {
		loggedArgs = redactThoughtArgs(args)
	}
	slog.Debug("ADK tool starting",
		"tool", t.Name(),
		"agent", ctx.AgentName(),
		"call_id", ctx.FunctionCallID(),
		"args", logFormatArgs(loggedArgs),
	)
	return nil, nil
}

func (p *slogPlugin) afterTool(ctx tool.Context, t tool.Tool, _ map[string]any, result map[string]any, err error) (map[string]any, error) {
	attrs := []any{"tool", t.Name(), "agent", ctx.AgentName(), "call_id", ctx.FunctionCallID()}
	if err != nil {
		attrs = append(attrs, "error", err)
	} else {
		attrs = append(attrs, "result", logFormatArgs(result))
	}
	slog.Debug("ADK tool completed", attrs...)
	return nil, nil
}

func (p *slogPlugin) onToolError(ctx tool.Context, t tool.Tool, _ map[string]any, err error) (map[string]any, error) {
	slog.Error("ADK tool error",
		"tool", t.Name(),
		"agent", ctx.AgentName(),
		"call_id", ctx.FunctionCallID(),
		"error", err,
	)
	return nil, nil
}

// --- beforeModel sub-functions (extracted to reduce cognitive complexity) ---

func logSystemInstruction(req *adkmodel.LLMRequest) {
	if req.Config == nil || req.Config.SystemInstruction == nil {
		return
	}
	si := req.Config.SystemInstruction
	slog.Debug("ADK LLM system instruction",
		"length", logContentLength(si),
		"text", logFormatContent(si),
	)
}

func logToolDeclarations(req *adkmodel.LLMRequest) {
	if req.Config == nil {
		return
	}
	for _, t := range req.Config.Tools {
		for _, fd := range t.FunctionDeclarations {
			slog.Debug("ADK LLM tool", "name", fd.Name, "description", fd.Description)
		}
		if t.GoogleSearch != nil {
			slog.Debug("ADK LLM tool", "name", "google_search")
		}
		if t.CodeExecution != nil {
			slog.Debug("ADK LLM tool", "name", "code_execution")
		}
	}
	if len(req.Tools) > 0 {
		names := make([]string, 0, len(req.Tools))
		for name := range req.Tools {
			names = append(names, name)
		}
		slog.Debug("ADK LLM tools", "count", len(req.Tools), "names", strings.Join(names, ", "))
	}
}

func logConversationContents(req *adkmodel.LLMRequest) {
	for i, c := range req.Contents {
		slog.Debug("ADK LLM content",
			"index", i,
			"role", c.Role,
			"parts", logFormatParts(c),
		)
	}
}

// --- Formatting helpers ---

func pluginAgentName(ctx adkagent.InvocationContext) string {
	if ctx.Agent() != nil {
		return ctx.Agent().Name()
	}
	return "unknown"
}

// logFormatContent returns the full text representation of a genai.Content.
func logFormatContent(c *genai.Content) string {
	if c == nil || len(c.Parts) == 0 {
		return "<empty>"
	}
	var parts []string
	for _, part := range c.Parts {
		switch {
		case part.Text != "":
			parts = append(parts, part.Text)
		case part.FunctionCall != nil:
			args, _ := json.Marshal(part.FunctionCall.Args)
			parts = append(parts, fmt.Sprintf("[call:%s id:%s args:%s]", part.FunctionCall.Name, part.FunctionCall.ID, args))
		case part.FunctionResponse != nil:
			resp, _ := json.Marshal(part.FunctionResponse.Response)
			parts = append(parts, fmt.Sprintf("[resp:%s id:%s result:%s]", part.FunctionResponse.Name, part.FunctionResponse.ID, resp))
		case part.Thought:
			parts = append(parts, "[thought]")
		case part.InlineData != nil:
			parts = append(parts, fmt.Sprintf("[inline_data:%s %d bytes]", part.InlineData.MIMEType, len(part.InlineData.Data)))
		default:
			parts = append(parts, "[other]")
		}
	}
	return strings.Join(parts, " | ")
}

// logFormatParts returns each part with its full text for content-level logging.
func logFormatParts(c *genai.Content) string {
	if c == nil || len(c.Parts) == 0 {
		return "<empty>"
	}
	var parts []string
	for _, part := range c.Parts {
		switch {
		case part.Thought && part.Text != "":
			parts = append(parts, fmt.Sprintf("thought(%d chars): %s", len(part.Text), part.Text))
		case part.Text != "":
			parts = append(parts, fmt.Sprintf("text(%d chars): %s", len(part.Text), part.Text))
		case part.FunctionCall != nil:
			args, _ := json.Marshal(part.FunctionCall.Args)
			parts = append(parts, fmt.Sprintf("call:%s(%s)", part.FunctionCall.Name, args))
		case part.FunctionResponse != nil:
			resp, _ := json.Marshal(part.FunctionResponse.Response)
			parts = append(parts, fmt.Sprintf("resp:%s(%s)", part.FunctionResponse.Name, resp))
		case part.InlineData != nil:
			parts = append(parts, fmt.Sprintf("inline_data:%s(%d bytes)", part.InlineData.MIMEType, len(part.InlineData.Data)))
		default:
			parts = append(parts, "other")
		}
	}
	return strings.Join(parts, " | ")
}

func logContentLength(c *genai.Content) int {
	if c == nil {
		return 0
	}
	n := 0
	for _, p := range c.Parts {
		n += len(p.Text)
	}
	return n
}

// redactThoughtArgs returns a shallow copy of args with the "reasoning" field
// truncated to avoid dumping full internal reasoning into debug logs.
func redactThoughtArgs(args map[string]any) map[string]any {
	const maxReasoningLog = 80
	redacted := make(map[string]any, len(args))
	for k, v := range args {
		if k == "reasoning" {
			if s, ok := v.(string); ok && len(s) > maxReasoningLog {
				redacted[k] = s[:maxReasoningLog] + "…[truncated]"
				continue
			}
		}
		redacted[k] = v
	}
	return redacted
}

func logFormatArgs(args map[string]any) string {
	if len(args) == 0 {
		return "{}"
	}
	b, err := json.Marshal(args)
	if err != nil {
		return fmt.Sprintf("%v", args)
	}
	return string(b)
}
