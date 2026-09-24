package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"
)

const (
	savedConfigFileName        = "katools_config.json"
	configProfilesDirectory    = "configs"
	configProfileFileExtension = ".json"
)

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

func configProfilesPath() string {
	return filepath.Join(filepath.Dir(savedConfigPath()), configProfilesDirectory)
}

func normalizeConfigProfileName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("enter a character name")
	}
	if utf8RuneCount := len([]rune(name)); utf8RuneCount > 48 {
		return "", fmt.Errorf("character name must be 48 characters or fewer")
	}

	for _, character := range name {
		if unicode.IsLetter(character) || unicode.IsDigit(character) || character == ' ' || character == '-' || character == '_' {
			continue
		}
		return "", fmt.Errorf("character name may use only letters, numbers, spaces, hyphens, and underscores")
	}

	return name, nil
}

func configProfilePath(name string) (string, string, error) {
	normalized, err := normalizeConfigProfileName(name)
	if err != nil {
		return "", "", err
	}
	return filepath.Join(configProfilesPath(), normalized+configProfileFileExtension), normalized, nil
}

func loadSavedWebConfig() (WebBotConfig, bool, error) {
	savedConfigMu.Lock()
	defer savedConfigMu.Unlock()

	return loadWebConfigAtPath(savedConfigPath())
}

func loadProfileWebConfig(name string) (WebBotConfig, bool, error) {
	savedConfigMu.Lock()
	defer savedConfigMu.Unlock()

	path, _, err := configProfilePath(name)
	if err != nil {
		return WebBotConfig{}, false, err
	}
	return loadWebConfigAtPath(path)
}

func loadWebConfigAtPath(path string) (WebBotConfig, bool, error) {
	data, err := os.ReadFile(path)
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
	return saveWebConfigAtPath(savedConfigPath(), cfg)
}

func saveProfileWebConfig(name string, cfg WebBotConfig) (string, error) {
	savedConfigMu.Lock()
	defer savedConfigMu.Unlock()

	path, normalized, err := configProfilePath(name)
	if err != nil {
		return "", err
	}
	// Window handles are created anew every time Kathana opens, so a profile
	// must not restore a stale selection on another session.
	cfg.HWND = ""
	if err := saveWebConfigAtPath(path, cfg); err != nil {
		return "", err
	}
	return normalized, nil
}

func listWebConfigProfiles() ([]string, error) {
	savedConfigMu.Lock()
	defer savedConfigMu.Unlock()

	entries, err := os.ReadDir(configProfilesPath())
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list saved configs: %w", err)
	}

	profiles := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), configProfileFileExtension) {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		if _, err := normalizeConfigProfileName(name); err == nil {
			profiles = append(profiles, name)
		}
	}
	sort.Slice(profiles, func(i, j int) bool {
		return strings.ToLower(profiles[i]) < strings.ToLower(profiles[j])
	})
	return profiles, nil
}

func saveWebConfigAtPath(path string, cfg WebBotConfig) error {

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode saved config: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create saved config folder: %w", err)
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0644); err != nil {
		return fmt.Errorf("write saved config: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("finalize saved config: %w", err)
	}

	return nil
}
