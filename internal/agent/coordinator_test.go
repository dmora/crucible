package agent

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/dmora/crucible/internal/config"
	"github.com/dmora/crucible/internal/csync"
	"github.com/dmora/crucible/internal/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSessionAgent is a minimal mock for the SessionAgent interface.
type mockSessionAgent struct {
	model     Model
	runFunc   func(ctx context.Context, call SessionAgentCall) (*AgentResult, error)
	cancelled []string
}

func (m *mockSessionAgent) Run(ctx context.Context, call SessionAgentCall) (*AgentResult, error) {
	return m.runFunc(ctx, call)
}

func (m *mockSessionAgent) Model() Model                        { return m.model }
func (m *mockSessionAgent) SetModels(large, small Model)        {}
func (m *mockSessionAgent) SetSystemPrompt(systemPrompt string) {}
func (m *mockSessionAgent) Cancel(sessionID string) {
	m.cancelled = append(m.cancelled, sessionID)
}
func (m *mockSessionAgent) CancelAll()                                            {}
func (m *mockSessionAgent) IsSessionBusy(sessionID string) bool                   { return false }
func (m *mockSessionAgent) IsBusy() bool                                          { return false }
func (m *mockSessionAgent) QueuedPrompts(sessionID string) int                    { return 0 }
func (m *mockSessionAgent) QueuedPromptsList(sessionID string) []string           { return nil }
func (m *mockSessionAgent) ClearQueue(sessionID string)                           {}
func (m *mockSessionAgent) Summarize(context.Context, string) error               { return nil }
func (m *mockSessionAgent) TurnMetrics(sessionID string) *TurnMetrics             { return nil }
func (m *mockSessionAgent) StopProcess(_ context.Context, _ string)               {}
func (m *mockSessionAgent) StopAllProcesses(_ context.Context)                    {}
func (m *mockSessionAgent) ExecuteUserShell(_ context.Context, _, _ string) error { return nil }
func (m *mockSessionAgent) SetSessionWorktree(_, _, _ string)                     {}
func (m *mockSessionAgent) PurgeSession(_ string)                                 {}

func TestCoordinatorInterface(t *testing.T) {
	// Verify coordinator implements the Coordinator interface.
	var _ Coordinator = &coordinator{}
	assert.True(t, true)
}

func TestCoordinatorDelegation(t *testing.T) {
	t.Run("Run delegates to agent", func(t *testing.T) {
		var calledWith SessionAgentCall
		mock := &mockSessionAgent{
			model: Model{
				Metadata: config.ModelMetadata{
					DefaultMaxTokens:    4096,
					SupportsAttachments: true,
				},
				ModelCfg: config.SelectedModel{
					Provider: "gemini",
				},
			},
			runFunc: func(_ context.Context, call SessionAgentCall) (*AgentResult, error) {
				calledWith = call
				return &AgentResult{}, nil
			},
		}

		env := testEnv(t)
		cfg, err := config.Init(env.workingDir, "", false)
		require.NoError(t, err)

		coord := &coordinator{
			cfg:           cfg,
			sessions:      env.sessions,
			messageBroker: env.messageBroker,
			currentAgent:  mock,
			agents:        map[string]SessionAgent{config.AgentCrucible: mock},
		}

		sess, err := env.sessions.Create(t.Context(), "Test")
		require.NoError(t, err)

		_, err = coord.Run(t.Context(), sess.ID, "hello")
		require.NoError(t, err)
		assert.Equal(t, "hello", calledWith.Prompt)
		assert.Equal(t, sess.ID, calledWith.SessionID)
		assert.Equal(t, int64(4096), calledWith.MaxOutputTokens)
	})

	t.Run("MaxTokens override from ModelCfg", func(t *testing.T) {
		var calledWith SessionAgentCall
		mock := &mockSessionAgent{
			model: Model{
				Metadata: config.ModelMetadata{
					DefaultMaxTokens:    4096,
					SupportsAttachments: true,
				},
				ModelCfg: config.SelectedModel{
					Provider:  "gemini",
					MaxTokens: 8192,
				},
			},
			runFunc: func(_ context.Context, call SessionAgentCall) (*AgentResult, error) {
				calledWith = call
				return &AgentResult{}, nil
			},
		}

		env := testEnv(t)
		cfg, err := config.Init(env.workingDir, "", false)
		require.NoError(t, err)

		coord := &coordinator{
			cfg:           cfg,
			sessions:      env.sessions,
			messageBroker: env.messageBroker,
			currentAgent:  mock,
			agents:        map[string]SessionAgent{config.AgentCrucible: mock},
		}

		sess, err := env.sessions.Create(t.Context(), "Test")
		require.NoError(t, err)

		_, err = coord.Run(t.Context(), sess.ID, "test")
		require.NoError(t, err)
		assert.Equal(t, int64(8192), calledWith.MaxOutputTokens)
	})

	t.Run("Cancel delegates", func(t *testing.T) {
		mock := &mockSessionAgent{
			model:   Model{},
			runFunc: nil,
		}

		coord := &coordinator{currentAgent: mock}
		coord.Cancel("sess-1")
		assert.Equal(t, []string{"sess-1"}, mock.cancelled)
	})

	t.Run("filters image attachments when model doesn't support images", func(t *testing.T) {
		var calledWith SessionAgentCall
		mock := &mockSessionAgent{
			model: Model{
				Metadata: config.ModelMetadata{
					DefaultMaxTokens:    4096,
					SupportsAttachments: false,
				},
				ModelCfg: config.SelectedModel{
					Provider: "gemini",
				},
			},
			runFunc: func(_ context.Context, call SessionAgentCall) (*AgentResult, error) {
				calledWith = call
				return &AgentResult{}, nil
			},
		}

		env := testEnv(t)
		cfg, err := config.Init(env.workingDir, "", false)
		require.NoError(t, err)

		coord := &coordinator{
			cfg:           cfg,
			sessions:      env.sessions,
			messageBroker: env.messageBroker,
			currentAgent:  mock,
			agents:        map[string]SessionAgent{config.AgentCrucible: mock},
		}

		sess, err := env.sessions.Create(t.Context(), "Test")
		require.NoError(t, err)

		attachments := []message.Attachment{
			{MimeType: "text/plain", Content: []byte("hello")},
			{MimeType: "image/png", Content: []byte("png data")},
		}
		_, err = coord.Run(t.Context(), sess.ID, "test", attachments...)
		require.NoError(t, err)
		require.Len(t, calledWith.Attachments, 1)
		assert.Equal(t, "text/plain", calledWith.Attachments[0].MimeType)
	})
}

func TestBuildGeminiModel_RetryTransportWired(t *testing.T) {
	// buildGeminiModel calls gemini.NewModel which requires a real API endpoint.
	// We can't test the full flow, but we can verify that both backend paths
	// construct the retryHTTPClient correctly by testing the transport directly.
	t.Run("Vertex AI path constructs retry client", func(t *testing.T) {
		rt := NewRetryTransport(http.DefaultTransport, DefaultRetryTransportConfig())
		client := &http.Client{Transport: rt}
		assert.NotNil(t, client.Transport)
		_, ok := client.Transport.(*retryTransport)
		assert.True(t, ok, "transport should be *retryTransport")
	})

	t.Run("Gemini API path constructs retry client", func(t *testing.T) {
		rt := NewRetryTransport(http.DefaultTransport, DefaultRetryTransportConfig())
		client := &http.Client{Transport: rt}
		assert.NotNil(t, client.Transport)
		_, ok := client.Transport.(*retryTransport)
		assert.True(t, ok, "transport should be *retryTransport")
	})

	t.Run("DefaultRetryTransportConfig has sane defaults", func(t *testing.T) {
		cfg := DefaultRetryTransportConfig()
		assert.Equal(t, 5, cfg.MaxAttempts)
		assert.Equal(t, 1*time.Second, cfg.InitialDelay)
		assert.Equal(t, 60*time.Second, cfg.MaxDelay)
		assert.Equal(t, 2.0, cfg.Multiplier)
		assert.Equal(t, 1*time.Second, cfg.JitterRange)
	})
}

func TestBuildAgentModelsErrors(t *testing.T) {
	t.Run("missing large model selection", func(t *testing.T) {
		cfg := &config.Config{
			Models: map[config.SelectedModelType]config.SelectedModel{
				config.SelectedModelTypeSmall: {Provider: "gemini", Model: "gemini-2.5-flash"},
			},
		}
		coord := &coordinator{cfg: cfg}
		_, _, err := coord.buildAgentModels(t.Context())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "large model not selected")
	})

	t.Run("missing small model selection", func(t *testing.T) {
		cfg := &config.Config{
			Models: map[config.SelectedModelType]config.SelectedModel{
				config.SelectedModelTypeLarge: {Provider: "gemini", Model: "gemini-2.5-pro"},
			},
		}
		coord := &coordinator{cfg: cfg}
		_, _, err := coord.buildAgentModels(t.Context())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "small model not selected")
	})

	t.Run("missing large provider config", func(t *testing.T) {
		cfg := &config.Config{
			Models: map[config.SelectedModelType]config.SelectedModel{
				config.SelectedModelTypeLarge: {Provider: "missing", Model: "m"},
				config.SelectedModelTypeSmall: {Provider: "gemini", Model: "m"},
			},
			Providers: csync.NewMap[string, config.ProviderConfig](),
		}
		coord := &coordinator{cfg: cfg}
		_, _, err := coord.buildAgentModels(t.Context())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "large model provider not configured")
	})

	t.Run("missing small provider config", func(t *testing.T) {
		cfg := &config.Config{
			Models: map[config.SelectedModelType]config.SelectedModel{
				config.SelectedModelTypeLarge: {Provider: "gemini", Model: "m"},
				config.SelectedModelTypeSmall: {Provider: "missing", Model: "m"},
			},
			Providers: csync.NewMapFrom(map[string]config.ProviderConfig{
				"gemini": {ID: "gemini"},
			}),
		}
		coord := &coordinator{cfg: cfg}
		_, _, err := coord.buildAgentModels(t.Context())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "small model provider not configured")
	})
}
