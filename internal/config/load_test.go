package config

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/dmora/crucible/internal/csync"
	"github.com/dmora/crucible/internal/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	exitVal := m.Run()
	os.Exit(exitVal)
}

func TestConfig_LoadFromBytes(t *testing.T) {
	data1 := []byte(`{"providers": {"openai": {"api_key": "key1", "base_url": "https://api.openai.com/v1"}}}`)
	data2 := []byte(`{"providers": {"openai": {"api_key": "key2", "base_url": "https://api.openai.com/v2"}}}`)
	data3 := []byte(`{"providers": {"openai": {}}}`)

	loadedConfig, err := loadFromBytes([][]byte{data1, data2, data3})

	require.NoError(t, err)
	require.NotNil(t, loadedConfig)
	require.Equal(t, 1, loadedConfig.Providers.Len())
	pc, _ := loadedConfig.Providers.Get("openai")
	require.Equal(t, "key2", pc.APIKey)
	require.Equal(t, "https://api.openai.com/v2", pc.BaseURL)
}

func TestConfig_setDefaults(t *testing.T) {
	cfg := &Config{}

	cfg.setDefaults("/tmp", "")

	require.NotNil(t, cfg.Options)
	require.NotNil(t, cfg.Options.TUI)
	require.Nil(t, cfg.Options.ContextPaths)
	require.NotNil(t, cfg.Providers)
	require.NotNil(t, cfg.Models)
	require.NotNil(t, cfg.LSP)
	require.NotNil(t, cfg.MCP)
	require.Equal(t, filepath.Join("/tmp", ".crucible"), cfg.Options.DataDirectory)
	require.Equal(t, "CLAUDE.md", cfg.Options.InitializeAs)
	require.Equal(t, "/tmp", cfg.workingDir)
}

func TestConfig_configureProviders(t *testing.T) {
	knownProviders := []ProviderMetadata{
		{
			ID:          "openai",
			APIKey:      "$OPENAI_API_KEY",
			APIEndpoint: "https://api.openai.com/v1",
			Models: []ModelMetadata{{
				ID: "test-model",
			}},
		},
	}

	cfg := &Config{}
	cfg.setDefaults("/tmp", "")
	env := env.NewFromMap(map[string]string{
		"OPENAI_API_KEY": "test-key",
	})
	resolver := NewEnvironmentVariableResolver(env)
	err := cfg.configureProviders(env, resolver, knownProviders)
	require.NoError(t, err)
	require.Equal(t, 1, cfg.Providers.Len())

	pc, _ := cfg.Providers.Get("openai")
	require.Equal(t, "test-key", pc.APIKey, "APIKey should hold the resolved value")
	require.Equal(t, "$OPENAI_API_KEY", pc.APIKeyTemplate, "APIKeyTemplate preserves the original template")
}

func TestConfig_configureProvidersWithOverride(t *testing.T) {
	knownProviders := []ProviderMetadata{
		{
			ID:          "openai",
			APIKey:      "$OPENAI_API_KEY",
			APIEndpoint: "https://api.openai.com/v1",
			Models: []ModelMetadata{{
				ID: "test-model",
			}},
		},
	}

	cfg := &Config{
		Providers: csync.NewMap[string, ProviderConfig](),
	}
	cfg.Providers.Set("openai", ProviderConfig{
		APIKey:  "xyz",
		BaseURL: "https://api.openai.com/v2",
		Models: []ModelMetadata{
			{
				ID:   "test-model",
				Name: "Updated",
			},
			{
				ID: "another-model",
			},
		},
	})
	cfg.setDefaults("/tmp", "")

	env := env.NewFromMap(map[string]string{
		"OPENAI_API_KEY": "test-key",
	})
	resolver := NewEnvironmentVariableResolver(env)
	err := cfg.configureProviders(env, resolver, knownProviders)
	require.NoError(t, err)
	require.Equal(t, 1, cfg.Providers.Len())

	// We want to make sure that we keep the configured API key as a placeholder
	pc, _ := cfg.Providers.Get("openai")
	require.Equal(t, "xyz", pc.APIKey)
	require.Equal(t, "https://api.openai.com/v2", pc.BaseURL)
	require.Len(t, pc.Models, 2)
	require.Equal(t, "Updated", pc.Models[0].Name)
}

func TestConfig_configureProvidersWithNewProvider(t *testing.T) {
	knownProviders := []ProviderMetadata{
		{
			ID:          "openai",
			APIKey:      "$OPENAI_API_KEY",
			APIEndpoint: "https://api.openai.com/v1",
			Models: []ModelMetadata{{
				ID: "test-model",
			}},
		},
	}

	cfg := &Config{
		Providers: csync.NewMapFrom(map[string]ProviderConfig{
			"custom": {
				APIKey:  "xyz",
				BaseURL: "https://api.someendpoint.com/v2",
				Models: []ModelMetadata{
					{
						ID: "test-model",
					},
				},
			},
		}),
	}
	cfg.setDefaults("/tmp", "")
	env := env.NewFromMap(map[string]string{
		"OPENAI_API_KEY": "test-key",
	})
	resolver := NewEnvironmentVariableResolver(env)
	err := cfg.configureProviders(env, resolver, knownProviders)
	require.NoError(t, err)
	// Should be to because of the env variable
	require.Equal(t, cfg.Providers.Len(), 2)

	// We want to make sure that we keep the configured API key as a placeholder
	pc, _ := cfg.Providers.Get("custom")
	require.Equal(t, "xyz", pc.APIKey)
	// Make sure we set the ID correctly
	require.Equal(t, "custom", pc.ID)
	require.Equal(t, "https://api.someendpoint.com/v2", pc.BaseURL)
	require.Len(t, pc.Models, 1)

	_, ok := cfg.Providers.Get("openai")
	require.True(t, ok, "OpenAI provider should still be present")
}

func TestConfig_configureProvidersSetProviderID(t *testing.T) {
	knownProviders := []ProviderMetadata{
		{
			ID:          "openai",
			APIKey:      "$OPENAI_API_KEY",
			APIEndpoint: "https://api.openai.com/v1",
			Models: []ModelMetadata{{
				ID: "test-model",
			}},
		},
	}

	cfg := &Config{}
	cfg.setDefaults("/tmp", "")
	env := env.NewFromMap(map[string]string{
		"OPENAI_API_KEY": "test-key",
	})
	resolver := NewEnvironmentVariableResolver(env)
	err := cfg.configureProviders(env, resolver, knownProviders)
	require.NoError(t, err)
	require.Equal(t, cfg.Providers.Len(), 1)

	// Provider ID should be set
	pc, _ := cfg.Providers.Get("openai")
	require.Equal(t, "openai", pc.ID)
}

func TestConfig_EnabledProviders(t *testing.T) {
	t.Run("all providers enabled", func(t *testing.T) {
		cfg := &Config{
			Providers: csync.NewMapFrom(map[string]ProviderConfig{
				"openai": {
					ID:      "openai",
					APIKey:  "key1",
					Disable: false,
				},
				"anthropic": {
					ID:      "anthropic",
					APIKey:  "key2",
					Disable: false,
				},
			}),
		}

		enabled := cfg.EnabledProviders()
		require.Len(t, enabled, 2)
	})

	t.Run("some providers disabled", func(t *testing.T) {
		cfg := &Config{
			Providers: csync.NewMapFrom(map[string]ProviderConfig{
				"openai": {
					ID:      "openai",
					APIKey:  "key1",
					Disable: false,
				},
				"anthropic": {
					ID:      "anthropic",
					APIKey:  "key2",
					Disable: true,
				},
			}),
		}

		enabled := cfg.EnabledProviders()
		require.Len(t, enabled, 1)
		require.Equal(t, "openai", enabled[0].ID)
	})

	t.Run("empty providers map", func(t *testing.T) {
		cfg := &Config{
			Providers: csync.NewMap[string, ProviderConfig](),
		}

		enabled := cfg.EnabledProviders()
		require.Len(t, enabled, 0)
	})
}

func TestConfig_IsConfigured(t *testing.T) {
	t.Run("returns true when at least one provider is enabled", func(t *testing.T) {
		cfg := &Config{
			Providers: csync.NewMapFrom(map[string]ProviderConfig{
				"openai": {
					ID:      "openai",
					APIKey:  "key1",
					Disable: false,
				},
			}),
		}

		require.True(t, cfg.IsConfigured())
	})

	t.Run("returns false when no providers are configured", func(t *testing.T) {
		cfg := &Config{
			Providers: csync.NewMap[string, ProviderConfig](),
		}

		require.False(t, cfg.IsConfigured())
	})

	t.Run("returns false when all providers are disabled", func(t *testing.T) {
		cfg := &Config{
			Providers: csync.NewMapFrom(map[string]ProviderConfig{
				"openai": {
					ID:      "openai",
					APIKey:  "key1",
					Disable: true,
				},
				"anthropic": {
					ID:      "anthropic",
					APIKey:  "key2",
					Disable: true,
				},
			}),
		}

		require.False(t, cfg.IsConfigured())
	})
}

func TestConfig_setupAgents(t *testing.T) {
	cfg := &Config{
		Options: &Options{},
	}

	cfg.SetupAgents()
	crucibleAgent, ok := cfg.Agents[AgentCrucible]
	require.True(t, ok)
	assert.Equal(t, "Crucible", crucibleAgent.Name)
	assert.Equal(t, SelectedModelTypeLarge, crucibleAgent.Model)
}

func TestConfig_configureProvidersWithDisabledProvider(t *testing.T) {
	knownProviders := []ProviderMetadata{
		{
			ID:          "openai",
			APIKey:      "$OPENAI_API_KEY",
			APIEndpoint: "https://api.openai.com/v1",
			Models: []ModelMetadata{{
				ID: "test-model",
			}},
		},
	}

	cfg := &Config{
		Providers: csync.NewMapFrom(map[string]ProviderConfig{
			"openai": {
				Disable: true,
			},
		}),
	}
	cfg.setDefaults("/tmp", "")

	env := env.NewFromMap(map[string]string{
		"OPENAI_API_KEY": "test-key",
	})
	resolver := NewEnvironmentVariableResolver(env)
	err := cfg.configureProviders(env, resolver, knownProviders)
	require.NoError(t, err)

	require.Equal(t, cfg.Providers.Len(), 1)
	prov, exists := cfg.Providers.Get("openai")
	require.True(t, exists)
	require.True(t, prov.Disable)
}

func TestConfig_configureProvidersCustomProviderValidation(t *testing.T) {
	t.Run("custom provider with missing API key is allowed, but not known providers", func(t *testing.T) {
		cfg := &Config{
			Providers: csync.NewMapFrom(map[string]ProviderConfig{
				"custom": {
					BaseURL: "https://api.custom.com/v1",
					Models: []ModelMetadata{{
						ID: "test-model",
					}},
				},
				"openai": {
					APIKey: "$MISSING",
				},
			}),
		}
		cfg.setDefaults("/tmp", "")

		env := env.NewFromMap(map[string]string{})
		resolver := NewEnvironmentVariableResolver(env)
		err := cfg.configureProviders(env, resolver, []ProviderMetadata{})
		require.NoError(t, err)

		require.Equal(t, cfg.Providers.Len(), 1)
		_, exists := cfg.Providers.Get("custom")
		require.True(t, exists)
	})

	t.Run("custom provider with missing BaseURL is removed", func(t *testing.T) {
		cfg := &Config{
			Providers: csync.NewMapFrom(map[string]ProviderConfig{
				"custom": {
					APIKey: "test-key",
					Models: []ModelMetadata{{
						ID: "test-model",
					}},
				},
			}),
		}
		cfg.setDefaults("/tmp", "")

		env := env.NewFromMap(map[string]string{})
		resolver := NewEnvironmentVariableResolver(env)
		err := cfg.configureProviders(env, resolver, []ProviderMetadata{})
		require.NoError(t, err)

		require.Equal(t, cfg.Providers.Len(), 0)
		_, exists := cfg.Providers.Get("custom")
		require.False(t, exists)
	})

	t.Run("custom provider with no models is removed", func(t *testing.T) {
		cfg := &Config{
			Providers: csync.NewMapFrom(map[string]ProviderConfig{
				"custom": {
					APIKey:  "test-key",
					BaseURL: "https://api.custom.com/v1",
					Models:  []ModelMetadata{},
				},
			}),
		}
		cfg.setDefaults("/tmp", "")

		env := env.NewFromMap(map[string]string{})
		resolver := NewEnvironmentVariableResolver(env)
		err := cfg.configureProviders(env, resolver, []ProviderMetadata{})
		require.NoError(t, err)

		require.Equal(t, cfg.Providers.Len(), 0)
		_, exists := cfg.Providers.Get("custom")
		require.False(t, exists)
	})

	t.Run("custom provider with unsupported type is removed", func(t *testing.T) {
		cfg := &Config{
			Providers: csync.NewMapFrom(map[string]ProviderConfig{
				"custom": {
					APIKey:  "test-key",
					BaseURL: "https://api.custom.com/v1",
					Type:    "unsupported",
					Models: []ModelMetadata{{
						ID: "test-model",
					}},
				},
			}),
		}
		cfg.setDefaults("/tmp", "")

		env := env.NewFromMap(map[string]string{})
		resolver := NewEnvironmentVariableResolver(env)
		err := cfg.configureProviders(env, resolver, []ProviderMetadata{})
		require.NoError(t, err)

		require.Equal(t, cfg.Providers.Len(), 0)
		_, exists := cfg.Providers.Get("custom")
		require.False(t, exists)
	})

	t.Run("valid custom provider is kept and ID is set", func(t *testing.T) {
		cfg := &Config{
			Providers: csync.NewMapFrom(map[string]ProviderConfig{
				"custom": {
					APIKey:  "test-key",
					BaseURL: "https://api.custom.com/v1",
					Type:    ProviderTypeOpenAI,
					Models: []ModelMetadata{{
						ID: "test-model",
					}},
				},
			}),
		}
		cfg.setDefaults("/tmp", "")

		env := env.NewFromMap(map[string]string{})
		resolver := NewEnvironmentVariableResolver(env)
		err := cfg.configureProviders(env, resolver, []ProviderMetadata{})
		require.NoError(t, err)

		require.Equal(t, cfg.Providers.Len(), 1)
		customProvider, exists := cfg.Providers.Get("custom")
		require.True(t, exists)
		require.Equal(t, "custom", customProvider.ID)
		require.Equal(t, "test-key", customProvider.APIKey)
		require.Equal(t, "https://api.custom.com/v1", customProvider.BaseURL)
	})

	t.Run("custom anthropic provider is supported", func(t *testing.T) {
		cfg := &Config{
			Providers: csync.NewMapFrom(map[string]ProviderConfig{
				"custom-anthropic": {
					APIKey:  "test-key",
					BaseURL: "https://api.anthropic.com/v1",
					Type:    ProviderTypeAnthropic,
					Models: []ModelMetadata{{
						ID: "claude-3-sonnet",
					}},
				},
			}),
		}
		cfg.setDefaults("/tmp", "")

		env := env.NewFromMap(map[string]string{})
		resolver := NewEnvironmentVariableResolver(env)
		err := cfg.configureProviders(env, resolver, []ProviderMetadata{})
		require.NoError(t, err)

		require.Equal(t, cfg.Providers.Len(), 1)
		customProvider, exists := cfg.Providers.Get("custom-anthropic")
		require.True(t, exists)
		require.Equal(t, "custom-anthropic", customProvider.ID)
		require.Equal(t, "test-key", customProvider.APIKey)
		require.Equal(t, "https://api.anthropic.com/v1", customProvider.BaseURL)
		require.Equal(t, ProviderTypeAnthropic, customProvider.Type)
	})

	t.Run("disabled custom provider is removed", func(t *testing.T) {
		cfg := &Config{
			Providers: csync.NewMapFrom(map[string]ProviderConfig{
				"custom": {
					APIKey:  "test-key",
					BaseURL: "https://api.custom.com/v1",
					Type:    ProviderTypeOpenAI,
					Disable: true,
					Models: []ModelMetadata{{
						ID: "test-model",
					}},
				},
			}),
		}
		cfg.setDefaults("/tmp", "")

		env := env.NewFromMap(map[string]string{})
		resolver := NewEnvironmentVariableResolver(env)
		err := cfg.configureProviders(env, resolver, []ProviderMetadata{})
		require.NoError(t, err)

		require.Equal(t, cfg.Providers.Len(), 0)
		_, exists := cfg.Providers.Get("custom")
		require.False(t, exists)
	})
}

func TestConfig_configureProvidersEnhancedCredentialValidation(t *testing.T) {
	t.Run("provider removed when API key missing with existing config", func(t *testing.T) {
		knownProviders := []ProviderMetadata{
			{
				ID:          "openai",
				APIKey:      "$MISSING_API_KEY",
				APIEndpoint: "https://api.openai.com/v1",
				Models: []ModelMetadata{{
					ID: "test-model",
				}},
			},
		}

		cfg := &Config{
			Providers: csync.NewMapFrom(map[string]ProviderConfig{
				"openai": {
					BaseURL: "custom-url",
				},
			}),
		}
		cfg.setDefaults("/tmp", "")

		env := env.NewFromMap(map[string]string{})
		resolver := NewEnvironmentVariableResolver(env)
		err := cfg.configureProviders(env, resolver, knownProviders)
		require.NoError(t, err)

		require.Equal(t, cfg.Providers.Len(), 0)
		_, exists := cfg.Providers.Get("openai")
		require.False(t, exists)
	})

	t.Run("known provider should still be added if the endpoint is missing the client will use default endpoints", func(t *testing.T) {
		knownProviders := []ProviderMetadata{
			{
				ID:          "openai",
				APIKey:      "$OPENAI_API_KEY",
				APIEndpoint: "$MISSING_ENDPOINT",
				Models: []ModelMetadata{{
					ID: "test-model",
				}},
			},
		}

		cfg := &Config{
			Providers: csync.NewMapFrom(map[string]ProviderConfig{
				"openai": {
					APIKey: "test-key",
				},
			}),
		}
		cfg.setDefaults("/tmp", "")

		env := env.NewFromMap(map[string]string{
			"OPENAI_API_KEY": "test-key",
		})
		resolver := NewEnvironmentVariableResolver(env)
		err := cfg.configureProviders(env, resolver, knownProviders)
		require.NoError(t, err)

		require.Equal(t, cfg.Providers.Len(), 1)
		_, exists := cfg.Providers.Get("openai")
		require.True(t, exists)
	})
}

func TestConfig_defaultModelSelection(t *testing.T) {
	t.Run("default behavior uses the default models for given provider", func(t *testing.T) {
		knownProviders := []ProviderMetadata{
			{
				ID:                  "openai",
				APIKey:              "abc",
				DefaultLargeModelID: "large-model",
				DefaultSmallModelID: "small-model",
				Models: []ModelMetadata{
					{
						ID:               "large-model",
						DefaultMaxTokens: 1000,
					},
					{
						ID:               "small-model",
						DefaultMaxTokens: 500,
					},
				},
			},
		}

		cfg := &Config{}
		cfg.setDefaults("/tmp", "")
		env := env.NewFromMap(map[string]string{})
		resolver := NewEnvironmentVariableResolver(env)
		err := cfg.configureProviders(env, resolver, knownProviders)
		require.NoError(t, err)

		large, small, err := cfg.defaultModelSelection(knownProviders)
		require.NoError(t, err)
		require.Equal(t, "large-model", large.Model)
		require.Equal(t, "openai", large.Provider)
		require.Equal(t, int64(1000), large.MaxTokens)
		require.Equal(t, "small-model", small.Model)
		require.Equal(t, "openai", small.Provider)
		require.Equal(t, int64(500), small.MaxTokens)
	})
	t.Run("should error if no providers configured", func(t *testing.T) {
		knownProviders := []ProviderMetadata{
			{
				ID:                  "openai",
				APIKey:              "$MISSING_KEY",
				DefaultLargeModelID: "large-model",
				DefaultSmallModelID: "small-model",
				Models: []ModelMetadata{
					{
						ID:               "large-model",
						DefaultMaxTokens: 1000,
					},
					{
						ID:               "small-model",
						DefaultMaxTokens: 500,
					},
				},
			},
		}

		cfg := &Config{}
		cfg.setDefaults("/tmp", "")
		env := env.NewFromMap(map[string]string{})
		resolver := NewEnvironmentVariableResolver(env)
		err := cfg.configureProviders(env, resolver, knownProviders)
		require.NoError(t, err)

		_, _, err = cfg.defaultModelSelection(knownProviders)
		require.Error(t, err)
	})
	t.Run("should error if model is missing", func(t *testing.T) {
		knownProviders := []ProviderMetadata{
			{
				ID:                  "openai",
				APIKey:              "abc",
				DefaultLargeModelID: "large-model",
				DefaultSmallModelID: "small-model",
				Models: []ModelMetadata{
					{
						ID:               "not-large-model",
						DefaultMaxTokens: 1000,
					},
					{
						ID:               "small-model",
						DefaultMaxTokens: 500,
					},
				},
			},
		}

		cfg := &Config{}
		cfg.setDefaults("/tmp", "")
		env := env.NewFromMap(map[string]string{})
		resolver := NewEnvironmentVariableResolver(env)
		err := cfg.configureProviders(env, resolver, knownProviders)
		require.NoError(t, err)
		_, _, err = cfg.defaultModelSelection(knownProviders)
		require.Error(t, err)
	})

	t.Run("should configure the default models with a custom provider", func(t *testing.T) {
		knownProviders := []ProviderMetadata{
			{
				ID:                  "openai",
				APIKey:              "$MISSING", // will not be included in the config
				DefaultLargeModelID: "large-model",
				DefaultSmallModelID: "small-model",
				Models: []ModelMetadata{
					{
						ID:               "not-large-model",
						DefaultMaxTokens: 1000,
					},
					{
						ID:               "small-model",
						DefaultMaxTokens: 500,
					},
				},
			},
		}

		cfg := &Config{
			Providers: csync.NewMapFrom(map[string]ProviderConfig{
				"custom": {
					APIKey:  "test-key",
					BaseURL: "https://api.custom.com/v1",
					Models: []ModelMetadata{
						{
							ID:               "model",
							DefaultMaxTokens: 600,
						},
					},
				},
			}),
		}
		cfg.setDefaults("/tmp", "")
		env := env.NewFromMap(map[string]string{})
		resolver := NewEnvironmentVariableResolver(env)
		err := cfg.configureProviders(env, resolver, knownProviders)
		require.NoError(t, err)
		large, small, err := cfg.defaultModelSelection(knownProviders)
		require.NoError(t, err)
		require.Equal(t, "model", large.Model)
		require.Equal(t, "custom", large.Provider)
		require.Equal(t, int64(600), large.MaxTokens)
		require.Equal(t, "model", small.Model)
		require.Equal(t, "custom", small.Provider)
		require.Equal(t, int64(600), small.MaxTokens)
	})

	t.Run("should fail if no model configured", func(t *testing.T) {
		knownProviders := []ProviderMetadata{
			{
				ID:                  "openai",
				APIKey:              "$MISSING", // will not be included in the config
				DefaultLargeModelID: "large-model",
				DefaultSmallModelID: "small-model",
				Models: []ModelMetadata{
					{
						ID:               "not-large-model",
						DefaultMaxTokens: 1000,
					},
					{
						ID:               "small-model",
						DefaultMaxTokens: 500,
					},
				},
			},
		}

		cfg := &Config{
			Providers: csync.NewMapFrom(map[string]ProviderConfig{
				"custom": {
					APIKey:  "test-key",
					BaseURL: "https://api.custom.com/v1",
					Models:  []ModelMetadata{},
				},
			}),
		}
		cfg.setDefaults("/tmp", "")
		env := env.NewFromMap(map[string]string{})
		resolver := NewEnvironmentVariableResolver(env)
		err := cfg.configureProviders(env, resolver, knownProviders)
		require.NoError(t, err)
		_, _, err = cfg.defaultModelSelection(knownProviders)
		require.Error(t, err)
	})
	t.Run("should use the default provider first", func(t *testing.T) {
		knownProviders := []ProviderMetadata{
			{
				ID:                  "openai",
				APIKey:              "set",
				DefaultLargeModelID: "large-model",
				DefaultSmallModelID: "small-model",
				Models: []ModelMetadata{
					{
						ID:               "large-model",
						DefaultMaxTokens: 1000,
					},
					{
						ID:               "small-model",
						DefaultMaxTokens: 500,
					},
				},
			},
		}

		cfg := &Config{
			Providers: csync.NewMapFrom(map[string]ProviderConfig{
				"custom": {
					APIKey:  "test-key",
					BaseURL: "https://api.custom.com/v1",
					Models: []ModelMetadata{
						{
							ID:               "large-model",
							DefaultMaxTokens: 1000,
						},
					},
				},
			}),
		}
		cfg.setDefaults("/tmp", "")
		env := env.NewFromMap(map[string]string{})
		resolver := NewEnvironmentVariableResolver(env)
		err := cfg.configureProviders(env, resolver, knownProviders)
		require.NoError(t, err)
		large, small, err := cfg.defaultModelSelection(knownProviders)
		require.NoError(t, err)
		require.Equal(t, "large-model", large.Model)
		require.Equal(t, "openai", large.Provider)
		require.Equal(t, int64(1000), large.MaxTokens)
		require.Equal(t, "small-model", small.Model)
		require.Equal(t, "openai", small.Provider)
		require.Equal(t, int64(500), small.MaxTokens)
	})
}

func TestConfig_configureProvidersDisableDefaultProviders(t *testing.T) {
	t.Run("when enabled, ignores all default providers and requires full specification", func(t *testing.T) {
		knownProviders := []ProviderMetadata{
			{
				ID:          "openai",
				APIKey:      "$OPENAI_API_KEY",
				APIEndpoint: "https://api.openai.com/v1",
				Models: []ModelMetadata{{
					ID: "gpt-4",
				}},
			},
		}

		// User references openai but doesn't fully specify it (no base_url, no
		// models). This should be rejected because disable_default_providers
		// treats all providers as custom.
		cfg := &Config{
			Options: &Options{
				DisableDefaultProviders: true,
			},
			Providers: csync.NewMapFrom(map[string]ProviderConfig{
				"openai": {
					APIKey: "$OPENAI_API_KEY",
				},
			}),
		}
		cfg.setDefaults("/tmp", "")

		env := env.NewFromMap(map[string]string{
			"OPENAI_API_KEY": "test-key",
		})
		resolver := NewEnvironmentVariableResolver(env)
		err := cfg.configureProviders(env, resolver, knownProviders)
		require.ErrorContains(t, err, "no custom providers")

		// openai should NOT be present because it lacks base_url and models.
		require.Equal(t, 0, cfg.Providers.Len())
		_, exists := cfg.Providers.Get("openai")
		require.False(t, exists, "openai should not be present without full specification")
	})

	t.Run("when enabled, fully specified providers work", func(t *testing.T) {
		knownProviders := []ProviderMetadata{
			{
				ID:          "openai",
				APIKey:      "$OPENAI_API_KEY",
				APIEndpoint: "https://api.openai.com/v1",
				Models: []ModelMetadata{{
					ID: "gpt-4",
				}},
			},
		}

		// User fully specifies their provider.
		cfg := &Config{
			Options: &Options{
				DisableDefaultProviders: true,
			},
			Providers: csync.NewMapFrom(map[string]ProviderConfig{
				"my-llm": {
					APIKey:  "$MY_API_KEY",
					BaseURL: "https://my-llm.example.com/v1",
					Models: []ModelMetadata{{
						ID: "my-model",
					}},
				},
			}),
		}
		cfg.setDefaults("/tmp", "")

		env := env.NewFromMap(map[string]string{
			"MY_API_KEY":     "test-key",
			"OPENAI_API_KEY": "test-key",
		})
		resolver := NewEnvironmentVariableResolver(env)
		err := cfg.configureProviders(env, resolver, knownProviders)
		require.NoError(t, err)

		// Only fully specified provider should be present.
		require.Equal(t, 1, cfg.Providers.Len())
		provider, exists := cfg.Providers.Get("my-llm")
		require.True(t, exists, "my-llm should be present")
		require.Equal(t, "https://my-llm.example.com/v1", provider.BaseURL)
		require.Len(t, provider.Models, 1)

		// Default openai should NOT be present.
		_, exists = cfg.Providers.Get("openai")
		require.False(t, exists, "openai should not be present")
	})

	t.Run("when disabled, includes all known providers with valid credentials", func(t *testing.T) {
		knownProviders := []ProviderMetadata{
			{
				ID:          "openai",
				APIKey:      "$OPENAI_API_KEY",
				APIEndpoint: "https://api.openai.com/v1",
				Models: []ModelMetadata{{
					ID: "gpt-4",
				}},
			},
			{
				ID:          "anthropic",
				APIKey:      "$ANTHROPIC_API_KEY",
				APIEndpoint: "https://api.anthropic.com/v1",
				Models: []ModelMetadata{{
					ID: "claude-3",
				}},
			},
		}

		// User only configures openai, both API keys are available, but option
		// is disabled.
		cfg := &Config{
			Options: &Options{
				DisableDefaultProviders: false,
			},
			Providers: csync.NewMapFrom(map[string]ProviderConfig{
				"openai": {
					APIKey: "$OPENAI_API_KEY",
				},
			}),
		}
		cfg.setDefaults("/tmp", "")

		env := env.NewFromMap(map[string]string{
			"OPENAI_API_KEY":    "test-key",
			"ANTHROPIC_API_KEY": "test-key",
		})
		resolver := NewEnvironmentVariableResolver(env)
		err := cfg.configureProviders(env, resolver, knownProviders)
		require.NoError(t, err)

		// Both providers should be present.
		require.Equal(t, 2, cfg.Providers.Len())
		_, exists := cfg.Providers.Get("openai")
		require.True(t, exists, "openai should be present")
		_, exists = cfg.Providers.Get("anthropic")
		require.True(t, exists, "anthropic should be present")
	})

	t.Run("when enabled, provider missing models is rejected", func(t *testing.T) {
		cfg := &Config{
			Options: &Options{
				DisableDefaultProviders: true,
			},
			Providers: csync.NewMapFrom(map[string]ProviderConfig{
				"my-llm": {
					APIKey:  "test-key",
					BaseURL: "https://my-llm.example.com/v1",
					Models:  []ModelMetadata{}, // No models.
				},
			}),
		}
		cfg.setDefaults("/tmp", "")

		env := env.NewFromMap(map[string]string{})
		resolver := NewEnvironmentVariableResolver(env)
		err := cfg.configureProviders(env, resolver, []ProviderMetadata{})
		require.ErrorContains(t, err, "no custom providers")

		// Provider should be rejected for missing models.
		require.Equal(t, 0, cfg.Providers.Len())
	})

	t.Run("when enabled, provider missing base_url is rejected", func(t *testing.T) {
		cfg := &Config{
			Options: &Options{
				DisableDefaultProviders: true,
			},
			Providers: csync.NewMapFrom(map[string]ProviderConfig{
				"my-llm": {
					APIKey: "test-key",
					Models: []ModelMetadata{{ID: "model"}},
					// No BaseURL.
				},
			}),
		}
		cfg.setDefaults("/tmp", "")

		env := env.NewFromMap(map[string]string{})
		resolver := NewEnvironmentVariableResolver(env)
		err := cfg.configureProviders(env, resolver, []ProviderMetadata{})
		require.ErrorContains(t, err, "no custom providers")

		// Provider should be rejected for missing base_url.
		require.Equal(t, 0, cfg.Providers.Len())
	})
}

func TestConfig_setDefaultsDisableDefaultProvidersEnvVar(t *testing.T) {
	t.Run("sets option from environment variable", func(t *testing.T) {
		t.Setenv("CRUCIBLE_DISABLE_DEFAULT_PROVIDERS", "true")

		cfg := &Config{}
		cfg.setDefaults("/tmp", "")

		require.True(t, cfg.Options.DisableDefaultProviders)
	})

	t.Run("does not override when env var is not set", func(t *testing.T) {
		cfg := &Config{
			Options: &Options{
				DisableDefaultProviders: true,
			},
		}
		cfg.setDefaults("/tmp", "")

		require.True(t, cfg.Options.DisableDefaultProviders)
	})
}

func TestDefaultStations(t *testing.T) {
	// All seven default stations must be present.
	require.Contains(t, DefaultStations, "design")
	require.Contains(t, DefaultStations, "plan")
	require.Contains(t, DefaultStations, "inspect")
	require.Contains(t, DefaultStations, "build")
	require.Contains(t, DefaultStations, "review")
	require.Contains(t, DefaultStations, "verify")
	require.Contains(t, DefaultStations, "ship")

	t.Run("build", func(t *testing.T) {
		build := DefaultStations["build"]
		assert.Equal(t, "claude", build.Backend)
		assert.NotEmpty(t, build.Description)
		assert.Contains(t, build.Description, "Implement")
		assert.Equal(t, "feature-dev:feature-dev", build.Skill)
		assert.Empty(t, build.Options)
		// build→build is allowed (no afterDone constraint).
		assert.Empty(t, build.AfterDone)
	})

	t.Run("inspect", func(t *testing.T) {
		inspect := DefaultStations["inspect"]
		assert.Equal(t, "opencode-acp", inspect.Backend)
		assert.NotEmpty(t, inspect.Description)
		assert.Contains(t, inspect.Description, "Validate")
		assert.Equal(t, "review-plan", inspect.Skill)
		assert.Empty(t, inspect.Options)
	})

	t.Run("review", func(t *testing.T) {
		review := DefaultStations["review"]
		assert.Equal(t, "claude", review.Backend)
		assert.NotEmpty(t, review.Description)
		assert.Contains(t, review.Description, "review")
		assert.Equal(t, "claude-code-quality:rigorous-pr-review", review.Skill)
		assert.Equal(t, "plan", review.Options["mode"])
	})

	t.Run("design", func(t *testing.T) {
		design := DefaultStations["design"]
		assert.Equal(t, "claude", design.Backend)
		assert.NotEmpty(t, design.Description)
		assert.Contains(t, design.Description, "architecture")
		assert.Equal(t, "claude-foundry:design", design.Skill)
		assert.Equal(t, "design", design.ArtifactType)
		assert.Contains(t, design.Steering, "plan")
		assert.Empty(t, design.Options)
	})

	t.Run("verify", func(t *testing.T) {
		verify := DefaultStations["verify"]
		assert.Equal(t, "claude", verify.Backend)
		assert.NotEmpty(t, verify.Description)
		assert.Contains(t, verify.Description, "Verify")
		assert.Equal(t, "verification", verify.ArtifactType)
		assert.Equal(t, "claude-foundry:verify", verify.Skill)
		assert.Empty(t, verify.Options)
		assert.False(t, verify.Gate)
		assert.Contains(t, verify.Steering, "ship")
	})

	t.Run("ship", func(t *testing.T) {
		ship := DefaultStations["ship"]
		assert.Equal(t, "claude", ship.Backend)
		assert.NotEmpty(t, ship.Description)
		assert.Contains(t, ship.Description, "pull request")
		assert.Equal(t, "pr", ship.ArtifactType)
		assert.Empty(t, ship.Skill)
		assert.Empty(t, ship.Options)
		require.True(t, ship.Gate)
		assert.Contains(t, ship.Steering, "PR URL")
	})
}

func TestConfig_configureSelectedModels(t *testing.T) {
	t.Run("should override defaults", func(t *testing.T) {
		knownProviders := []ProviderMetadata{
			{
				ID:                  "openai",
				APIKey:              "abc",
				DefaultLargeModelID: "large-model",
				DefaultSmallModelID: "small-model",
				Models: []ModelMetadata{
					{
						ID:               "larger-model",
						DefaultMaxTokens: 2000,
					},
					{
						ID:               "large-model",
						DefaultMaxTokens: 1000,
					},
					{
						ID:               "small-model",
						DefaultMaxTokens: 500,
					},
				},
			},
		}

		cfg := &Config{
			Models: map[SelectedModelType]SelectedModel{
				"large": {
					Model: "larger-model",
				},
			},
		}
		cfg.setDefaults("/tmp", "")
		env := env.NewFromMap(map[string]string{})
		resolver := NewEnvironmentVariableResolver(env)
		err := cfg.configureProviders(env, resolver, knownProviders)
		require.NoError(t, err)

		err = cfg.configureSelectedModels(knownProviders)
		require.NoError(t, err)
		large := cfg.Models[SelectedModelTypeLarge]
		small := cfg.Models[SelectedModelTypeSmall]
		require.Equal(t, "larger-model", large.Model)
		require.Equal(t, "openai", large.Provider)
		require.Equal(t, int64(2000), large.MaxTokens)
		require.Equal(t, "small-model", small.Model)
		require.Equal(t, "openai", small.Provider)
		require.Equal(t, int64(500), small.MaxTokens)
	})
	t.Run("should be possible to use multiple providers", func(t *testing.T) {
		knownProviders := []ProviderMetadata{
			{
				ID:                  "openai",
				APIKey:              "abc",
				DefaultLargeModelID: "large-model",
				DefaultSmallModelID: "small-model",
				Models: []ModelMetadata{
					{
						ID:               "large-model",
						DefaultMaxTokens: 1000,
					},
					{
						ID:               "small-model",
						DefaultMaxTokens: 500,
					},
				},
			},
			{
				ID:                  "anthropic",
				APIKey:              "abc",
				DefaultLargeModelID: "a-large-model",
				DefaultSmallModelID: "a-small-model",
				Models: []ModelMetadata{
					{
						ID:               "a-large-model",
						DefaultMaxTokens: 1000,
					},
					{
						ID:               "a-small-model",
						DefaultMaxTokens: 200,
					},
				},
			},
		}

		cfg := &Config{
			Models: map[SelectedModelType]SelectedModel{
				"small": {
					Model:     "a-small-model",
					Provider:  "anthropic",
					MaxTokens: 300,
				},
			},
		}
		cfg.setDefaults("/tmp", "")
		env := env.NewFromMap(map[string]string{})
		resolver := NewEnvironmentVariableResolver(env)
		err := cfg.configureProviders(env, resolver, knownProviders)
		require.NoError(t, err)

		err = cfg.configureSelectedModels(knownProviders)
		require.NoError(t, err)
		large := cfg.Models[SelectedModelTypeLarge]
		small := cfg.Models[SelectedModelTypeSmall]
		require.Equal(t, "large-model", large.Model)
		require.Equal(t, "openai", large.Provider)
		require.Equal(t, int64(1000), large.MaxTokens)
		require.Equal(t, "a-small-model", small.Model)
		require.Equal(t, "anthropic", small.Provider)
		require.Equal(t, int64(300), small.MaxTokens)
	})

	t.Run("should override the max tokens only", func(t *testing.T) {
		knownProviders := []ProviderMetadata{
			{
				ID:                  "openai",
				APIKey:              "abc",
				DefaultLargeModelID: "large-model",
				DefaultSmallModelID: "small-model",
				Models: []ModelMetadata{
					{
						ID:               "large-model",
						DefaultMaxTokens: 1000,
					},
					{
						ID:               "small-model",
						DefaultMaxTokens: 500,
					},
				},
			},
		}

		cfg := &Config{
			Models: map[SelectedModelType]SelectedModel{
				"large": {
					MaxTokens: 100,
				},
			},
		}
		cfg.setDefaults("/tmp", "")
		env := env.NewFromMap(map[string]string{})
		resolver := NewEnvironmentVariableResolver(env)
		err := cfg.configureProviders(env, resolver, knownProviders)
		require.NoError(t, err)

		err = cfg.configureSelectedModels(knownProviders)
		require.NoError(t, err)
		large := cfg.Models[SelectedModelTypeLarge]
		require.Equal(t, "large-model", large.Model)
		require.Equal(t, "openai", large.Provider)
		require.Equal(t, int64(100), large.MaxTokens)
	})
}
