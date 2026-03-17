package agent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewThoughtTool_Creates(t *testing.T) {
	tt, err := newThoughtTool()
	require.NoError(t, err)
	assert.NotNil(t, tt)
}

func TestThoughtHandler_ValidReasoning(t *testing.T) {
	input := thoughtInput{Reasoning: "Task requires draft then build because the change is non-trivial."}
	output, err := thoughtHandler(input)
	require.NoError(t, err)
	assert.True(t, output.Acknowledged)
}

func TestThoughtHandler_EmptyReasoning(t *testing.T) {
	input := thoughtInput{Reasoning: "   "}
	_, err := thoughtHandler(input)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "reasoning is required")
}

func TestThoughtHandler_WithNextAction(t *testing.T) {
	input := thoughtInput{
		Reasoning:  "Multiple approaches available, picking simplest.",
		NextAction: "dispatch to draft",
	}
	output, err := thoughtHandler(input)
	require.NoError(t, err)
	assert.True(t, output.Acknowledged)
}

func TestThoughtHandler_WithIsRevision(t *testing.T) {
	input := thoughtInput{
		Reasoning:  "Revising after station returned unexpected error.",
		IsRevision: true,
	}
	output, err := thoughtHandler(input)
	require.NoError(t, err)
	assert.True(t, output.Acknowledged)
}

func TestRedactThoughtArgs_LongReasoning(t *testing.T) {
	long := strings.Repeat("x", 200)
	args := map[string]any{"reasoning": long, "next_action": "dispatch"}
	redacted := redactThoughtArgs(args)

	r, ok := redacted["reasoning"].(string)
	require.True(t, ok)
	assert.LessOrEqual(t, len(r), 100) // 80 + "…[truncated]"
	assert.Contains(t, r, "…[truncated]")
	// Other keys pass through.
	assert.Equal(t, "dispatch", redacted["next_action"])
}

func TestRedactThoughtArgs_ShortReasoning(t *testing.T) {
	args := map[string]any{"reasoning": "short"}
	redacted := redactThoughtArgs(args)
	assert.Equal(t, "short", redacted["reasoning"])
}
