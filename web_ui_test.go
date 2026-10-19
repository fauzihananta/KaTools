package main

import (
	"strings"
	"testing"
)

func TestWebUIIncludesTopAndBottomNavigation(t *testing.T) {
	required := []string{
		`id="scrollTopButton"`,
		`id="scrollBottomButton"`,
		`scrollPageTo('top')`,
		`scrollPageTo('bottom')`,
		`function updateScrollNav()`,
	}
	for _, token := range required {
		if !strings.Contains(webUIHTML, token) {
			t.Fatalf("web UI is missing scroll navigation token %q", token)
		}
	}
}

func TestPausedStatusExplainsHotkeyStopsRuntime(t *testing.T) {
	if !strings.Contains(webUIHTML, `DEATH DETECTED &mdash; Alt+F12 to Stop`) {
		t.Fatal("death-paused status does not explain that Alt+F12 stops the runtime")
	}
}

func TestChatKeywordPartyInviteIsAvailableInAllInputModes(t *testing.T) {
	for _, token := range []string{
		`value="keypress"`,
		"Chat keyword party invite",
		"Available in Keyboard, Keypress, and Click Method.",
		"chatTextClickEnabled: document.getElementById(\"chatTextClickEnabled\").checked,",
		"clickAreaControlsChatParty",
		"clickAreaControlsPartyAutomatic",
		"clickAreaControlsAutoAccept",
		"clickAreaControlsAutoReject",
		"chatTextInviteControls",
		"chat-text-invite-controls",
		"keyboardPopupScanAreas",
		"isSharedChatInviteControl",
		"isKeyboardPopupScanControl",
		"popupAndPartyClickControls",
		"isPopupScanControl",
		"isAutoLoginControl",
		"openClickPopupScanPicker('dc')",
		".roi-picker-row[hidden] { display: none !important; }",
	} {
		if !strings.Contains(webUIHTML, token) {
			t.Fatalf("web UI is missing shared party-invite token %q", token)
		}
	}
}

func TestNormalizeInputMethodPreservesKeypress(t *testing.T) {
	cfg := WebBotConfig{InputMethod: "keypress", TargetWithClick: true}
	normalizeInputMethod(&cfg)
	if !cfg.KeypressMethod() || !cfg.PopupClickMethod() {
		t.Fatalf("keypress mode was not preserved: %+v", cfg)
	}
	if cfg.ClickMethod() || cfg.TargetWithClick {
		t.Fatalf("keypress mode incorrectly became Click Method: %+v", cfg)
	}
}

func TestKeyboardMethodUsesSharedClickPopupFlows(t *testing.T) {
	cfg := WebBotConfig{InputMethod: "keyboard"}
	normalizeInputMethod(&cfg)
	if !cfg.PopupClickMethod() {
		t.Fatalf("keyboard method must use shared click popup flows: %+v", cfg)
	}
	if cfg.ClickMethod() || cfg.KeypressMethod() {
		t.Fatalf("keyboard combat input changed while enabling click popups: %+v", cfg)
	}
	for _, token := range []string{
		`method === "keyboard" || method === "keypress" || method === "click"`,
		"All methods use the same saved click areas for party, resu, DC, and chat invites.",
	} {
		if !strings.Contains(webUIHTML, token) {
			t.Fatalf("keyboard popup UI is missing token %q", token)
		}
	}
}

func TestWebUIIncludesGameLauncherControls(t *testing.T) {
	for _, token := range []string{
		`data-tab="autoLogin"`,
		`id="autoLoginTab"`,
		`id="gameLaunchSection"`,
		`id="autoLoginEnabled"`,
		`id="chooseGameFolderButton"`,
		`id="gameExecutableSelect"`,
		`clickAreaControlsAutoLoginCharacter`,
		`clickAreaControlsAutoLoginOK`,
		`clickAreaControlsAutoLoginAutoHunt`,
		`openClickSkillPicker('AutoLoginCharacter')`,
		`openClickSkillPicker('AutoLoginOK')`,
		`openClickSkillPicker('AutoLoginAutoHunt')`,
		`#launchGameButton`,
		`function syncAutoLoginControls()`,
		`id="autoLoginCharacterSet"`,
		`id="autoLoginOKSet"`,
		`id="autoLoginAutoHuntSet"`,
		`/api/game-launch/run`,
		`/api/game-launch/enabled`,
		`function loadGameLauncher()`,
		`autoLoginDCScanReady`,
		`autoLoginDCScanGuide`,
		`OPEN DC SCAN AREA`,
		`id="autoLoginRunAutoHunt"`,
		`/api/game-launch/auto-hunt`,
		`id="autoLoginAutoHuntKey"`,
		`/api/game-launch/auto-hunt-key`,
		`setAutoLoginAutoHuntKey()`,
	} {
		if !strings.Contains(webUIHTML, token) {
			t.Fatalf("web UI is missing game launcher token %q", token)
		}
	}
}

func TestWebUINavigationRemainsUsableWithoutTargetWindow(t *testing.T) {
	if !strings.Contains(webUIHTML, `:not(.tab-button):not(.auto-login-dc-guide-button):not(#scrollTopButton):not(#scrollBottomButton)`) {
		t.Fatal("window-dependent control handling must not disable navigation")
	}
}
