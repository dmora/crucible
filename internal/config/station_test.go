package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetupDefaultStations_UserOverrideFullyReplaces(t *testing.T) {
	cfg := &Config{
		Stations: map[string]StationConfig{
			"draft": {Backend: "codex"},
		},
	}

	cfg.SetupDefaultStations()

	draft := cfg.Stations["draft"]
	assert.Equal(t, "codex", draft.Backend, "user backend should be kept")
	assert.Empty(t, draft.Description, "user did not set Description — should remain empty (full replace)")
	assert.Empty(t, draft.Steering, "user did not set Steering — should remain empty (full replace)")
	assert.Nil(t, draft.Options, "user did not set Options — should remain nil (full replace)")

	// Other default stations should exist unchanged.
	_, hasInspect := cfg.Stations["inspect"]
	_, hasBuild := cfg.Stations["build"]
	_, hasReview := cfg.Stations["review"]
	assert.True(t, hasInspect, "inspect should be added from defaults")
	assert.True(t, hasBuild, "build should be added from defaults")
	assert.True(t, hasReview, "review should be added from defaults")
}

func TestSetupDefaultStations_NilStations(t *testing.T) {
	cfg := &Config{}

	cfg.SetupDefaultStations()

	require.NotNil(t, cfg.Stations)
	assert.Len(t, cfg.Stations, len(DefaultStations))
	for name := range DefaultStations {
		_, exists := cfg.Stations[name]
		assert.True(t, exists, "station %q should exist", name)
	}
}

func TestSetupDefaultStations_DefaultsHaveSteering(t *testing.T) {
	cfg := &Config{}
	cfg.SetupDefaultStations()

	for name, station := range cfg.Stations {
		assert.NotEmpty(t, station.Steering, "default station %q should have steering text", name)
	}
}

func TestValidateMCPNames_RejectsUnderscores(t *testing.T) {
	mcps := MCPs{
		"my_server": {Type: MCPStdio, Command: "npx"},
	}
	err := mcps.ValidateMCPNames()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "my_server")
	assert.Contains(t, err.Error(), "my-server")
}

func TestValidateMCPNames_AcceptsHyphens(t *testing.T) {
	mcps := MCPs{
		"my-server": {Type: MCPStdio, Command: "npx"},
	}
	err := mcps.ValidateMCPNames()
	assert.NoError(t, err)
}

func TestValidateMCPNames_NilMap(t *testing.T) {
	var mcps MCPs
	err := mcps.ValidateMCPNames()
	assert.NoError(t, err)
}
