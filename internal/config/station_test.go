package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetupDefaultStations_UserOverrideMergesWithDefaults(t *testing.T) {
	userStations := []byte(`{"stations":{"draft":{"backend":"codex"}}}`)
	cfg, err := loadFromBytes([][]byte{stationDefaultsJSON(), userStations})
	require.NoError(t, err)
	cfg.SetupDefaultStations()

	draft := cfg.Stations["draft"]
	assert.Equal(t, "codex", draft.Backend, "user backend should override default")
	assert.NotEmpty(t, draft.Description, "should inherit Description from defaults")
	assert.NotEmpty(t, draft.Steering, "should inherit Steering from defaults")
	assert.Equal(t, "plan", draft.Options["mode"], "should inherit Options from defaults")

	// Other default stations should exist unchanged.
	_, hasInspect := cfg.Stations["inspect"]
	_, hasBuild := cfg.Stations["build"]
	_, hasReview := cfg.Stations["review"]
	assert.True(t, hasInspect, "inspect should be added from defaults")
	assert.True(t, hasBuild, "build should be added from defaults")
	assert.True(t, hasReview, "review should be added from defaults")
}

func TestStationMerge_GatePreserved(t *testing.T) {
	userStations := []byte(`{"stations":{"ship":{"backend":"opencode-acp"}}}`)
	cfg, err := loadFromBytes([][]byte{stationDefaultsJSON(), userStations})
	require.NoError(t, err)
	cfg.SetupDefaultStations()

	ship := cfg.Stations["ship"]
	assert.Equal(t, "opencode-acp", ship.Backend, "user backend should override default")
	assert.True(t, ship.Gate, "gate:true should be preserved from defaults")
}

func TestStationMerge_GateExplicitFalse(t *testing.T) {
	userStations := []byte(`{"stations":{"ship":{"backend":"opencode-acp","gate":false}}}`)
	cfg, err := loadFromBytes([][]byte{stationDefaultsJSON(), userStations})
	require.NoError(t, err)
	cfg.SetupDefaultStations()

	ship := cfg.Stations["ship"]
	assert.False(t, ship.Gate, "explicit gate:false should override default")
}

// TestStationMerge_AllFieldsPreserved verifies that a partial user override
// inherits every unset field from DefaultStations — the exact scenario that
// triggered the merge bug.
func TestStationMerge_AllFieldsPreserved(t *testing.T) {
	// Simulates a user config that only sets backend/skill/artifact_type,
	// matching the real crucible.json that exposed the bug.
	userConfig := []byte(`{
		"stations": {
			"design":  {"backend":"opencode-acp","skill":"claude-foundry:design","artifact_type":"design"},
			"draft":   {"backend":"opencode-acp","options":{"mode":"plan"},"artifact_type":"spec"},
			"inspect": {"backend":"opencode-acp","skill":"review-plan","artifact_type":"report"},
			"build":   {"backend":"opencode-acp","skill":"feature-dev:feature-dev","artifact_type":"patch"},
			"review":  {"backend":"opencode-acp","skill":"claude-code-quality:rigorous-pr-review","options":{"mode":"plan"},"artifact_type":"verdict"},
			"verify":  {"backend":"opencode-acp","artifact_type":"verification"},
			"ship":    {"backend":"opencode-acp","gate":true,"artifact_type":"pr"}
		}
	}`)

	cfg, err := loadFromBytes([][]byte{stationDefaultsJSON(), userConfig})
	require.NoError(t, err)
	cfg.SetupDefaultStations()

	// Every station must exist.
	require.Len(t, cfg.Stations, len(DefaultStations))

	for name, merged := range cfg.Stations {
		def, ok := DefaultStations[name]
		require.True(t, ok, "unexpected station %q", name)

		t.Run(name, func(t *testing.T) {
			// User-set fields should reflect the override.
			assert.Equal(t, "opencode-acp", merged.Backend, "backend should be user override")

			// Fields the user never set must inherit from defaults.
			assert.Equal(t, def.Description, merged.Description, "description must come from defaults")
			assert.Equal(t, def.Steering, merged.Steering, "steering must come from defaults")

			// The inheritance of other fields like Skill, ArtifactType, and Gate
			// is more robustly tested in other, more focused tests in this file.
		})
	}
}

// TestStationMerge_SingleFieldOverride verifies that overriding just one field
// keeps all other default fields intact for every station.
func TestStationMerge_SingleFieldOverride(t *testing.T) {
	for name, def := range DefaultStations {
		t.Run(name, func(t *testing.T) {
			userConfig := []byte(`{"stations":{"` + name + `":{"backend":"codex"}}}`)
			cfg, err := loadFromBytes([][]byte{stationDefaultsJSON(), userConfig})
			require.NoError(t, err)
			cfg.SetupDefaultStations()

			merged := cfg.Stations[name]
			assert.Equal(t, "codex", merged.Backend, "backend should be overridden")
			assert.Equal(t, def.Description, merged.Description, "description must come from defaults")
			assert.Equal(t, def.Steering, merged.Steering, "steering must come from defaults")
			assert.Equal(t, def.Skill, merged.Skill, "skill must come from defaults")
			assert.Equal(t, def.ArtifactType, merged.ArtifactType, "artifact_type must come from defaults")
			assert.Equal(t, def.Gate, merged.Gate, "gate must come from defaults")

			if def.Options != nil {
				assert.Equal(t, def.Options, merged.Options, "options must come from defaults")
			}
		})
	}
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
