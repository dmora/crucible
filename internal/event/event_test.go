package event

import "testing"

func TestError(t *testing.T) {
	// All event functions are no-ops after telemetry removal.
	// Verify they don't panic.
	Error(nil)
	Error("test error")
	Error("test error", "key", "value")
}

func TestNoOps(t *testing.T) {
	Init()
	AppInitialized()
	PromptSent("model", "test")
	PromptResponded("tokens", 100)
	TokensUsed("prompt", 50)
	SessionCreated()
	AppExited()
	Flush()

	if id := GetID(); id != "" {
		t.Errorf("GetID() = %q, want empty", id)
	}
}
