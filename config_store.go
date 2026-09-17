package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const savedConfigFileName = "katools_config.json"

var savedConfigMu sync.Mutex

// savedConfigPath normally lives beside the executable. `go run`, however,
// executes a temporary binary under Go's cache, so use the project working
// directory whenever it contains go.mod. That lets source testing and the
// KaTools executable share the same saved preset.
func savedConfigPath() string {
	if workingDir, err := os.Getwd(); err == nil {
		if _, err := os.Stat(filepath.Join(workingDir, "go.mod")); err == nil {
			return filepath.Join(workingDir, savedConfigFileName)
		}
	}

	executable, err := os.Executable()
	if err == nil && executable != "" {
		return filepath.Join(filepath.Dir(executable), savedConfigFileName)
	}

	return savedConfigFileName
}

func loadSavedWebConfig() (WebBotConfig, bool, error) {
	savedConfigMu.Lock()
	defer savedConfigMu.Unlock()

	data, err := os.ReadFile(savedConfigPath())
	if os.IsNotExist(err) {
		return WebBotConfig{}, false, nil
	}
	if err != nil {
		return WebBotConfig{}, false, fmt.Errorf("read saved config: %w", err)
	}

	var cfg WebBotConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return WebBotConfig{}, false, fmt.Errorf("read saved config: %w", err)
	}

	return cfg, true, nil
}

func saveWebConfig(cfg WebBotConfig) error {
	savedConfigMu.Lock()
	defer savedConfigMu.Unlock()

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode saved config: %w", err)
	}

	path := savedConfigPath()
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0644); err != nil {
		return fmt.Errorf("write saved config: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("finalize saved config: %w", err)
	}

	return nil
}
