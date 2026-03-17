package message

import (
	"fmt"
	"strings"
	"testing"
)

func makeTestAttachments(n int, contentSize int) []Attachment {
	attachments := make([]Attachment, n)
	content := []byte(strings.Repeat("x", contentSize))
	for i := range n {
		attachments[i] = Attachment{
			FilePath: fmt.Sprintf("/path/to/file%d.txt", i),
			MimeType: "text/plain",
			Content:  content,
		}
	}
	return attachments
}

func TestToolState_IsTerminal(t *testing.T) {
	tests := []struct {
		state    ToolState
		terminal bool
	}{
		{ToolStatePending, false},
		{ToolStateRunning, false},
		{ToolStateDone, true},
		{ToolStateCanceled, true},
		{"", false},
	}
	for _, tt := range tests {
		if got := tt.state.IsTerminal(); got != tt.terminal {
			t.Errorf("ToolState(%q).IsTerminal() = %v, want %v", tt.state, got, tt.terminal)
		}
	}
}

func TestFinishToolCall_SetsStateDone(t *testing.T) {
	msg := &Message{
		Parts: []ContentPart{
			ToolCall{ID: "tc1", Name: "read", State: ToolStatePending},
			ToolCall{ID: "tc2", Name: "write", State: ToolStateRunning},
		},
	}
	msg.FinishToolCall("tc1")

	tc1 := msg.ToolCalls()[0]
	if tc1.State != ToolStateDone {
		t.Errorf("tc1.State = %q, want %q", tc1.State, ToolStateDone)
	}
	tc2 := msg.ToolCalls()[1]
	if tc2.State != ToolStateRunning {
		t.Errorf("tc2.State = %q, want %q (should be unchanged)", tc2.State, ToolStateRunning)
	}
}

func TestCancelPendingToolCalls(t *testing.T) {
	msg := &Message{
		Parts: []ContentPart{
			ToolCall{ID: "tc1", Name: "read", State: ToolStatePending},
			ToolCall{ID: "tc2", Name: "write", State: ToolStateRunning},
			ToolCall{ID: "tc3", Name: "bash", State: ToolStateDone},
		},
	}
	msg.CancelPendingToolCalls()

	tcs := msg.ToolCalls()
	if tcs[0].State != ToolStateCanceled {
		t.Errorf("tc1 (Pending) State = %q, want %q", tcs[0].State, ToolStateCanceled)
	}
	if tcs[1].State != ToolStateCanceled {
		t.Errorf("tc2 (Running) State = %q, want %q", tcs[1].State, ToolStateCanceled)
	}
	if tcs[2].State != ToolStateDone {
		t.Errorf("tc3 (Done) State = %q, want %q (terminal should be preserved)", tcs[2].State, ToolStateDone)
	}
}

func BenchmarkPromptWithTextAttachments(b *testing.B) {
	cases := []struct {
		name        string
		numFiles    int
		contentSize int
	}{
		{"1file_100bytes", 1, 100},
		{"5files_1KB", 5, 1024},
		{"10files_10KB", 10, 10 * 1024},
		{"20files_50KB", 20, 50 * 1024},
	}

	for _, tc := range cases {
		attachments := makeTestAttachments(tc.numFiles, tc.contentSize)
		prompt := "Process these files"

		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				_ = PromptWithTextAttachments(prompt, attachments)
			}
		})
	}
}
