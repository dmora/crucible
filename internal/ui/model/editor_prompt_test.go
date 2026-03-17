package model

import "testing"

func TestResolveEditorMode(t *testing.T) {
	tests := []struct {
		name        string
		hold, yolo  bool
		shellPrefix bool
		want        editorMode
	}{
		{"normal", false, false, false, editorModeNormal},
		{"yolo only", false, true, false, editorModeYolo},
		{"hold only", true, false, false, editorModeHold},
		{"hold beats yolo", true, true, false, editorModeHold},
		{"shell only", false, false, true, editorModeShell},
		{"shell beats hold", true, false, true, editorModeShell},
		{"shell beats yolo", false, true, true, editorModeShell},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveEditorMode(tt.hold, tt.yolo, tt.shellPrefix)
			if got != tt.want {
				t.Errorf("resolveEditorMode(%v, %v, %v) = %v, want %v",
					tt.hold, tt.yolo, tt.shellPrefix, got, tt.want)
			}
		})
	}
}

func TestResolveEditorPlaceholder(t *testing.T) {
	const ready = "Ready for instructions"
	tests := []struct {
		name string
		mode editorMode
		want string
	}{
		{"normal", editorModeNormal, ready},
		{"yolo", editorModeYolo, "Yolo mode!"},
		{"hold", editorModeHold, "Hold active"},
		{"shell", editorModeShell, "Shell command"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveEditorPlaceholder(tt.mode, ready)
			if got != tt.want {
				t.Errorf("resolveEditorPlaceholder(%v) = %q, want %q",
					tt.mode, got, tt.want)
			}
		})
	}
}
