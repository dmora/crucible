package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlattenContextTiers(t *testing.T) {
	paths := FlattenContextTiers()

	// Should include paths from all 4 tiers.
	assert.Contains(t, paths, "CRUCIBLE.md")
	assert.Contains(t, paths, "AGENTS.md")
	assert.Contains(t, paths, "CLAUDE.md")
	assert.Contains(t, paths, "claude.md")
	assert.Contains(t, paths, ".github/copilot-instructions.md")
	assert.Contains(t, paths, ".cursorrules")
	assert.Contains(t, paths, "GEMINI.md")

	// Count total paths across all tiers.
	total := 0
	for _, tier := range DefaultContextTiers() {
		total += len(tier.Paths)
	}
	assert.Len(t, paths, total)
}

func TestDefaultContextTiers_ordering(t *testing.T) {
	tiers := DefaultContextTiers()
	require.Len(t, tiers, 4)

	// Tier 1: Crucible-specific
	assert.Contains(t, tiers[0].Paths, "CRUCIBLE.md")
	// Tier 2: AGENTS.md
	assert.Contains(t, tiers[1].Paths, "AGENTS.md")
	// Tier 3: Claude
	assert.Contains(t, tiers[2].Paths, "CLAUDE.md")
	// Tier 4: Community
	assert.Contains(t, tiers[3].Paths, ".cursorrules")
}

func TestContextPathsExist(t *testing.T) {
	t.Run("CLAUDE.md exists", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("x"), 0o644))

		exists, err := contextPathsExist(dir)
		require.NoError(t, err)
		assert.True(t, exists)
	})

	t.Run("CRUCIBLE.md exists", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "CRUCIBLE.md"), []byte("x"), 0o644))

		exists, err := contextPathsExist(dir)
		require.NoError(t, err)
		assert.True(t, exists)
	})

	t.Run("nested .github/copilot-instructions.md exists", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, ".github"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".github", "copilot-instructions.md"), []byte("x"), 0o644))

		exists, err := contextPathsExist(dir)
		require.NoError(t, err)
		assert.True(t, exists)
	})

	t.Run(".cursor/rules/ directory with files", func(t *testing.T) {
		dir := t.TempDir()
		rulesDir := filepath.Join(dir, ".cursor", "rules")
		require.NoError(t, os.MkdirAll(rulesDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(rulesDir, "my-rule.md"), []byte("x"), 0o644))

		exists, err := contextPathsExist(dir)
		require.NoError(t, err)
		assert.True(t, exists)
	})

	t.Run("empty .cursor/rules/ directory does not count", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, ".cursor", "rules"), 0o755))

		exists, err := contextPathsExist(dir)
		require.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("no context files", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("x"), 0o644))

		exists, err := contextPathsExist(dir)
		require.NoError(t, err)
		assert.False(t, exists)
	})
}
