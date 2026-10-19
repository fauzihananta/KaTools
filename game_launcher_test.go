package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestListGameExecutablesReturnsOnlyRootExecutables(t *testing.T) {
	folder := t.TempDir()
	for _, name := range []string{"zeta.exe", "Alpha.EXE", "readme.txt"} {
		if err := os.WriteFile(filepath.Join(folder, name), []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(folder, "subfolder.exe"), 0755); err != nil {
		t.Fatal(err)
	}

	executables, err := listGameExecutables(folder)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Alpha.EXE", "zeta.exe"}
	if !reflect.DeepEqual(executables, want) {
		t.Fatalf("executables = %v, want %v", executables, want)
	}
}

func TestConfiguredGameExecutableRejectsPathOutsideFolder(t *testing.T) {
	folder := t.TempDir()
	if _, err := configuredGameExecutable(GameLaunchConfig{Folder: folder, EXE: "..\\outside.exe"}); err == nil {
		t.Fatal("expected executable outside selected folder to be rejected")
	}
}

func TestConfiguredGameExecutableRequiresExistingEXE(t *testing.T) {
	folder := t.TempDir()
	path := filepath.Join(folder, "GameClient.exe")
	if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := configuredGameExecutable(GameLaunchConfig{Folder: folder, EXE: "GameClient.exe"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(got, path) {
		t.Fatalf("executable = %q, want %q", got, path)
	}
}

func TestAutoLoginPointerSlotsAreAccepted(t *testing.T) {
	for _, slot := range []string{"AutoLoginCharacter", "AutoLoginOK", "AutoLoginAutoHunt"} {
		if !isClickSkillSlot(slot) {
			t.Fatalf("Auto Login pointer slot %q is not accepted", slot)
		}
	}
}

func TestAutoLoginPointersRequireBothPointers(t *testing.T) {
	character := ClickSkillROI{Selected: true, X: 100, Y: 200, Width: 1, Height: 1}
	ok := ClickSkillROI{Selected: true, X: 300, Y: 400, Width: 1, Height: 1}
	autoHunt := ClickSkillROI{Selected: true, X: 500, Y: 600, Width: 1, Height: 1}

	if err := autoLoginPointersFromAreas(map[string]ClickSkillROI{"AutoLoginCharacter": character}); err == nil {
		t.Fatal("expected missing Auto Login pointers to be rejected")
	}
	err := autoLoginPointersFromAreas(map[string]ClickSkillROI{
		"AutoLoginCharacter": character,
		"AutoLoginOK":        ok,
		"AutoLoginAutoHunt":  autoHunt,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAutoLoginCanSkipAutoHuntPointer(t *testing.T) {
	areas := map[string]ClickSkillROI{
		"AutoLoginCharacter": {Selected: true, X: 100, Y: 200, Width: 1, Height: 1},
		"AutoLoginOK":        {Selected: true, X: 300, Y: 400, Width: 1, Height: 1},
	}
	if err := autoLoginPointersFromAreasForHunt(areas, false); err != nil {
		t.Fatalf("Auto Hunt disabled should not require its pointer: %v", err)
	}
	if err := autoLoginPointersFromAreasForHunt(areas, true); err == nil {
		t.Fatal("Auto Hunt enabled must require its pointer")
	}
}

func TestAutoLoginLaunchRetryIsBounded(t *testing.T) {
	if autoLoginLaunchAttempts != 3 {
		t.Fatalf("Auto Login launch attempts = %d, want 3", autoLoginLaunchAttempts)
	}
	if autoLoginSlowStartGrace != 15*time.Second {
		t.Fatalf("Auto Login slow-start grace = %s, want 15s", autoLoginSlowStartGrace)
	}
}

func TestAutoLoginPostHuntSequenceTiming(t *testing.T) {
	if autoLoginPostHuntKeyDelay != 7*time.Second || autoLoginPostHuntMenuDelay != 7*time.Second {
		t.Fatalf("post-hunt timing = %s then %s, want 7s then 7s", autoLoginPostHuntKeyDelay, autoLoginPostHuntMenuDelay)
	}
	if autoLoginPostHuntScrolls != 20 {
		t.Fatalf("post-hunt scroll count = %d, want 20", autoLoginPostHuntScrolls)
	}
}

func TestLegacyGameLaunchConfigRunsAutoHunt(t *testing.T) {
	if !(GameLaunchConfig{}).RunAutoHuntEnabled() {
		t.Fatal("settings without the new field must preserve Auto Hunt behaviour")
	}
	disabled := false
	if (GameLaunchConfig{RunAutoHunt: &disabled}).RunAutoHuntEnabled() {
		t.Fatal("explicitly disabled Auto Hunt was ignored")
	}
	if got := (GameLaunchConfig{}).AutoHuntKeyValue(); got != "Y" {
		t.Fatalf("default Auto Hunt key = %q, want Y", got)
	}
}

func TestNormalizeAutoHuntKey(t *testing.T) {
	got, err := normalizeAutoHuntKey(" q ")
	if err != nil || got != "Q" {
		t.Fatalf("normalizeAutoHuntKey returned %q, %v; want Q, nil", got, err)
	}
	for _, value := range []string{"", "12", "?", "1"} {
		if _, err := normalizeAutoHuntKey(value); err == nil {
			t.Fatalf("normalizeAutoHuntKey(%q) should fail", value)
		}
	}
}

func TestWebBotConfigEmbedsAutoLoginProfileSettings(t *testing.T) {
	enabled := true
	characterPointer := ClickSkillROI{Selected: true, X: 243, Y: 537, Width: 1, Height: 1, ClientWidth: 1920, ClientHeight: 1440}
	want := WebBotConfig{AutoLogin: GameLaunchConfig{
		Enabled:     true,
		RunAutoHunt: &enabled,
		AutoHuntKey: "Q",
		Folder:      `D:\Games\Kathana`,
		EXE:         "KathanaGame.exe",
		Pointers: map[string]ClickSkillROI{
			"AutoLoginCharacter": characterPointer,
		},
	}}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got WebBotConfig
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.AutoLogin.Folder != want.AutoLogin.Folder || got.AutoLogin.EXE != want.AutoLogin.EXE || got.AutoLogin.AutoHuntKey != "Q" || !got.AutoLogin.Enabled || !got.AutoLogin.RunAutoHuntEnabled() {
		t.Fatalf("Auto Login profile settings were not preserved: %+v", got.AutoLogin)
	}
	if gotPointer, ok := got.AutoLogin.Pointers["AutoLoginCharacter"]; !ok || !reflect.DeepEqual(gotPointer, characterPointer) {
		t.Fatalf("Auto Login profile pointer was not preserved: %+v", got.AutoLogin.Pointers)
	}
}

func TestAutoLoginPointersUseProfileConfig(t *testing.T) {
	disabled := false
	cfg := GameLaunchConfig{
		RunAutoHunt: &disabled,
		Pointers: map[string]ClickSkillROI{
			"AutoLoginCharacter": {Selected: true, X: 100, Y: 200, Width: 1, Height: 1},
			"AutoLoginOK":        {Selected: true, X: 300, Y: 400, Width: 1, Height: 1},
		},
	}
	if err := autoLoginPointersForConfig(cfg); err != nil {
		t.Fatalf("profile pointers should satisfy Auto Login with Auto Hunt disabled: %v", err)
	}
	if got := cloneAutoLoginPointers(map[string]ClickSkillROI{"1": {}, "AutoLoginOK": cfg.Pointers["AutoLoginOK"]}); len(got) != 1 || !got["AutoLoginOK"].Selected {
		t.Fatalf("unexpected Auto Login pointer clone: %+v", got)
	}
}

func TestGlobalAutoLoginPointersExcludeCharacterPointer(t *testing.T) {
	pointers := map[string]ClickSkillROI{
		"AutoLoginCharacter": {Selected: true, X: 100, Y: 200, Width: 1, Height: 1},
		"AutoLoginOK":        {Selected: true, X: 300, Y: 400, Width: 1, Height: 1},
		"AutoLoginAutoHunt":  {Selected: true, X: 500, Y: 600, Width: 1, Height: 1},
	}
	global := cloneGlobalAutoLoginPointers(pointers)
	if _, found := global["AutoLoginCharacter"]; found {
		t.Fatal("character pointer must not be saved in global Auto Login settings")
	}
	if len(global) != 2 || !global["AutoLoginOK"].Selected || !global["AutoLoginAutoHunt"].Selected {
		t.Fatalf("global Auto Login pointers = %+v, want Login OK and Auto Hunt", global)
	}
}
