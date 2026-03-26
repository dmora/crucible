package agent

import (
	"fmt"
	"log/slog"
	"sync"

	adkagent "google.golang.org/adk/agent"
	adkmodel "google.golang.org/adk/model"
	"google.golang.org/adk/plugin"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

// defaultPipelineBreakerThreshold is the number of consecutive station
// failures before the breaker halts the pipeline.
const defaultPipelineBreakerThreshold = 3

// BreakerAction is the result of recording a verdict.
type BreakerAction int

const (
	BreakerContinue BreakerAction = iota
	BreakerHalt
)

// PipelineBreaker tracks consecutive station failures across a pipeline.
// When maxConsecutive failures occur in a row, it signals a halt so the
// supervisor can ask the operator what to do.
type PipelineBreaker struct {
	mu                  sync.Mutex
	consecutiveFailures int
	maxConsecutive      int
	halted              bool
}

// NewPipelineBreaker creates a breaker that halts after maxConsecutive failures.
func NewPipelineBreaker(maxConsecutive int) *PipelineBreaker {
	return &PipelineBreaker{maxConsecutive: maxConsecutive}
}

// RecordVerdict updates the breaker based on a station outcome and returns the
// action plus the current failure count, captured atomically in the same
// critical section to avoid TOCTOU races.
// VerdictDone resets the counter. VerdictFailed increments it.
// Other verdicts (Running, Canceled) are ignored.
func (b *PipelineBreaker) RecordVerdict(verdict DispatchVerdict) (BreakerAction, int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch verdict {
	case VerdictDone:
		b.consecutiveFailures = 0
		b.halted = false
		return BreakerContinue, 0
	case VerdictFailed:
		b.consecutiveFailures++
		if b.consecutiveFailures >= b.maxConsecutive {
			b.halted = true
			return BreakerHalt, b.consecutiveFailures
		}
		return BreakerContinue, b.consecutiveFailures
	default:
		return BreakerContinue, b.consecutiveFailures
	}
}

// Failures returns the current consecutive failure count.
func (b *PipelineBreaker) Failures() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.consecutiveFailures
}

// IsHalted reports whether the breaker has tripped.
func (b *PipelineBreaker) IsHalted() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.halted
}

// circuitBreakerPlugin monitors station tool outcomes and injects a halt
// message into the system instruction when the pipeline breaker trips.
type circuitBreakerPlugin struct {
	stations        map[string]struct{} // station tool names
	breaker         *PipelineBreaker
	mu              sync.Mutex
	pending         bool // true when halt message needs injection
	pendingFailures int  // failure count snapshot at time of halt
}

// afterTool records station verdicts. Non-station tools are ignored.
func (p *circuitBreakerPlugin) afterTool(_ tool.Context, t tool.Tool, _ map[string]any, output map[string]any, err error) (map[string]any, error) {
	if _, ok := p.stations[t.Name()]; !ok {
		return nil, nil
	}

	var verdict DispatchVerdict
	switch {
	case err != nil:
		verdict = VerdictFailed
	case isAbort(output):
		// Gate denial — not a real failure, skip.
		return nil, nil
	case hasError(output):
		verdict = VerdictFailed
	default:
		verdict = VerdictDone
	}

	action, failures := p.breaker.RecordVerdict(verdict)
	slog.Debug("Circuit breaker recorded verdict",
		"station", t.Name(),
		"verdict", verdict,
		"failures", failures,
		"action", action,
	)

	if action == BreakerHalt {
		p.mu.Lock()
		p.pending = true
		p.pendingFailures = failures
		p.mu.Unlock()
	}
	return nil, nil
}

// beforeModel injects a <circuit_breaker> system instruction when the
// breaker has tripped, telling the supervisor to ask the operator.
func (p *circuitBreakerPlugin) beforeModel(_ adkagent.CallbackContext, req *adkmodel.LLMRequest) (*adkmodel.LLMResponse, error) {
	p.mu.Lock()
	if !p.pending {
		p.mu.Unlock()
		return nil, nil
	}
	p.pending = false
	failures := p.pendingFailures
	p.mu.Unlock()

	msg := fmt.Sprintf(
		"\n<circuit_breaker>Pipeline halted: %d consecutive station failures. Ask the operator what to do.</circuit_breaker>",
		failures,
	)

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
		&genai.Part{Text: msg},
	)

	slog.Info("Circuit breaker tripped", "failures", failures)
	return nil, nil
}

// newCircuitBreakerPlugin creates an ADK plugin that halts the pipeline
// after consecutive station failures.
func newCircuitBreakerPlugin(stationNames []string, breaker *PipelineBreaker) (*plugin.Plugin, error) {
	stations := make(map[string]struct{}, len(stationNames))
	for _, name := range stationNames {
		stations[name] = struct{}{}
	}
	p := &circuitBreakerPlugin{
		stations: stations,
		breaker:  breaker,
	}
	return plugin.New(plugin.Config{
		Name:                "crucible_circuit_breaker",
		AfterToolCallback:   p.afterTool,
		BeforeModelCallback: p.beforeModel,
	})
}

// isAbort checks if the tool output has _abort set to true (gate denial).
func isAbort(output map[string]any) bool {
	if output == nil {
		return false
	}
	v, ok := output["_abort"]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

// hasError checks if the tool output has a non-empty error field.
func hasError(output map[string]any) bool {
	if output == nil {
		return false
	}
	v, ok := output["error"]
	if !ok {
		return false
	}
	s, ok := v.(string)
	return ok && s != ""
}
