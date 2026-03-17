package agent

import (
	"strings"

	"google.golang.org/genai"

	"github.com/dmora/crucible/internal/config"
	"github.com/dmora/crucible/internal/message"
)

// buildGenConfig constructs a GenerateContentConfig from model metadata and call options.
func (a *sessionAgent) buildGenConfig(model Model, call SessionAgentCall) *genai.GenerateContentConfig {
	cfg := &genai.GenerateContentConfig{}

	if call.MaxOutputTokens > 0 {
		cfg.MaxOutputTokens = safeInt32(call.MaxOutputTokens)
	}
	if call.Temperature != nil {
		cfg.Temperature = genai.Ptr(float32(*call.Temperature))
	}
	if call.TopP != nil {
		cfg.TopP = genai.Ptr(float32(*call.TopP))
	}
	if call.TopK != nil {
		cfg.TopK = genai.Ptr(float32(*call.TopK))
	}

	// Configure thinking from model metadata (static defaults).
	if opts := model.Metadata.Options.ProviderOptions; opts != nil {
		if tc, ok := opts["thinking_config"].(map[string]any); ok {
			thinkCfg := &genai.ThinkingConfig{}
			if include, ok := tc["include_thoughts"].(bool); ok {
				thinkCfg.IncludeThoughts = include
			}
			// ThinkingLevel and ThinkingBudget are mutually exclusive.
			// Level takes precedence (Gemini 3.x), budget is fallback (Gemini 2.x).
			if level, ok := tc["thinking_level"].(string); ok {
				thinkCfg.ThinkingLevel = genai.ThinkingLevel(level)
			} else if budget, ok := tc["thinking_budget"]; ok {
				switch v := budget.(type) {
				case int:
					thinkCfg.ThinkingBudget = genai.Ptr(safeInt32(int64(v)))
				case float64:
					thinkCfg.ThinkingBudget = genai.Ptr(safeInt32(int64(v)))
				}
			}
			// User-selected reasoning effort overrides the static default.
			if model.ModelCfg.ReasoningEffort != "" {
				thinkCfg.ThinkingLevel = genai.ThinkingLevel(strings.ToUpper(model.ModelCfg.ReasoningEffort))
				thinkCfg.ThinkingBudget = nil // level and budget are mutually exclusive
			}
			cfg.ThinkingConfig = thinkCfg
		}
	}

	return cfg
}

// callToGenaiParts converts a SessionAgentCall's prompt and attachments into genai.Parts.
// Used by both buildUserContent (for the initial LLM call) and the mid-loop plugin
// (for injecting queued messages).
func callToGenaiParts(call SessionAgentCall) []*genai.Part {
	promptText := message.PromptWithTextAttachments(call.Prompt, call.Attachments)
	var parts []*genai.Part
	if promptText != "" {
		parts = append(parts, &genai.Part{Text: promptText})
	}

	// Add binary attachments as inline data.
	for _, att := range call.Attachments {
		if att.IsText() {
			continue
		}
		parts = append(parts, &genai.Part{
			InlineData: &genai.Blob{
				MIMEType: att.MimeType,
				Data:     att.Content,
			},
		})
	}
	return parts
}

// buildUserContent constructs a genai.Content from the call prompt and attachments.
func (a *sessionAgent) buildUserContent(call SessionAgentCall) *genai.Content {
	parts := callToGenaiParts(call)
	if len(parts) == 0 {
		return genai.NewContentFromText("", genai.RoleUser)
	}
	return &genai.Content{Role: genai.RoleUser, Parts: parts}
}

// mapFinishReason converts a genai.FinishReason to a message.FinishReason.
func mapFinishReason(fr genai.FinishReason) message.FinishReason {
	switch fr {
	case "STOP":
		return message.FinishReasonEndTurn
	case "MAX_TOKENS":
		return message.FinishReasonMaxTokens
	default:
		return message.FinishReasonUnknown
	}
}

// usageFromMetadata converts genai usage metadata to our UsageInfo.
func usageFromMetadata(m *genai.GenerateContentResponseUsageMetadata) UsageInfo {
	return UsageInfo{
		PromptTokens:     int64(m.PromptTokenCount),
		CandidatesTokens: int64(m.CandidatesTokenCount),
		TotalTokens:      int64(m.TotalTokenCount),
		ThoughtsTokens:   int64(m.ThoughtsTokenCount),
	}
}

// safeInt32 converts an int64 to int32, clamping to math.MaxInt32.
func safeInt32(v int64) int32 {
	const maxInt32 = 1<<31 - 1
	if v > maxInt32 {
		return maxInt32
	}
	return int32(v) //nolint:gosec // clamped above
}

// computeCost calculates the dollar cost for a given usage and model metadata.
func computeCost(meta config.ModelMetadata, usage UsageInfo) float64 {
	return meta.CostPer1MIn/1e6*float64(usage.PromptTokens) +
		meta.CostPer1MOut/1e6*float64(usage.CandidatesTokens)
}
