// Package tools provides tool name constants and parameter/response types
// used by the UI layer for rendering tool calls and permission dialogs.
package tools

import "github.com/dmora/crucible/internal/session"

// Tool name constants.
const (
	BashToolName        = "bash"
	JobOutputToolName   = "job_output"
	JobKillToolName     = "job_kill"
	ViewToolName        = "view"
	WriteToolName       = "write"
	EditToolName        = "edit"
	MultiEditToolName   = "multiedit"
	GlobToolName        = "glob"
	GrepToolName        = "grep"
	LSToolName          = "ls"
	DownloadToolName    = "download"
	SourcegraphToolName = "sourcegraph"
	TodosToolName       = "todos"
	AskUserToolName     = "ask_user"
	ThoughtToolName     = "thought"
)

// BashNoOutput is the sentinel value for bash commands that produce no output.
const BashNoOutput = "no output"

// ViewResourceType represents the type of resource being viewed.
type ViewResourceType string

const (
	ViewResourceUnset ViewResourceType = ""
	ViewResourceSkill ViewResourceType = "skill"
)

// --- Param types (JSON-deserialized from tool call input for display) ---

// BashParams defines the parameters for the bash tool.
type BashParams struct {
	Description     string `json:"description"`
	Command         string `json:"command"`
	WorkingDir      string `json:"working_dir,omitempty"`
	RunInBackground bool   `json:"run_in_background,omitempty"`
}

// BashPermissionsParams defines the permission parameters for the bash tool.
type BashPermissionsParams struct {
	Command     string `json:"command"`
	Description string `json:"description,omitempty"`
	WorkingDir  string `json:"working_dir,omitempty"`
}

// ViewParams defines the parameters for the view tool.
type ViewParams struct {
	FilePath string `json:"file_path"`
	Offset   int    `json:"offset,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

// ViewPermissionsParams defines the permission parameters for the view tool.
type ViewPermissionsParams struct {
	FilePath string `json:"file_path"`
	Offset   int    `json:"offset,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

// WriteParams defines the parameters for the write tool.
type WriteParams struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

// WritePermissionsParams defines the permission parameters for the write tool.
type WritePermissionsParams struct {
	FilePath   string `json:"file_path"`
	OldContent string `json:"old_content,omitempty"`
	NewContent string `json:"new_content,omitempty"`
}

// EditParams defines the parameters for the edit tool.
type EditParams struct {
	FilePath   string `json:"file_path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

// EditPermissionsParams defines the permission parameters for the edit tool.
type EditPermissionsParams struct {
	FilePath   string `json:"file_path"`
	OldContent string `json:"old_content,omitempty"`
	NewContent string `json:"new_content,omitempty"`
}

// MultiEditOperation defines a single edit within a multi-edit.
type MultiEditOperation struct {
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

// MultiEditParams defines the parameters for the multiedit tool.
type MultiEditParams struct {
	FilePath string               `json:"file_path"`
	Edits    []MultiEditOperation `json:"edits"`
}

// MultiEditPermissionsParams defines the permission parameters for the multiedit tool.
type MultiEditPermissionsParams struct {
	FilePath   string `json:"file_path"`
	OldContent string `json:"old_content,omitempty"`
	NewContent string `json:"new_content,omitempty"`
}

// GlobParams defines the parameters for the glob tool.
type GlobParams struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
}

// GrepParams defines the parameters for the grep tool.
type GrepParams struct {
	Pattern     string `json:"pattern"`
	Path        string `json:"path,omitempty"`
	Include     string `json:"include,omitempty"`
	LiteralText bool   `json:"literal_text,omitempty"`
}

// LSParams defines the parameters for the ls tool.
type LSParams struct {
	Path   string   `json:"path,omitempty"`
	Ignore []string `json:"ignore,omitempty"`
	Depth  int      `json:"depth,omitempty"`
}

// LSPermissionsParams defines the permission parameters for the ls tool.
type LSPermissionsParams struct {
	Path   string   `json:"path,omitempty"`
	Ignore []string `json:"ignore,omitempty"`
}

// DownloadParams defines the parameters for the download tool.
type DownloadParams struct {
	URL      string `json:"url"`
	FilePath string `json:"file_path"`
	Timeout  int    `json:"timeout,omitempty"`
}

// DownloadPermissionsParams defines the permission parameters for the download tool.
type DownloadPermissionsParams struct {
	URL      string `json:"url"`
	FilePath string `json:"file_path"`
	Timeout  int    `json:"timeout,omitempty"`
}

// SourcegraphParams defines the parameters for the sourcegraph tool.
type SourcegraphParams struct {
	Query         string `json:"query"`
	Count         int    `json:"count,omitempty"`
	ContextWindow int    `json:"context_window,omitempty"`
	Timeout       int    `json:"timeout,omitempty"`
}

// JobOutputParams defines the parameters for the job_output tool.
type JobOutputParams struct {
	ShellID string `json:"shell_id"`
}

// JobKillParams defines the parameters for the job_kill tool.
type JobKillParams struct {
	ShellID string `json:"shell_id"`
}

// TodoItem represents a single todo item.
type TodoItem struct {
	Content    string `json:"content"`
	Status     string `json:"status"`
	ActiveForm string `json:"active_form"`
}

// TodosParams defines the parameters for the todos tool.
type TodosParams struct {
	Todos []TodoItem `json:"todos"`
}

// ThoughtParams defines the parameters for the thought tool.
type ThoughtParams struct {
	Reasoning  string `json:"reasoning"`
	NextAction string `json:"next_action,omitempty"`
	IsRevision bool   `json:"is_revision,omitempty"`
}

// --- Response metadata types (JSON-deserialized from tool result metadata for display) ---

// BashResponseMetadata contains metadata about a bash command execution.
type BashResponseMetadata struct {
	StartTime        int64  `json:"start_time"`
	EndTime          int64  `json:"end_time"`
	Output           string `json:"output"`
	Description      string `json:"description"`
	WorkingDirectory string `json:"working_directory"`
	Background       bool   `json:"background,omitempty"`
	ShellID          string `json:"shell_id,omitempty"`
}

// ViewResponseMetadata contains metadata about a view tool execution.
type ViewResponseMetadata struct {
	FilePath            string           `json:"file_path"`
	Content             string           `json:"content"`
	ResourceType        ViewResourceType `json:"resource_type,omitempty"`
	ResourceName        string           `json:"resource_name,omitempty"`
	ResourceDescription string           `json:"resource_description,omitempty"`
}

// WriteResponseMetadata contains metadata about a write tool execution.
type WriteResponseMetadata struct {
	Diff      string `json:"diff"`
	Additions int    `json:"additions"`
	Removals  int    `json:"removals"`
}

// EditResponseMetadata contains metadata about an edit tool execution.
type EditResponseMetadata struct {
	Additions  int    `json:"additions"`
	Removals   int    `json:"removals"`
	OldContent string `json:"old_content,omitempty"`
	NewContent string `json:"new_content,omitempty"`
}

// FailedEdit represents a failed edit operation in a multi-edit.
type FailedEdit struct {
	Index int                `json:"index"`
	Error string             `json:"error"`
	Edit  MultiEditOperation `json:"edit"`
}

// MultiEditResponseMetadata contains metadata about a multi-edit tool execution.
type MultiEditResponseMetadata struct {
	Additions    int          `json:"additions"`
	Removals     int          `json:"removals"`
	OldContent   string       `json:"old_content,omitempty"`
	NewContent   string       `json:"new_content,omitempty"`
	EditsApplied int          `json:"edits_applied"`
	EditsFailed  []FailedEdit `json:"edits_failed,omitempty"`
}

// JobOutputResponseMetadata contains metadata about a job output tool execution.
type JobOutputResponseMetadata struct {
	ShellID          string `json:"shell_id"`
	Command          string `json:"command"`
	Description      string `json:"description"`
	Done             bool   `json:"done"`
	WorkingDirectory string `json:"working_directory"`
}

// JobKillResponseMetadata contains metadata about a job kill tool execution.
type JobKillResponseMetadata struct {
	ShellID     string `json:"shell_id"`
	Command     string `json:"command"`
	Description string `json:"description"`
}

// TodosResponseMetadata contains metadata about a todos tool execution.
type TodosResponseMetadata struct {
	IsNew         bool           `json:"is_new"`
	Todos         []session.Todo `json:"todos"`
	JustCompleted []string       `json:"just_completed,omitempty"`
	JustStarted   string         `json:"just_started,omitempty"`
	Completed     int            `json:"completed"`
	Total         int            `json:"total"`
}
