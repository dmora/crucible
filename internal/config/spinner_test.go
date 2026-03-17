package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSpinnerPreset_DefaultIsMeter(t *testing.T) {
	cfg := &Config{}
	if got := cfg.SpinnerPreset(); got != "meter" {
		t.Errorf("default SpinnerPreset() = %q, want %q", got, "meter")
	}
}

func TestSetSpinner_ValidPreset(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "crucible.json")
	_ = os.WriteFile(configPath, []byte("{}"), 0o600)

	cfg := &Config{dataConfigDir: configPath}

	if err := cfg.SetSpinner("pulse"); err != nil {
		t.Fatalf("SetSpinner(pulse): %v", err)
	}
	if got := cfg.SpinnerPreset(); got != "pulse" {
		t.Errorf("after set: SpinnerPreset() = %q, want %q", got, "pulse")
	}
}

func TestSetSpinner_InvalidPreset(t *testing.T) {
	cfg := &Config{}
	if err := cfg.SetSpinner("bogus"); err == nil {
		t.Error("SetSpinner(bogus) should return error")
	}
}

func TestSetSpinner_AllValidPresets(t *testing.T) {
	for _, preset := range []string{"industrial", "pulse", "dots", "ellipsis", "points", "meter"} {
		dir := t.TempDir()
		configPath := filepath.Join(dir, "crucible.json")
		_ = os.WriteFile(configPath, []byte("{}"), 0o600)

		cfg := &Config{dataConfigDir: configPath}
		if err := cfg.SetSpinner(preset); err != nil {
			t.Errorf("SetSpinner(%q) returned error: %v", preset, err)
		}
		if got := cfg.SpinnerPreset(); got != preset {
			t.Errorf("SpinnerPreset() = %q, want %q", got, preset)
		}
	}
}
