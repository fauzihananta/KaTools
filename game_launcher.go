package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

const gameLaunchConfigFileName = "katools_game_launch.json"

// GameLaunchConfig is intentionally separate from a character profile. A game
// installation belongs to the computer, while profiles belong to characters.
// Keeping them separate also means old saved profiles remain byte-for-byte
// compatible with this optional feature.
type GameLaunchConfig struct {
	Enabled     bool   `json:"enabled"`
	RunAutoHunt *bool  `json:"runAutoHunt,omitempty"`
	AutoHuntKey string `json:"autoHuntKey,omitempty"`
	Folder      string `json:"folder"`
	EXE         string `json:"exe"`
	WindowTitle string `json:"windowTitle"`
}

// Older launcher settings predate the checkbox and therefore omit the field.
// Keep their established behaviour (run Auto Hunt) until the user chooses
// otherwise in the new setting.
func (cfg GameLaunchConfig) RunAutoHuntEnabled() bool {
	return cfg.RunAutoHunt == nil || *cfg.RunAutoHunt
}

const defaultAutoHuntKey = "Y"

func normalizeAutoHuntKey(key string) (string, error) {
	key = strings.ToUpper(strings.TrimSpace(key))
	if len(key) != 1 || key[0] < 'A' || key[0] > 'Z' {
		return "", fmt.Errorf("Auto Hunt key must be one letter from A to Z")
	}
	return key, nil
}

func (cfg GameLaunchConfig) AutoHuntKeyValue() string {
	key, err := normalizeAutoHuntKey(cfg.AutoHuntKey)
	if err != nil {
		return defaultAutoHuntKey
	}
	return key
}

var gameLaunchConfigMu sync.Mutex

const (
	// The old game process can still be shutting down immediately after the
	// disconnect dialog is acknowledged. Give it time to release its single
	// instance lock before launching the selected executable again.
	autoLoginLaunchDelay       = 30 * time.Second
	autoLoginFirstEnterDelay   = 35 * time.Second
	autoLoginSecondEnterDelay  = 6 * time.Second
	autoLoginCharacterDelay    = 10 * time.Second
	autoLoginLoginOKRetryDelay = 3 * time.Second
	autoLoginGameReadyDelay    = 10 * time.Second
	autoLoginHuntMenuDelay     = time.Second
	autoLoginWindowPoll        = time.Second
	autoLoginWindowAttempts    = 30
)

var autoLoginRecoveryMu struct {
	sync.Mutex
	running bool
}

func gameLaunchConfigPath() string {
	return filepath.Join(filepath.Dir(savedConfigPath()), gameLaunchConfigFileName)
}

func loadGameLaunchConfig() (GameLaunchConfig, error) {
	gameLaunchConfigMu.Lock()
	defer gameLaunchConfigMu.Unlock()

	data, err := os.ReadFile(gameLaunchConfigPath())
	if os.IsNotExist(err) {
		return GameLaunchConfig{}, nil
	}
	if err != nil {
		return GameLaunchConfig{}, fmt.Errorf("read game launcher settings: %w", err)
	}

	var cfg GameLaunchConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return GameLaunchConfig{}, fmt.Errorf("read game launcher settings: %w", err)
	}
	return cfg, nil
}

func saveGameLaunchConfig(cfg GameLaunchConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode game launcher settings: %w", err)
	}

	path := gameLaunchConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create game launcher settings folder: %w", err)
	}
	if err := os.WriteFile(path+".tmp", data, 0644); err != nil {
		return fmt.Errorf("write game launcher settings: %w", err)
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		return fmt.Errorf("finalize game launcher settings: %w", err)
	}
	return nil
}

// listGameExecutables deliberately looks only in the selected folder. This
// keeps the chosen folder as a clear security boundary and avoids guessing
// which executable inside a launcher or anti-cheat subdirectory is the game.
func listGameExecutables(folder string) ([]string, error) {
	folder = strings.TrimSpace(folder)
	info, err := os.Stat(folder)
	if err != nil {
		return nil, fmt.Errorf("open game folder: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("selected game path is not a folder")
	}

	entries, err := os.ReadDir(folder)
	if err != nil {
		return nil, fmt.Errorf("list game executables: %w", err)
	}

	executables := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".exe") {
			continue
		}
		info, err := entry.Info()
		if err == nil && info.Mode().IsRegular() {
			executables = append(executables, entry.Name())
		}
	}
	sort.Slice(executables, func(i, j int) bool {
		return strings.ToLower(executables[i]) < strings.ToLower(executables[j])
	})
	return executables, nil
}

func configuredGameExecutable(cfg GameLaunchConfig) (string, error) {
	folder := strings.TrimSpace(cfg.Folder)
	exe := strings.TrimSpace(cfg.EXE)
	if folder == "" {
		return "", fmt.Errorf("select a game folder first")
	}
	if exe == "" {
		return "", fmt.Errorf("select a game executable first")
	}
	if filepath.Base(exe) != exe || !strings.EqualFold(filepath.Ext(exe), ".exe") {
		return "", fmt.Errorf("invalid game executable")
	}

	path := filepath.Join(folder, exe)
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("game executable is unavailable: %w", err)
	}
	if info.IsDir() || !info.Mode().IsRegular() {
		return "", fmt.Errorf("selected game executable is not a file")
	}
	return path, nil
}

func selectGameLaunchFolder(folder string) (GameLaunchConfig, []string, error) {
	gameLaunchConfigMu.Lock()
	defer gameLaunchConfigMu.Unlock()

	if _, err := listGameExecutables(folder); err != nil {
		return GameLaunchConfig{}, nil, err
	}
	absoluteFolder, err := filepath.Abs(folder)
	if err != nil {
		return GameLaunchConfig{}, nil, fmt.Errorf("resolve game folder: %w", err)
	}
	executables, err := listGameExecutables(absoluteFolder)
	if err != nil {
		return GameLaunchConfig{}, nil, err
	}

	cfg := GameLaunchConfig{Folder: absoluteFolder}
	previousData, err := os.ReadFile(gameLaunchConfigPath())
	if err == nil {
		var previous GameLaunchConfig
		if json.Unmarshal(previousData, &previous) == nil {
			cfg.Enabled = previous.Enabled
			cfg.RunAutoHunt = previous.RunAutoHunt
			cfg.AutoHuntKey = previous.AutoHuntKey
			cfg.WindowTitle = previous.WindowTitle
			if previous.Folder == absoluteFolder {
				for _, name := range executables {
					if name == previous.EXE {
						cfg.EXE = name
						break
					}
				}
			}
		}
	}
	if cfg.EXE == "" && len(executables) == 1 {
		cfg.EXE = executables[0]
	}
	if err := saveGameLaunchConfig(cfg); err != nil {
		return GameLaunchConfig{}, nil, err
	}
	return cfg, executables, nil
}

func selectGameLaunchExecutable(exe string) (GameLaunchConfig, error) {
	gameLaunchConfigMu.Lock()
	defer gameLaunchConfigMu.Unlock()

	data, err := os.ReadFile(gameLaunchConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return GameLaunchConfig{}, fmt.Errorf("select a game folder first")
		}
		return GameLaunchConfig{}, fmt.Errorf("read game launcher settings: %w", err)
	}
	var cfg GameLaunchConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return GameLaunchConfig{}, fmt.Errorf("read game launcher settings: %w", err)
	}
	candidate := GameLaunchConfig{Enabled: cfg.Enabled, RunAutoHunt: cfg.RunAutoHunt, AutoHuntKey: cfg.AutoHuntKey, Folder: cfg.Folder, EXE: strings.TrimSpace(exe), WindowTitle: cfg.WindowTitle}
	if _, err := configuredGameExecutable(candidate); err != nil {
		return GameLaunchConfig{}, err
	}
	if err := saveGameLaunchConfig(candidate); err != nil {
		return GameLaunchConfig{}, err
	}
	return candidate, nil
}

func setGameLaunchEnabled(enabled bool) (GameLaunchConfig, error) {
	if enabled && !autoLoginOnlineAccessAllowed() {
		return GameLaunchConfig{}, fmt.Errorf("online access is required before enabling Auto Login")
	}
	if enabled && !autoLoginDCScanConfigured() {
		return GameLaunchConfig{}, fmt.Errorf("set the DC popup scan area before enabling Auto Login")
	}

	gameLaunchConfigMu.Lock()
	defer gameLaunchConfigMu.Unlock()

	cfg := GameLaunchConfig{}
	data, err := os.ReadFile(gameLaunchConfigPath())
	if err == nil {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return GameLaunchConfig{}, fmt.Errorf("read game launcher settings: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return GameLaunchConfig{}, fmt.Errorf("read game launcher settings: %w", err)
	}
	cfg.Enabled = enabled
	if err := saveGameLaunchConfig(cfg); err != nil {
		return GameLaunchConfig{}, err
	}
	return cfg, nil
}

func autoLoginOnlineAccessAllowed() bool {
	return getGlobalRuntimeManager().accessStatus().Allowed
}

func setGameLaunchAutoHunt(enabled bool) (GameLaunchConfig, error) {
	gameLaunchConfigMu.Lock()
	defer gameLaunchConfigMu.Unlock()

	cfg := GameLaunchConfig{}
	data, err := os.ReadFile(gameLaunchConfigPath())
	if err == nil {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return GameLaunchConfig{}, fmt.Errorf("read game launcher settings: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return GameLaunchConfig{}, fmt.Errorf("read game launcher settings: %w", err)
	}
	cfg.RunAutoHunt = &enabled
	if err := saveGameLaunchConfig(cfg); err != nil {
		return GameLaunchConfig{}, err
	}
	return cfg, nil
}

func setGameLaunchAutoHuntKey(key string) (GameLaunchConfig, error) {
	key, err := normalizeAutoHuntKey(key)
	if err != nil {
		return GameLaunchConfig{}, err
	}

	gameLaunchConfigMu.Lock()
	defer gameLaunchConfigMu.Unlock()

	cfg := GameLaunchConfig{}
	data, err := os.ReadFile(gameLaunchConfigPath())
	if err == nil {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return GameLaunchConfig{}, fmt.Errorf("read game launcher settings: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return GameLaunchConfig{}, fmt.Errorf("read game launcher settings: %w", err)
	}
	cfg.AutoHuntKey = key
	if err := saveGameLaunchConfig(cfg); err != nil {
		return GameLaunchConfig{}, err
	}
	return cfg, nil
}

// Auto Login relies on the same targeted DC detector as the popup input
// modes. A manually chosen area is deliberately required: the generic default
// crop is useful as a fallback detector, but is not dependable enough to
// trigger a full game relaunch.
func autoLoginDCScanConfigured() bool {
	area, ok := LoadClickPopupScanAreas()["dc"]
	return ok && area.Custom && area.ROI.Selected
}

// recordAutoLoginWindowTitle captures the actual title selected by the user
// when the bot starts. It is stored only for enabled Auto Login, avoiding a
// free-text title that could accidentally match an unrelated application.
func recordAutoLoginWindowTitle(hwnd uintptr) {
	title := strings.TrimSpace(getWindowTitle(windows.Handle(hwnd)))
	if title == "" {
		appendOCRLog("AUTO LOGIN TITLE NOT SAVED | Selected window has no title")
		return
	}

	gameLaunchConfigMu.Lock()
	defer gameLaunchConfigMu.Unlock()
	data, err := os.ReadFile(gameLaunchConfigPath())
	if err != nil {
		if !os.IsNotExist(err) {
			appendOCRLog("AUTO LOGIN TITLE NOT SAVED | Read settings failed | %v", err)
		}
		return
	}
	var cfg GameLaunchConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		appendOCRLog("AUTO LOGIN TITLE NOT SAVED | Read settings failed | %v", err)
		return
	}
	if !cfg.Enabled || cfg.WindowTitle == title {
		return
	}
	cfg.WindowTitle = title
	if err := saveGameLaunchConfig(cfg); err != nil {
		appendOCRLog("AUTO LOGIN TITLE NOT SAVED | Write settings failed | %v", err)
		return
	}
	appendOCRLog("AUTO LOGIN TITLE SAVED | Title=%q", title)
}

func launchConfiguredGame() (GameLaunchConfig, error) {
	gameLaunchConfigMu.Lock()
	defer gameLaunchConfigMu.Unlock()

	data, err := os.ReadFile(gameLaunchConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return GameLaunchConfig{}, fmt.Errorf("select a game folder and executable first")
		}
		return GameLaunchConfig{}, fmt.Errorf("read game launcher settings: %w", err)
	}
	var cfg GameLaunchConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return GameLaunchConfig{}, fmt.Errorf("read game launcher settings: %w", err)
	}
	path, err := configuredGameExecutable(cfg)
	if err != nil {
		return GameLaunchConfig{}, err
	}

	cmd := exec.Command(path)
	cmd.Dir = cfg.Folder
	if err := cmd.Start(); err != nil {
		return GameLaunchConfig{}, fmt.Errorf("start selected game executable: %w", err)
	}
	_ = cmd.Process.Release()
	return cfg, nil
}

// startAutoLoginRecovery starts one detached recovery sequence after the
// existing DC handler has successfully acknowledged the disconnect dialog.
// The manual Run Game button remains independent from this sequence.
func startAutoLoginRecovery(targetHWND uintptr) {
	if targetHWND == 0 {
		return
	}
	if !autoLoginOnlineAccessAllowed() {
		appendOCRLog("AUTO LOGIN SKIPPED | Online access is not available")
		return
	}

	cfg, err := loadGameLaunchConfig()
	if err != nil {
		appendOCRLog("AUTO LOGIN SKIPPED | Read settings failed | %v", err)
		return
	}
	if !cfg.Enabled {
		return
	}
	if !autoLoginDCScanConfigured() {
		appendOCRLog("AUTO LOGIN SKIPPED | DC popup scan area is not set")
		return
	}
	if _, err := configuredGameExecutable(cfg); err != nil {
		appendOCRLog("AUTO LOGIN SKIPPED | Game executable invalid | %v", err)
		return
	}
	if err := autoLoginPointersForHunt(cfg.RunAutoHuntEnabled()); err != nil {
		appendOCRLog("AUTO LOGIN SKIPPED | %v", err)
		return
	}

	autoLoginRecoveryMu.Lock()
	if autoLoginRecoveryMu.running {
		autoLoginRecoveryMu.Unlock()
		appendOCRLog("AUTO LOGIN SKIPPED | Recovery is already running")
		return
	}
	autoLoginRecoveryMu.running = true
	autoLoginRecoveryMu.Unlock()

	targetTitle := strings.TrimSpace(cfg.WindowTitle)
	if targetTitle == "" {
		targetTitle = strings.TrimSpace(getWindowTitle(windows.Handle(targetHWND)))
		appendOCRLog("AUTO LOGIN TITLE FALLBACK | Title=%q", targetTitle)
	} else {
		appendOCRLog("AUTO LOGIN TITLE LOADED | Title=%q", targetTitle)
	}
	go func() {
		defer func() {
			autoLoginRecoveryMu.Lock()
			autoLoginRecoveryMu.running = false
			autoLoginRecoveryMu.Unlock()
		}()
		runAutoLoginRecovery(targetHWND, targetTitle)
	}()
}

func autoLoginPointers() error {
	return autoLoginPointersForHunt(true)
}

func autoLoginPointersForHunt(runAutoHunt bool) error {
	return autoLoginPointersFromAreasForHunt(LoadClickSkillROIs(), runAutoHunt)
}

func autoLoginPointersFromAreas(areas map[string]ClickSkillROI) error {
	return autoLoginPointersFromAreasForHunt(areas, true)
}

func autoLoginPointersFromAreasForHunt(areas map[string]ClickSkillROI, runAutoHunt bool) error {
	pointers := []struct {
		slot  string
		label string
	}{
		{slot: "AutoLoginCharacter", label: "Select Character"},
		{slot: "AutoLoginOK", label: "Login OK"},
	}
	if runAutoHunt {
		pointers = append(pointers, struct {
			slot  string
			label string
		}{slot: "AutoLoginAutoHunt", label: "Activate Auto Hunt"})
	}
	for _, pointer := range pointers {
		area, set := areas[pointer.slot]
		if !set || !area.Selected {
			return fmt.Errorf("%s pointer is not set", pointer.label)
		}
	}
	return nil
}

func autoLoginStillEnabled() bool {
	cfg, err := loadGameLaunchConfig()
	return err == nil && cfg.Enabled && autoLoginOnlineAccessAllowed() && autoLoginDCScanConfigured()
}

func waitForAutoLoginStage(delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	check := time.NewTicker(time.Second)
	defer check.Stop()
	for {
		select {
		case <-timer.C:
			return autoLoginStillEnabled()
		case <-check.C:
			if !autoLoginStillEnabled() {
				return false
			}
		}
	}
}

// autoLoginWindow keeps the originally configured game window when it is
// still alive. Some launchers replace it during startup, so a same-title
// window is a conservative fallback for the later Enter/click actions.
func autoLoginWindow(original uintptr, title string) uintptr {
	if isWindowValid(original) {
		return original
	}
	if strings.TrimSpace(title) == "" {
		return 0
	}
	for _, candidate := range enumerateWindows() {
		if candidate.Title != title {
			continue
		}
		hwnd, err := parseHWND(candidate.HWND)
		if err == nil && isWindowValid(hwnd) {
			return hwnd
		}
	}
	return 0
}

func autoLoginWindowByTitle(title string) uintptr {
	title = strings.TrimSpace(title)
	if title == "" {
		return 0
	}
	for _, candidate := range enumerateWindows() {
		if candidate.Title != title {
			continue
		}
		hwnd, err := parseHWND(candidate.HWND)
		if err == nil && isWindowValid(hwnd) {
			return hwnd
		}
	}
	return 0
}

func restartKaToolsAfterAutoLogin(title string) {
	appendOCRLog("AUTO LOGIN REATTACH STARTED | Title=%q | Attempts=%d | Poll=%s", title, autoLoginWindowAttempts, autoLoginWindowPoll)
	for attempt := 1; attempt <= autoLoginWindowAttempts; attempt++ {
		if !autoLoginStillEnabled() {
			appendOCRLog("AUTO LOGIN REATTACH CANCELED | Auto Login disabled")
			return
		}
		if hwnd := autoLoginWindowByTitle(title); hwnd != 0 {
			appendOCRLog("AUTO LOGIN REATTACH WINDOW FOUND | Attempt=%d/%d | HWND=0x%X | Title=%q", attempt, autoLoginWindowAttempts, hwnd, title)
			manager := getGlobalRuntimeManager()
			if manager == nil {
				stopKaToolsForAutoLoginFailure("runtime manager is unavailable")
				return
			}
			if err := manager.RestartAfterAutoLogin(hwnd); err != nil {
				stopKaToolsForAutoLoginFailure("restart failed: " + err.Error())
				return
			}
			appendOCRLog("AUTO LOGIN REATTACH COMPLETE | HWND=0x%X | Title=%q", hwnd, title)
			return
		}
		appendOCRLog("AUTO LOGIN REATTACH SEARCH | Attempt=%d/%d | Title=%q | Window not found", attempt, autoLoginWindowAttempts, title)
		if attempt < autoLoginWindowAttempts {
			time.Sleep(autoLoginWindowPoll)
		}
	}
	stopKaToolsForAutoLoginFailure(fmt.Sprintf("game window %q was not found after %d attempts", title, autoLoginWindowAttempts))
}

// stopKaToolsForAutoLoginFailure intentionally closes the whole application:
// after a bounded title search fails there is no safe target window left for
// KaTools to control, and continuing would leave a misleading idle dashboard.
func stopKaToolsForAutoLoginFailure(reason string) {
	appendOCRLog("AUTO LOGIN FATAL | %s | Stopping KaTools application", reason)
	if manager := getGlobalRuntimeManager(); manager != nil {
		manager.StopWithReason(stopReasonAutoLoginFail)
		manager.CloseTelemetry()
	}
	time.Sleep(200 * time.Millisecond)
	os.Exit(1)
}

func runAutoLoginRecovery(originalHWND uintptr, targetTitle string) {
	appendOCRLog("AUTO LOGIN | DC acknowledged | Waiting=%s before starting the game executable", autoLoginLaunchDelay)
	if !waitForAutoLoginStage(autoLoginLaunchDelay) {
		appendOCRLog("AUTO LOGIN CANCELED | Disabled while waiting to start the game executable")
		return
	}

	cfg, err := launchConfiguredGame()
	if err != nil {
		appendOCRLog("AUTO LOGIN FAILED | Start game executable | %v", err)
		return
	}
	appendOCRLog("AUTO LOGIN STARTED | EXE=%s | Waiting=%s before first Enter", cfg.EXE, autoLoginFirstEnterDelay)

	if !waitForAutoLoginStage(autoLoginFirstEnterDelay) {
		appendOCRLog("AUTO LOGIN CANCELED | Disabled while waiting for first Enter")
		return
	}
	targetHWND := autoLoginWindow(originalHWND, targetTitle)
	if targetHWND == 0 || !pressEnterToWindow(targetHWND) {
		appendOCRLog("AUTO LOGIN FAILED | First Enter could not be sent")
		return
	}
	appendOCRLog("AUTO LOGIN | First Enter sent | Waiting=%s before second Enter", autoLoginSecondEnterDelay)

	if !waitForAutoLoginStage(autoLoginSecondEnterDelay) {
		appendOCRLog("AUTO LOGIN CANCELED | Disabled while waiting for second Enter")
		return
	}
	targetHWND = autoLoginWindow(targetHWND, targetTitle)
	if targetHWND == 0 || !pressEnterToWindow(targetHWND) {
		appendOCRLog("AUTO LOGIN FAILED | Second Enter could not be sent")
		return
	}
	appendOCRLog("AUTO LOGIN | Second Enter sent")

	if !autoLoginStillEnabled() {
		appendOCRLog("AUTO LOGIN CANCELED | Disabled before character selection")
		return
	}
	if sent, err := clickConfiguredAction(targetHWND, "AutoLoginCharacter"); err != nil || !sent {
		appendOCRLog("AUTO LOGIN FAILED | Select Character click | %v", err)
		return
	}
	appendOCRLog("AUTO LOGIN | Select Character click sent | Waiting=%s before Login OK", autoLoginCharacterDelay)
	if !waitForAutoLoginStage(autoLoginCharacterDelay) {
		appendOCRLog("AUTO LOGIN CANCELED | Disabled while waiting for character selection to load")
		return
	}
	if !autoLoginStillEnabled() {
		appendOCRLog("AUTO LOGIN CANCELED | Disabled before Login OK")
		return
	}
	if sent, err := clickConfiguredAction(targetHWND, "AutoLoginOK"); err != nil || !sent {
		appendOCRLog("AUTO LOGIN FAILED | First Login OK click | %v", err)
		return
	}
	appendOCRLog("AUTO LOGIN | First Login OK click sent | Waiting=%s before second Login OK click", autoLoginLoginOKRetryDelay)
	if !waitForAutoLoginStage(autoLoginLoginOKRetryDelay) {
		appendOCRLog("AUTO LOGIN CANCELED | Disabled while waiting for second Login OK click")
		return
	}
	targetHWND = autoLoginWindow(targetHWND, targetTitle)
	if targetHWND == 0 {
		appendOCRLog("AUTO LOGIN FAILED | Game window was unavailable for second Login OK click")
		return
	}
	if sent, err := clickConfiguredAction(targetHWND, "AutoLoginOK"); err != nil || !sent {
		appendOCRLog("AUTO LOGIN FAILED | Second Login OK click | %v", err)
		return
	}
	appendOCRLog("AUTO LOGIN | Second Login OK click sent | Waiting=%s for the game to load", autoLoginGameReadyDelay)
	if !waitForAutoLoginStage(autoLoginGameReadyDelay) {
		appendOCRLog("AUTO LOGIN CANCELED | Disabled while waiting for game load")
		return
	}
	latestCfg, err := loadGameLaunchConfig()
	if err != nil {
		appendOCRLog("AUTO LOGIN FAILED | Read Auto Hunt setting | %v", err)
		return
	}
	if !latestCfg.RunAutoHuntEnabled() {
		appendOCRLog("AUTO LOGIN | Run Auto Hunt is disabled | Skipping virtual Y and Activate Auto Hunt")
		appendOCRLog("AUTO LOGIN COMPLETE | Character and Login OK clicks sent; restarting KaTools bot without Auto Hunt")
		restartKaToolsAfterAutoLogin(targetTitle)
		return
	}
	if err := autoLoginPointersForHunt(true); err != nil {
		appendOCRLog("AUTO LOGIN FAILED | %v", err)
		return
	}
	targetHWND = autoLoginWindow(targetHWND, targetTitle)
	autoHuntKey := latestCfg.AutoHuntKeyValue()
	if targetHWND == 0 || !pressKeyToWindow(targetHWND, uintptr(autoHuntKey[0])) {
		appendOCRLog("AUTO LOGIN FAILED | Virtual %s for Auto Hunt could not be sent", autoHuntKey)
		return
	}
	appendOCRLog("AUTO LOGIN | Virtual %s sent | Waiting=%s for Auto Hunt panel", autoHuntKey, autoLoginHuntMenuDelay)
	if !waitForAutoLoginStage(autoLoginHuntMenuDelay) {
		appendOCRLog("AUTO LOGIN CANCELED | Disabled while waiting for Auto Hunt panel")
		return
	}
	if sent, err := clickConfiguredAction(targetHWND, "AutoLoginAutoHunt"); err != nil || !sent {
		appendOCRLog("AUTO LOGIN FAILED | Activate Auto Hunt click | %v", err)
		return
	}
	appendOCRLog("AUTO LOGIN | Activate Auto Hunt click sent")
	appendOCRLog("AUTO LOGIN COMPLETE | Character, Login OK, and Activate Auto Hunt clicks sent")
	restartKaToolsAfterAutoLogin(targetTitle)
}
