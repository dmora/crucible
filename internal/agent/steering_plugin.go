package agent

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"

	adkagent "google.golang.org/adk/agent"
	adkmodel "google.golang.org/adk/model"
	"google.golang.org/adk/plugin"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

// steeringEntry pairs a station name with its steering text.
type steeringEntry struct {
	station  string
	steering string
}

// steeringStore is a goroutine-safe queue with push/drain semantics.
// Push appends entries; drain atomically reads and clears.
type steeringStore struct {
	mu      sync.Mutex
	pending []steeringEntry
}

// push appends a steering entry to the queue.
func (s *steeringStore) push(station, steering string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = append(s.pending, steeringEntry{station: station, steering: steering})
}

// drain atomically reads and clears all pending entries.
// Returns nil if the queue is empty.
func (s *steeringStore) drain() []steeringEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pending) == 0 {
		return nil
	}
	entries := s.pending
	s.pending = nil
	return entries
}

// steeringPlugin enqueues steering reminders after station tools complete
// and injects them into the system instruction before the next model call.
type steeringPlugin struct {
	stations map[string]string // station name → steering text
	store    *steeringStore
}

// afterTool enqueues steering when a station tool completes successfully.
func (p *steeringPlugin) afterTool(_ tool.Context, t tool.Tool, _ map[string]any, _ map[string]any, err error) (map[string]any, error) {
	if err != nil {
		return nil, err //nolint:wrapcheck // propagate tool error as-is through plugin chain
	}
	steering, ok := p.stations[t.Name()]
	if !ok || steering == "" {
		return nil, nil
	}
	p.store.push(t.Name(), steering)
	slog.Debug("Steering enqueued", "station", t.Name())
	return nil, nil
}

// beforeModel drains pending steering entries and appends them to the
// system instruction as a <steering_reminder> XML block.
func (p *steeringPlugin) beforeModel(_ adkagent.CallbackContext, req *adkmodel.LLMRequest) (*adkmodel.LLMResponse, error) {
	entries := p.store.drain()
	if len(entries) == 0 {
		return nil, nil
	}

	var b strings.Builder
	b.WriteString("\n<steering_reminder>\n")
	for _, e := range entries {
		fmt.Fprintf(&b, "[%s] %s\n", e.station, e.steering)
	}
	b.WriteString("</steering_reminder>")

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
		&genai.Part{Text: b.String()},
	)

	slog.Debug("Steering injected",
		"count", len(entries),
		"text", b.String(),
	)
	return nil, nil
}

// newSteeringPlugin creates an ADK plugin that injects ephemeral steering
// reminders into the supervisor's system instruction after station tools complete.
// The stations map keys are station tool names; values are steering text.
func newSteeringPlugin(stations map[string]string) *plugin.Plugin {
	p := &steeringPlugin{
		stations: stations,
		store:    &steeringStore{},
	}
	plug, _ := plugin.New(plugin.Config{
		Name:                "crucible_steering",
		AfterToolCallback:   p.afterTool,
		BeforeModelCallback: p.beforeModel,
	})
	return plug
}
