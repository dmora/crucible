package model

import "testing"

func TestResolveEditorMode(t *testing.T) {
	station := "build"
	tests := []struct {
		name        string
		hold, yolo  bool
		shellPrefix bool
		relayTarget *string
		want        editorMode
	}{
		{"normal", false, false, false, nil, editorModeNormal},
		{"yolo only", false, true, false, nil, editorModeYolo},
		{"hold only", true, false, false, nil, editorModeHold},
		{"hold beats yolo", true, true, false, nil, editorModeHold},
		{"shell only", false, false, true, nil, editorModeShell},
		{"shell beats hold", true, false, true, nil, editorModeShell},
		{"shell beats yolo", false, true, true, nil, editorModeShell},
		{"relay beats shell", false, false, true, &station, editorModeRelay},
		{"relay beats hold", true, false, false, &station, editorModeRelay},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveEditorMode(tt.hold, tt.yolo, tt.shellPrefix, tt.relayTarget)
			if got != tt.want {
				t.Errorf("resolveEditorMode(%v, %v, %v, %v) = %v, want %v",
					tt.hold, tt.yolo, tt.shellPrefix, tt.relayTarget, got, tt.want)
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
		{"relay", editorModeRelay, "Relay mode — messages go directly to the station"},
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
