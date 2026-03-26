package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetGetActiveArtifact(t *testing.T) {
	tctx := newMockToolContext()

	setActiveArtifact(tctx, "plan", "/tmp/spec.md", 3)

	art, err := getActiveArtifact(tctx, "plan")
	require.NoError(t, err)
	require.NotNil(t, art)
	assert.Equal(t, "/tmp/spec.md", art.Path)
	assert.Equal(t, "plan", art.Station)
	assert.Equal(t, 3, art.Seq)
	assert.Equal(t, 1, art.Version)
}

func TestActiveArtifact_VersionIncrement(t *testing.T) {
	tctx := newMockToolContext()

	setActiveArtifact(tctx, "plan", "/tmp/v1.md", 1)
	setActiveArtifact(tctx, "plan", "/tmp/v2.md", 5)

	art, err := getActiveArtifact(tctx, "plan")
	require.NoError(t, err)
	require.NotNil(t, art)
	assert.Equal(t, "/tmp/v2.md", art.Path)
	assert.Equal(t, 5, art.Seq)
	assert.Equal(t, 2, art.Version)
}

func TestGetActiveArtifact_Missing(t *testing.T) {
	tctx := newMockToolContext()

	art, err := getActiveArtifact(tctx, "nonexistent")
	assert.NoError(t, err)
	assert.Nil(t, art)
}
