package anim

import (
	"fmt"
	"time"

	"charm.land/bubbles/v2/spinner"
)

// Preset constants for spinner styles.
const (
	PresetIndustrial = "industrial"
	PresetPulse      = "pulse"
	PresetDots       = "dots"
	PresetEllipsis   = "ellipsis"
	PresetPoints     = "points"
	PresetMeter      = "meter"
	PresetHamburger  = "hamburger"
	PresetTrigram    = "trigram"
)

// wideMeter is an 8-cell looping meter. Fills left-to-right, then
// empties left-to-right, using only ▰ and ▱ for maximum font compatibility.
var wideMeter = spinner.Spinner{
	Frames: []string{
		"▱▱▱▱▱▱▱▱",
		"▰▱▱▱▱▱▱▱",
		"▰▰▱▱▱▱▱▱",
		"▰▰▰▱▱▱▱▱",
		"▰▰▰▰▱▱▱▱",
		"▰▰▰▰▰▱▱▱",
		"▰▰▰▰▰▰▱▱",
		"▰▰▰▰▰▰▰▱",
		"▰▰▰▰▰▰▰▰",
		"▰▰▰▰▰▰▰▰",
		"▰▰▰▰▰▰▰▰",
		"▱▰▰▰▰▰▰▰",
		"▱▱▰▰▰▰▰▰",
		"▱▱▱▰▰▰▰▰",
		"▱▱▱▱▰▰▰▰",
		"▱▱▱▱▱▰▰▰",
		"▱▱▱▱▱▱▰▰",
		"▱▱▱▱▱▱▱▰",
		"▱▱▱▱▱▱▱▱",
		"▱▱▱▱▱▱▱▱",
	},
	FPS: time.Second / 12,
}

// presetMap maps preset names to bubbles spinner definitions.
var presetMap = map[string]spinner.Spinner{
	PresetPulse:     spinner.Pulse,
	PresetDots:      spinner.MiniDot,
	PresetEllipsis:  spinner.Ellipsis,
	PresetPoints:    spinner.Points,
	PresetMeter:     wideMeter,
	PresetHamburger: spinner.Hamburger,
	PresetTrigram: {
		Frames: []string{"☷", "☴", "☲", "☰", "☲", "☴"},
		FPS:    time.Second / 4,
	},
}

// Presets returns the ordered list of all spinner preset names.
func Presets() []string {
	return []string{
		PresetIndustrial,
		PresetPulse,
		PresetDots,
		PresetEllipsis,
		PresetPoints,
		PresetMeter,
		PresetHamburger,
		PresetTrigram,
	}
}

// NewSpinner creates a SpinnerBackend for the given preset name.
// Unknown presets return an error; the caller should fall back to industrial.
func NewSpinner(preset string, opts Settings) (SpinnerBackend, error) {
	if preset == "" || preset == PresetIndustrial {
		return New(opts), nil
	}
	sp, ok := presetMap[preset]
	if !ok {
		return nil, fmt.Errorf("unknown spinner preset %q", preset)
	}
	return newClassicSpinner(opts.ID, sp, opts.GradColorA, opts.LabelColor, opts.Label), nil
}

// ValidPreset reports whether name is a known spinner preset.
func ValidPreset(name string) bool {
	if name == PresetIndustrial {
		return true
	}
	_, ok := presetMap[name]
	return ok
}
