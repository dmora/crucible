package agent

import (
	"errors"
	"strings"
	"testing"

	adkmodel "google.golang.org/adk/model"
	"google.golang.org/genai"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- steeringStore tests ---

func TestSteeringStore_PushDrain(t *testing.T) {
	s := &steeringStore{}
	s.push("draft", "steering-a")
	s.push("build", "steering-b")

	entries := s.drain()
	require.Len(t, entries, 2)
	assert.Equal(t, "draft", entries[0].station)
	assert.Equal(t, "steering-a", entries[0].steering)
	assert.Equal(t, "build", entries[1].station)
	assert.Equal(t, "steering-b", entries[1].steering)

	// Second drain returns nil — queue was cleared.
	assert.Nil(t, s.drain())
}

func TestSteeringStore_DrainIsAtomic(t *testing.T) {
	s := &steeringStore{}
	s.push("draft", "a")
	_ = s.drain()

	// Push after drain starts fresh.
	s.push("review", "b")
	entries := s.drain()
	require.Len(t, entries, 1)
	assert.Equal(t, "review", entries[0].station)
}

// --- steeringPlugin tests ---

// fakeTool implements tool.Tool for testing.
type fakeTool struct {
	name string
}

func (f fakeTool) Name() string        { return f.name }
func (f fakeTool) Description() string { return "" }
func (f fakeTool) IsLongRunning() bool { return false }

func TestSteeringPlugin_AfterTool_StationSuccess(t *testing.T) {
	p := &steeringPlugin{
		stations: map[string]string{"draft": "do something"},
		store:    &steeringStore{},
	}
	_, err := p.afterTool(nil, fakeTool{name: "draft"}, nil, nil, nil)
	require.NoError(t, err)

	entries := p.store.drain()
	require.Len(t, entries, 1)
	assert.Equal(t, "draft", entries[0].station)
	assert.Equal(t, "do something", entries[0].steering)
}

func TestSteeringPlugin_AfterTool_StationError(t *testing.T) {
	p := &steeringPlugin{
		stations: map[string]string{"draft": "do something"},
		store:    &steeringStore{},
	}
	_, err := p.afterTool(nil, fakeTool{name: "draft"}, nil, nil, errors.New("tool failed"))
	require.Error(t, err)

	// Steering should NOT be enqueued on error.
	assert.Nil(t, p.store.drain())
}

func TestSteeringPlugin_AfterTool_NonStationTool(t *testing.T) {
	p := &steeringPlugin{
		stations: map[string]string{"draft": "do something"},
		store:    &steeringStore{},
	}
	_, err := p.afterTool(nil, fakeTool{name: "google_search"}, nil, nil, nil)
	require.NoError(t, err)

	assert.Nil(t, p.store.drain())
}

func TestSteeringPlugin_AfterTool_EmptySteering(t *testing.T) {
	p := &steeringPlugin{
		stations: map[string]string{"draft": ""},
		store:    &steeringStore{},
	}
	_, err := p.afterTool(nil, fakeTool{name: "draft"}, nil, nil, nil)
	require.NoError(t, err)

	assert.Nil(t, p.store.drain())
}

func TestSteeringPlugin_InjectOnce(t *testing.T) {
	p := &steeringPlugin{
		stations: map[string]string{"draft": "extract plan path"},
		store:    &steeringStore{},
	}
	// Simulate station tool completing.
	_, err := p.afterTool(nil, fakeTool{name: "draft"}, nil, nil, nil)
	require.NoError(t, err)

	// First beforeModel call — steering injected.
	req1 := &adkmodel.LLMRequest{
		Config: &genai.GenerateContentConfig{
			SystemInstruction: &genai.Content{
				Role:  "user",
				Parts: []*genai.Part{{Text: "base prompt"}},
			},
		},
	}
	_, err = p.beforeModel(nil, req1)
	require.NoError(t, err)

	require.Len(t, req1.Config.SystemInstruction.Parts, 2)
	injected := req1.Config.SystemInstruction.Parts[1].Text
	assert.Contains(t, injected, "<steering_reminder>")
	assert.Contains(t, injected, "[draft] extract plan path")
	assert.Contains(t, injected, "</steering_reminder>")

	// Second beforeModel call — steering NOT injected (drain consumed it).
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

func TestSteeringPlugin_BeforeModel_NilConfig(t *testing.T) {
	p := &steeringPlugin{
		stations: map[string]string{"draft": "steer"},
		store:    &steeringStore{},
	}
	p.store.push("draft", "steer")

	req := &adkmodel.LLMRequest{}
	_, err := p.beforeModel(nil, req)
	require.NoError(t, err)

	require.NotNil(t, req.Config)
	require.NotNil(t, req.Config.SystemInstruction)
	require.Len(t, req.Config.SystemInstruction.Parts, 1)
	assert.Contains(t, req.Config.SystemInstruction.Parts[0].Text, "<steering_reminder>")
}

func TestSteeringPlugin_MultipleSteerings(t *testing.T) {
	p := &steeringPlugin{
		stations: map[string]string{"draft": "steer-a", "build": "steer-b"},
		store:    &steeringStore{},
	}
	p.store.push("draft", "steer-a")
	p.store.push("build", "steer-b")

	req := &adkmodel.LLMRequest{
		Config: &genai.GenerateContentConfig{
			SystemInstruction: &genai.Content{
				Role:  "user",
				Parts: []*genai.Part{{Text: "base"}},
			},
		},
	}
	_, err := p.beforeModel(nil, req)
	require.NoError(t, err)

	require.Len(t, req.Config.SystemInstruction.Parts, 2)
	text := req.Config.SystemInstruction.Parts[1].Text
	assert.True(t, strings.Contains(text, "[draft] steer-a") && strings.Contains(text, "[build] steer-b"),
		"both steerings should appear in the reminder block")
}
