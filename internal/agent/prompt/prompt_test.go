package prompt

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmora/crucible/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestConfig returns a minimal config with optional user context paths.
func newTestConfig(userPaths ...string) config.Config {
	return config.Config{
		Options: &config.Options{
			ContextPaths: userPaths,
		},
	}
}

func TestTierResolution(t *testing.T) {
	fixedTime := func() time.Time { return time.Date(2026, 3, 12, 0, 0, 0, 0, time.UTC) }

	t.Run("tier 1 only - CRUCIBLE.md loaded", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "CRUCIBLE.md"), []byte("crucible content"), 0o644))
		// Also create a tier 3 file — should NOT be loaded
		require.NoError(t, os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("claude content"), 0o644))

		p, err := NewPrompt("test", "{{range .ContextFiles}}{{.Path}}|{{end}}",
			WithWorkingDir(dir), WithTimeFunc(fixedTime), WithPlatform("test"))
		require.NoError(t, err)

		cfg := newTestConfig()
		result, err := p.Build(context.Background(), "crucible", "gemini", "test-model", cfg)
		require.NoError(t, err)

		assert.Contains(t, result, "CRUCIBLE.md")
		assert.NotContains(t, result, "CLAUDE.md")
	})

	t.Run("tier 1 + local - both loaded from same tier", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "CRUCIBLE.md"), []byte("crucible"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "CRUCIBLE.local.md"), []byte("local"), 0o644))

		p, err := NewPrompt("test", "{{range .ContextFiles}}{{.Path}}|{{end}}",
			WithWorkingDir(dir), WithTimeFunc(fixedTime), WithPlatform("test"))
		require.NoError(t, err)

		cfg := newTestConfig()
		result, err := p.Build(context.Background(), "crucible", "gemini", "test-model", cfg)
		require.NoError(t, err)

		assert.Contains(t, result, "CRUCIBLE.md")
		assert.Contains(t, result, "CRUCIBLE.local.md")
	})

	t.Run("tier 2 only - AGENTS.md loaded", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("agents content"), 0o644))

		p, err := NewPrompt("test", "{{range .ContextFiles}}{{.Path}}|{{end}}",
			WithWorkingDir(dir), WithTimeFunc(fixedTime), WithPlatform("test"))
		require.NoError(t, err)

		cfg := newTestConfig()
		result, err := p.Build(context.Background(), "crucible", "gemini", "test-model", cfg)
		require.NoError(t, err)

		assert.Contains(t, result, "AGENTS.md")
	})

	t.Run("tier 3 only - CLAUDE.md loaded", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("claude content"), 0o644))

		p, err := NewPrompt("test", "{{range .ContextFiles}}{{.Path}}|{{end}}",
			WithWorkingDir(dir), WithTimeFunc(fixedTime), WithPlatform("test"))
		require.NoError(t, err)

		cfg := newTestConfig()
		result, err := p.Build(context.Background(), "crucible", "gemini", "test-model", cfg)
		require.NoError(t, err)

		assert.Contains(t, result, "CLAUDE.md")
	})

	t.Run("tier 4 only - .cursorrules loaded", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".cursorrules"), []byte("rules"), 0o644))

		p, err := NewPrompt("test", "{{range .ContextFiles}}{{.Path}}|{{end}}",
			WithWorkingDir(dir), WithTimeFunc(fixedTime), WithPlatform("test"))
		require.NoError(t, err)

		cfg := newTestConfig()
		result, err := p.Build(context.Background(), "crucible", "gemini", "test-model", cfg)
		require.NoError(t, err)

		assert.Contains(t, result, ".cursorrules")
	})

	t.Run("AGENTS.md overrides CLAUDE.md - tier 2 wins over tier 3", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("agents"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("claude"), 0o644))

		p, err := NewPrompt("test", "{{range .ContextFiles}}{{.Path}}|{{end}}",
			WithWorkingDir(dir), WithTimeFunc(fixedTime), WithPlatform("test"))
		require.NoError(t, err)

		cfg := newTestConfig()
		result, err := p.Build(context.Background(), "crucible", "gemini", "test-model", cfg)
		require.NoError(t, err)

		// Use full paths to avoid false matches from temp dir names containing test case names.
		assert.Contains(t, result, filepath.Join(dir, "AGENTS.md"))
		assert.NotContains(t, result, filepath.Join(dir, "CLAUDE.md"))
	})

	t.Run("tier 1 overrides tier 2", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "CRUCIBLE.md"), []byte("crucible"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("agents"), 0o644))

		p, err := NewPrompt("test", "{{range .ContextFiles}}{{.Path}}|{{end}}",
			WithWorkingDir(dir), WithTimeFunc(fixedTime), WithPlatform("test"))
		require.NoError(t, err)

		cfg := newTestConfig()
		result, err := p.Build(context.Background(), "crucible", "gemini", "test-model", cfg)
		require.NoError(t, err)

		assert.Contains(t, result, "CRUCIBLE.md")
		assert.NotContains(t, result, "AGENTS.md")
	})

	t.Run("user paths always loaded alongside tier", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "CRUCIBLE.md"), []byte("crucible"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "custom.md"), []byte("custom"), 0o644))

		p, err := NewPrompt("test", "{{range .ContextFiles}}{{.Path}}|{{end}}",
			WithWorkingDir(dir), WithTimeFunc(fixedTime), WithPlatform("test"))
		require.NoError(t, err)

		cfg := newTestConfig("custom.md")
		result, err := p.Build(context.Background(), "crucible", "gemini", "test-model", cfg)
		require.NoError(t, err)

		assert.Contains(t, result, "CRUCIBLE.md")
		assert.Contains(t, result, "custom.md")
	})

	t.Run("no context files - empty result", func(t *testing.T) {
		dir := t.TempDir()

		p, err := NewPrompt("test", "[{{range .ContextFiles}}{{.Path}}|{{end}}]",
			WithWorkingDir(dir), WithTimeFunc(fixedTime), WithPlatform("test"))
		require.NoError(t, err)

		cfg := newTestConfig()
		result, err := p.Build(context.Background(), "crucible", "gemini", "test-model", cfg)
		require.NoError(t, err)

		assert.Equal(t, "[]", result)
	})

	t.Run("tier 4 nested .github/copilot-instructions.md loaded", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, ".github"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".github", "copilot-instructions.md"), []byte("copilot"), 0o644))

		p, err := NewPrompt("test", "{{range .ContextFiles}}{{.Path}}|{{end}}",
			WithWorkingDir(dir), WithTimeFunc(fixedTime), WithPlatform("test"))
		require.NoError(t, err)

		cfg := newTestConfig()
		result, err := p.Build(context.Background(), "crucible", "gemini", "test-model", cfg)
		require.NoError(t, err)

		assert.Contains(t, result, "copilot-instructions.md")
	})

	t.Run("empty .cursor/rules/ directory not loaded as context", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, ".cursor", "rules"), 0o755))

		p, err := NewPrompt("test", "[{{range .ContextFiles}}{{.Path}}|{{end}}]",
			WithWorkingDir(dir), WithTimeFunc(fixedTime), WithPlatform("test"))
		require.NoError(t, err)

		cfg := newTestConfig()
		result, err := p.Build(context.Background(), "crucible", "gemini", "test-model", cfg)
		require.NoError(t, err)

		// Empty directory produces no ContextFiles, so tier 4 has no content and is skipped.
		assert.Equal(t, "[]", result)
	})
}

// readCoderTemplate reads the embedded coder.md.tpl template from the source tree.
func readCoderTemplate(t *testing.T) string {
	t.Helper()
	tmpl, err := os.ReadFile(filepath.Join("..", "templates", "coder.md.tpl"))
	require.NoError(t, err, "reading coder.md.tpl — run tests from the prompt/ directory or module root")
	return string(tmpl)
}

// newStationTestConfig returns a config with SetupDefaultStations applied.
func newStationTestConfig() config.Config {
	cfg := config.Config{
		Options: &config.Options{},
	}
	cfg.SetupDefaultStations()
	return cfg
}

func TestCoderTemplateRendersWithAllDefaultStations(t *testing.T) {
	cfg := newStationTestConfig()

	p, err := NewPrompt("coder", readCoderTemplate(t),
		WithPlatform("linux"), WithWorkingDir(t.TempDir()))
	require.NoError(t, err)

	result, err := p.Build(context.Background(), "crucible", "gemini", "gemini-2.5-pro", cfg)
	require.NoError(t, err)

	// All 7 stations must appear in the rendered prompt.
	for _, station := range []string{"design", "plan", "inspect", "build", "review", "verify", "ship"} {
		assert.Contains(t, result, station, "prompt output missing station %q", station)
	}
}

func TestCoderTemplateRendersWithDisabledVerifyShip(t *testing.T) {
	cfg := newStationTestConfig()

	// Disable verify and ship.
	s := cfg.Stations["verify"]
	s.Disabled = true
	cfg.Stations["verify"] = s
	s = cfg.Stations["ship"]
	s.Disabled = true
	cfg.Stations["ship"] = s

	p, err := NewPrompt("coder", readCoderTemplate(t),
		WithPlatform("linux"), WithWorkingDir(t.TempDir()))
	require.NoError(t, err)

	result, err := p.Build(context.Background(), "crucible", "gemini", "gemini-2.5-pro", cfg)
	require.NoError(t, err)

	// Original 5 stations should still be present.
	for _, station := range []string{"design", "plan", "inspect", "build", "review"} {
		assert.Contains(t, result, station, "prompt output missing enabled station %q", station)
	}
}

func TestCoderTemplateStructuredDispatchExamples(t *testing.T) {
	cfg := newStationTestConfig()

	p, err := NewPrompt("coder", readCoderTemplate(t),
		WithPlatform("linux"), WithWorkingDir(t.TempDir()))
	require.NoError(t, err)

	result, err := p.Build(context.Background(), "crucible", "gemini", "gemini-2.5-pro", cfg)
	require.NoError(t, err)

	// Verify tool_input section is present with structured input fields.
	assert.Contains(t, result, "<tool_input>", "prompt missing tool_input section")
	assert.Contains(t, result, "context_hints", "prompt missing context_hints field")
	assert.Contains(t, result, "success_criteria", "prompt missing success_criteria field")
	assert.Contains(t, result, "constraints", "prompt missing constraints field")
	assert.Contains(t, result, "task_description", "prompt missing task_description field")
}

func TestCoderTemplateNoExcessiveBlankLines(t *testing.T) {
	cfg := newStationTestConfig()

	p, err := NewPrompt("coder", readCoderTemplate(t),
		WithPlatform("linux"), WithWorkingDir(t.TempDir()))
	require.NoError(t, err)

	result, err := p.Build(context.Background(), "crucible", "gemini", "gemini-2.5-pro", cfg)
	require.NoError(t, err)

	lines := strings.Split(result, "\n")
	consecutiveEmpty := 0
	maxConsecutiveEmpty := 0
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			consecutiveEmpty++
			if consecutiveEmpty > maxConsecutiveEmpty {
				maxConsecutiveEmpty = consecutiveEmpty
			}
		} else {
			consecutiveEmpty = 0
		}
	}
	assert.LessOrEqual(t, maxConsecutiveEmpty, 3,
		"template should not produce more than 3 consecutive blank lines, got %d", maxConsecutiveEmpty)
}
