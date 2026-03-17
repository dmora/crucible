package agent

import (
	"log/slog"
	"sync"

	adkagent "google.golang.org/adk/agent"
	adkmodel "google.golang.org/adk/model"
	"google.golang.org/adk/plugin"
	"google.golang.org/genai"
)

const (
	maxConsecutiveStalls = 2
	stallRecoveryText    = "Your last turn produced reasoning but no response was delivered.\nResume from where you left off and respond. The user can't see your reasoning."
)

// stallRecoveryState tracks consecutive deliberation stalls with mutex-guarded counters.
type stallRecoveryState struct {
	mu                sync.Mutex
	consecutiveStalls int
	pendingSteering   bool
}

func (s *stallRecoveryState) recordStall() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.consecutiveStalls++
	if s.consecutiveStalls <= maxConsecutiveStalls {
		s.pendingSteering = true
		slog.Warn("Deliberation stall detected",
			"consecutive", s.consecutiveStalls,
			"max", maxConsecutiveStalls)
	} else {
		s.pendingSteering = false
		slog.Warn("Deliberation stall cap reached — no further recovery",
			"consecutive", s.consecutiveStalls)
	}
}

func (s *stallRecoveryState) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.consecutiveStalls = 0
	s.pendingSteering = false
}

// drainPending atomically reads and clears the pending flag.
func (s *stallRecoveryState) drainPending() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.pendingSteering {
		return false
	}
	s.pendingSteering = false
	return true
}

// stallRecoveryPlugin detects deliberation stalls (thinking-only output with
// zero candidate tokens) and injects corrective steering text.
type stallRecoveryPlugin struct {
	state *stallRecoveryState
}

// afterModel detects stall conditions after each model response.
func (p *stallRecoveryPlugin) afterModel(
	_ adkagent.CallbackContext,
	resp *adkmodel.LLMResponse,
	err error,
) (*adkmodel.LLMResponse, error) {
	if err != nil {
		return nil, err //nolint:wrapcheck // propagate model error as-is through plugin chain
	}
	if resp == nil {
		return nil, nil
	}
	if resp.ErrorCode != "" {
		return nil, nil
	}
	if hasFunctionCalls(resp.Content) {
		p.state.reset()
		return nil, nil
	}
	if resp.UsageMetadata != nil &&
		resp.UsageMetadata.CandidatesTokenCount == 0 &&
		resp.FinishReason == genai.FinishReasonStop {
		p.state.recordStall()
		return nil, nil
	}
	p.state.reset()
	return nil, nil
}

// hasFunctionCalls returns true if the content contains any function call parts.
func hasFunctionCalls(content *genai.Content) bool {
	if content == nil {
		return false
	}
	for _, part := range content.Parts {
		if part.FunctionCall != nil {
			return true
		}
	}
	return false
}

// beforeModel injects stall recovery steering into the system instruction.
func (p *stallRecoveryPlugin) beforeModel(
	_ adkagent.CallbackContext,
	req *adkmodel.LLMRequest,
) (*adkmodel.LLMResponse, error) {
	if !p.state.drainPending() {
		return nil, nil
	}

	text := "\n<stall_recovery>\n" + stallRecoveryText + "\n</stall_recovery>"

	if req.Config == nil {
		req.Config = &genai.GenerateContentConfig{}
	}
	if req.Config.SystemInstruction == nil {
		req.Config.SystemInstruction = &genai.Content{
			Role:  "user",
			Parts: []*genai.Part{},
		}
	}
	req.Config.SystemInstruction.Parts = append(
		req.Config.SystemInstruction.Parts,
		&genai.Part{Text: text},
	)

	slog.Info("Stall recovery steering injected")
	return nil, nil
}

// newStallRecoveryPlugin creates an ADK plugin that detects deliberation stalls
// and injects corrective steering text.
func newStallRecoveryPlugin() *plugin.Plugin {
	p := &stallRecoveryPlugin{
		state: &stallRecoveryState{},
	}
	plug, _ := plugin.New(plugin.Config{
		Name:                "crucible_stall_recovery",
		AfterModelCallback:  p.afterModel,
		BeforeModelCallback: p.beforeModel,
	})
	return plug
}
