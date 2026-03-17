package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/dmora/crucible/internal/fsext"
)

const (
	InitFlagFilename = "init"
)

type ProjectInitFlag struct {
	Initialized bool `json:"initialized"`
}

func Init(workingDir, dataDir string, debug bool) (*Config, error) {
	cfg, err := Load(workingDir, dataDir, debug)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func ProjectNeedsInitialization(cfg *Config) (bool, error) {
	if cfg == nil {
		return false, fmt.Errorf("config not loaded")
	}

	flagFilePath := filepath.Join(cfg.Options.DataDirectory, InitFlagFilename)

	_, err := os.Stat(flagFilePath)
	if err == nil {
		return false, nil
	}

	if !os.IsNotExist(err) {
		return false, fmt.Errorf("failed to check init flag file: %w", err)
	}

	someContextFileExists, err := contextPathsExist(cfg.WorkingDir())
	if err != nil {
		return false, fmt.Errorf("failed to check for context files: %w", err)
	}
	if someContextFileExists {
		return false, nil
	}

	// If the working directory has no non-ignored files, skip initialization step
	empty, err := dirHasNoVisibleFiles(cfg.WorkingDir())
	if err != nil {
		return false, fmt.Errorf("failed to check if directory is empty: %w", err)
	}
	if empty {
		return false, nil
	}

	return true, nil
}

func contextPathsExist(dir string) (bool, error) {
	for _, relPath := range defaultContextPaths {
		absPath := filepath.Join(dir, relPath)
		info, err := os.Stat(absPath)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			return true, nil
		}
		// For directories, only count them if they contain at least one file.
		if dirHasFile(absPath) {
			return true, nil
		}
	}
	return false, nil
}

// dirHasFile returns true if the directory contains at least one regular file.
func dirHasFile(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() {
			return true
		}
	}
	return false
}

// dirHasNoVisibleFiles returns true if the directory has no files/dirs after applying ignore rules
func dirHasNoVisibleFiles(dir string) (bool, error) {
	files, _, err := fsext.ListDirectory(dir, nil, 1, 1)
	if err != nil {
		return false, err
	}
	return len(files) == 0, nil
}

func MarkProjectInitialized(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}
	flagFilePath := filepath.Join(cfg.Options.DataDirectory, InitFlagFilename)

	file, err := os.Create(flagFilePath)
	if err != nil {
		return fmt.Errorf("failed to create init flag file: %w", err)
	}
	defer file.Close()

	return nil
}

func HasInitialDataConfig(cfg *Config) bool {
	if cfg == nil {
		return false
	}
	cfgPath := GlobalConfigData()
	if _, err := os.Stat(cfgPath); err != nil {
		return false
	}
	return cfg.IsConfigured()
}
