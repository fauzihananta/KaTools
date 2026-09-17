package main

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// ============================================================
// Bot Config
// ============================================================

type BotConfig struct {
	Enabled bool

	AssistEnabled    bool
	AssistSkillVK    uintptr
	AssistSkillDelay time.Duration

	TargetEnabled bool
	TargetDelay   time.Duration

	AttackEnabled bool
	AttackDelay   time.Duration

	PickEnabled bool
	PickDelay   time.Duration

	Skills []SkillConfig
}

type SkillConfig struct {
	Name    string
	VK      uintptr
	Enabled bool
	Delay   time.Duration

	// Support-only options. Normal attacker skills retain their existing
	// behaviour because both fields default to false.
	//
	// BypassTargetGate is for harmless party support skills such as area heal:
	// it can run even while Target Until Dead is still reading a target name.
	// WaitForTargetClear holds a potentially attacking support skill until the
	// monster bar has disappeared and that disappearance has been confirmed.
	BypassTargetGate   bool
	WaitForTargetClear bool
}

// ============================================================
// Bot Controller
// ============================================================

type BotController struct {
	hwnd uintptr

	mu     sync.Mutex
	config BotConfig

	stop    chan struct{}
	wg      sync.WaitGroup
	running bool

	// chatPaused blocks every bot key while the player is writing in the game
	// chat box. Timers keep advancing, so closing chat never causes a burst of
	// overdue skills.
	chatPaused bool

	// targetActionReady is used by the skip-target filter. While the current
	// target panel is still being identified, it holds both Skills and Attack
	// (R). Target (E) remains available so the bot can acquire another target.
	targetActionReady         bool
	targetActionFilterEnabled bool
	targetGeneration          uint64
	// targetPanelClear becomes true only after Target Until Dead has confirmed
	// that the monster HP bar disappeared. It is used exclusively by Support
	// Skills configured with WaitForTargetClear.
	targetPanelClear             bool
	supportClearDispatchComplete bool
}

// ============================================================
// Constructor
// ============================================================

func NewBotController(
	hwnd uintptr,
	config BotConfig,
) *BotController {

	return &BotController{
		hwnd:              hwnd,
		config:            config,
		stop:              make(chan struct{}),
		running:           false,
		targetActionReady: true,
	}
}

// ============================================================
// IsRunning
// ============================================================

func (b *BotController) IsRunning() bool {

	b.mu.Lock()
	defer b.mu.Unlock()

	return b.running
}

func (b *BotController) SetChatPaused(paused bool) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.chatPaused == paused {
		return false
	}

	b.chatPaused = paused
	return true
}

func (b *BotController) IsChatPaused() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.chatPaused
}

func (b *BotController) SetTargetActionReady(ready bool) {
	b.mu.Lock()
	b.targetActionReady = ready
	b.mu.Unlock()
}

// SetTargetPanelClear supplies the confirmed target-bar state from the WGC
// monitor. A one-frame visual dropout never reaches this method as true.
func (b *BotController) SetTargetPanelClear(clear bool) {
	b.mu.Lock()
	if b.targetPanelClear != clear {
		// A new target-clear window has to be acknowledged by the scheduler
		// before Target Until Dead may send E again. Without this handshake a
		// busy machine can acquire a fresh, full-HP target before its held
		// support skill actually leaves the scheduler.
		b.supportClearDispatchComplete = false
		if clear {
			hasHeldSupportSkill := false
			for _, skill := range b.config.Skills {
				if skill.Enabled && skill.WaitForTargetClear {
					hasHeldSupportSkill = true
					break
				}
			}
			// No non-With Target slots means there is nothing for the
			// scheduler to flush before E.
			b.supportClearDispatchComplete = !hasHeldSupportSkill
		}
	}
	b.targetPanelClear = clear
	b.mu.Unlock()
}

func (b *BotController) supportTargetClearDispatchComplete() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.targetPanelClear && b.supportClearDispatchComplete
}

// shouldDeferEmergencyTarget protects the brief Support target-clear window.
// Emergency non-target skills still run immediately, but an emergency E must
// not acquire a new monster before held non-With Target skills are flushed.
func (b *BotController) shouldDeferEmergencyTarget() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.targetPanelClear && !b.supportClearDispatchComplete
}

func (b *BotController) markSupportTargetClearDispatchComplete() {
	b.mu.Lock()
	if b.targetPanelClear {
		b.supportClearDispatchComplete = true
	}
	b.mu.Unlock()
}

// SetTargetActionFilterEnabled controls whether every successful Target (E)
// press must wait for the target panel to be checked before R or any skill
// may be sent.
func (b *BotController) SetTargetActionFilterEnabled(enabled bool) {
	b.mu.Lock()
	b.targetActionFilterEnabled = enabled
	if !enabled {
		b.targetActionReady = true
	}
	b.mu.Unlock()
}

func (b *BotController) areTargetActionsReady() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.targetActionReady
}

// canRunScheduledTarget prevents the normal repeating Target (E) loop from
// replacing a target while its name is still being validated. The first
// Target after Start remains allowed so the bot can acquire an initial target;
// Target Until Dead and emergency retargets intentionally use CastTarget and
// are not subject to this scheduler-only guard.
func (b *BotController) canRunScheduledTarget() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !b.targetActionFilterEnabled || b.targetActionReady
}

// canRunSkill is intentionally per-slot. In Support mode a safe heal may run
// with a target, while a skill that can start an attack is held until the
// confirmed target-clear window. Attacker mode has neither flag and therefore
// behaves exactly as before.
func (b *BotController) canRunSkill(skill SkillConfig) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if skill.WaitForTargetClear && !b.targetPanelClear {
		return false
	}
	return skill.BypassTargetGate || b.targetActionReady
}

// supportClearDispatchDrained reports whether every non-With Target skill
// that was already due has either been queued or dispatched. Future timer
// periods are intentionally not waited for: only a cast held by the just-dead
// monster must precede E.
func supportClearDispatchDrained(schedules []skillSchedule, queue []queuedCast, now time.Time) bool {
	for _, queued := range queue {
		if queued.skill.WaitForTargetClear {
			return false
		}
	}
	for _, schedule := range schedules {
		if schedule.skill.WaitForTargetClear && !now.Before(schedule.nextCast) {
			return false
		}
	}
	return true
}

// TargetActionsReady exposes the target-name validation gate to emergency
// skills, so they cannot bypass an excluded target while OCR is checking it.
func (b *BotController) TargetActionsReady() bool {
	return b.areTargetActionsReady()
}

func (b *BotController) holdTargetActionsAfterTarget() {
	b.mu.Lock()
	if b.targetActionFilterEnabled {
		b.targetActionReady = false
		b.targetGeneration++
	}
	b.mu.Unlock()
}

// TargetGeneration advances after every successful Target (E) press while
// skip-target validation is enabled. The screen monitor uses it to avoid
// reusing the previous target's OCR result when E selects the same name again.
func (b *BotController) TargetGeneration() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.targetGeneration
}

// ============================================================
// Start
// ============================================================

func (b *BotController) Start() {

	b.mu.Lock()

	// Prevent double start.
	if b.running {
		b.mu.Unlock()

		fmt.Println("[Bot] Already running.")
		return
	}

	if !b.config.Enabled {
		b.mu.Unlock()

		fmt.Println("[Bot] Disabled.")
		return
	}

	// New stop channel every time bot starts.
	b.stop = make(chan struct{})

	// Copy config so scheduler can work without holding mutex.
	config := b.config

	b.running = true

	b.mu.Unlock()

	// ========================================================
	// Assist skill
	// ========================================================
	// In Assist / Off mode the optional selected hotkey is repeatedly pressed.
	// Leaving its dropdown on Off keeps this loop disabled.
	if config.AssistEnabled {
		b.startIndependentLoop(
			"Assist",
			config.AssistSkillDelay,
			config.AssistSkillVK,
		)
	}

	// ========================================================
	// Target
	// ========================================================

	if config.TargetEnabled {

		b.startIndependentLoop(
			"Target",
			config.TargetDelay,
			0x45, // E
		)
	}

	// ========================================================
	// Attack
	// ========================================================

	if config.AttackEnabled {

		b.startIndependentLoop(
			"Attack",
			config.AttackDelay,
			0x52, // R
		)
	}

	// ========================================================
	// Pick
	// ========================================================

	if config.PickEnabled {

		b.startIndependentLoop(
			"Pick",
			config.PickDelay,
			0x46, // F
		)
	}

	// ========================================================
	// Skills
	// ========================================================
	//
	// ALL skills are controlled by ONE scheduler.
	//
	// BUT:
	//
	// Each skill still has its OWN timer.
	//
	// Example:
	//
	// Skill 7 = 1 sec
	// Skill 8 = 35 sec
	//
	// Timeline:
	//
	// 0   -> 7
	// 1   -> 7
	// 2   -> 7
	// ...
	// 35  -> 7 + 8
	//
	// If they collide:
	//
	// 7 executes
	// 8 executes immediately afterward
	//
	// We NEVER reset skill 8 timer to
	// "now + 35 seconds" just because it
	// was waiting in the queue.
	//
	// ========================================================

	b.startSkillScheduler(config.Skills)
}

// ============================================================
// Independent Loop
// ============================================================
//
// Target / Attack / Pick are independent from skill scheduler.
//

func (b *BotController) startIndependentLoop(
	name string,
	delay time.Duration,
	vk uintptr,
) {

	if delay <= 0 {

		fmt.Printf(
			"[Bot] %s disabled: invalid delay %v\n",
			name,
			delay,
		)

		return
	}

	b.wg.Add(1)

	go func() {

		defer b.wg.Done()

		// ----------------------------------------------------
		// First cast immediately
		// ----------------------------------------------------

		select {

		case <-b.stop:
			return

		default:
		}

		cast := func(initial bool) {
			// With a skip-name filter, the first E begins validation. Do not
			// send another scheduled E until that validation has allowed the
			// target, otherwise a slower OCR machine can keep Skills blocked
			// forever while Target itself appears to work normally.
			if name == "Target" && !initial && !b.canRunScheduledTarget() {
				return
			}
			b.castKey(name, vk)
		}

		cast(true)

		// ----------------------------------------------------
		// Timer
		// ----------------------------------------------------

		ticker := time.NewTicker(delay)
		defer ticker.Stop()

		for {

			select {

			case <-ticker.C:

				cast(false)

			case <-b.stop:

				return
			}
		}

	}()

}

// ============================================================
// Skill Schedule
// ============================================================

type skillSchedule struct {
	skill SkillConfig

	// Next scheduled cast.
	nextCast time.Time

	// Used to preserve skill order when two skills
	// have exactly the same due time.
	order int
}

type queuedCast struct {
	skill SkillConfig

	// Original scheduled time.
	//
	// IMPORTANT:
	// This is NOT changed when another skill blocks it.
	dueAt time.Time

	order int
}

// ============================================================
// Start Skill Scheduler
// ============================================================

func (b *BotController) startSkillScheduler(
	skills []SkillConfig,
) {

	schedules := make(
		[]skillSchedule,
		0,
	)

	now := time.Now()

	order := 0

	for _, skill := range skills {

		if !skill.Enabled {
			continue
		}

		if skill.Delay <= 0 {

			fmt.Printf(
				"[Bot] Skill %s ignored: invalid delay %v\n",
				skill.Name,
				skill.Delay,
			)

			continue
		}

		schedules = append(
			schedules,
			skillSchedule{
				skill: skill,

				// First cast immediately.
				nextCast: now,

				order: order,
			},
		)

		order++
	}

	if len(schedules) == 0 {

		fmt.Println("[Bot] No enabled skills.")

		return
	}

	b.wg.Add(1)

	go func() {

		defer b.wg.Done()

		b.runSkillScheduler(
			schedules,
		)

	}()

}

// ============================================================
// Skill Scheduler
// ============================================================

func (b *BotController) runSkillScheduler(
	schedules []skillSchedule,
) {

	// Global queue.
	//
	// Only one key is sent at a time.
	queue := make(
		[]queuedCast,
		0,
	)

	for {

		// ----------------------------------------------------
		// Stop check
		// ----------------------------------------------------

		select {

		case <-b.stop:
			return

		default:
		}

		now := time.Now()

		// ----------------------------------------------------
		// Find skills whose timer has expired.
		// ----------------------------------------------------

		for i := range schedules {

			schedule := &schedules[i]

			if !now.Before(schedule.nextCast) {
				// Do not advance this individual timer while its target rule
				// prevents a cast. When the rule clears, one due cast is sent;
				// missed intervals are coalesced rather than queued as a burst.
				if !b.canRunSkill(schedule.skill) {
					continue
				}

				queue = append(
					queue,
					queuedCast{
						skill: schedule.skill,
						dueAt: schedule.nextCast,
						order: schedule.order,
					},
				)

				// ------------------------------------------------
				// IMPORTANT
				//
				// Advance timer from its ORIGINAL schedule.
				//
				// NOT:
				//
				// nextCast = now + delay
				//
				// This prevents timer reset/drift.
				// ------------------------------------------------

				schedule.nextCast =
					schedule.nextCast.Add(
						schedule.skill.Delay,
					)

				// ------------------------------------------------
				// If scheduler was delayed long enough that
				// several periods were missed, skip missed
				// periods instead of flooding the queue.
				// ------------------------------------------------

				for !schedule.nextCast.After(now) {

					schedule.nextCast =
						schedule.nextCast.Add(
							schedule.skill.Delay,
						)
				}
			}
		}

		// ----------------------------------------------------
		// Sort queue.
		//
		// Earliest scheduled skill first.
		//
		// If same time:
		// original UI/config order wins.
		// ----------------------------------------------------

		if len(queue) > 1 {

			sort.SliceStable(
				queue,
				func(i, j int) bool {

					if queue[i].dueAt.Equal(
						queue[j].dueAt,
					) {

						return queue[i].order <
							queue[j].order
					}

					return queue[i].dueAt.Before(
						queue[j].dueAt,
					)
				},
			)
		}

		if b.targetPanelClear && supportClearDispatchDrained(schedules, queue, now) {
			b.markSupportTargetClearDispatchComplete()
		}

		// ----------------------------------------------------
		// Execute queued skill
		// ----------------------------------------------------

		if len(queue) > 0 {

			// A queued non-With Target skill can become blocked again if E
			// acquires the next monster between scheduling and dispatch. Keep it
			// pending, but do not let it block a later With Target heal that is
			// explicitly allowed to run during an active target.
			readyIndex := -1
			for index, candidate := range queue {
				if b.canRunSkill(candidate.skill) {
					readyIndex = index
					break
				}
			}
			if readyIndex < 0 {
				timer := time.NewTimer(20 * time.Millisecond)
				select {
				case <-timer.C:
				case <-b.stop:
					timer.Stop()
					return
				}
				continue
			}

			next := queue[readyIndex]
			queue = append(queue[:readyIndex], queue[readyIndex+1:]...)

			// Check stop again before casting.
			select {

			case <-b.stop:
				return

			default:
			}

			b.castSkill(next.skill)

			// ------------------------------------------------
			// IMPORTANT:
			//
			// NO SLEEP HERE.
			//
			// If skill 7 and skill 8 collide:
			//
			// 7 executes
			// 8 executes immediately
			//
			// Skill 8's timer remains based on
			// its ORIGINAL schedule.
			// ------------------------------------------------

			continue
		}

		// ----------------------------------------------------
		// Nothing ready.
		//
		// Find the closest next skill.
		// ----------------------------------------------------

		sleepDuration :=
			20 * time.Millisecond

		earliest :=
			schedules[0].nextCast

		for i := 1; i < len(schedules); i++ {

			if schedules[i].nextCast.Before(
				earliest,
			) {

				earliest =
					schedules[i].nextCast
			}
		}

		untilNext :=
			time.Until(earliest)

		if untilNext > 0 &&
			untilNext < sleepDuration {

			sleepDuration =
				untilNext
		}

		if sleepDuration <
			time.Millisecond {

			sleepDuration =
				time.Millisecond
		}

		timer :=
			time.NewTimer(
				sleepDuration,
			)

		select {

		case <-timer.C:

		case <-b.stop:

			timer.Stop()
			return
		}
	}

}

// ============================================================
// Cast Key
// ============================================================

func (b *BotController) castSkill(skill SkillConfig) bool {
	return b.castKeyWithTargetGate(
		"Skill "+skill.Name,
		skill.VK,
		skill.BypassTargetGate,
	)
}

func (b *BotController) castKey(
	name string,
	vk uintptr,
) bool {
	return b.castKeyWithTargetGate(name, vk, false)
}

func (b *BotController) castKeyWithTargetGate(
	name string,
	vk uintptr,
	bypassTargetGate bool,
) bool {
	// Screen monitors (Target Until Dead and target validation) can request a
	// target key outside the regular scheduler. Never let those requests send
	// input after an Auto Pause on Death has stopped the bot.
	if !b.IsRunning() {
		return false
	}
	if (strings.HasPrefix(name, "Skill ") || name == "Attack") && !bypassTargetGate && !b.areTargetActionsReady() {
		return false
	}

	if b.IsChatPaused() {
		return false
	}

	sent :=
		pressKeyToWindow(
			b.hwnd,
			vk,
		)

	if !sent {

		fmt.Printf(
			"[Bot] %-12s -> FAILED key 0x%X\n",
			name,
			vk,
		)
		return false
	}

	if name == "Target" {
		b.holdTargetActionsAfterTarget()
	}
	return true

}

// CastTarget is used by the target-until-dead monitor. Keeping it here means
// the press follows the same chat guard as every other bot action.
func (b *BotController) CastTarget() bool {
	return b.castKey("Target", 0x45)
}

// ============================================================
// Stop
// ============================================================

func (b *BotController) Stop() {

	b.mu.Lock()

	if !b.running {

		b.mu.Unlock()

		return
	}

	// Mark stopped FIRST.
	b.running = false

	stop := b.stop

	b.mu.Unlock()

	fmt.Println("----------------------------------------")
	fmt.Println("[Bot] STOPPING...")
	fmt.Println("----------------------------------------")

	// --------------------------------------------------------
	// Close current stop channel.
	// --------------------------------------------------------

	close(stop)

	// --------------------------------------------------------
	// Wait all goroutines.
	// --------------------------------------------------------

	b.wg.Wait()

	fmt.Println("----------------------------------------")
	fmt.Println("[Bot] STOPPED")
	fmt.Println("----------------------------------------")
}

// ============================================================
// Helpers
// ============================================================

func countEnabledSkills(
	skills []SkillConfig,
) int {

	count := 0

	for _, skill := range skills {

		if skill.Enabled &&
			skill.Delay > 0 {

			count++
		}
	}

	return count
}
