package main

import (
	"image"
	"image/color"
	"testing"
)

func TestIsDeathRespawnMessage(t *testing.T) {
	if !isDeathRespawnMessage("If you click OK, you will respawn at the last saved location.") {
		t.Fatal("death respawn message was not detected")
	}
	if isDeathRespawnMessage("S4MBO has invited you to join the party") {
		t.Fatal("party invite was incorrectly detected as death")
	}
}

func TestIsResurrectionPromptRequiresBothStablePhrases(t *testing.T) {
	if !isResurrectionPrompt("You are about to recover 90% of your lost Prana. Do you wish to resurrect?") {
		t.Fatal("resurrection prompt was not detected")
	}
	if isResurrectionPrompt("If you click OK, you will respawn at the last saved location.") {
		t.Fatal("ordinary death dialog was treated as resurrection prompt")
	}
	if isResurrectionPrompt("You lost Prana during battle.") {
		t.Fatal("unrelated Prana text was treated as resurrection prompt")
	}
}

func TestDeathPauseResumesOnlyAfterConfirmedHealthyHP(t *testing.T) {
	p := NewDeathPauseController(true, true)
	if !p.TriggerOnce() || !p.ShouldScanResurrection() {
		t.Fatal("death did not enter resurrection wait state")
	}
	p.ObserveDeathDialog(true)
	if p.ShouldWatchHP() {
		t.Fatal("HP must not be watched while the death dialog is still visible")
	}
	if p.ObserveHPPercent(100, true, 70) {
		t.Fatal("full HP behind a visible death dialog must not resume the bot")
	}
	if !p.MarkResurrectionAccepted() {
		t.Fatal("resurrection acceptance was not recorded")
	}
	p.ObserveDeathDialog(false)
	p.ObserveDeathDialog(false)
	if !p.ShouldWatchHP() {
		t.Fatal("HP watching did not begin after the death dialog disappeared")
	}
	if p.ObserveHPPercent(70, true, 70) {
		t.Fatal("HP equal to threshold must not resume")
	}
	if p.ObserveHPPercent(71, true, 70) {
		t.Fatal("bot resumed after only one healthy scan")
	}
	if !p.ObserveHPPercent(72, true, 70) {
		t.Fatal("bot did not resume after consecutive healthy scans")
	}
}

func TestDeathPauseAllowsManualRespawnRecovery(t *testing.T) {
	p := NewDeathPauseController(true, false)
	if !p.TriggerOnce() {
		t.Fatal("manual respawn path did not enter the death state")
	}
	if p.ShouldScanResurrection() {
		t.Fatal("manual respawn path must not wait for an automatic resurrection prompt")
	}
	p.ObserveDeathDialog(false)
	p.ObserveDeathDialog(false)
	if !p.ShouldWatchHP() {
		t.Fatal("manual respawn path did not begin watching HP after the dialog cleared")
	}
	if p.ObserveHPPercent(71, true, 70) {
		t.Fatal("manual respawn resumed after only one healthy scan")
	}
	if !p.ObserveHPPercent(72, true, 70) {
		t.Fatal("manual respawn did not resume after consecutive healthy scans")
	}
}

func TestResurrectionPromptNeedsTwoForegroundScans(t *testing.T) {
	p := NewDeathPauseController(true, true)
	if !p.TriggerOnce() {
		t.Fatal("death state was not entered")
	}
	if p.ObserveResurrectionPrompt(true) {
		t.Fatal("one resurrection scan must not authorize Enter")
	}
	if !p.ObserveResurrectionPrompt(true) {
		t.Fatal("two consecutive resurrection scans must authorize Enter")
	}
	p.ObserveResurrectionPrompt(false)
	if p.ObserveResurrectionPrompt(true) {
		t.Fatal("a missing scan must reset resurrection confirmation")
	}
}

func TestResurrectionDialogForegroundAppearance(t *testing.T) {
	active := image.NewRGBA(image.Rect(0, 0, 160, 80))
	for y := 25; y < 35; y++ {
		for x := 35; x < 80; x++ {
			active.Set(x, y, color.RGBA{225, 225, 225, 255})
		}
	}
	if !resurrectionDialogLooksForeground(active) {
		t.Fatal("bright Message text was not recognized as foreground")
	}

	dim := image.NewRGBA(image.Rect(0, 0, 160, 80))
	for y := 25; y < 35; y++ {
		for x := 35; x < 80; x++ {
			dim.Set(x, y, color.RGBA{95, 95, 95, 255})
		}
	}
	if resurrectionDialogLooksForeground(dim) {
		t.Fatal("dimmed dialog text was treated as foreground")
	}
}

func TestDeathDialogFingerprintBlocksResumeUntilItChanges(t *testing.T) {
	dialog := image.NewRGBA(image.Rect(0, 0, 160, 80))
	for y := 10; y < 70; y++ {
		for x := 10; x < 150; x++ {
			dialog.Set(x, y, color.RGBA{8, 8, 8, 255})
		}
	}
	p := NewDeathPauseController(true, false)
	if !p.TriggerOnce() {
		t.Fatal("death state was not entered")
	}
	p.RememberDeathDialog(deathDialogMaskFromImage(dialog))
	if !p.DeathDialogMatches(deathDialogMaskFromImage(dialog)) {
		t.Fatal("saved death dialog fingerprint was not recognized")
	}

	background := image.NewRGBA(image.Rect(0, 0, 160, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 160; x++ {
			background.Set(x, y, color.RGBA{72, 98, 68, 255})
		}
	}
	if p.DeathDialogMatches(deathDialogMaskFromImage(background)) {
		t.Fatal("ordinary game background matched the death dialog fingerprint")
	}
}
