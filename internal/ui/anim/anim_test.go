package anim

import (
	"image/color"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestNewSpinner_AllPresetsResolve(t *testing.T) {
	for _, p := range Presets() {
		backend, err := NewSpinner(p, Settings{
			ID: "test-" + p, Size: 10, GradColorA: color.White,
		})
		if err != nil {
			t.Errorf("NewSpinner(%q) returned error: %v", p, err)
		}
		if backend == nil {
			t.Errorf("NewSpinner(%q) returned nil backend", p)
		}
	}
}

func TestNewSpinner_UnknownPresetReturnsError(t *testing.T) {
	_, err := NewSpinner("nonexistent", Settings{ID: "test", Size: 10})
	if err == nil {
		t.Error("NewSpinner with unknown preset should return error")
	}
}

func TestNewSpinner_EmptyPresetReturnsIndustrial(t *testing.T) {
	backend, err := NewSpinner("", Settings{ID: "test", Size: 10, GradColorA: color.White})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify it's an *Anim (industrial)
	if _, ok := backend.(*Anim); !ok {
		t.Error("empty preset should return industrial (*Anim)")
	}
}

func TestSpinnerBackend_BothBackendsSatisfyInterface(t *testing.T) {
	settings := Settings{ID: "test", Size: 10, GradColorA: color.White, GradColorB: color.White}

	ind, err := NewSpinner("industrial", settings)
	if err != nil {
		t.Fatal(err)
	}

	cls, err := NewSpinner("pulse", settings)
	if err != nil {
		t.Fatal(err)
	}

	for name, b := range map[string]SpinnerBackend{"industrial": ind, "pulse": cls} {
		t.Run(name, func(t *testing.T) {
			// All SpinnerBackend methods callable without panic
			_ = b.Start()
			_ = b.Render()
			b.SetLabel("test")
			_ = b.Width()
			_ = b.Animate(StepMsg{ID: "test"})
		})
	}
}

func TestIndustrialRender_WidthMatchesSize(t *testing.T) {
	size := 15
	b, err := NewSpinner("industrial", Settings{
		ID: "t", Size: size,
		GradColorA: color.White, GradColorB: color.White,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = b.Start()
	for i := 0; i < 30; i++ {
		_ = b.Animate(StepMsg{ID: "t"})
	}
	output := b.Render()
	w := lipgloss.Width(output)
	if w != size {
		t.Errorf("industrial render width = %d, want %d (output: %q)", w, size, output)
	}
}

func TestIndustrialRender_CharsetConstraints(t *testing.T) {
	b, err := NewSpinner("industrial", Settings{
		ID: "t", Size: 10,
		GradColorA: color.White, GradColorB: color.White,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = b.Start()
	for i := 0; i < 30; i++ {
		_ = b.Animate(StepMsg{ID: "t"})
	}
	output := b.Render()
	stripped := ansi.Strip(output)
	for _, r := range stripped {
		if !isIndustrialChar(r) {
			t.Errorf("unexpected character %q (U+%04X) in industrial render", string(r), r)
		}
	}
}

// isIndustrialChar returns true if r is in the industrial spinner's charset.
func isIndustrialChar(r rune) bool {
	return r == '.' || r == ' ' || r == '·' ||
		(r >= '▁' && r <= '█') ||
		(r >= '░' && r <= '▓') ||
		(r >= '⠀' && r <= '⣿') || // braille patterns
		strings.ContainsRune("0123456789ABCDEF▄▀", r)
}

func TestClassicSpinner_FrameCycling(t *testing.T) {
	for _, preset := range []string{"pulse", "dots", "ellipsis", "points", "meter"} {
		t.Run(preset, func(t *testing.T) {
			b, err := NewSpinner(preset, Settings{ID: "test", GradColorA: color.White})
			if err != nil {
				t.Fatal(err)
			}
			_ = b.Start()
			frames := make([]string, 0, 20)
			for i := 0; i < 20; i++ {
				frames = append(frames, ansi.Strip(b.Render()))
				_ = b.Animate(StepMsg{ID: "test"})
			}
			allSame := true
			for _, f := range frames[1:] {
				if f != frames[0] {
					allSame = false
					break
				}
			}
			if allSame {
				t.Errorf("classic spinner %q: all frames identical (%q)", preset, frames[0])
			}
		})
	}
}
