package main

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestQueuedVirtualClickCancelsBeforePhysicalDelivery(t *testing.T) {
	virtualClickMu.Lock()
	defer virtualClickMu.Unlock()

	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- clickWindowClientPointReferenceCancelable(stop, 0, 0, 0, 0, 0)
	}()
	time.Sleep(20 * time.Millisecond)
	close(stop)

	select {
	case err := <-done:
		if !errors.Is(err, errClickCanceled) {
			t.Fatalf("queued click returned %v, want cancellation", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("queued virtual click did not cancel while the physical lane was busy")
	}
}

func TestRegularVirtualClickYieldsToHigherPriorityInput(t *testing.T) {
	stop := make(chan struct{})
	err := clickWindowClientPointReferencePriority(
		stop,
		func() bool { return true },
		0, 0, 0, 0, 0,
	)
	if !errors.Is(err, errClickPreempted) {
		t.Fatalf("regular click returned %v, want priority preemption", err)
	}
}

func TestSkillPriorityGateOrdersEmergencyBuffAttack(t *testing.T) {
	bot := NewBotController(0, BotConfig{})
	if bot.normalSkillsBlocked() {
		t.Fatal("idle controller unexpectedly blocked attack skills")
	}

	atomic.AddInt32(&bot.buffPending, 1)
	if !bot.normalSkillsBlocked() {
		t.Fatal("pending Buff did not block Attack")
	}
	if !bot.inputPreempted(inputPriorityNormal) {
		t.Fatal("pending Buff did not preempt keyboard Attack")
	}
	if bot.inputPreempted(inputPriorityBuff) || bot.inputPreempted(inputPriorityEmergency) {
		t.Fatal("pending Buff incorrectly preempted Buff/Emergency")
	}
	atomic.AddInt32(&bot.buffPending, -1)

	atomic.AddInt32(&bot.emergencyPending, 1)
	if !bot.normalSkillsBlocked() {
		t.Fatal("pending Emergency/AutoPot did not block Attack")
	}
	if !bot.inputPreempted(inputPriorityNormal) || !bot.inputPreempted(inputPriorityBuff) {
		t.Fatal("pending Emergency/AutoPot did not preempt Attack and Buff")
	}
	if bot.inputPreempted(inputPriorityEmergency) {
		t.Fatal("Emergency priority action preempted itself")
	}
	atomic.AddInt32(&bot.emergencyPending, -1)

	atomic.StoreInt64(&bot.normalHoldUntil, time.Now().Add(time.Second).UnixNano())
	if !bot.normalSkillsBlocked() {
		t.Fatal("post-Buff casting window did not block Attack")
	}
	atomic.StoreInt64(&bot.normalHoldUntil, time.Now().Add(-time.Second).UnixNano())
	if bot.normalSkillsBlocked() {
		t.Fatal("expired casting window still blocked Attack")
	}

	if clickBuffRepeatCount != 3 {
		t.Fatalf("Buff click burst = %d, want 3", clickBuffRepeatCount)
	}
}

func TestPostStopClickSpacingDoesNotSpin(t *testing.T) {
	bot := NewBotController(0, BotConfig{})
	bot.lastInputAt = time.Now()
	close(bot.stop)

	done := make(chan bool, 1)
	go func() {
		locked := bot.lockInputAfterSpacing(20*time.Millisecond, false, false)
		if locked {
			bot.inputMu.Unlock()
		}
		done <- locked
	}()

	select {
	case locked := <-done:
		if !locked {
			t.Fatal("post-stop action allowed by caller should complete its bounded spacing wait")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("post-stop click spacing loop did not terminate")
	}
}

func TestHotkeyResumesBotPausedByDeathWithoutRuntimeRestart(t *testing.T) {
	bot := NewBotController(0, BotConfig{Enabled: true})
	runtime := NewRuntimeManager()
	runtime.running = true
	runtime.bot = bot
	runtime.runningFlag.Store(true)
	runtime.activeControl.Store(&runtimeControl{bot: bot, stopCh: make(chan struct{})})

	if got := runtime.ToggleFromHotkey(); got != "resumed" {
		t.Fatalf("ToggleFromHotkey() = %q, want resumed", got)
	}
	if !bot.IsRunning() {
		t.Fatal("hotkey did not resume the paused bot")
	}
	bot.Stop()
}

func TestClickSkillLiveUpdateDoesNotTrapOldWorkers(t *testing.T) {
	skill := SkillConfig{Name: "1", Enabled: true, Delay: time.Hour, Click: true}
	bot := NewBotController(0, BotConfig{
		Enabled:   true,
		ClickMode: true,
		Skills:    []SkillConfig{skill},
	})
	bot.Start()
	bot.UpdateSkills([]SkillConfig{{Name: "2", Enabled: true, Delay: time.Hour, Click: true}})

	// Give the scheduler time to enter its live-update rebuild. Before the
	// generation channel was captured per worker, that rebuild waited forever.
	time.Sleep(75 * time.Millisecond)
	done := make(chan struct{})
	go func() {
		bot.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop blocked after a live click-skill update")
	}
}

func TestRuntimeStopSignalDoesNotWaitForBotLifecycle(t *testing.T) {
	bot := NewBotController(0, BotConfig{Enabled: true})
	bot.Start()
	bot.lifecycleMu.Lock() // simulate a slow worker cleanup/recovery path

	detectorDone := make(chan struct{})
	close(detectorDone)
	runtime := NewRuntimeManager()
	runtime.running = true
	runtime.bot = bot
	runtime.stopCh = make(chan struct{})
	runtime.doneCh = detectorDone
	runtime.runningFlag.Store(true)
	runtime.activeControl.Store(&runtimeControl{bot: bot, stopCh: runtime.stopCh})

	done := make(chan struct{})
	go func() {
		runtime.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		bot.lifecycleMu.Unlock()
		t.Fatal("Runtime Stop waited behind BotController lifecycle")
	}

	bot.lifecycleMu.Unlock()
}

func TestRuntimeStopSignalDoesNotWaitForRuntimeMutex(t *testing.T) {
	bot := NewBotController(0, BotConfig{Enabled: true})
	bot.Start()
	detectorStop := make(chan struct{})
	runtime := NewRuntimeManager()
	runtime.running = true
	runtime.bot = bot
	runtime.stopCh = detectorStop
	runtime.runningFlag.Store(true)
	runtime.activeControl.Store(&runtimeControl{bot: bot, stopCh: detectorStop})

	runtime.mu.Lock() // simulate a status/window call stuck while holding the lock
	bot.mu.Lock()     // simulate a worker stuck while holding controller state
	done := make(chan struct{})
	go func() {
		runtime.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		bot.mu.Unlock()
		runtime.mu.Unlock()
		t.Fatal("Runtime Stop waited behind RuntimeManager mutex")
	}
	if bot.IsRunning() {
		bot.mu.Unlock()
		runtime.mu.Unlock()
		t.Fatal("lock-free Runtime Stop did not cancel the bot")
	}
	select {
	case <-detectorStop:
	default:
		bot.mu.Unlock()
		runtime.mu.Unlock()
		t.Fatal("lock-free Runtime Stop did not close the detector token")
	}
	bot.mu.Unlock()
	runtime.mu.Unlock()
}

func TestBotStopClosesOnlyCapturedWorkerGeneration(t *testing.T) {
	bot := NewBotController(0, BotConfig{Enabled: true})
	bot.Start()
	first := bot.activeRun.Load()
	bot.Stop()
	bot.Start()
	second := bot.activeRun.Load()

	if first == second {
		t.Fatal("resume reused the previous worker generation")
	}
	select {
	case <-first.stop:
	default:
		t.Fatal("previous worker generation was not cancelled")
	}
	select {
	case <-second.stop:
		t.Fatal("previous Stop leaked into the resumed generation")
	default:
	}
	bot.Stop()
}

func TestStopIssuedBeforeWorkerLaunchCannotBeUndone(t *testing.T) {
	bot := NewBotController(0, BotConfig{Enabled: true})
	publicationGeneration := atomic.LoadUint64(&bot.stopGeneration)
	bot.RequestStop()

	if bot.startWithGeneration(publicationGeneration) {
		t.Fatal("worker generation started after its runtime Stop was already issued")
	}
	if bot.IsRunning() {
		t.Fatal("bot is running after pre-launch Stop")
	}
}

func TestRepeatedPauseResumeDoesNotReuseWorkerTokens(t *testing.T) {
	bot := NewBotController(0, BotConfig{Enabled: true})
	var previous *botRunState
	for i := 0; i < 100; i++ {
		bot.Start()
		current := bot.activeRun.Load()
		if current == previous {
			t.Fatalf("iteration %d reused a worker token", i)
		}
		bot.RequestStop()
		if !bot.WaitForWorkers(time.Second) {
			t.Fatalf("iteration %d did not drain its workers", i)
		}
		select {
		case <-current.stop:
		default:
			t.Fatalf("iteration %d left its worker token open", i)
		}
		previous = current
	}
}

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

// supportClearDispatchDrained always returns true with per-skill goroutines
// (no queue to drain).
func TestSupportClearDispatchDrainedIsAlwaysTrue(t *testing.T) {
	now := time.Now()
	held := SkillConfig{WaitForTargetClear: true}
	if !supportClearDispatchDrained(
		[]skillSchedule{{skill: held, nextCast: now.Add(-time.Second)}},
		nil,
		now,
	) {
		t.Fatal("supportClearDispatchDrained returned false with per-skill goroutines")
	}
	if !supportClearDispatchDrained(
		nil,
		[]queuedCast{{skill: held}},
		now,
	) {
		t.Fatal("supportClearDispatchDrained returned false for queued cast with per-skill goroutines")
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
