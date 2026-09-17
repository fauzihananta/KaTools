package main

import "testing"

func TestParseStatusPairs(t *testing.T) {
	hpCurrent, hpMax, tpCurrent, tpMax, ok := parseStatusPairs("1945/2102\n2352 / 2497")
	if !ok || hpCurrent != 1945 || hpMax != 2102 || tpCurrent != 2352 || tpMax != 2497 {
		t.Fatalf("unexpected parse: hp=%d/%d tp=%d/%d ok=%v", hpCurrent, hpMax, tpCurrent, tpMax, ok)
	}
}

func TestParseStatusPairsRejectsInvalidValues(t *testing.T) {
	if _, _, _, _, ok := parseStatusPairs("2200/2102 2352/2497"); ok {
		t.Fatal("invalid current value was accepted")
	}
}

func TestParseStatusPairsRepairsSingleLeadingDigitOnFullValue(t *testing.T) {
	hpCurrent, hpMax, tpCurrent, tpMax, ok := parseStatusPairs("70 3414/2414 2364/2423")
	if !ok {
		t.Fatal("unambiguous OCR typo was not repaired")
	}
	if hpCurrent != 2414 || hpMax != 2414 || tpCurrent != 2364 || tpMax != 2423 {
		t.Fatalf("unexpected repaired values: hp=%d/%d tp=%d/%d", hpCurrent, hpMax, tpCurrent, tpMax)
	}
}

func TestAutoPotRejectsUnrealisticOCRMaximum(t *testing.T) {
	p := NewAutoPotController(0, WebBotConfig{})
	p.Observe(2, 19, 3, 19)
	if p.HasStatusMaxima() {
		t.Fatal("incorrect OCR maximum was accepted")
	}

	p.Observe(888, 1360, 1000, 1500)
	if !p.HasStatusMaxima() {
		t.Fatal("plausible OCR maximum was rejected")
	}
}

func TestAutoPotAcceptsLowLevelStatusMaximum(t *testing.T) {
	p := NewAutoPotController(0, WebBotConfig{})
	p.Observe(50, 50, 30, 30)
	if !p.HasStatusMaxima() {
		t.Fatal("low-level HP/TP maxima were rejected")
	}
}

func TestAutoPotLearnsRepeatedFullMaximumWithoutTrustingCurrentBarRead(t *testing.T) {
	p := NewAutoPotController(0, WebBotConfig{})

	if hpLearned, _ := p.LearnFullStatusMaximums(6334, 6334, 0, 0); hpLearned {
		t.Fatal("one full OCR reading must not immediately lock a maximum")
	}
	if hpLearned, _ := p.LearnFullStatusMaximums(6334, 6334, 0, 0); !hpLearned {
		t.Fatal("second matching full OCR reading did not lock HP maximum")
	}

	status := p.Status()
	if status.HPMax != 6334 || status.HPCurrent != 0 || status.TPMax != 0 {
		t.Fatalf("learned status = %+v, want HP max only with no OCR current", status)
	}
}

func TestAutoPotDoesNotLearnMalformedFullMaximum(t *testing.T) {
	p := NewAutoPotController(0, WebBotConfig{})
	if hpLearned, _ := p.LearnFullStatusMaximums(6334, 63347, 0, 0); hpLearned {
		t.Fatal("non-full OCR pair must not lock a maximum")
	}
	if p.Status().HPMax != 0 {
		t.Fatal("malformed maximum was stored")
	}
}

func TestAutoPotTreatsMissingHPFillAsZeroWhenTPBarIsVisible(t *testing.T) {
	p := NewAutoPotController(0, WebBotConfig{
		AutoPotHPEnabled: true,
		AutoPotHPPercent: 70,
		AutoPotHPSlotVK:  50,
	})
	p.Observe(50, 50, 30, 30)

	events := p.ObserveBarPercents(0, false, 100, true)
	status := p.Status()
	if status.HPCurrent != 0 || status.HPMax != 50 {
		t.Fatalf("empty HP bar = %d/%d, want 0/50", status.HPCurrent, status.HPMax)
	}
	if len(events) != 1 || events[0].Resource != "HP" || events[0].Current != 0 {
		t.Fatalf("unexpected events for empty HP bar: %+v", events)
	}
}

func TestAutoPotResetStatusDiscardsPreviousCrop(t *testing.T) {
	p := NewAutoPotController(0, WebBotConfig{})
	p.Observe(50, 50, 30, 30)
	p.ResetStatus()

	if p.HasStatusMaxima() {
		t.Fatal("status maxima from the previous crop were retained")
	}
	status := p.Status()
	if status.HPCurrent != 0 || status.HPMax != 0 || status.TPCurrent != 0 || status.TPMax != 0 {
		t.Fatalf("status after reset = %+v, want empty values", status)
	}
}

func TestAutoPotCalibratesOverestimatedBarPercent(t *testing.T) {
	p := NewAutoPotController(0, WebBotConfig{})
	p.Observe(2464, 2464, 2557, 2557)

	// A crop with uneven borders can report both fills as 100% even though the
	// verified same-frame HUD values are below full.
	p.CalibrateBarPercents(2364, 2464, 100, true, 2237, 2557, 100, true)
	p.ObserveBarPercents(100, true, 100, true)

	status := p.Status()
	if status.HPCurrent != 2364 || status.TPCurrent != 2237 {
		t.Fatalf("calibrated status = HP %d/%d TP %d/%d, want 2364/2464 2237/2557", status.HPCurrent, status.HPMax, status.TPCurrent, status.TPMax)
	}
}

func TestStatusPairMatchesBarRejectsIncorrectFullTP(t *testing.T) {
	if statusPairMatchesBar(23964, 23964, 94.7, true) {
		t.Fatal("full OCR value was accepted despite a visibly non-full bar")
	}
	if !statusPairMatchesBar(2238, 2363, 94.7, true) {
		t.Fatal("OCR value matching the visible bar was rejected")
	}
	if !statusPairMatchesBar(2364, 2464, 100, true) {
		t.Fatal("credible OCR value was rejected when the colour bar overestimated it")
	}
}

func TestConfiguredHPThresholdIsAvailableWhenHPPotIsOff(t *testing.T) {
	p := NewAutoPotController(0, WebBotConfig{
		AutoPotHPEnabled: false,
		AutoPotHPPercent: 70,
	})
	if p.HPThreshold() != 0 {
		t.Fatal("disabled HP Pot must not expose a potion-use threshold")
	}
	if p.ConfiguredHPThreshold() != 70 {
		t.Fatal("death recovery must retain the configured HP threshold")
	}
}
