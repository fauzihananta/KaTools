package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"KaTools/cursor_hook"

	"golang.org/x/sys/windows"
)

// getWindowPID returns the process ID for the given window handle.
func getWindowPID(hwnd uintptr) (uint32, error) {
	user32 := windows.NewLazyDLL("user32.dll")
	proc := user32.NewProc("GetWindowThreadProcessId")
	var pid uint32
	ret, _, _ := proc.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if ret == 0 {
		return 0, fmt.Errorf("GetWindowThreadProcessId returned 0 for HWND %x", hwnd)
	}
	return pid, nil
}

// ============================================================
// Bot Config
// ============================================================

type BotConfig struct {
	Enabled bool

	AssistEnabled    bool
	AssistSkillVK    uintptr
	AssistSkillDelay time.Duration

	TargetEnabled      bool
	TargetDelay        time.Duration
	ClickMode          bool
	ClickWhitelistMode bool

	AttackEnabled bool
	AttackDelay   time.Duration

	PickEnabled bool
	PickDelay   time.Duration

	Skills []SkillConfig
}

type SkillConfig struct {
	Name                 string
	VK                   uintptr
	Enabled              bool
	Delay                time.Duration
	Click                bool
	ClickAreaSet         bool
	ClickX               int
	ClickY               int
	ClickReferenceWidth  int
	ClickReferenceHeight int

	// Support-only options. Normal attacker skills retain their existing
	// behaviour because both fields default to false.
	//
	// BypassTargetGate is for harmless party support skills such as area heal:
	// it can run even while Target Until Dead is still reading a target name.
	// WaitForTargetClear holds a potentially attacking support skill until the
	// monster bar has disappeared and that disappearance has been confirmed.
	BypassTargetGate   bool
	WaitForTargetClear bool
	TargetSearch       bool
	IsBuff             bool
}

// ============================================================
// Bot Controller
// ============================================================

type BotController struct {
	hwnd           uintptr
	cursorHookCtrl *cursor_hook.Controller
	lifecycleMu    sync.Mutex
	stopGeneration uint64

	mu     sync.Mutex
	config BotConfig

	// inputMu is the single lane for game hotkeys. Target has its own timer
	// goroutine while normal skills share a scheduler, so without this lock E
	// could reach the game at exactly the same time as a skill (or two queued
	// skills). Kathana can stop responding when that happens. Keeping the lock
	// through the small cooldown makes every action wait for the preceding one.
	inputMu           sync.Mutex
	lastInputAt       time.Time
	emergencyPending  int32 // atomic; emergency skill wants to fire, break spacing wait

	stop                 chan struct{}
	wg                   sync.WaitGroup
	running              bool
	skillScheduleUpdates chan []SkillConfig

	cursorRecoveryMu      sync.Mutex
	cursorAPIFailureCount int
	cursorAPIFailureSince time.Time
	cursorRecoveryUsed    bool

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

func (b *BotController) ClickMethodEnabled() bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.config.ClickMode
}

// inputQueueSpacing gives independent timer loops a short input frame to finish
// handling a hotkey. The shared input lane prevents overlapping window messages.
const inputQueueSpacing = 80 * time.Millisecond
const clickInputSpacing = 200 * time.Millisecond

// ============================================================
// Constructor
// ============================================================

func NewBotController(
	hwnd uintptr,
	config BotConfig,
) *BotController {

	return &BotController{
		hwnd:                 hwnd,
		config:               config,
		stop:                 make(chan struct{}),
		skillScheduleUpdates: make(chan []SkillConfig, 1),
		running:              false,
		targetActionReady:    true,
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

	if b.config.ClickMode && b.config.ClickWhitelistMode {
		if skill.TargetSearch {
			return !b.targetActionReady
		}
		return b.targetActionReady
	}

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

func (b *BotController) IsClickWhitelistMode() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.config.ClickMode && b.config.ClickWhitelistMode
}

// TargetDelay exposes the active normal Target interval to the screen monitor.
// The monitor uses it only as a retry deadline when normal Target's name OCR
// cannot establish a result; Target Until Dead keeps its independent cadence.
func (b *BotController) TargetDelay() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.config.TargetDelay
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
	b.lifecycleMu.Lock()
	defer b.lifecycleMu.Unlock()

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
// Target / Attack / Pick have independent timers, but their actual key presses
// still pass through the common input queue with every skill.
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
	b.mu.Lock()
	if b.skillScheduleUpdates == nil {
		b.skillScheduleUpdates = make(chan []SkillConfig, 1)
	}
	updates := b.skillScheduleUpdates
	clickMode := b.config.ClickMode
	b.mu.Unlock()
	schedules := makeSkillSchedules(skills, clickMode)
	if len(schedules) == 0 {
		fmt.Println("[Bot] No enabled skills.")
	}

	b.wg.Add(1)

	go func() {

		defer b.wg.Done()

		b.runSkillScheduler(
			schedules,
			updates,
		)

	}()

}

func makeSkillSchedules(skills []SkillConfig, clickMode bool) []skillSchedule {
	schedules := make([]skillSchedule, 0, len(skills))
	initialSkillOffset := time.Duration(0)
	if clickMode {
		initialSkillOffset = clickInputSpacing
	}
	now := time.Now()
	order := 0
	for _, skill := range skills {
		if !skill.Enabled {
			continue
		}
		if skill.Delay <= 0 {
			fmt.Printf("[Bot] Skill %s ignored: invalid delay %v\n", skill.Name, skill.Delay)
			continue
		}
		derived := skill
		// F1–F10 use VK 0x70–0x79; slots 1–0 use VK 0x30–0x39.
		derived.IsBuff = derived.VK >= 0x70 && derived.VK <= 0x79
		schedules = append(schedules, skillSchedule{
			skill: derived,
			// In click mode, offset the initial casts so equal timers are
			// delivered as a steady one-at-a-time stream instead of a burst.
			nextCast: now.Add(time.Duration(order) * initialSkillOffset),
			order:    order,
		})
		order++
	}
	return schedules
}

// UpdateSkills swaps the active scheduler list without stopping any runtime
// workers. The scheduler drops pending casts for unchecked slots and starts
// newly enabled slots on their normal staggered Click cadence.
func (b *BotController) UpdateSkills(skills []SkillConfig) bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	b.config.Skills = append([]SkillConfig(nil), skills...)
	if !b.running {
		b.mu.Unlock()
		return false
	}
	if b.skillScheduleUpdates == nil {
		b.skillScheduleUpdates = make(chan []SkillConfig, 1)
	}
	updates := b.skillScheduleUpdates
	clickMode := b.config.ClickMode
	b.mu.Unlock()

	copySkills := append([]SkillConfig(nil), skills...)
	select {
	case updates <- copySkills:
	default:
		select {
		case <-updates:
		default:
		}
		select {
		case updates <- copySkills:
		default:
		}
	}
	if clickMode {
		appendOCRLog("CLICK SKILLS LIVE | %d enabled slot(s)", len(makeSkillSchedules(skills, true)))
	}
	return true
}

// ============================================================
// Skill Scheduler
// ============================================================

const buffPreFire = 300 * time.Millisecond

func (b *BotController) runSkillScheduler(
	schedules []skillSchedule,
	updates <-chan []SkillConfig,
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
		case skills := <-updates:
			schedules = makeSkillSchedules(skills, b.ClickMethodEnabled())
			queue = queue[:0]
			continue

		default:
		}

		now := time.Now()

		// ----------------------------------------------------
		// Find skills whose timer has expired (or within pre-fire
		// window for buff skills).
		// ----------------------------------------------------

		for i := range schedules {

			schedule := &schedules[i]

			// Buff skills pre-fire 300ms early to compensate for their
			// longer casting time and to win timing clashes with attack
			// skills.
			preFireTime := schedule.nextCast
			if schedule.skill.IsBuff {
				preFireTime = schedule.nextCast.Add(-buffPreFire)
			}

			if !now.Before(preFireTime) {
				// Do not advance this individual timer while its target rule
				// prevents a cast. When the rule clears, one due cast is sent;
				// missed intervals are coalesced rather than queued as a burst.
				if !b.canRunSkill(schedule.skill) {
					continue
				}

				// Use original nextCast as dueAt (not preFireTime) so the
				// sort reflects the real schedule, and so the next pre-fire
				// calculation is based on the correct anchor.
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
		// Buff skills always execute before attack skills at the
		// same scheduled time. Within each group, earliest time
		// wins; ties are broken by original UI/config order.
		// ----------------------------------------------------

		if len(queue) > 1 {

			sort.SliceStable(
				queue,
				func(i, j int) bool {

					// Buffs always win over attacks.
					if queue[i].skill.IsBuff != queue[j].skill.IsBuff {
						return queue[i].skill.IsBuff
					}

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

		if len(schedules) == 0 {
			select {
			case <-b.stop:
				return
			case skills := <-updates:
				schedules = makeSkillSchedules(skills, b.ClickMethodEnabled())
			}
			continue
		}

		earliest := schedules[0].nextCast

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
		case skills := <-updates:
			timer.Stop()
			schedules = makeSkillSchedules(skills, b.ClickMethodEnabled())
			queue = queue[:0]
			continue
		}
	}

}

// ============================================================
// Cast Key
// ============================================================

func (b *BotController) castSkill(skill SkillConfig) bool {
	return b.castInputWithTargetGate(
		"Skill "+skill.Name,
		skill.BypassTargetGate || skill.TargetSearch,
		func() bool {
			if !b.canRunSkill(skill) {
				return false
			}
			if skill.Click {
				if !skill.ClickAreaSet {
					fmt.Printf("[Bot] Skill %s -> FAILED click area is not set\n", skill.Name)
					return false
				}
				if err := clickWindowClientPointReference(b.hwnd, skill.ClickX, skill.ClickY, skill.ClickReferenceWidth, skill.ClickReferenceHeight); err != nil {
					b.observeVirtualClickResult(err)
					fmt.Printf("[Bot] Skill %s -> FAILED click: %v\n", skill.Name, err)
					appendOCRLog("VIRTUAL CLICK FAILED | Skill %s | Saved=%d,%d | Reference=%dx%d | %v", skill.Name, skill.ClickX, skill.ClickY, skill.ClickReferenceWidth, skill.ClickReferenceHeight, err)
					return false
				}
				b.observeVirtualClickResult(nil)
				return true
			}
			return pressKeyToWindow(b.hwnd, skill.VK)
		},
	)
}

func (b *BotController) CastEmergencySkill(skill WebEmergencySkillConfig) bool {
	name := fmt.Sprintf("Emergency Skill %d", skill.Index)
	// Signal that an emergency wants to fire immediately. This interrupts any
	// regular skill that is mid-spacing-wait so the emergency click is not
	// delayed by attack skills saturating the input channel.
	atomic.StoreInt32(&b.emergencyPending, 1)
	defer atomic.StoreInt32(&b.emergencyPending, 0)
	return b.castInputWithTargetGate(name, true, func() bool {
		if skill.Click {
			if !skill.ClickAreaSet {
				fmt.Printf("[Bot] %s -> FAILED click area is not set\n", name)
				return false
			}
			if err := clickWindowClientPointReference(b.hwnd, skill.ClickX, skill.ClickY, skill.ClickReferenceWidth, skill.ClickReferenceHeight); err != nil {
				b.observeVirtualClickResult(err)
				fmt.Printf("[Bot] %s -> FAILED click: %v\n", name, err)
				appendOCRLog("VIRTUAL CLICK FAILED | %s | Saved=%d,%d | Reference=%dx%d | %v", name, skill.ClickX, skill.ClickY, skill.ClickReferenceWidth, skill.ClickReferenceHeight, err)
				return false
			}
			b.observeVirtualClickResult(nil)
			return true
		}
		return pressKeyToWindow(b.hwnd, skill.VK)
	})
}

// CastClickAction handles AutoAccept, AutoResu, and DCOk clicks through the
// same input channel as skills so they don't race with skill clicks. These
// are critical survival actions and get emergency-level priority.
func (b *BotController) CastClickAction(name string, hwnd uintptr, x, y, refW, refH int) bool {
	if !b.IsRunning() || b.IsChatPaused() {
		return false
	}

	// Same atomic signaling as CastEmergencySkill: regular skills mid-spacing-wait
	// will detect this and abort so the critical click fires immediately.
	atomic.StoreInt32(&b.emergencyPending, 1)
	defer atomic.StoreInt32(&b.emergencyPending, 0)

	b.inputMu.Lock()

	if !b.lastInputAt.IsZero() {
		wait := clickInputSpacing - time.Since(b.lastInputAt)
		if wait > 0 {
			timer := time.NewTimer(wait)
			timer.Stop()
			for time.Since(b.lastInputAt) < clickInputSpacing {
				b.inputMu.Unlock()
				select {
				case <-time.After(10 * time.Millisecond):
				case <-b.stop:
					return false
				}
				if atomic.LoadInt32(&b.emergencyPending) != 0 {
					return false
				}
				b.inputMu.Lock()
				if !b.IsRunning() || b.IsChatPaused() {
					return false
				}
			}
		}
	}

	b.inputMu.Unlock()

	if !b.IsRunning() || b.IsChatPaused() {
		return false
	}

	b.inputMu.Lock()
	defer b.inputMu.Unlock()

	err := clickWindowClientPointReference(hwnd, x, y, refW, refH)
	if err != nil {
		b.observeVirtualClickResult(err)
		appendOCRLog("VIRTUAL CLICK FAILED | %s | Saved=%d,%d | Reference=%dx%d | %v", name, x, y, refW, refH, err)
		return false
	}
	b.observeVirtualClickResult(nil)
	b.lastInputAt = time.Now()
	return true
}

// CastClickAction2 sends two sequential clicks through the same input channel as
// skills. Used by ChatParty which clicks the party button first, then the
// character name/position. Both clicks are atomic — if the first fails the second
// is skipped.
func (b *BotController) CastClickAction2(hwnd uintptr, x1, y1, refW1, refH1, x2, y2, refW2, refH2 int) bool {
	if !b.IsRunning() || b.IsChatPaused() {
		return false
	}

	atomic.StoreInt32(&b.emergencyPending, 1)
	defer atomic.StoreInt32(&b.emergencyPending, 0)

	b.inputMu.Lock()

	if !b.lastInputAt.IsZero() {
		wait := clickInputSpacing - time.Since(b.lastInputAt)
		if wait > 0 {
			timer := time.NewTimer(wait)
			timer.Stop()
			for time.Since(b.lastInputAt) < clickInputSpacing {
				b.inputMu.Unlock()
				select {
				case <-time.After(10 * time.Millisecond):
				case <-b.stop:
					return false
				}
				if atomic.LoadInt32(&b.emergencyPending) != 0 {
					return false
				}
				b.inputMu.Lock()
				if !b.IsRunning() || b.IsChatPaused() {
					return false
				}
			}
		}
	}

	b.inputMu.Unlock()

	if !b.IsRunning() || b.IsChatPaused() {
		return false
	}

	b.inputMu.Lock()
	defer b.inputMu.Unlock()

	err := clickWindowClientPointReference(hwnd, x1, y1, refW1, refH1)
	if err != nil {
		b.observeVirtualClickResult(err)
		appendOCRLog("CHAT PARTY CLICK FAILED | Saved=%d,%d | Reference=%dx%d | %v", x1, y1, refW1, refH1, err)
		return false
	}
	b.observeVirtualClickResult(nil)
	time.Sleep(150 * time.Millisecond)

	err = clickWindowClientPointReference(hwnd, x2, y2, refW2, refH2)
	if err != nil {
		b.observeVirtualClickResult(err)
		appendOCRLog("CHAT CHARACTER CLICK FAILED | Saved=%d,%d | Reference=%dx%d | %v", x2, y2, refW2, refH2, err)
		return false
	}
	b.observeVirtualClickResult(nil)
	b.lastInputAt = time.Now()
	return true
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
	return b.castInputWithTargetGate(name, bypassTargetGate, func() bool {
		return pressKeyToWindow(b.hwnd, vk)
	})
}

func (b *BotController) castInputWithTargetGate(
	name string,
	bypassTargetGate bool,
	sendInput func() bool,
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

	// E and skills are scheduled by different goroutines. Serialize the final
	// key delivery here (rather than only in the skill scheduler) so Target
	// Until Dead and any future caller use the same queue as well.

	b.inputMu.Lock()

	if !b.lastInputAt.IsZero() {
		wait := b.minimumInputSpacing(name) - time.Since(b.lastInputAt)
		if wait > 0 {
			timer := time.NewTimer(wait)
			timer.Stop()
			// Emergency skills interrupt the spacing wait so they can fire
			// immediately even when attack skills are saturating the input channel.
			for time.Since(b.lastInputAt) < b.minimumInputSpacing(name) {
				b.inputMu.Unlock()
				select {
				case <-time.After(10 * time.Millisecond):
				case <-b.stop:
					return false
				}
				if atomic.LoadInt32(&b.emergencyPending) != 0 {
					return false
				}
				b.inputMu.Lock()
				// Re-check after re-acquiring: state or running may have changed
				if !b.IsRunning() || b.IsChatPaused() {
					return false
				}
			}
		}
	}

	b.inputMu.Unlock()

	// State may have changed while this action waited behind another hotkey.
	// Recheck it so a Skill does not sneak past an E that just began target-name
	// validation, and so Stop/Chat Pause cancels pending deliveries.
	if !b.IsRunning() || b.IsChatPaused() {
		return false
	}
	if (strings.HasPrefix(name, "Skill ") || name == "Attack") && !bypassTargetGate && !b.areTargetActionsReady() {
		return false
	}

	b.inputMu.Lock()
	defer b.inputMu.Unlock()

	sent := sendInput()

	if !sent {

		fmt.Printf(
			"[Bot] %-12s -> FAILED input\n",
			name,
		)
		return false
	}

	b.lastInputAt = time.Now()

	if name == "Target" {
		b.holdTargetActionsAfterTarget()
	}
	return true

}

// CastTarget is used by the target-until-dead monitor. Keeping it here means
// the press follows the same chat guard as every other bot action.
func (b *BotController) CastTarget() bool {
	if !b.IsRunning() || b.IsChatPaused() {
		return false
	}
	return b.castKey("Target", 0x45)
}

func (b *BotController) minimumInputSpacing(name string) time.Duration {
	b.mu.Lock()
	clickMode := b.config.ClickMode
	b.mu.Unlock()
	if clickMode && (strings.HasPrefix(name, "Skill ") || strings.HasPrefix(name, "Emergency Skill ")) {
		return clickInputSpacing
	}
	return inputQueueSpacing
}

// ============================================================
// Stop
// ============================================================

func (b *BotController) Stop() {
	_ = b.stopBot(true)
}

func (b *BotController) stopForRecovery() bool {
	return b.stopBot(false)
}

func (b *BotController) stopBot(userRequested bool) bool {
	b.lifecycleMu.Lock()
	defer b.lifecycleMu.Unlock()
	if userRequested {
		b.stopGeneration++
	}

	b.mu.Lock()

	if !b.running {

		b.mu.Unlock()

		return false
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
	return true
}

func (b *BotController) observeVirtualClickResult(err error) {
	b.cursorRecoveryMu.Lock()
	if err == nil {
		b.cursorAPIFailureCount = 0
		b.cursorAPIFailureSince = time.Time{}
		b.cursorRecoveryUsed = false
		b.cursorRecoveryMu.Unlock()
		return
	}
	if !errors.Is(err, errCursorAPI) {
		b.cursorAPIFailureCount = 0
		b.cursorAPIFailureSince = time.Time{}
		b.cursorRecoveryMu.Unlock()
		return
	}
	now := time.Now()
	if b.cursorAPIFailureSince.IsZero() || now.Sub(b.cursorAPIFailureSince) > 20*time.Second {
		b.cursorAPIFailureSince = now
		b.cursorAPIFailureCount = 0
	}
	b.cursorAPIFailureCount++
	shouldRecover := b.cursorAPIFailureCount >= 3 && !b.cursorRecoveryUsed
	if shouldRecover {
		b.cursorRecoveryUsed = true
	}
	failureCount := b.cursorAPIFailureCount
	b.cursorRecoveryMu.Unlock()

	if !shouldRecover {
		return
	}
	appendOCRLog("BOT RECOVERY | %d cursor API failures within 20s; restarting bot once", failureCount)
	fmt.Printf("[Bot] Cursor API failed %d times; restarting bot once.\n", failureCount)
	go func() {
		if !b.IsRunning() {
			appendOCRLog("BOT RECOVERY | skipped because bot was already stopped")
			return
		}
		if !b.stopForRecovery() {
			appendOCRLog("BOT RECOVERY | skipped because bot was stopped before recovery began")
			return
		}
		b.lifecycleMu.Lock()
		stopGeneration := b.stopGeneration
		b.lifecycleMu.Unlock()
		time.Sleep(1500 * time.Millisecond)
		b.lifecycleMu.Lock()
		cancelled := b.running || b.stopGeneration != stopGeneration
		b.lifecycleMu.Unlock()
		if cancelled {
			// A user or another workflow restarted it during the recovery delay.
			return
		}
		b.Start()
		if b.IsRunning() {
			appendOCRLog("BOT RECOVERY | bot restarted; persistent cursor access errors will not trigger a restart loop")
		} else {
			appendOCRLog("BOT RECOVERY | restart requested but bot did not start (disabled or already stopped)")
		}
	}()
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
