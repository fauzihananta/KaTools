package main

import "testing"

func TestTargetUntilDeadOnlyTargetsAfterEmptyConfirmations(t *testing.T) {
	c := NewTargetUntilDeadController(true, true, false, "MangAep")
	if c.Observe(true) {
		t.Fatal("visible monster bar must block target")
	}
	for i := 0; i < targetEmptyConfirmScans-1; i++ {
		if c.Observe(false) {
			t.Fatal("target was sent before enough empty scans")
		}
	}
	if !c.Observe(false) {
		t.Fatal("target was not sent after confirmed empty scans")
	}
	if c.Observe(false) {
		t.Fatal("target repeated immediately")
	}
}

func TestTargetTextMatchesExcludedName(t *testing.T) {
	if !targetTextMatchesExcludedName("MangAep\n2085/2085", "mangaep;Dadati") {
		t.Fatal("first excluded name was not found")
	}
	if !targetTextMatchesExcludedName("Dadati\n1000/1000", "MangAep; Dadati ") {
		t.Fatal("second excluded name was not found")
	}
	if targetTextMatchesExcludedName("Ghorayogi\n1000/1000", "MangAep;Dadati") {
		t.Fatal("different target was treated as own character")
	}
	if !targetTextMatchesExcludedName("rharavani jou 1", "MangAep;Ghorayogi") {
		t.Fatal("common Ghorayogi OCR typo was not matched")
	}
	if !targetTextMatchesExcludedName("fsharayvearil jou 1", "Ghorayogi") {
		t.Fatal("long Ghorayogi OCR typo was not matched")
	}
	if targetTextMatchesExcludedName("farley boodhir", "Ghorayogi") {
		t.Fatal("unrelated OCR text was treated as excluded")
	}
	if !targetTextMatchesExcludedName("at vasabhum caura lv3", "Vasabhum Caura") {
		t.Fatal("multi-word excluded target was not found")
	}
	if targetTextMatchesExcludedName("at vasabhum lv1", "Vasabhum Caura") {
		t.Fatal("shorter target was incorrectly matched to multi-word exclusion")
	}
	if !targetTextMatchesExcludedName("Vasabhum\nLv3", "Vasabhum") {
		t.Fatal("exact one-word excluded target was not found")
	}
	if targetTextMatchesExcludedName("Vasabhum Caura\nLv3", "Vasabhum") {
		t.Fatal("one-word exclusion incorrectly matched a longer monster name")
	}
	if targetTextMatchesExcludedName("Kubasang\nLv58", "Rasuna;Ban;Ban Gosu") {
		t.Fatal("Rasuna must not fuzzy-match the unrelated monster Kubasang")
	}
	if !targetTextMatchesExcludedName("Rasua\nLv7", "Rasuna") {
		t.Fatal("short excluded name lost its one-letter OCR tolerance")
	}
}

func TestTargetTextHasReadableName(t *testing.T) {
	if !targetTextHasReadableName("Zarku Rudhira\n3509/3509") {
		t.Fatal("target name was not recognized")
	}
	if targetTextHasReadableName("3509/3509") {
		t.Fatal("numbers alone were accepted as a target name")
	}
}

func TestTargetTextPotentiallyMatchesIncompleteMultiWordExclusion(t *testing.T) {
	const excluded = "KaTools; Vasabum Caura"
	if !targetTextPotentiallyMatchesExcludedName("ivasabhum lv", excluded) {
		t.Fatal("partial Vasabhum Caura OCR must wait instead of allowing attack")
	}
	if !targetTextPotentiallyMatchesExcludedName("vasabhum tap", excluded) {
		t.Fatal("partial target OCR with the matching first word must wait")
	}
	if targetTextPotentiallyMatchesExcludedName("zarku rudhira lv", excluded) {
		t.Fatal("unrelated target was treated as a possible excluded target")
	}
	if targetTextPotentiallyMatchesExcludedName("vasabhum caura lv", "Vasabhum") {
		t.Fatal("one-word exclusion must not make a multi-word target ambiguous")
	}
}

func TestExcludedTargetsCanRetargetBackToBack(t *testing.T) {
	c := NewTargetUntilDeadController(true, true, false, "MangAep")
	if !c.ForceRetarget() {
		t.Fatal("first excluded target did not retarget")
	}
	if !c.ForceRetarget() {
		t.Fatal("second excluded target was incorrectly blocked by empty-bar cooldown")
	}
}

func TestSupportTargetUntilDeadConfirmsClearBeforeRetarget(t *testing.T) {
	c := NewTargetUntilDeadController(true, true, true, "MangAep")
	for i := 0; i < targetEmptyConfirmScans; i++ {
		if c.Observe(false) {
			t.Fatal("Support mode must reserve a target-free dispatch window before retargeting")
		}
	}
	if !c.TargetClearConfirmed() {
		t.Fatal("Support mode did not expose the confirmed target-clear window")
	}
	c.Observe(true)
	if c.TargetClearConfirmed() {
		t.Fatal("a visible target bar must close the Support target-clear window")
	}
}

func TestTargetUntilDeadStartsWithImmediateTargetRequest(t *testing.T) {
	c := NewTargetUntilDeadController(true, true, false, "MangAep")
	if !c.StartInitialTarget() {
		t.Fatal("enabled Target Until Dead did not authorize its initial target")
	}

	disabled := NewTargetUntilDeadController(false, true, false, "MangAep")
	if disabled.StartInitialTarget() {
		t.Fatal("disabled Target Until Dead authorized an initial target")
	}
}
