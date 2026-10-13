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

func TestChatKeywordPartyInviteIsAvailableInKeyboardAndClickModes(t *testing.T) {
	for _, token := range []string{
		"Chat keyword party invite",
		"Available in Keyboard and Click Method.",
		"chatTextClickEnabled: document.getElementById(\"chatTextClickEnabled\").checked,",
		"clickAreaControlsChatParty",
		"clickAreaControlsPartyAutomatic",
		"clickAreaControlsAutoAccept",
		"clickAreaControlsAutoReject",
		"chatTextInviteControls",
		".roi-picker-row[hidden] { display: none !important; }",
	} {
		if !strings.Contains(webUIHTML, token) {
			t.Fatalf("web UI is missing shared party-invite token %q", token)
		}
	}
}
