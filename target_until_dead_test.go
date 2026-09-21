package main

import "testing"

func TestTargetUntilDeadOnlyTargetsAfterEmptyConfirmations(t *testing.T) {
	c := NewTargetUntilDeadController(true, true, false, "MangAep", targetNameFilterModeSkip)
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
	if !targetTextMatchesExcludedName("kembs tsyns aes lv75", "Kembo Tonyo") {
		t.Fatal("camera-noisy Kembo Tonyo OCR was not matched")
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

func TestTargetTextMatchesWhitelistedNameStrictly(t *testing.T) {
	const names = "Mob Tonyo;Vasabhum"
	if !targetTextMatchesWhitelistedName("mob tonyo lv75", names) {
		t.Fatal("exact whitelisted target was not found")
	}
	if !targetTextMatchesWhitelistedName("mab tonyp lv75", names) {
		t.Fatal("one-character OCR typo in each whitelist word was not accepted")
	}
	if !targetTextMatchesWhitelistedName("mob to nyo lw75", names) {
		t.Fatal("a short OCR word split was not accepted for the whitelisted target")
	}
	if targetTextMatchesWhitelistedName("kembo tonyo lv75", names) {
		t.Fatal("Kembo Tonyo must not match the Mob Tonyo whitelist entry")
	}
	if targetTextMatchesWhitelistedName("mob caura lv75", names) {
		t.Fatal("shared first word was enough to whitelist a different target")
	}
}

func TestOverlappingKathanaNamesOnlyMatchTheirCompleteName(t *testing.T) {
	names := []string{
		"Ugra Ulkhamuka Satvan", "Ugra Ulkhamuka", "Ulkhamuka Satvan",
		"Ulkhamuka", "Ulkhamuka Caura",
		"Mlechas", "Mlechas Caura",
		"Srbinda", "Srbinda Satvan",
		"Ananga", "Ananga Dvanta",
		"Pizac", "Pizac Aggana",
		"Zarku", "Zarku Rudhira",
	}
	for _, configured := range names {
		for _, target := range names {
			text := "at " + target + " Lv75"
			want := configured == target
			if got := targetTextMatchesExcludedName(text, configured); got != want {
				t.Errorf("skip configured=%q target=%q: got %t, want %t", configured, target, got, want)
			}
			if got := targetTextMatchesWhitelistedName(text, configured); got != want {
				t.Errorf("whitelist configured=%q target=%q: got %t, want %t", configured, target, got, want)
			}
		}
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
	if targetTextPotentiallyMatchesExcludedName("vasabhum tap", excluded) {
		t.Fatal("complete two-word target was treated as a clipped exclusion")
	}
	if targetTextPotentiallyMatchesExcludedName("zarku rudhira lv", excluded) {
		t.Fatal("unrelated target was treated as a possible excluded target")
	}
	if targetTextPotentiallyMatchesExcludedName("vasabhum caura lv", "Vasabhum") {
		t.Fatal("one-word exclusion must not make a multi-word target ambiguous")
	}
}

func TestTargetSingleWordNameKey(t *testing.T) {
	if got := targetSingleWordNameKey("Vasabhum\nLv75"); got != "vasabhum" {
		t.Fatalf("got %q, want vasabhum", got)
	}
	if got := targetSingleWordNameKey("Vasabhum Caura\nLv75"); got != "" {
		t.Fatalf("multi-word target produced one-word key %q", got)
	}
}

func TestExcludedTargetsCanRetargetBackToBack(t *testing.T) {
	c := NewTargetUntilDeadController(true, true, false, "MangAep", targetNameFilterModeSkip)
	if !c.ForceRetarget() {
		t.Fatal("first excluded target did not retarget")
	}
	if !c.ForceRetarget() {
		t.Fatal("second excluded target was incorrectly blocked by empty-bar cooldown")
	}
}

func TestSupportTargetUntilDeadConfirmsClearBeforeRetarget(t *testing.T) {
	c := NewTargetUntilDeadController(true, true, true, "MangAep", targetNameFilterModeSkip)
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
	c := NewTargetUntilDeadController(true, true, false, "MangAep", targetNameFilterModeSkip)
	if !c.StartInitialTarget() {
		t.Fatal("enabled Target Until Dead did not authorize its initial target")
	}

	disabled := NewTargetUntilDeadController(false, true, false, "MangAep", targetNameFilterModeSkip)
	if disabled.StartInitialTarget() {
		t.Fatal("disabled Target Until Dead authorized an initial target")
	}
}

func TestTargetNameFilterModeDefaultsToSkip(t *testing.T) {
	c := NewTargetUntilDeadController(true, true, false, "Vasabhum", "")
	if c.IsWhitelistMode() {
		t.Fatal("legacy empty filter mode must preserve skip behaviour")
	}
	c.Update(true, true, false, "Vasabhum", targetNameFilterModeWhitelist)
	if !c.IsWhitelistMode() {
		t.Fatal("whitelist mode was not retained")
	}
	whitelist := NewTargetUntilDeadController(true, true, false, "Vasabhum", targetNameFilterModeWhitelist)
	if !whitelist.StartInitialTarget() {
		t.Fatal("whitelist mode did not preserve the immediate initial target request")
	}
}
