package chat

import (
	"testing"

	"github.com/dmora/crucible/internal/agent"
	"github.com/dmora/crucible/internal/message"
)

func TestDeriveOperatorState(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		activity   []agent.ProcessActivity
		phase      agent.ProcessPhase
		toolStatus ToolStatus
		resultErr  bool
		want       agent.OperatorState
	}{
		{
			name:       "ToolStatus permission overrides everything",
			activity:   []agent.ProcessActivity{{Kind: agent.ActivityTool, Name: "Read"}},
			phase:      agent.PhaseGenerating,
			toolStatus: ToolStatusAwaitingPermission,
			want:       agent.OpStateWaitingPermission,
		},
		{
			name:       "ToolStatus canceled",
			toolStatus: ToolStatusCanceled,
			want:       agent.OpStateCanceled,
		},
		{
			name:       "ToolStatus error",
			toolStatus: ToolStatusError,
			want:       agent.OpStateFailed,
		},
		{
			name:       "ToolStatus success",
			toolStatus: ToolStatusSuccess,
			want:       agent.OpStateDone,
		},
		{
			name:       "resultErr overrides phase",
			phase:      agent.PhaseThinking,
			toolStatus: ToolStatusRunning,
			resultErr:  true,
			want:       agent.OpStateFailed,
		},
		{
			name:       "phase thinking",
			phase:      agent.PhaseThinking,
			toolStatus: ToolStatusRunning,
			want:       agent.OpStateThinking,
		},
		{
			name:       "empty activity",
			toolStatus: ToolStatusRunning,
			want:       agent.OpStateIdle,
		},
		{
			name:       "last activity Read",
			activity:   []agent.ProcessActivity{{Kind: agent.ActivityTool, Name: "Read"}},
			toolStatus: ToolStatusRunning,
			want:       agent.OpStateReading,
		},
		{
			name:       "last activity Edit",
			activity:   []agent.ProcessActivity{{Kind: agent.ActivityTool, Name: "Edit"}},
			toolStatus: ToolStatusRunning,
			want:       agent.OpStateEditing,
		},
		{
			name:       "last activity Bash",
			activity:   []agent.ProcessActivity{{Kind: agent.ActivityTool, Name: "Bash", Detail: "ls"}},
			toolStatus: ToolStatusRunning,
			want:       agent.OpStateRunning,
		},
		{
			name:       "last activity Bash test",
			activity:   []agent.ProcessActivity{{Kind: agent.ActivityTool, Name: "Bash", Detail: "go test ./..."}},
			toolStatus: ToolStatusRunning,
			want:       agent.OpStateTesting,
		},
		{
			name:       "last activity grep",
			activity:   []agent.ProcessActivity{{Kind: agent.ActivityTool, Name: "grep"}},
			toolStatus: ToolStatusRunning,
			want:       agent.OpStateSearching,
		},
		{
			name:       "last activity error",
			activity:   []agent.ProcessActivity{{Kind: agent.ActivityError, Name: "Bash"}},
			toolStatus: ToolStatusRunning,
			want:       agent.OpStateFailed,
		},
		{
			name:       "last activity thinking",
			activity:   []agent.ProcessActivity{{Kind: agent.ActivityThinking}},
			toolStatus: ToolStatusRunning,
			want:       agent.OpStateThinking,
		},
		{
			name:       "uses last activity not first",
			activity:   []agent.ProcessActivity{{Kind: agent.ActivityTool, Name: "Read"}, {Kind: agent.ActivityTool, Name: "Edit"}},
			toolStatus: ToolStatusRunning,
			want:       agent.OpStateEditing,
		},
		// ACP human-readable tool titles.
		{
			name:       "ACP read title",
			activity:   []agent.ProcessActivity{{Kind: agent.ActivityTool, Name: "Read file contents"}},
			toolStatus: ToolStatusRunning,
			want:       agent.OpStateReading,
		},
		{
			name:       "ACP edit title",
			activity:   []agent.ProcessActivity{{Kind: agent.ActivityTool, Name: "Edit existing file"}},
			toolStatus: ToolStatusRunning,
			want:       agent.OpStateEditing,
		},
		{
			name:       "ACP search title",
			activity:   []agent.ProcessActivity{{Kind: agent.ActivityTool, Name: "Search codebase for pattern"}},
			toolStatus: ToolStatusRunning,
			want:       agent.OpStateSearching,
		},
		{
			name:       "ACP test title",
			activity:   []agent.ProcessActivity{{Kind: agent.ActivityTool, Name: "Run test suite for package"}},
			toolStatus: ToolStatusRunning,
			want:       agent.OpStateTesting,
		},
		{
			name:       "ACP generic long name",
			activity:   []agent.ProcessActivity{{Kind: agent.ActivityTool, Name: "Analyze project dependencies"}},
			toolStatus: ToolStatusRunning,
			want:       agent.OpStateRunning,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := DeriveOperatorState(tt.activity, tt.phase, tt.toolStatus, tt.resultErr)
			if got != tt.want {
				t.Errorf("DeriveOperatorState() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTranslateActivity(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		act  agent.ProcessActivity
		want string
	}{
		{"read file", agent.ProcessActivity{Kind: agent.ActivityTool, Name: "Read", Detail: "/src/main.go"}, "Reading main.go"},
		{"view file", agent.ProcessActivity{Kind: agent.ActivityTool, Name: "view", Detail: "/src/util.go"}, "Reading util.go"},
		{"edit file", agent.ProcessActivity{Kind: agent.ActivityTool, Name: "Edit", Detail: "/src/parse.go"}, "Editing parse.go"},
		{"write file", agent.ProcessActivity{Kind: agent.ActivityTool, Name: "Write", Detail: "/src/new.go"}, "Creating new.go"},
		{"multiedit", agent.ProcessActivity{Kind: agent.ActivityTool, Name: "multiedit", Detail: "/src/x.go"}, "Editing x.go"},
		{"bash test", agent.ProcessActivity{Kind: agent.ActivityTool, Name: "Bash", Detail: "go test ./..."}, "Running tests"},
		{"bash git", agent.ProcessActivity{Kind: agent.ActivityTool, Name: "Bash", Detail: "git status"}, "Running git status"},
		{"bash install", agent.ProcessActivity{Kind: agent.ActivityTool, Name: "Bash", Detail: "npm install"}, "Installing deps"},
		{"bash build", agent.ProcessActivity{Kind: agent.ActivityTool, Name: "Bash", Detail: "go build ./..."}, "Building"},
		{"bash default", agent.ProcessActivity{Kind: agent.ActivityTool, Name: "Bash", Detail: "ls -la"}, "Running: ls -la"},
		{"grep", agent.ProcessActivity{Kind: agent.ActivityTool, Name: "grep", Detail: "TODO"}, "Searching for TODO"},
		{"glob", agent.ProcessActivity{Kind: agent.ActivityTool, Name: "glob", Detail: "**/*.go"}, "Searching for **/*.go"},
		{"ls", agent.ProcessActivity{Kind: agent.ActivityTool, Name: "ls", Detail: "/src"}, "Listing src"},
		{"web_search", agent.ProcessActivity{Kind: agent.ActivityTool, Name: "web_search", Detail: "golang generics"}, "Searching: golang generics"},
		{"todos", agent.ProcessActivity{Kind: agent.ActivityTool, Name: "todos"}, "Updating tasks"},
		{"agent", agent.ProcessActivity{Kind: agent.ActivityTool, Name: "agent"}, "Delegating subtask"},
		{"mcp", agent.ProcessActivity{Kind: agent.ActivityTool, Name: "mcp_slack_send"}, "Using mcp_slack_send"},
		{"error with code", agent.ProcessActivity{Kind: agent.ActivityError, Name: "Bash"}, "Error: Bash"},
		{"error with detail", agent.ProcessActivity{Kind: agent.ActivityError, Name: "Error", Detail: "connection refused"}, "Error: connection refused"},
		{"error detail preferred over name", agent.ProcessActivity{Kind: agent.ActivityError, Name: "timeout", Detail: "request timed out after 30s"}, "Error: request timed out after 30s"},
		{"error no detail falls back to name", agent.ProcessActivity{Kind: agent.ActivityError, Name: "PermissionDenied"}, "Error: PermissionDenied"},
		{"error long detail truncated", agent.ProcessActivity{Kind: agent.ActivityError, Name: "Error", Detail: "This is a very long error message that exceeds the eighty character limit and should be truncated"}, "Error: This is a very long error message that exceeds the eighty character limit and s…"},
		{"thinking empty", agent.ProcessActivity{Kind: agent.ActivityThinking}, "Thinking"},
		{"thinking detail", agent.ProcessActivity{Kind: agent.ActivityThinking, Detail: "Short thought"}, "Short thought"},
		{"read no detail", agent.ProcessActivity{Kind: agent.ActivityTool, Name: "Read"}, "Reading file"},
		{"truncated path", agent.ProcessActivity{Kind: agent.ActivityTool, Name: "Read", Detail: "…/deeply/nested/file.go"}, "Reading file.go"},
		{"empty detail bash", agent.ProcessActivity{Kind: agent.ActivityTool, Name: "Bash", Detail: ""}, "Running command"},
		// ACP human-readable tool titles passed through as-is.
		{"ACP read title", agent.ProcessActivity{Kind: agent.ActivityTool, Name: "Read file contents"}, "Read file contents"},
		{"ACP long title", agent.ProcessActivity{Kind: agent.ActivityTool, Name: "Analyze project dependencies and report"}, "Analyze project dependencies and report"},
		{"ACP title truncated", agent.ProcessActivity{Kind: agent.ActivityTool, Name: "This is an extremely long human readable tool title that exceeds fifty chars"}, "This is an extremely long human readable tool tit…"},
		{"ACP title detail ignored", agent.ProcessActivity{Kind: agent.ActivityTool, Name: "Search codebase for pattern", Detail: "/src"}, "Search codebase for pattern"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := translateActivity(tt.act)
			if got != tt.want {
				t.Errorf("translateActivity() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSummarizeActivity(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		acts []agent.ProcessActivity
		want []string
	}{
		{
			name: "3+ reads collapse",
			acts: []agent.ProcessActivity{
				{Kind: agent.ActivityTool, Name: "Read", Detail: "/a.go"},
				{Kind: agent.ActivityTool, Name: "Read", Detail: "/b.go"},
				{Kind: agent.ActivityTool, Name: "Read", Detail: "/c.go"},
			},
			want: []string{"Reading 3 files"},
		},
		{
			name: "2 reads no collapse",
			acts: []agent.ProcessActivity{
				{Kind: agent.ActivityTool, Name: "Read", Detail: "/a.go"},
				{Kind: agent.ActivityTool, Name: "Read", Detail: "/b.go"},
			},
			want: []string{"Reading a.go", "Reading b.go"},
		},
		{
			name: "same-file edits collapse",
			acts: []agent.ProcessActivity{
				{Kind: agent.ActivityTool, Name: "Edit", Detail: "/src/parse.go"},
				{Kind: agent.ActivityTool, Name: "Edit", Detail: "/src/parse.go"},
			},
			want: []string{"Editing parse.go (2 changes)"},
		},
		{
			name: "mixed sequence",
			acts: []agent.ProcessActivity{
				{Kind: agent.ActivityTool, Name: "Read", Detail: "/a.go"},
				{Kind: agent.ActivityTool, Name: "Edit", Detail: "/b.go"},
				{Kind: agent.ActivityTool, Name: "Bash", Detail: "go test ./..."},
			},
			want: []string{"Reading a.go", "Editing b.go", "Running tests"},
		},
		{
			name: "empty",
			acts: nil,
			want: nil,
		},
		{
			name: "3+ searches collapse",
			acts: []agent.ProcessActivity{
				{Kind: agent.ActivityTool, Name: "grep", Detail: "foo"},
				{Kind: agent.ActivityTool, Name: "glob", Detail: "*.go"},
				{Kind: agent.ActivityTool, Name: "grep", Detail: "bar"},
			},
			want: []string{"Searching (3 queries)"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := summarizeActivity(tt.acts)
			if len(got) != len(tt.want) {
				t.Fatalf("summarizeActivity() len = %d, want %d\ngot: %v", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("summarizeActivity()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestComputeSummary(t *testing.T) {
	t.Parallel()
	activity := []agent.ProcessActivity{
		{Kind: agent.ActivityTool, Name: "Read", Detail: "/src/a.go"},
		{Kind: agent.ActivityTool, Name: "Read", Detail: "/src/b.go"},
		{Kind: agent.ActivityTool, Name: "Read", Detail: "/src/a.go"}, // duplicate
		{Kind: agent.ActivityTool, Name: "Edit", Detail: "/src/c.go"},
		{Kind: agent.ActivityTool, Name: "Bash", Detail: "go test ./..."},
		{Kind: agent.ActivityTool, Name: "Bash", Detail: "ls"},
		{Kind: agent.ActivityError, Name: "Write", Detail: "fail"},
	}
	result := &message.ToolResult{Content: "All tasks completed successfully."}
	s := ComputeSummary(activity, agent.PhaseIdle, ToolStatusSuccess, result)

	if s.State != agent.OpStateDone {
		t.Errorf("State = %q, want Done", s.State)
	}
	if s.FilesRead != 2 { // a.go + b.go (deduped)
		t.Errorf("FilesRead = %d, want 2", s.FilesRead)
	}
	if s.FilesEdited != 1 {
		t.Errorf("FilesEdited = %d, want 1", s.FilesEdited)
	}
	if s.CommandsRun != 2 {
		t.Errorf("CommandsRun = %d, want 2", s.CommandsRun)
	}
	if s.TestsRun != 1 {
		t.Errorf("TestsRun = %d, want 1", s.TestsRun)
	}
	if s.Errors != 1 {
		t.Errorf("Errors = %d, want 1", s.Errors)
	}
	if s.ResultLine != "All tasks completed successfully." {
		t.Errorf("ResultLine = %q, want %q", s.ResultLine, "All tasks completed successfully.")
	}
}

func TestComputeSummaryACP(t *testing.T) {
	t.Parallel()
	activity := []agent.ProcessActivity{
		{Kind: agent.ActivityTool, Name: "Read file contents"},
		{Kind: agent.ActivityTool, Name: "Read file contents"},
		{Kind: agent.ActivityTool, Name: "Edit existing file"},
		{Kind: agent.ActivityTool, Name: "Run test suite for package"},
		{Kind: agent.ActivityTool, Name: "Analyze project dependencies"},
	}
	s := ComputeSummary(activity, agent.PhaseIdle, ToolStatusRunning, nil)

	if s.FilesRead != 2 {
		t.Errorf("FilesRead = %d, want 2", s.FilesRead)
	}
	if s.FilesEdited != 1 {
		t.Errorf("FilesEdited = %d, want 1", s.FilesEdited)
	}
	if s.TestsRun != 1 {
		t.Errorf("TestsRun = %d, want 1", s.TestsRun)
	}
	// "Run test suite" counts as a command + test; "Analyze" counts as a command.
	if s.CommandsRun != 2 {
		t.Errorf("CommandsRun = %d, want 2", s.CommandsRun)
	}
}

func TestExtractResultLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		result *message.ToolResult
		want   string
	}{
		{"nil result", nil, ""},
		{"empty content", &message.ToolResult{Content: ""}, ""},
		{"normal content", &message.ToolResult{Content: "All tests pass."}, "All tests pass."},
		{"skips blank lines", &message.ToolResult{Content: "\n\n  \nReal content here."}, "Real content here."},
		{"skips markdown heading", &message.ToolResult{Content: "## Summary\n\nActual content."}, "Actual content."},
		{"strips leading formatting", &message.ToolResult{Content: "- List item here"}, "List item here"},
		{
			"truncates long lines",
			&message.ToolResult{Content: "This is a very long line that should be truncated because it exceeds eighty characters total length in its output"},
			"This is a very long line that should be truncated because it exceeds eighty c…",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractResultLine(tt.result)
			if got != tt.want {
				t.Errorf("extractResultLine() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsHumanReadableTitle(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		want bool
	}{
		// Machine names (short, no spaces).
		{"Read", false},
		{"Edit", false},
		{"Bash", false},
		{"grep", false},
		{"web_search", false},
		{"mcp_slack_send", false},
		// Human-readable (has spaces).
		{"Read file contents", true},
		{"Edit existing file", true},
		{"Search codebase for pattern", true},
		// Human-readable (long, no spaces).
		{"analyze_project_dependencies_report", true}, // >20 chars
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isHumanReadableTitle(tt.name); got != tt.want {
				t.Errorf("isHumanReadableTitle(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestStateChipStyle(t *testing.T) {
	t.Parallel()
	sty := testStyles()

	// Just verify no panics and that different states return different styles.
	failedStyle := stateChipStyle(sty, agent.OpStateFailed)
	warningStyle := stateChipStyle(sty, agent.OpStateWaitingPermission)
	idleStyle := stateChipStyle(sty, agent.OpStateIdle)

	// Failed should use StationChipError (redDark fg), different from idle (subtle).
	if failedStyle.GetForeground() == idleStyle.GetForeground() {
		t.Error("Failed style should differ from idle style")
	}
	// Warning should use StationChipWarning (warning fg), different from idle.
	if warningStyle.GetForeground() == idleStyle.GetForeground() {
		t.Error("Warning style should differ from idle style")
	}
}
