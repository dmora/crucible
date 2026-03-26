package agent

import (
	"strings"
	"testing"

	adkmodel "google.golang.org/adk/model"
	"google.golang.org/genai"
)

// --- PipelineBreaker unit tests ---

func TestPipelineBreaker_ConsecutiveFailuresTripsBreaker(t *testing.T) {
	b := NewPipelineBreaker(3)

	if a, _ := b.RecordVerdict(VerdictFailed); a != BreakerContinue {
		t.Fatalf("1st failure: got %d, want BreakerContinue", a)
	}
	if a, _ := b.RecordVerdict(VerdictFailed); a != BreakerContinue {
		t.Fatalf("2nd failure: got %d, want BreakerContinue", a)
	}
	if a, n := b.RecordVerdict(VerdictFailed); a != BreakerHalt {
		t.Fatalf("3rd failure: got %d, want BreakerHalt", a)
	} else if n != 3 {
		t.Fatalf("failures count = %d, want 3", n)
	}
	if !b.IsHalted() {
		t.Fatal("expected IsHalted() == true after 3 failures")
	}
	if b.Failures() != 3 {
		t.Fatalf("Failures() = %d, want 3", b.Failures())
	}
}

func TestPipelineBreaker_SuccessResetsCounter(t *testing.T) {
	b := NewPipelineBreaker(3)

	b.RecordVerdict(VerdictFailed)
	b.RecordVerdict(VerdictFailed)
	b.RecordVerdict(VerdictDone) // reset

	if b.Failures() != 0 {
		t.Fatalf("Failures() = %d after success, want 0", b.Failures())
	}
	if b.IsHalted() {
		t.Fatal("expected IsHalted() == false after success")
	}
}

func TestPipelineBreaker_MixedSequenceNoHalt(t *testing.T) {
	b := NewPipelineBreaker(3)

	b.RecordVerdict(VerdictFailed) // 1
	b.RecordVerdict(VerdictFailed) // 2
	b.RecordVerdict(VerdictDone)   // reset
	b.RecordVerdict(VerdictFailed) // 1

	if b.Failures() != 1 {
		t.Fatalf("Failures() = %d, want 1", b.Failures())
	}
	if b.IsHalted() {
		t.Fatal("should not halt after reset + 1 failure")
	}
}

func TestPipelineBreaker_IgnoresRunningAndCanceled(t *testing.T) {
	b := NewPipelineBreaker(3)

	b.RecordVerdict(VerdictFailed)   // 1
	b.RecordVerdict(VerdictRunning)  // ignored
	b.RecordVerdict(VerdictCanceled) // ignored

	if b.Failures() != 1 {
		t.Fatalf("Failures() = %d, want 1 (Running/Canceled should be ignored)", b.Failures())
	}
}

func TestPipelineBreaker_CustomThreshold(t *testing.T) {
	b := NewPipelineBreaker(1)

	if a, _ := b.RecordVerdict(VerdictFailed); a != BreakerHalt {
		t.Fatalf("threshold=1: got %d, want BreakerHalt on first failure", a)
	}
	if !b.IsHalted() {
		t.Fatal("expected IsHalted() == true")
	}
}

func TestPipelineBreaker_SuccessResetsHaltedState(t *testing.T) {
	b := NewPipelineBreaker(2)

	b.RecordVerdict(VerdictFailed)
	b.RecordVerdict(VerdictFailed) // halted
	if !b.IsHalted() {
		t.Fatal("expected halted after 2 failures")
	}

	b.RecordVerdict(VerdictDone) // reset
	if b.IsHalted() {
		t.Fatal("expected not halted after success")
	}
	if b.Failures() != 0 {
		t.Fatalf("Failures() = %d, want 0", b.Failures())
	}
}

func TestPipelineBreaker_RecordVerdictReturnsFailureCount(t *testing.T) {
	b := NewPipelineBreaker(5)

	_, n := b.RecordVerdict(VerdictFailed)
	if n != 1 {
		t.Fatalf("after 1st fail: count = %d, want 1", n)
	}
	_, n = b.RecordVerdict(VerdictFailed)
	if n != 2 {
		t.Fatalf("after 2nd fail: count = %d, want 2", n)
	}
	_, n = b.RecordVerdict(VerdictDone)
	if n != 0 {
		t.Fatalf("after success: count = %d, want 0", n)
	}
}

// --- Helper function tests ---

func TestIsAbort(t *testing.T) {
	tests := []struct {
		name   string
		output map[string]any
		want   bool
	}{
		{"nil map", nil, false},
		{"no key", map[string]any{"result": "ok"}, false},
		{"false", map[string]any{"_abort": false}, false},
		{"true", map[string]any{"_abort": true}, true},
		{"wrong type", map[string]any{"_abort": "yes"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAbort(tt.output); got != tt.want {
				t.Errorf("isAbort() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasError(t *testing.T) {
	tests := []struct {
		name   string
		output map[string]any
		want   bool
	}{
		{"nil map", nil, false},
		{"no key", map[string]any{"result": "ok"}, false},
		{"empty string", map[string]any{"error": ""}, false},
		{"non-empty", map[string]any{"error": "station crashed"}, true},
		{"wrong type", map[string]any{"error": 42}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasError(tt.output); got != tt.want {
				t.Errorf("hasError() = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- afterTool tests ---

// fakeTool is declared in steering_plugin_test.go (same package).

func newTestPlugin(stations []string, threshold int) (*circuitBreakerPlugin, *PipelineBreaker) {
	breaker := NewPipelineBreaker(threshold)
	stationSet := make(map[string]struct{}, len(stations))
	for _, s := range stations {
		stationSet[s] = struct{}{}
	}
	return &circuitBreakerPlugin{
		stations: stationSet,
		breaker:  breaker,
	}, breaker
}

func TestAfterTool_IgnoresNonStationTools(t *testing.T) {
	p, breaker := newTestPlugin([]string{"build"}, 2)

	p.afterTool(nil, fakeTool{name: "google_search"}, nil, nil, nil)

	if breaker.Failures() != 0 {
		t.Fatal("non-station tool should not affect breaker")
	}
}

func TestAfterTool_SuccessResetsBreaker(t *testing.T) {
	p, breaker := newTestPlugin([]string{"build"}, 3)

	// Record a failure first.
	p.afterTool(nil, fakeTool{name: "build"}, nil, map[string]any{"error": "fail"}, nil)
	if breaker.Failures() != 1 {
		t.Fatalf("expected 1 failure, got %d", breaker.Failures())
	}

	// Success resets.
	p.afterTool(nil, fakeTool{name: "build"}, nil, map[string]any{"result": "ok"}, nil)
	if breaker.Failures() != 0 {
		t.Fatalf("expected 0 failures after success, got %d", breaker.Failures())
	}
}

func TestAfterTool_ErrorArgTriggersFailure(t *testing.T) {
	p, breaker := newTestPlugin([]string{"build"}, 3)

	p.afterTool(nil, fakeTool{name: "build"}, nil, nil, errForTest("tool error"))

	if breaker.Failures() != 1 {
		t.Fatalf("expected 1 failure from err, got %d", breaker.Failures())
	}
}

func TestAfterTool_OutputErrorTriggersFailure(t *testing.T) {
	p, breaker := newTestPlugin([]string{"draft"}, 3)

	p.afterTool(nil, fakeTool{name: "draft"}, nil, map[string]any{"error": "station crashed"}, nil)

	if breaker.Failures() != 1 {
		t.Fatalf("expected 1 failure from output error, got %d", breaker.Failures())
	}
}

func TestAfterTool_AbortSkipsBreaker(t *testing.T) {
	p, breaker := newTestPlugin([]string{"build"}, 2)

	p.afterTool(nil, fakeTool{name: "build"}, nil, map[string]any{"_abort": true}, nil)

	if breaker.Failures() != 0 {
		t.Fatal("abort should not count as failure")
	}
}

func TestAfterTool_SetsPendingOnHalt(t *testing.T) {
	p, _ := newTestPlugin([]string{"build"}, 2)

	p.afterTool(nil, fakeTool{name: "build"}, nil, map[string]any{"error": "e1"}, nil)
	if p.pending {
		t.Fatal("should not be pending after 1 failure")
	}

	p.afterTool(nil, fakeTool{name: "build"}, nil, map[string]any{"error": "e2"}, nil)
	if !p.pending {
		t.Fatal("should be pending after 2 failures (threshold)")
	}
	if p.pendingFailures != 2 {
		t.Fatalf("pendingFailures = %d, want 2", p.pendingFailures)
	}
}

// --- beforeModel tests ---

func TestBeforeModel_NoOpWhenNotPending(t *testing.T) {
	p, _ := newTestPlugin([]string{"build"}, 3)
	req := &adkmodel.LLMRequest{}

	resp, err := p.beforeModel(nil, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != nil {
		t.Fatal("expected nil response when not pending")
	}
	if req.Config != nil {
		t.Fatal("should not modify request when not pending")
	}
}

func TestBeforeModel_InjectsSystemInstruction(t *testing.T) {
	p, _ := newTestPlugin([]string{"build"}, 2)

	// Trip the breaker.
	p.afterTool(nil, fakeTool{name: "build"}, nil, map[string]any{"error": "e1"}, nil)
	p.afterTool(nil, fakeTool{name: "build"}, nil, map[string]any{"error": "e2"}, nil)

	req := &adkmodel.LLMRequest{}
	resp, err := p.beforeModel(nil, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != nil {
		t.Fatal("expected nil response")
	}

	if req.Config == nil || req.Config.SystemInstruction == nil {
		t.Fatal("expected system instruction to be set")
	}
	parts := req.Config.SystemInstruction.Parts
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if !strings.Contains(parts[0].Text, "<circuit_breaker>") {
		t.Fatalf("expected <circuit_breaker> tag, got: %s", parts[0].Text)
	}
	if !strings.Contains(parts[0].Text, "2 consecutive station failures") {
		t.Fatalf("expected failure count 2 in message, got: %s", parts[0].Text)
	}
}

func TestBeforeModel_ClearsPendingAfterInjection(t *testing.T) {
	p, _ := newTestPlugin([]string{"build"}, 1)

	p.afterTool(nil, fakeTool{name: "build"}, nil, map[string]any{"error": "e"}, nil)

	// First call injects.
	req1 := &adkmodel.LLMRequest{}
	p.beforeModel(nil, req1)
	if p.pending {
		t.Fatal("pending should be false after injection")
	}

	// Second call is a no-op.
	req2 := &adkmodel.LLMRequest{}
	p.beforeModel(nil, req2)
	if req2.Config != nil {
		t.Fatal("second beforeModel should be a no-op")
	}
}

func TestBeforeModel_AppendsToExistingParts(t *testing.T) {
	p, _ := newTestPlugin([]string{"build"}, 1)
	p.afterTool(nil, fakeTool{name: "build"}, nil, map[string]any{"error": "e"}, nil)

	existing := &genai.Part{Text: "existing instruction"}
	req := &adkmodel.LLMRequest{
		Config: &genai.GenerateContentConfig{
			SystemInstruction: &genai.Content{
				Role:  "user",
				Parts: []*genai.Part{existing},
			},
		},
	}

	p.beforeModel(nil, req)

	parts := req.Config.SystemInstruction.Parts
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts (existing + breaker), got %d", len(parts))
	}
	if parts[0].Text != "existing instruction" {
		t.Fatal("existing part should be preserved")
	}
	if !strings.Contains(parts[1].Text, "<circuit_breaker>") {
		t.Fatal("second part should be circuit breaker injection")
	}
}

// errForTest is a simple error type for testing.
type errForTest string

func (e errForTest) Error() string { return string(e) }
