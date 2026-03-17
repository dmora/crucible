package agent

import (
	"errors"
	"testing"

	adkmodel "google.golang.org/adk/model"
	"google.golang.org/genai"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeStallResponse() *adkmodel.LLMResponse {
	return &adkmodel.LLMResponse{
		FinishReason: genai.FinishReasonStop,
		UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
			CandidatesTokenCount: 0,
		},
		Content: &genai.Content{
			Parts: []*genai.Part{{Text: "Planning...", Thought: true}},
		},
	}
}

func makeSuccessResponse(outputTokens int32) *adkmodel.LLMResponse {
	return &adkmodel.LLMResponse{
		FinishReason: genai.FinishReasonStop,
		UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
			CandidatesTokenCount: outputTokens,
		},
		Content: &genai.Content{
			Parts: []*genai.Part{{Text: "Here is the plan..."}},
		},
	}
}

func makeFunctionCallResponse() *adkmodel.LLMResponse {
	return &adkmodel.LLMResponse{
		FinishReason: genai.FinishReasonStop,
		UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
			CandidatesTokenCount: 0,
		},
		Content: &genai.Content{
			Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "draft"}}},
		},
	}
}

func TestStallRecovery_DetectStall(t *testing.T) {
	p := &stallRecoveryPlugin{state: &stallRecoveryState{}}

	_, _ = p.afterModel(nil, makeStallResponse(), nil)

	assert.Equal(t, 1, p.state.consecutiveStalls)
	assert.True(t, p.state.pendingSteering)
}

func TestStallRecovery_InjectSteering(t *testing.T) {
	p := &stallRecoveryPlugin{state: &stallRecoveryState{}}

	// Detect a stall.
	_, _ = p.afterModel(nil, makeStallResponse(), nil)

	// First beforeModel — steering injected.
	req := &adkmodel.LLMRequest{
		Config: &genai.GenerateContentConfig{
			SystemInstruction: &genai.Content{
				Role:  "user",
				Parts: []*genai.Part{{Text: "base prompt"}},
			},
		},
	}
	_, err := p.beforeModel(nil, req)
	require.NoError(t, err)

	require.Len(t, req.Config.SystemInstruction.Parts, 2)
	injected := req.Config.SystemInstruction.Parts[1].Text
	assert.Contains(t, injected, "<stall_recovery>")
	assert.Contains(t, injected, stallRecoveryText)
	assert.Contains(t, injected, "</stall_recovery>")

	// Second beforeModel — NO injection (drain consumed it).
	req2 := &adkmodel.LLMRequest{
		Config: &genai.GenerateContentConfig{
			SystemInstruction: &genai.Content{
				Role:  "user",
				Parts: []*genai.Part{{Text: "base prompt"}},
			},
		},
	}
	_, err = p.beforeModel(nil, req2)
	require.NoError(t, err)

	assert.Len(t, req2.Config.SystemInstruction.Parts, 1, "steering should not persist to second call")
}

func TestStallRecovery_ResetOnSuccess(t *testing.T) {
	p := &stallRecoveryPlugin{state: &stallRecoveryState{}}

	// Detect a stall.
	_, _ = p.afterModel(nil, makeStallResponse(), nil)
	assert.Equal(t, 1, p.state.consecutiveStalls)

	// Success resets.
	_, _ = p.afterModel(nil, makeSuccessResponse(100), nil)
	assert.Equal(t, 0, p.state.consecutiveStalls)
	assert.False(t, p.state.pendingSteering)
}

func TestStallRecovery_RetryCap(t *testing.T) {
	p := &stallRecoveryPlugin{state: &stallRecoveryState{}}

	// Stall 1 — steering pending.
	_, _ = p.afterModel(nil, makeStallResponse(), nil)
	assert.Equal(t, 1, p.state.consecutiveStalls)
	assert.True(t, p.state.pendingSteering)

	// Drain the pending steering before next stall.
	p.state.drainPending()

	// Stall 2 — steering pending.
	_, _ = p.afterModel(nil, makeStallResponse(), nil)
	assert.Equal(t, 2, p.state.consecutiveStalls)
	assert.True(t, p.state.pendingSteering)

	// Drain the pending steering before next stall.
	p.state.drainPending()

	// Stall 3 — cap exceeded, no steering.
	_, _ = p.afterModel(nil, makeStallResponse(), nil)
	assert.Equal(t, 3, p.state.consecutiveStalls)
	assert.False(t, p.state.pendingSteering)
}

func TestStallRecovery_FunctionCallExclusion(t *testing.T) {
	p := &stallRecoveryPlugin{state: &stallRecoveryState{}}

	_, _ = p.afterModel(nil, makeFunctionCallResponse(), nil)

	assert.Equal(t, 0, p.state.consecutiveStalls)
	assert.False(t, p.state.pendingSteering)
}

func TestStallRecovery_ErrorResponseExclusion(t *testing.T) {
	p := &stallRecoveryPlugin{state: &stallRecoveryState{}}

	// err != nil — should not detect stall.
	_, _ = p.afterModel(nil, makeStallResponse(), errors.New("model error"))
	assert.Equal(t, 0, p.state.consecutiveStalls)

	// ErrorCode set — should not detect stall.
	resp := makeStallResponse()
	resp.ErrorCode = "rate_limit"
	_, _ = p.afterModel(nil, resp, nil)
	assert.Equal(t, 0, p.state.consecutiveStalls)
	assert.False(t, p.state.pendingSteering)
}

func TestStallRecovery_NilUsageMetadata(t *testing.T) {
	p := &stallRecoveryPlugin{state: &stallRecoveryState{}}

	resp := &adkmodel.LLMResponse{
		FinishReason: genai.FinishReasonStop,
		Content: &genai.Content{
			Parts: []*genai.Part{{Text: "hello"}},
		},
	}
	_, _ = p.afterModel(nil, resp, nil)

	// nil UsageMetadata — should reset (treated as success), not stall.
	assert.Equal(t, 0, p.state.consecutiveStalls)
	assert.False(t, p.state.pendingSteering)
}

func TestStallRecovery_BeforeModel_NilConfig(t *testing.T) {
	p := &stallRecoveryPlugin{state: &stallRecoveryState{}}

	// Detect a stall.
	_, _ = p.afterModel(nil, makeStallResponse(), nil)

	// beforeModel with nil Config — should create it.
	req := &adkmodel.LLMRequest{}
	_, err := p.beforeModel(nil, req)
	require.NoError(t, err)

	require.NotNil(t, req.Config)
	require.NotNil(t, req.Config.SystemInstruction)
	require.Len(t, req.Config.SystemInstruction.Parts, 1)
	assert.Contains(t, req.Config.SystemInstruction.Parts[0].Text, "<stall_recovery>")
}

func TestStallRecovery_ResetClearsCounter(t *testing.T) {
	p := &stallRecoveryPlugin{state: &stallRecoveryState{}}

	// 2 stalls.
	_, _ = p.afterModel(nil, makeStallResponse(), nil)
	p.state.drainPending()
	_, _ = p.afterModel(nil, makeStallResponse(), nil)
	assert.Equal(t, 2, p.state.consecutiveStalls)

	// Success resets.
	_, _ = p.afterModel(nil, makeSuccessResponse(50), nil)
	assert.Equal(t, 0, p.state.consecutiveStalls)

	// New stall starts fresh at 1 (not 3).
	_, _ = p.afterModel(nil, makeStallResponse(), nil)
	assert.Equal(t, 1, p.state.consecutiveStalls)
	assert.True(t, p.state.pendingSteering)
}
