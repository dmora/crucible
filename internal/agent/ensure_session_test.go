package agent

import (
	"context"
	"testing"

	adksession "google.golang.org/adk/session"

	"github.com/dmora/crucible/internal/agent/prompt"
	"github.com/dmora/crucible/internal/csync"
	"github.com/dmora/crucible/internal/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureADKSession_SetsVersionTags(t *testing.T) {
	svc := adksession.InMemoryService()

	a := &sessionAgent{
		adkSessionService:  svc,
		systemPrompt:       csync.NewValue("You are a helpful assistant."),
		systemPromptPrefix: csync.NewValue("PREFIX"),
	}

	ctx := context.Background()
	_, err := a.ensureADKSession(ctx, "test-session-1")
	require.NoError(t, err)

	// Reload from service to verify persistence (not just in-memory mutation).
	resp, err := svc.Get(ctx, &adksession.GetRequest{
		AppName: adkAppName, UserID: adkUserID, SessionID: "test-session-1",
	})
	require.NoError(t, err)
	reloaded := resp.Session

	bv, err := reloaded.State().Get("build_version")
	require.NoError(t, err)
	assert.Equal(t, version.Version, bv)

	ph, err := reloaded.State().Get("prompt_hash")
	require.NoError(t, err)
	assert.Equal(t, prompt.Hash("PREFIX\n\nYou are a helpful assistant."), ph)
	assert.Len(t, ph, 12)

	// Verify artifact registry version marker is set on new sessions.
	arv, err := reloaded.State().Get("app:artifact_registry_version")
	require.NoError(t, err)
	assert.Equal(t, "1", arv, "new sessions must have artifact_registry_version marker")

	// Calling again returns the same session (Get path, not Create).
	sess2, err := a.ensureADKSession(ctx, "test-session-1")
	require.NoError(t, err)
	assert.Equal(t, "test-session-1", sess2.ID())
}

func TestEnsureADKSession_LegacySessionNoMarker(t *testing.T) {
	svc := adksession.InMemoryService()
	ctx := context.Background()

	// Pre-create a session WITHOUT the version marker (simulates legacy).
	_, err := svc.Create(ctx, &adksession.CreateRequest{
		AppName:   adkAppName,
		UserID:    adkUserID,
		SessionID: "legacy-session",
		State:     map[string]any{"build_version": "v0.1"},
	})
	require.NoError(t, err)

	a := &sessionAgent{
		adkSessionService:  svc,
		systemPrompt:       csync.NewValue("prompt"),
		systemPromptPrefix: csync.NewValue(""),
	}

	// ensureADKSession takes the Get path — must NOT inject the marker.
	sess, err := a.ensureADKSession(ctx, "legacy-session")
	require.NoError(t, err)

	arv, err := sess.State().Get("app:artifact_registry_version")
	// ADK State().Get returns an error for missing keys — either nil or error is acceptable.
	if err == nil {
		assert.Nil(t, arv, "legacy sessions must NOT get the version marker via Get path")
	}
}

func TestEnsureADKSession_PrefixOnly_NoHash(t *testing.T) {
	svc := adksession.InMemoryService()

	// Prefix set but prompt body empty — simulates shell/relay early creation.
	a := &sessionAgent{
		adkSessionService:  svc,
		systemPrompt:       csync.NewValue(""),
		systemPromptPrefix: csync.NewValue("PREFIX"),
	}

	ctx := context.Background()
	sess, err := a.ensureADKSession(ctx, "test-session-2")
	require.NoError(t, err)

	// Reload — prompt_hash must be empty (not a hash of just the prefix).
	resp, err := svc.Get(ctx, &adksession.GetRequest{
		AppName: adkAppName, UserID: adkUserID, SessionID: "test-session-2",
	})
	require.NoError(t, err)
	reloaded := resp.Session

	ph, err := reloaded.State().Get("prompt_hash")
	require.NoError(t, err)
	assert.Equal(t, "", ph, "prompt_hash must be empty when prompt body is empty")

	bv, err := reloaded.State().Get("build_version")
	require.NoError(t, err)
	assert.Equal(t, version.Version, bv)

	// Simulate prompt becoming ready, then backfill via backfillPromptHash.
	a.systemPrompt = csync.NewValue("The real prompt")

	a.backfillPromptHash(ctx, sess, "test-session-2")

	// Reload — prompt_hash should now be persisted with correct value.
	resp2, err := svc.Get(ctx, &adksession.GetRequest{
		AppName: adkAppName, UserID: adkUserID, SessionID: "test-session-2",
	})
	require.NoError(t, err)

	ph2, err := resp2.Session.State().Get("prompt_hash")
	require.NoError(t, err)
	expected := prompt.Hash("PREFIX\n\nThe real prompt")
	assert.Equal(t, expected, ph2)
}

func TestBackfillPromptHash_SkipsWhenAlreadySet(t *testing.T) {
	svc := adksession.InMemoryService()
	ctx := context.Background()

	promptText := "Some prompt"
	hash := prompt.Hash(promptText)

	// Create session with prompt_hash already set.
	resp, err := svc.Create(ctx, &adksession.CreateRequest{
		AppName:   adkAppName,
		UserID:    adkUserID,
		SessionID: "test-session-3",
		State:     map[string]any{"prompt_hash": hash, "build_version": "v1.0"},
	})
	require.NoError(t, err)

	a := &sessionAgent{
		adkSessionService:  svc,
		systemPrompt:       csync.NewValue("Different prompt"),
		systemPromptPrefix: csync.NewValue(""),
	}

	// backfillPromptHash should be a no-op since prompt_hash is already set.
	a.backfillPromptHash(ctx, resp.Session, "test-session-3")

	// Reload and verify the hash was NOT overwritten.
	resp2, err := svc.Get(ctx, &adksession.GetRequest{
		AppName: adkAppName, UserID: adkUserID, SessionID: "test-session-3",
	})
	require.NoError(t, err)

	ph, err := resp2.Session.State().Get("prompt_hash")
	require.NoError(t, err)
	assert.Equal(t, hash, ph, "backfill should not overwrite existing prompt_hash")
}
