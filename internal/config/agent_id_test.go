package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig_AgentRegistry(t *testing.T) {
	cfg := &Config{
		Options: &Options{},
	}
	cfg.SetupAgents()

	t.Run("Crucible agent should exist", func(t *testing.T) {
		agent, ok := cfg.Agents[AgentCrucible]
		require.True(t, ok)
		assert.Equal(t, "Crucible", agent.Name)
		assert.NotEmpty(t, agent.Description)
		assert.Equal(t, SelectedModelTypeLarge, agent.Model)
	})
}
