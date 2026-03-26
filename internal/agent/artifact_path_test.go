package agent

import (
	"encoding/json"
	"testing"
	"time"
)

func TestPendingFileOpConfirm(t *testing.T) {
	t.Run("write confirmed on success", func(t *testing.T) {
		buf := NewContextBuffer("task")
		buf.StartTurn()
		buf.RecordToolStart("Write", `{"file_path":"/plans/spec.md","content":"x"}`)
		buf.ConfirmPendingFileOp("Write")
		buf.FinalizeTurn(time.Second)
		if len(buf.turns[0].Files) != 1 {
			t.Fatalf("expected 1 file op, got %d", len(buf.turns[0].Files))
		}
		f := buf.turns[0].Files[0]
		if f.Path != "/plans/spec.md" || f.Op != fileOpWrite {
			t.Errorf("got {%q, %q}, want {/plans/spec.md, write}", f.Path, f.Op)
		}
	})
	t.Run("edit confirmed on success", func(t *testing.T) {
		buf := NewContextBuffer("task")
		buf.StartTurn()
		buf.RecordToolStart("Edit", `{"file_path":"/src/main.go"}`)
		buf.ConfirmPendingFileOp("Edit")
		buf.FinalizeTurn(time.Second)
		if len(buf.turns[0].Files) != 1 || buf.turns[0].Files[0].Op != fileOpEdit {
			t.Error("expected 1 edit file op")
		}
	})
	t.Run("cleared on failure", func(t *testing.T) {
		buf := NewContextBuffer("task")
		buf.StartTurn()
		buf.RecordToolStart("Write", `{"file_path":"/plans/spec.md","content":"x"}`)
		buf.ClearPendingFileOp("Write")
		buf.FinalizeTurn(time.Second)
		if len(buf.turns[0].Files) != 0 {
			t.Errorf("expected 0 file ops after clear, got %d", len(buf.turns[0].Files))
		}
	})
	t.Run("read does not create pending", func(t *testing.T) {
		buf := NewContextBuffer("task")
		buf.StartTurn()
		buf.RecordToolStart("Read", `{"file_path":"/a.go"}`)
		buf.ConfirmPendingFileOp("Read")
		buf.FinalizeTurn(time.Second)
		if len(buf.turns[0].Files) != 0 {
			t.Error("Read should not record file op")
		}
	})
	t.Run("malformed json skips pending", func(t *testing.T) {
		buf := NewContextBuffer("task")
		buf.StartTurn()
		buf.RecordToolStart("Write", `not json`)
		buf.ConfirmPendingFileOp("Write")
		buf.FinalizeTurn(time.Second)
		if len(buf.turns[0].Files) != 0 {
			t.Error("malformed json should not record file op")
		}
	})
	t.Run("empty file_path skips pending", func(t *testing.T) {
		buf := NewContextBuffer("task")
		buf.StartTurn()
		buf.RecordToolStart("Write", `{"file_path":""}`)
		buf.ConfirmPendingFileOp("Write")
		buf.FinalizeTurn(time.Second)
		if len(buf.turns[0].Files) != 0 {
			t.Error("empty file_path should not record file op")
		}
	})
}

func TestLastWrittenMD(t *testing.T) {
	t.Run("empty when no writes", func(t *testing.T) {
		buf := NewContextBuffer("task")
		buf.StartTurn()
		buf.FinalizeTurn(time.Second)
		if got := buf.LastWrittenMD(); got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})
	t.Run("returns last md write", func(t *testing.T) {
		buf := NewContextBuffer("task")
		buf.StartTurn()
		buf.RecordToolStart("Write", `{"file_path":"/plans/first.md"}`)
		buf.ConfirmPendingFileOp("Write")
		buf.RecordToolStart("Write", `{"file_path":"/plans/second.md"}`)
		buf.ConfirmPendingFileOp("Write")
		buf.FinalizeTurn(time.Second)
		if got := buf.LastWrittenMD(); got != "/plans/second.md" {
			t.Errorf("got %q, want /plans/second.md", got)
		}
	})
	t.Run("ignores non-md writes", func(t *testing.T) {
		buf := NewContextBuffer("task")
		buf.StartTurn()
		buf.RecordToolStart("Write", `{"file_path":"/src/main.go"}`)
		buf.ConfirmPendingFileOp("Write")
		buf.FinalizeTurn(time.Second)
		if got := buf.LastWrittenMD(); got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})
	t.Run("ignores edits", func(t *testing.T) {
		buf := NewContextBuffer("task")
		buf.StartTurn()
		buf.RecordToolStart("Edit", `{"file_path":"/plans/plan.md"}`)
		buf.ConfirmPendingFileOp("Edit")
		buf.FinalizeTurn(time.Second)
		if got := buf.LastWrittenMD(); got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})
	t.Run("spans multiple turns", func(t *testing.T) {
		buf := NewContextBuffer("task")
		buf.StartTurn()
		buf.RecordToolStart("Write", `{"file_path":"/plans/t1.md"}`)
		buf.ConfirmPendingFileOp("Write")
		buf.FinalizeTurn(time.Second)
		buf.StartTurn()
		buf.RecordToolStart("Write", `{"file_path":"/plans/t2.md"}`)
		buf.ConfirmPendingFileOp("Write")
		buf.FinalizeTurn(time.Second)
		if got := buf.LastWrittenMD(); got != "/plans/t2.md" {
			t.Errorf("got %q, want /plans/t2.md", got)
		}
	})
}

func TestResolveArtifactPath(t *testing.T) {
	t.Run("empty returns empty", func(t *testing.T) {
		if got := resolveArtifactPath("", "/cwd"); got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})
	t.Run("absolute path unchanged", func(t *testing.T) {
		got := resolveArtifactPath("/home/user/.claude/plans/plan.md", "/cwd")
		if got != "/home/user/.claude/plans/plan.md" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("relative path resolved against cwd", func(t *testing.T) {
		got := resolveArtifactPath(".claude/plans/plan.md", "/home/user/project")
		if got != "/home/user/project/.claude/plans/plan.md" {
			t.Errorf("got %q", got)
		}
	})
}

func TestIsToolResultError(t *testing.T) {
	tests := []struct {
		name string
		raw  json.RawMessage
		want bool
	}{
		{"nil", nil, false},
		{"empty object", json.RawMessage(`{}`), false},
		{"is_error true", json.RawMessage(`{"is_error":true}`), true},
		{"is_error false", json.RawMessage(`{"is_error":false}`), false},
		{"status error", json.RawMessage(`{"status":"error"}`), true},
		{"status success", json.RawMessage(`{"status":"success"}`), false},
		{"malformed json", json.RawMessage(`not json`), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isToolResultError(tt.raw); got != tt.want {
				t.Errorf("isToolResultError() = %v, want %v", got, tt.want)
			}
		})
	}
}
