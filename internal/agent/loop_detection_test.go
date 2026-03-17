package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoopDetector(t *testing.T) {
	t.Run("no loop with different tools", func(t *testing.T) {
		ld := &loopDetector{}
		tools := []string{"read_file", "write_file", "search", "read_file", "list_dir"}
		for _, tool := range tools {
			assert.False(t, ld.track(tool), "should not detect loop for %s", tool)
		}
	})

	t.Run("detects consecutive identical calls", func(t *testing.T) {
		ld := &loopDetector{}
		for i := 0; i < maxConsecutiveToolCalls-1; i++ {
			assert.False(t, ld.track("read_file"), "should not trigger at call %d", i+1)
		}
		assert.True(t, ld.track("read_file"), "should trigger at call %d", maxConsecutiveToolCalls)
	})

	t.Run("resets count on different tool", func(t *testing.T) {
		ld := &loopDetector{}
		for i := 0; i < maxConsecutiveToolCalls-1; i++ {
			ld.track("read_file")
		}
		// Different tool resets the counter.
		assert.False(t, ld.track("write_file"))
		// New tool starts from 1.
		for i := 0; i < maxConsecutiveToolCalls-2; i++ {
			assert.False(t, ld.track("write_file"))
		}
		assert.True(t, ld.track("write_file"))
	})

	t.Run("continues reporting after detection", func(t *testing.T) {
		ld := &loopDetector{}
		for i := 0; i < maxConsecutiveToolCalls; i++ {
			ld.track("read_file")
		}
		// Further calls still report loop.
		assert.True(t, ld.track("read_file"))
	})

	t.Run("total call cap triggers on alternating tools", func(t *testing.T) {
		ld := &loopDetector{}
		// Alternate between two tools — consecutive detector won't fire.
		for i := 0; i < maxTotalToolCalls-1; i++ {
			tool := "tool_a"
			if i%2 == 1 {
				tool = "tool_b"
			}
			assert.False(t, ld.track(tool), "should not trigger at total call %d", i+1)
		}
		// The 50th call should trigger.
		assert.True(t, ld.track("tool_a"), "should trigger at total call %d", maxTotalToolCalls)
	})

	t.Run("tracks total calls across resets", func(t *testing.T) {
		ld := &loopDetector{}
		// Make calls below consecutive threshold, switching tools.
		for i := 0; i < 3; i++ {
			ld.track("read_file")
		}
		ld.track("write_file")
		assert.Equal(t, 4, ld.totalCalls)
		assert.Equal(t, 1, ld.count) // consecutive reset to 1
	})
}
