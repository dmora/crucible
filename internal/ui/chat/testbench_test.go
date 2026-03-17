package chat

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"
	"github.com/dmora/crucible/internal/agent"
	"github.com/dmora/crucible/internal/message"
	"github.com/dmora/crucible/internal/ui/styles"
)

func testStyles() *styles.Styles {
	s := styles.NewStyles(styles.DefaultTheme, false)
	return &s
}

func TestStationToolTreeAlignment(t *testing.T) {
	t.Parallel()
	sty := testStyles()

	activity := []agent.ProcessActivity{
		{Kind: agent.ActivityThinking, Detail: "Analyzing the repository structure and understanding how the modules connect together"},
		{Kind: agent.ActivityTool, Name: "Read", Detail: "/src/internal/ui/chat/testbench.go"},
		{Kind: agent.ActivityTool, Name: "Bash", Detail: "go build ./..."},
		{Kind: agent.ActivityError, Name: "Write", Detail: "permission denied: cannot write to read-only filesystem path"},
		{Kind: agent.ActivityThinking, Detail: "Short thought"},
	}

	toolCall := message.ToolCall{
		ID:    "tc_1",
		Name:  "draft",
		Input: `{"task":"Implement the new feature for handling user authentication with OAuth2 tokens"}`,
		State: message.ToolStateDone,
	}
	result := &message.ToolResult{
		ToolCallID: "tc_1",
		Name:       "draft",
		Content:    "",
	}

	for _, width := range []int{60, 100} {
		t.Run(widthName(width), func(t *testing.T) {
			t.Parallel()

			st := &StationToolMessageItem{
				stationName: "Draft",
				activity:    activity,
			}
			rc := &stationToolRenderContext{st: st}

			opts := &ToolRenderOpts{
				ToolCall:        toolCall,
				Result:          result,
				ExpandedContent: true,
				Status:          ToolStatusSuccess,
			}

			output := rc.RenderTool(sty, width, opts)
			golden.RequireEqual(t, []byte(output))
			assertMaxLineWidth(t, width, output)
		})
	}
}

func TestStationCardRunningCompact(t *testing.T) {
	t.Parallel()
	sty := testStyles()

	activity := []agent.ProcessActivity{
		{Kind: agent.ActivityTool, Name: "Read", Detail: "/src/main.go"},
		{Kind: agent.ActivityTool, Name: "Read", Detail: "/src/util.go"},
		{Kind: agent.ActivityTool, Name: "Edit", Detail: "/src/parse.go"},
		{Kind: agent.ActivityTool, Name: "Bash", Detail: "go test ./..."},
	}

	toolCall := message.ToolCall{
		ID:    "tc_rc",
		Name:  "draft",
		Input: `{"task":"Implement parser"}`,
		State: message.ToolStatePending,
	}

	for _, width := range []int{60, 100} {
		t.Run(widthName(width), func(t *testing.T) {
			t.Parallel()

			st := &StationToolMessageItem{
				stationName: "Draft",
				activity:    activity,
				phase:       agent.PhaseThinking,
			}
			rc := &stationToolRenderContext{st: st}

			opts := &ToolRenderOpts{
				ToolCall: toolCall,
				Compact:  true,
				Status:   ToolStatusRunning,
			}

			output := rc.RenderTool(sty, width, opts)
			golden.RequireEqual(t, []byte(output))
			assertMaxLineWidth(t, width, output)
		})
	}
}

func TestStationCardCompleted(t *testing.T) {
	t.Parallel()
	sty := testStyles()

	activity := []agent.ProcessActivity{
		{Kind: agent.ActivityTool, Name: "Read", Detail: "/src/a.go"},
		{Kind: agent.ActivityTool, Name: "Read", Detail: "/src/b.go"},
		{Kind: agent.ActivityTool, Name: "Read", Detail: "/src/c.go"},
		{Kind: agent.ActivityTool, Name: "Read", Detail: "/src/d.go"},
		{Kind: agent.ActivityTool, Name: "Read", Detail: "/src/e.go"},
		{Kind: agent.ActivityTool, Name: "Edit", Detail: "/src/parse.go"},
		{Kind: agent.ActivityTool, Name: "Edit", Detail: "/src/parse.go"},
		{Kind: agent.ActivityTool, Name: "Bash", Detail: "go test ./..."},
		{Kind: agent.ActivityTool, Name: "Bash", Detail: "go build ./..."},
		{Kind: agent.ActivityTool, Name: "Bash", Detail: "go vet ./..."},
	}

	toolCall := message.ToolCall{
		ID:    "tc_comp",
		Name:  "draft",
		Input: `{"task":"Implement the parser module for the new grammar"}`,
		State: message.ToolStateDone,
	}
	result := &message.ToolResult{
		ToolCallID: "tc_comp",
		Name:       "draft",
		Content:    "## Summary\n\nImplemented parser module with full test coverage across all grammar rules.",
	}

	for _, width := range []int{60, 100} {
		t.Run(widthName(width), func(t *testing.T) {
			t.Parallel()

			st := &StationToolMessageItem{
				stationName: "Draft",
				activity:    activity,
			}
			rc := &stationToolRenderContext{st: st}

			opts := &ToolRenderOpts{
				ToolCall:        toolCall,
				Result:          result,
				ExpandedContent: true,
				Status:          ToolStatusSuccess,
			}

			output := rc.RenderTool(sty, width, opts)
			golden.RequireEqual(t, []byte(output))
			assertMaxLineWidth(t, width, output)
		})
	}
}

func TestStationCardCompletedCompact(t *testing.T) {
	t.Parallel()
	sty := testStyles()

	activity := []agent.ProcessActivity{
		{Kind: agent.ActivityTool, Name: "Read", Detail: "/src/a.go"},
		{Kind: agent.ActivityTool, Name: "Read", Detail: "/src/b.go"},
		{Kind: agent.ActivityTool, Name: "Edit", Detail: "/src/parse.go"},
		{Kind: agent.ActivityTool, Name: "Bash", Detail: "go test ./..."},
	}

	toolCall := message.ToolCall{
		ID:    "tc_cc",
		Name:  "draft",
		Input: `{"task":"Fix parser"}`,
		State: message.ToolStateDone,
	}
	result := &message.ToolResult{
		ToolCallID: "tc_cc",
		Name:       "draft",
		Content:    "Fixed.",
	}

	for _, width := range []int{60, 100} {
		t.Run(widthName(width), func(t *testing.T) {
			t.Parallel()

			st := &StationToolMessageItem{
				stationName: "Draft",
				activity:    activity,
			}
			rc := &stationToolRenderContext{st: st}

			opts := &ToolRenderOpts{
				ToolCall: toolCall,
				Result:   result,
				Compact:  true,
				Status:   ToolStatusSuccess,
			}

			output := rc.RenderTool(sty, width, opts)
			golden.RequireEqual(t, []byte(output))
			assertMaxLineWidth(t, width, output)
		})
	}
}

func TestStationCardFailed(t *testing.T) {
	t.Parallel()
	sty := testStyles()

	activity := []agent.ProcessActivity{
		{Kind: agent.ActivityTool, Name: "Read", Detail: "/src/grammar.go"},
		{Kind: agent.ActivityTool, Name: "Edit", Detail: "/src/grammar.go"},
		{Kind: agent.ActivityError, Name: "Bash", Detail: "exit code 1"},
		{Kind: agent.ActivityError, Name: "Write", Detail: "permission denied"},
	}

	toolCall := message.ToolCall{
		ID:    "tc_fail",
		Name:  "draft",
		Input: `{"task":"Implement the parser module for the new grammar"}`,
		State: message.ToolStateDone,
	}
	result := &message.ToolResult{
		ToolCallID: "tc_fail",
		Name:       "draft",
		Content:    "Error: could not resolve dependency cycle in grammar.go",
		IsError:    true,
	}

	for _, width := range []int{60, 100} {
		t.Run(widthName(width), func(t *testing.T) {
			t.Parallel()

			st := &StationToolMessageItem{
				stationName: "Draft",
				activity:    activity,
			}
			rc := &stationToolRenderContext{st: st}

			opts := &ToolRenderOpts{
				ToolCall:        toolCall,
				Result:          result,
				ExpandedContent: true,
				Status:          ToolStatusError,
			}

			output := rc.RenderTool(sty, width, opts)
			golden.RequireEqual(t, []byte(output))
			assertMaxLineWidth(t, width, output)
		})
	}
}

func TestStationCardFailedCompact(t *testing.T) {
	t.Parallel()
	sty := testStyles()

	activity := []agent.ProcessActivity{
		{Kind: agent.ActivityTool, Name: "Read", Detail: "/src/grammar.go"},
		{Kind: agent.ActivityError, Name: "Bash", Detail: "exit code 1"},
		{Kind: agent.ActivityError, Name: "Write", Detail: "permission denied"},
	}

	toolCall := message.ToolCall{
		ID:    "tc_fc",
		Name:  "draft",
		Input: `{"task":"Fix grammar"}`,
		State: message.ToolStateDone,
	}
	result := &message.ToolResult{
		ToolCallID: "tc_fc",
		Name:       "draft",
		Content:    "Failed.",
		IsError:    true,
	}

	for _, width := range []int{60, 100} {
		t.Run(widthName(width), func(t *testing.T) {
			t.Parallel()

			st := &StationToolMessageItem{
				stationName: "Draft",
				activity:    activity,
			}
			rc := &stationToolRenderContext{st: st}

			opts := &ToolRenderOpts{
				ToolCall: toolCall,
				Result:   result,
				Compact:  true,
				Status:   ToolStatusError,
			}

			output := rc.RenderTool(sty, width, opts)
			golden.RequireEqual(t, []byte(output))
			assertMaxLineWidth(t, width, output)
		})
	}
}

func TestStationCardCanceled(t *testing.T) {
	t.Parallel()
	sty := testStyles()

	activity := []agent.ProcessActivity{
		{Kind: agent.ActivityTool, Name: "Read", Detail: "/src/main.go"},
		{Kind: agent.ActivityTool, Name: "Read", Detail: "/src/util.go"},
	}

	toolCall := message.ToolCall{
		ID:    "tc_cancel",
		Name:  "draft",
		Input: `{"task":"Implement parser"}`,
		State: message.ToolStateDone,
	}

	for _, width := range []int{60, 100} {
		t.Run(widthName(width), func(t *testing.T) {
			t.Parallel()

			st := &StationToolMessageItem{
				stationName: "Draft",
				activity:    activity,
			}
			rc := &stationToolRenderContext{st: st}

			opts := &ToolRenderOpts{
				ToolCall:        toolCall,
				ExpandedContent: true,
				Status:          ToolStatusCanceled,
			}

			output := rc.RenderTool(sty, width, opts)
			golden.RequireEqual(t, []byte(output))
			assertMaxLineWidth(t, width, output)
		})
	}
}

func TestStationCardPermissionWaiting(t *testing.T) {
	t.Parallel()
	sty := testStyles()

	activity := []agent.ProcessActivity{
		{Kind: agent.ActivityTool, Name: "Read", Detail: "/src/main.go"},
		{Kind: agent.ActivityTool, Name: "Edit", Detail: "/src/parse.go"},
	}

	toolCall := message.ToolCall{
		ID:    "tc_perm",
		Name:  "draft",
		Input: `{"task":"Implement parser"}`,
		State: message.ToolStatePending,
	}

	for _, width := range []int{60, 100} {
		t.Run(widthName(width), func(t *testing.T) {
			t.Parallel()

			st := &StationToolMessageItem{
				stationName: "Draft",
				activity:    activity,
			}
			rc := &stationToolRenderContext{st: st}

			opts := &ToolRenderOpts{
				ToolCall:        toolCall,
				ExpandedContent: true,
				Status:          ToolStatusAwaitingPermission,
			}

			output := rc.RenderTool(sty, width, opts)
			golden.RequireEqual(t, []byte(output))
			assertMaxLineWidth(t, width, output)
		})
	}
}

func TestAgentToolTreeAlignment(t *testing.T) {
	t.Parallel()
	sty := testStyles()

	// Build nested compact tools.
	nestedTC1 := message.ToolCall{
		ID: "ntc_1", Name: "Read",
		Input: `{"path":"/src/very/long/file/path/that/may/cause/wrapping/issues.go"}`,
		State: message.ToolStateDone,
	}
	nestedResult1 := &message.ToolResult{
		ToolCallID: "ntc_1", Name: "Read", Content: "file content",
	}
	nested1 := NewGenericToolMessageItem(sty, nestedTC1, nestedResult1, false)
	nested1.(Compactable).SetCompact(true)

	nestedTC2 := message.ToolCall{
		ID: "ntc_2", Name: "Bash",
		Input: `{"command":"echo hello world"}`,
		State: message.ToolStateDone,
	}
	nestedResult2 := &message.ToolResult{
		ToolCallID: "ntc_2", Name: "Bash", Content: "hello world",
	}
	nested2 := NewGenericToolMessageItem(sty, nestedTC2, nestedResult2, false)
	nested2.(Compactable).SetCompact(true)

	agentItem := &AgentToolMessageItem{
		nestedTools: []ToolMessageItem{nested1, nested2},
	}

	toolCall := message.ToolCall{
		ID:    "tc_agent",
		Name:  "agent",
		Input: `{"prompt":"Search for all files related to authentication and analyze their structure"}`,
		State: message.ToolStateDone,
	}
	result := &message.ToolResult{
		ToolCallID: "tc_agent",
		Name:       "agent",
		Content:    "",
	}

	rc := &AgentToolRenderContext{agent: agentItem}

	opts := &ToolRenderOpts{
		ToolCall:        toolCall,
		Result:          result,
		ExpandedContent: true,
		Status:          ToolStatusSuccess,
	}

	t.Run("Width60", func(t *testing.T) {
		t.Parallel()
		output := rc.RenderTool(sty, 60, opts)
		golden.RequireEqual(t, []byte(output))
		assertMaxLineWidth(t, 60, output)
	})
}

func TestAgenticFetchToolTreeAlignment(t *testing.T) {
	t.Parallel()
	sty := testStyles()

	// Build nested compact tools.
	nestedTC1 := message.ToolCall{
		ID: "ntc_f1", Name: "Read",
		Input: `{"path":"/src/very/long/file/path/that/may/cause/wrapping/issues.go"}`,
		State: message.ToolStateDone,
	}
	nestedResult1 := &message.ToolResult{
		ToolCallID: "ntc_f1", Name: "Read", Content: "file content",
	}
	nested1 := NewGenericToolMessageItem(sty, nestedTC1, nestedResult1, false)
	nested1.(Compactable).SetCompact(true)

	nestedTC2 := message.ToolCall{
		ID: "ntc_f2", Name: "Bash",
		Input: `{"command":"echo hello world"}`,
		State: message.ToolStateDone,
	}
	nestedResult2 := &message.ToolResult{
		ToolCallID: "ntc_f2", Name: "Bash", Content: "hello world",
	}
	nested2 := NewGenericToolMessageItem(sty, nestedTC2, nestedResult2, false)
	nested2.(Compactable).SetCompact(true)

	fetchItem := &AgenticFetchToolMessageItem{
		nestedTools: []ToolMessageItem{nested1, nested2},
	}

	toolCall := message.ToolCall{
		ID:    "tc_fetch",
		Name:  "agentic_fetch",
		Input: `{"prompt":"Retrieve the API documentation and summarize the key endpoints","url":"https://example.com/api/docs"}`,
		State: message.ToolStateDone,
	}
	result := &message.ToolResult{
		ToolCallID: "tc_fetch",
		Name:       "agentic_fetch",
		Content:    "",
	}

	rc := &AgenticFetchToolRenderContext{fetch: fetchItem}

	opts := &ToolRenderOpts{
		ToolCall:        toolCall,
		Result:          result,
		ExpandedContent: true,
		Status:          ToolStatusSuccess,
	}

	t.Run("Width60", func(t *testing.T) {
		t.Parallel()
		output := rc.RenderTool(sty, 60, opts)
		golden.RequireEqual(t, []byte(output))
		assertMaxLineWidth(t, 60, output)
	})
}

// assertMaxLineWidth checks that no rendered line exceeds the given width.
func assertMaxLineWidth(t *testing.T, maxWidth int, output string) {
	t.Helper()
	for i, line := range strings.Split(output, "\n") {
		w := ansi.StringWidth(line)
		if w > maxWidth {
			t.Errorf("line %d width %d exceeds max %d: %q", i, w, maxWidth, line)
		}
	}
}

func widthName(w int) string {
	return fmt.Sprintf("Width%d", w)
}
