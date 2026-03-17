package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmbeddedProviders(t *testing.T) {
	providers := embeddedProviders()

	require.NotEmpty(t, providers, "should have at least one provider")

	// Verify we have a Gemini provider.
	var geminiFound bool
	for _, p := range providers {
		if p.ID == "gemini" {
			geminiFound = true
			assert.Equal(t, ProviderTypeGemini, p.Type)
			assert.NotEmpty(t, p.DefaultLargeModelID, "default large model should be set")
			assert.NotEmpty(t, p.DefaultSmallModelID, "default small model should be set")

			// Verify models exist and have required fields.
			require.NotEmpty(t, p.Models, "should have at least one model")
			for _, m := range p.Models {
				assert.NotEmpty(t, m.ID, "model ID should be set")
				assert.NotEmpty(t, m.Name, "model Name should be set")
				assert.Greater(t, m.ContextWindow, int64(0), "model %s: context window should be positive", m.ID)
				assert.Greater(t, m.DefaultMaxTokens, int64(0), "model %s: default max tokens should be positive", m.ID)
			}

			// Verify default model IDs refer to actual models.
			var largeFound, smallFound bool
			for _, m := range p.Models {
				if m.ID == p.DefaultLargeModelID {
					largeFound = true
				}
				if m.ID == p.DefaultSmallModelID {
					smallFound = true
				}
			}
			assert.True(t, largeFound, "default large model %q should exist in Models", p.DefaultLargeModelID)
			assert.True(t, smallFound, "default small model %q should exist in Models", p.DefaultSmallModelID)
		}
	}
	assert.True(t, geminiFound, "should have a gemini provider")
}

func TestProviderTypeConstants(t *testing.T) {
	// Ensure provider type constants are distinct.
	types := []ProviderType{
		ProviderTypeGemini,
		ProviderTypeOpenAI,
		ProviderTypeAnthropic,
		ProviderTypeOpenAICompat,
	}
	seen := make(map[ProviderType]bool)
	for _, pt := range types {
		assert.NotEmpty(t, pt, "provider type should not be empty")
		assert.False(t, seen[pt], "duplicate provider type: %s", pt)
		seen[pt] = true
	}
}
