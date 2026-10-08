package main

import "testing"

func TestEmergencySkillAcceptsOnlyNumberAndFunctionKeys(t *testing.T) {
	if !isEmergencySkillVK(0x31) || !isEmergencySkillVK(0x70) || !isEmergencySkillVK(0x79) {
		t.Fatal("valid emergency skill key was rejected")
	}
	if isEmergencySkillVK(0x45) || isEmergencySkillVK(0xA0) {
		t.Fatal("non-skill key was accepted")
	}
}

func TestEmergencySkillOnlyKeepsEnabledValidSlots(t *testing.T) {
	c := NewEmergencySkillController(0, WebBotConfig{EmergencySkills: []WebEmergencySkillConfig{
		{Enabled: true, VK: 0x31},
		{Enabled: false, VK: 0x32},
		{Enabled: true, VK: 0x45},
	}})
	if !c.IsAnyEnabled() {
		t.Fatal("valid enabled emergency slot was not kept")
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.slots) != 1 || c.slots[0].VK != 0x31 {
		t.Fatalf("unexpected active emergency slots: %#v", c.slots)
	}
}

func TestEmergencyTargetIsRequestedOncePerLowHPEpisode(t *testing.T) {
	c := NewEmergencySkillController(0, WebBotConfig{
		TargetEnabled: true,
		EmergencySkills: []WebEmergencySkillConfig{{
			Enabled:     true,
			VK:          0x31,
			NeedsTarget: true,
		}},
	})
	c.ObserveTargetPanel(false, true)
	if !c.ObserveHPPercent(50, true, 70) {
		t.Fatal("solo target-required emergency should request E when no target is active")
	}
	if c.ObserveHPPercent(50, true, 70) {
		t.Fatal("E must not repeat during the same low-HP episode")
	}
	c.ObserveHPPercent(90, true, 70)
	if !c.ObserveHPPercent(50, true, 70) {
		t.Fatal("HP recovery must re-arm emergency target acquisition")
	}
}

func TestAssistEmergencyDoesNotTargetWithoutPanicOption(t *testing.T) {
	c := NewEmergencySkillController(0, WebBotConfig{
		EmergencySkills: []WebEmergencySkillConfig{{
			Enabled:     true,
			VK:          0x31,
			NeedsTarget: true,
		}},
	})
	c.ObserveTargetPanel(false, true)
	if c.ObserveHPPercent(50, true, 70) {
		t.Fatal("ordinary assist mode must not send E")
	}

	c.Update(WebBotConfig{
		AssistPanicTarget: true,
		EmergencySkills: []WebEmergencySkillConfig{{
			Enabled:     true,
			VK:          0x31,
			NeedsTarget: true,
		}},
	})
	c.ObserveTargetPanel(false, true)
	if !c.ObserveHPPercent(50, true, 70) {
		t.Fatal("Assist Panic Target should request one E when no target is active")
	}
}

func TestAssistPanicDoesNotWaitForFirstTargetMonitorFrame(t *testing.T) {
	c := NewEmergencySkillController(0, WebBotConfig{
		AssistPanicTarget: true,
		EmergencySkills: []WebEmergencySkillConfig{{
			Enabled:     true,
			VK:          0x31,
			NeedsTarget: true,
		}},
	})
	if !c.ObserveHPPercent(50, true, 70) {
		t.Fatal("Assist Panic Target should acquire once even before the first target-panel sample")
	}
}

func TestEmergencySkillCooldownPreventsScannerFlood(t *testing.T) {
	c := NewEmergencySkillController(0, WebBotConfig{EmergencySkills: []WebEmergencySkillConfig{{
		Index:   1,
		Enabled: true,
		VK:      0x31,
	}}})
	sent := 0
	c.SetInputSender(func(WebEmergencySkillConfig) bool {
		// Re-entering the controller here also verifies that its mutex is not
		// held while the BotController input callback runs.
		_ = c.IsAnyEnabled()
		sent++
		return true
	})

	c.ObserveHPPercent(50, true, 70)
	c.ObserveHPPercent(50, true, 70)
	if sent != 1 {
		t.Fatalf("two adjacent low-HP scans sent %d emergency casts, want 1", sent)
	}

	// A recovered-then-low episode must fire immediately rather than inherit
	// the previous episode's cooldown.
	c.ObserveHPPercent(90, true, 70)
	c.ObserveHPPercent(50, true, 70)
	if sent != 2 {
		t.Fatalf("new low-HP episode sent %d total emergency casts, want 2", sent)
	}
}

func TestEmergencySkillFailureStillConsumesCooldown(t *testing.T) {
	c := NewEmergencySkillController(0, WebBotConfig{EmergencySkills: []WebEmergencySkillConfig{{
		Index:   1,
		Enabled: true,
		VK:      0x31,
	}}})
	attempts := 0
	c.SetInputSender(func(WebEmergencySkillConfig) bool {
		attempts++
		return false
	})

	c.ObserveHPPercent(50, true, 70)
	c.ObserveHPPercent(50, true, 70)
	if attempts != 1 {
		t.Fatalf("two adjacent failed sends made %d attempts, want 1", attempts)
	}
}
