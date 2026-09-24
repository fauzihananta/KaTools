package main

import (
	"testing"
	"time"
)

func TestScheduledTargetWaitsForNameValidationButInitialTargetCanStartIt(t *testing.T) {
	bot := NewBotController(0, BotConfig{})
	bot.SetTargetActionFilterEnabled(true)
	bot.SetTargetActionReady(false)

	if bot.canRunScheduledTarget() {
		t.Fatal("repeating Target must wait while name validation is pending")
	}

	bot.SetTargetActionReady(true)
	if !bot.canRunScheduledTarget() {
		t.Fatal("repeating Target must resume after validation allows the target")
	}

	bot.SetTargetActionFilterEnabled(false)
	bot.SetTargetActionReady(false)
	if !bot.canRunScheduledTarget() {
		t.Fatal("Target must not be gated when skip-name filtering is disabled")
	}
}

func TestTargetDelayIsAvailableToValidationFallback(t *testing.T) {
	want := 2 * time.Second
	bot := NewBotController(0, BotConfig{TargetDelay: want})
	if got := bot.TargetDelay(); got != want {
		t.Fatalf("TargetDelay() = %s, want %s", got, want)
	}
}

func TestSupportSkillRulesKeepWithTargetIndependentFromHeldSlots(t *testing.T) {
	bot := NewBotController(0, BotConfig{})
	bot.SetTargetActionFilterEnabled(true)
	bot.SetTargetActionReady(false)
	bot.SetTargetPanelClear(false)

	withTarget := SkillConfig{BypassTargetGate: true}
	if !bot.canRunSkill(withTarget) {
		t.Fatal("With Target support skill must bypass target validation")
	}

	waitForClear := SkillConfig{BypassTargetGate: true, WaitForTargetClear: true}
	if bot.canRunSkill(waitForClear) {
		t.Fatal("non-With Target support skill ran before target clear was confirmed")
	}

	bot.SetTargetPanelClear(true)
	if !bot.canRunSkill(waitForClear) {
		t.Fatal("held support skill did not become eligible after target clear")
	}

	if bot.canRunSkill(SkillConfig{}) {
		t.Fatal("normal attacker skill bypassed pending target validation")
	}
}

func TestSupportClearDispatchWaitsForDueHeldSkill(t *testing.T) {
	now := time.Now()
	held := SkillConfig{WaitForTargetClear: true}
	if supportClearDispatchDrained(
		[]skillSchedule{{skill: held, nextCast: now.Add(-time.Second)}},
		nil,
		now,
	) {
		t.Fatal("a due held Support Skill was treated as dispatched")
	}
	if supportClearDispatchDrained(
		[]skillSchedule{{skill: held, nextCast: now.Add(time.Second)}},
		nil,
		now,
	) != true {
		t.Fatal("a future Support timer should not delay retargeting")
	}
	if supportClearDispatchDrained(
		nil,
		[]queuedCast{{skill: held}},
		now,
	) {
		t.Fatal("a queued held Support Skill was treated as dispatched")
	}
}

func TestEmergencyTargetWaitsForHeldSupportSkillDispatch(t *testing.T) {
	bot := NewBotController(0, BotConfig{Skills: []SkillConfig{{
		Enabled:            true,
		WaitForTargetClear: true,
	}}})
	bot.SetTargetPanelClear(true)
	if !bot.shouldDeferEmergencyTarget() {
		t.Fatal("emergency E bypassed a held Support Skill")
	}

	bot.markSupportTargetClearDispatchComplete()
	if bot.shouldDeferEmergencyTarget() {
		t.Fatal("emergency E remained blocked after Support dispatch completed")
	}
}
