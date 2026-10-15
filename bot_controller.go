package main

import (
	"errors"
	"fmt"
	"math/rand"
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
	KeypressMode       bool
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
	runningFlag    atomic.Bool
	activeRun      atomic.Pointer[botRunState]
	lastStoppedRun atomic.Pointer[botRunState]

	mu     sync.Mutex
	config BotConfig

	// inputMu is the single lane for game hotkeys. Target has its own timer
	// goroutine while normal skills share a scheduler, so without this lock E
	// could reach the game at exactly the same time as a skill (or two queued
	// skills). Kathana can stop responding when that happens. Keeping the lock
	// through the small cooldown makes every action wait for the preceding one.
	inputMu     sync.Mutex
	lastInputAt time.Time
	// Pending flags are atomic counters, not booleans: multiple detector events
	// may overlap. Popup/party actions outrank survival input; both outrank Buff
	// and Attack.
	popupPending     int32
	emergencyPending int32
	buffPending      int32
	normalHoldUntil  int64
	buffCastMu       sync.Mutex

	// castMu serializes skill casts so click-mode cursor operations don't
	// race with each other or with target-panel actions.
	castMu sync.Mutex

	// Test hook for validating multi-click transactions without sending real
	// Windows input. Production controllers leave it nil.
	clickPointOverride func(allowWhenStopped bool, hwnd uintptr, x, y, refW, refH int) error

	// stop mirrors activeRun.stop for the small amount of legacy/test code that
	// inspects it directly. Workers never read this mutable field: every worker
	// receives its own generation token so an old worker cannot accidentally
	// attach itself to a later Death-Pause resume.
	stop                 chan struct{}
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

type botRunState struct {
	stop     chan struct{}
	stopOnce sync.Once
	stopped  atomic.Bool
	wg       sync.WaitGroup
}

func newBotRunState() *botRunState {
	return &botRunState{stop: make(chan struct{})}
}

func (r *botRunState) requestStop() bool {
	if r == nil || !r.stopped.CompareAndSwap(false, true) {
		return false
	}
	r.stopOnce.Do(func() { close(r.stop) })
	return true
}

func (b *BotController) ClickMethodEnabled() bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.config.ClickMode
}

// PopupClickMethodEnabled is true for modes that retain Click Method's
// pointer-based popup flows (party, resurrection, DC, and chat invites).
func (b *BotController) PopupClickMethodEnabled() bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.config.ClickMode || b.config.KeypressMode
}

// pressCombatKey chooses the configured keyboard delivery path. Keypress
// Method uses SendInput (a real foreground key press); Keyboard Method keeps
// the existing targeted WM_KEYDOWN behavior.
func (b *BotController) pressCombatKey(vk uintptr) bool {
	b.mu.Lock()
	keypressMode := b.config.KeypressMode
	hwnd := b.hwnd
	b.mu.Unlock()
	if keypressMode {
		return pressKeyToForegroundWindow(hwnd, vk)
	}
	return pressKeyToWindow(hwnd, vk)
}

// inputQueueSpacing gives independent timer loops a short input frame to finish
// handling a hotkey. The shared input lane prevents overlapping window messages.
const inputQueueSpacing = 80 * time.Millisecond

const clickInputSpacing = 200 * time.Millisecond

// popupDoubleClickDelay is the gap between the first and second click sent to
// popup dialogs (party invite, resurrection, DC). The first click focuses/
// activates the popup; the second click confirms it. Both clicks are sent
// while holding inputMu so no skill click can slip in between.
const popupDoubleClickDelay = 250 * time.Millisecond

const (
	// The Party action has a visible in-game activation phase before the
	// character can be selected. Keep this deliberately longer than a normal
	// UI click so lower-frame-rate clients do not consume the character click
	// as a regular world click.
	partyInviteArmDelay       = 700 * time.Millisecond
	partyInviteCharacterTries = 3
	partyInviteRetryGap       = 120 * time.Millisecond
	// Hold recurring skills after the character click while the server/UI
	// registers the party invitation; the full transaction holds popupPending.
	partyInvitePostClickSettle = 700 * time.Millisecond
)

// ============================================================
// Constructor
// ============================================================

func NewBotController(
	hwnd uintptr,
	config BotConfig,
) *BotController {
	initialRun := newBotRunState()
	b := &BotController{
		hwnd:                 hwnd,
		config:               config,
		stop:                 initialRun.stop,
		skillScheduleUpdates: make(chan []SkillConfig, 1),
		targetActionReady:    true,
	}
	b.activeRun.Store(initialRun)
	return b
}

// ============================================================
// IsRunning
// ============================================================

func (b *BotController) IsRunning() bool {
	return b != nil && b.runningFlag.Load()
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
// that was already due has been dispatched. With per-skill goroutines, there
// is no queue to drain; a held skill fires as soon as targetPanelClear goes
// true. Returns true so markSupportTargetClearDispatchComplete is called
// immediately.
func supportClearDispatchDrained(schedules []skillSchedule, queue []queuedCast, now time.Time) bool {
	return true
}

// Minimal stub type to satisfy test signature; no longer used by scheduler.
type queuedCast struct{ skill SkillConfig }

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
	b.startWithGeneration(atomic.LoadUint64(&b.stopGeneration))
}

// startWithGeneration is used when a controller is published before its
// workers start. A lock-free Runtime Stop increments stopGeneration; passing
// the generation captured before publication prevents that already-issued
// Stop from being mistaken for permission to start a fresh worker set.
func (b *BotController) startWithGeneration(expectedGeneration uint64) bool {
	b.lifecycleMu.Lock()
	defer b.lifecycleMu.Unlock()
	if atomic.LoadUint64(&b.stopGeneration) != expectedGeneration {
		return false
	}

	// Prevent double start.
	if b.runningFlag.Load() {
		fmt.Println("[Bot] Already running.")
		return false
	}

	b.mu.Lock()
	if !b.config.Enabled {
		b.mu.Unlock()

		fmt.Println("[Bot] Disabled.")
		return false
	}

	// Copy config so scheduler can work without holding mutex.
	config := b.config
	run := newBotRunState()
	b.stop = run.stop
	// Updates belong to this exact worker generation. Reusing the channel after
	// Death Pause let a late update be consumed by an old scheduler (or let the
	// old scheduler consume an update intended for the resumed one).
	b.skillScheduleUpdates = make(chan []SkillConfig, 1)
	b.activeRun.Store(run)
	b.runningFlag.Store(true)
	b.mu.Unlock()

	// A lock-free Runtime Stop can race this short publication window. The
	// generation check guarantees that such a Stop wins and no new workers are
	// started after it has already broadcast cancellation.
	if atomic.LoadUint64(&b.stopGeneration) != expectedGeneration {
		b.runningFlag.Store(false)
		run.requestStop()
		b.lastStoppedRun.Store(run)
		return false
	}

	// ========================================================
	// Assist skill
	// ========================================================
	// In Assist / Off mode the optional selected hotkey is repeatedly pressed.
	// Leaving its dropdown on Off keeps this loop disabled.
	if config.AssistEnabled {
		b.startIndependentLoop(
			run,
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
			run,
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
			run,
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
			run,
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

	b.startSkillScheduler(run, config.Skills)
	return true
}

// ============================================================
// Independent Loop
// ============================================================
//
// Target / Attack / Pick have independent timers, but their actual key presses
// still pass through the common input queue with every skill.
//

func (b *BotController) startIndependentLoop(
	run *botRunState,
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

	run.wg.Add(1)

	go func() {

		defer run.wg.Done()

		// ----------------------------------------------------
		// First cast immediately
		// ----------------------------------------------------

		select {

		case <-run.stop:
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
			// The standalone keyboard Attack (R) follows the same priority
			// window as numbered attack skills, so it cannot cut through a Buff
			// animation or a pending Emergency/AutoPot action.
			if name == "Attack" && b.normalSkillsBlocked() {
				return
			}
			b.castKeyForRun(name, vk, false, run.stop)
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

			case <-run.stop:

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

// ============================================================
// Start Skill Scheduler
// ============================================================

func (b *BotController) startSkillScheduler(
	run *botRunState,
	skills []SkillConfig,
) {
	b.mu.Lock()
	if b.skillScheduleUpdates == nil {
		b.skillScheduleUpdates = make(chan []SkillConfig, 1)
	}
	updates := b.skillScheduleUpdates
	clickMode := b.config.ClickMode
	b.mu.Unlock()

	run.wg.Add(1)
	go func() {
		defer run.wg.Done()
		b.runSkillScheduler(run.stop, updates, clickMode, skills)
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
	if !b.runningFlag.Load() {
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

const (
	buffPreFire          = 300 * time.Millisecond
	clickBuffRepeatCount = 3
	clickBuffRepeatGap   = 120 * time.Millisecond
	buffAttackHold       = 1200 * time.Millisecond
	emergencyAttackHold  = 600 * time.Millisecond
)

const (
	inputPriorityNormal = iota
	inputPriorityBuff
	inputPriorityEmergency
	inputPriorityPopup
)

func (b *BotController) runSkillScheduler(
	botStop <-chan struct{},
	updates <-chan []SkillConfig,
	clickMode bool,
	initialSkills []SkillConfig,
) {
	type skillLoopState struct {
		skill     SkillConfig
		nextCast  time.Time
		interrupt chan struct{}
	}

	var mu sync.Mutex
	var activeStates []*skillLoopState

	stopAll := make(chan struct{})
	stopAllMu := sync.Mutex{}
	var stopAllRef chan struct{} = stopAll
	wg := sync.WaitGroup{}

	buildLoops := func(skills []SkillConfig) {
		stopAllMu.Lock()
		if stopAllRef != nil {
			close(stopAllRef)
		}
		stopAll = make(chan struct{})
		stopAllRef = stopAll
		stopAllMu.Unlock()

		wg.Wait()

		mu.Lock()
		activeStates = activeStates[:0]
		mu.Unlock()

		now := time.Now()

		// Collect enabled skills and shuffle them so skill goroutines are created
		// in random order and their initial staggered offsets are unpredictable.
		// This breaks the deterministic 1→2→3→4→5 click sequence when multiple
		// skills share the same delay interval.
		enabled := make([]SkillConfig, 0, len(skills))
		for _, skill := range skills {
			if skill.Enabled && skill.Delay > 0 {
				enabled = append(enabled, skill)
			}
		}
		// Fisher-Yates shuffle using nano time as entropy
		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		for i := len(enabled) - 1; i > 0; i-- {
			j := r.Intn(i + 1)
			enabled[i], enabled[j] = enabled[j], enabled[i]
		}

		order := 0
		for _, skill := range enabled {
			derived := skill
			derived.IsBuff = derived.VK >= 0x70 && derived.VK <= 0x79

			initialOffset := time.Duration(0)
			if clickMode {
				// Base stagger from shuffled position
				baseOffset := time.Duration(order) * clickInputSpacing
				// Add random jitter (0–499ms) so skills with equal delays don't
				// all fire at the same instant, and the first-fire order is not
				// predictable from the config slot numbers.
				jitter := time.Duration(r.Intn(500)) * time.Millisecond
				initialOffset = baseOffset + jitter
			}

			state := &skillLoopState{
				skill:     derived,
				nextCast:  now.Add(initialOffset),
				interrupt: make(chan struct{}),
			}

			mu.Lock()
			activeStates = append(activeStates, state)
			mu.Unlock()

			// Capture the exact generation channel for this worker. Referring to
			// the outer stopAll variable here is unsafe because buildLoops replaces
			// it during a live skill update; old workers would then wait on the new
			// open channel and wg.Wait() could block forever.
			loopStop := stopAll
			wg.Add(1)
			go func(s *skillLoopState, generationStop <-chan struct{}) {
				defer wg.Done()
				for {
					fireAt := s.nextCast
					if s.skill.IsBuff {
						// Announce/cast Buff slightly early so an Attack timer due on
						// the same second cannot enter the physical input lane first.
						fireAt = fireAt.Add(-buffPreFire)
					}
					wait := fireAt.Sub(time.Now())
					if wait > 0 {
						timer := time.NewTimer(wait)
						select {
						case <-timer.C:
						case <-s.interrupt:
							timer.Stop()
							return
						case <-generationStop:
							timer.Stop()
							return
						}
					}

					select {
					case <-s.interrupt:
						return
					case <-generationStop:
						return
					default:
					}

					if !b.canRunSkill(s.skill) {
						timer := time.NewTimer(20 * time.Millisecond)
						select {
						case <-timer.C:
						case <-s.interrupt:
							timer.Stop()
							return
						case <-generationStop:
							timer.Stop()
							return
						}
						select {
						case <-s.interrupt:
							return
						case <-generationStop:
							return
						default:
						}
						if !b.canRunSkill(s.skill) {
							s.nextCast = s.nextCast.Add(s.skill.Delay)
							continue
						}
					}

					select {
					case <-s.interrupt:
						return
					case <-generationStop:
						return
					default:
					}
					// Timers remain independent. Priority is expressed as a gate:
					// Emergency/AutoPot > Buff > Attack. A lower-priority cast is
					// skipped rather than placed in a growing scheduler queue.
					b.castScheduledSkill(s.skill, generationStop)

					s.nextCast = s.nextCast.Add(s.skill.Delay)

					now := time.Now()
					for !s.nextCast.After(now) {
						s.nextCast = s.nextCast.Add(s.skill.Delay)
					}
				}
			}(state, loopStop)

			order++
		}
	}

	buildLoops(initialSkills)
	mu.Lock()
	hasSkills := len(activeStates) > 0
	mu.Unlock()
	if !hasSkills {
		fmt.Println("[Bot] No enabled skills.")
	}
	if clickMode {
		mu.Lock()
		appendOCRLog("CLICK SKILLS LIVE | %d enabled slot(s)", len(activeStates))
		mu.Unlock()
	}

	for {
		select {
		case <-botStop:
			stopAllMu.Lock()
			if stopAllRef != nil {
				close(stopAllRef)
			}
			stopAllMu.Unlock()
			wg.Wait()
			return
		case skills := <-updates:
			buildLoops(skills)
		}
	}
}

// ============================================================
// Cast Key
// ============================================================

func (b *BotController) holdNormalSkillsFor(duration time.Duration) {
	until := time.Now().Add(duration).UnixNano()
	for {
		previous := atomic.LoadInt64(&b.normalHoldUntil)
		if previous >= until || atomic.CompareAndSwapInt64(&b.normalHoldUntil, previous, until) {
			return
		}
	}
}

func (b *BotController) normalSkillsBlocked() bool {
	return atomic.LoadInt32(&b.popupPending) > 0 ||
		atomic.LoadInt32(&b.emergencyPending) > 0 ||
		atomic.LoadInt32(&b.buffPending) > 0 ||
		time.Now().UnixNano() < atomic.LoadInt64(&b.normalHoldUntil)
}

func (b *BotController) waitForEmergencyClear(stop <-chan struct{}) bool {
	for atomic.LoadInt32(&b.popupPending) > 0 || atomic.LoadInt32(&b.emergencyPending) > 0 {
		timer := time.NewTimer(5 * time.Millisecond)
		select {
		case <-stop:
			if !timer.Stop() {
				<-timer.C
			}
			return false
		case <-timer.C:
		}
	}
	return true
}

func waitForSkillGap(stop <-chan struct{}, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-stop:
		return false
	}
}

// waitForInputGap keeps a multi-click transaction responsive to Stop. Actions
// explicitly allowed after Stop (Auto Resu) still receive their bounded delay.
func waitForInputGap(stop <-chan struct{}, duration time.Duration, abortOnStop bool) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	if !abortOnStop {
		<-timer.C
		return true
	}
	select {
	case <-timer.C:
		return true
	case <-stop:
		return false
	}
}

func (b *BotController) castScheduledSkill(skill SkillConfig, stop <-chan struct{}) bool {
	if skill.IsBuff {
		return b.castBuffSkillForRun(skill, stop)
	}
	if b.normalSkillsBlocked() {
		return false
	}
	if skill.Click {
		return b.castClickSkillDirect(skill, stop)
	}

	b.castMu.Lock()
	defer b.castMu.Unlock()
	if b.normalSkillsBlocked() {
		return false
	}
	return b.castSkillForRun(skill, stop)
}

func (b *BotController) castBuffSkillForRun(skill SkillConfig, stop <-chan struct{}) bool {
	atomic.AddInt32(&b.buffPending, 1)
	defer atomic.AddInt32(&b.buffPending, -1)

	b.buffCastMu.Lock()
	defer b.buffCastMu.Unlock()
	if !b.waitForEmergencyClear(stop) {
		return false
	}

	if !skill.Click {
		b.castMu.Lock()
		sent := b.castInputWithTargetGateForRunPriority(
			"Skill "+skill.Name,
			skill.BypassTargetGate || skill.TargetSearch,
			stop,
			inputPriorityBuff,
			func() bool {
				if !b.canRunSkill(skill) {
					return false
				}
				return b.pressCombatKey(skill.VK)
			},
		)
		b.castMu.Unlock()
		if sent {
			b.holdNormalSkillsFor(buffAttackHold)
			appendOCRLog("BUFF PRIORITY | Skill %s | Keyboard cast | Attack hold=%s", skill.Name, buffAttackHold)
		}
		return sent
	}

	if !skill.ClickAreaSet || !b.IsRunning() || b.IsChatPaused() || !b.canRunSkill(skill) {
		return false
	}

	sentAny := false
	for clickIndex := 0; clickIndex < clickBuffRepeatCount; clickIndex++ {
		for {
			if !b.waitForEmergencyClear(stop) {
				return sentAny
			}
			err := clickWindowClientPointReferencePriority(
				stop,
				func() bool {
					return atomic.LoadInt32(&b.popupPending) > 0 || atomic.LoadInt32(&b.emergencyPending) > 0
				},
				b.hwnd,
				skill.ClickX,
				skill.ClickY,
				skill.ClickReferenceWidth,
				skill.ClickReferenceHeight,
			)
			if errors.Is(err, errClickPreempted) {
				continue
			}
			if errors.Is(err, errClickCanceled) {
				return sentAny
			}
			if err != nil {
				b.observeVirtualClickResult(err)
				appendOCRLog("VIRTUAL CLICK FAILED | Buff %s (%d/%d) | Saved=%d,%d | Reference=%dx%d | %v", skill.Name, clickIndex+1, clickBuffRepeatCount, skill.ClickX, skill.ClickY, skill.ClickReferenceWidth, skill.ClickReferenceHeight, err)
			} else {
				b.observeVirtualClickResult(nil)
				sentAny = true
			}
			break
		}
		if clickIndex+1 < clickBuffRepeatCount && !waitForSkillGap(stop, clickBuffRepeatGap) {
			return sentAny
		}
	}

	if sentAny {
		// Attacks are not queued during this window; their own timers simply
		// advance. This protects the longer Buff animation without creating a
		// burst of overdue attack clicks afterward.
		b.holdNormalSkillsFor(buffAttackHold)
		appendOCRLog("BUFF PRIORITY | Skill %s | Click burst=%d | Gap=%s | Attack hold=%s", skill.Name, clickBuffRepeatCount, clickBuffRepeatGap, buffAttackHold)
	}
	return sentAny
}

func (b *BotController) castSkill(skill SkillConfig) bool {
	return b.castScheduledSkill(skill, b.currentStopChannel())
}

func (b *BotController) castSkillForRun(skill SkillConfig, stop <-chan struct{}) bool {
	// Click skills deliberately bypass inputMu. They have independent timers and
	// must not wait for keyboard actions (E/R/F) or the 200 ms click spacing.
	// clickWindowClientPointReference owns only the short physical-cursor
	// critical section required by Windows; it is not a scheduler queue.
	if skill.Click {
		return b.castClickSkillDirect(skill, stop)
	}
	return b.castInputWithTargetGateForRun(
		"Skill "+skill.Name,
		skill.BypassTargetGate || skill.TargetSearch,
		stop,
		func() bool {
			if !b.canRunSkill(skill) {
				return false
			}
			if skill.Click {
				if !skill.ClickAreaSet {
					fmt.Printf("[Bot] Skill %s -> FAILED click area is not set\n", skill.Name)
					return false
				}
				if err := b.clickPoint(false, b.hwnd, skill.ClickX, skill.ClickY, skill.ClickReferenceWidth, skill.ClickReferenceHeight); err != nil {
					if errors.Is(err, errClickCanceled) {
						return false
					}
					b.observeVirtualClickResult(err)
					fmt.Printf("[Bot] Skill %s -> FAILED click: %v\n", skill.Name, err)
					appendOCRLog("VIRTUAL CLICK FAILED | Skill %s | Saved=%d,%d | Reference=%dx%d | %v", skill.Name, skill.ClickX, skill.ClickY, skill.ClickReferenceWidth, skill.ClickReferenceHeight, err)
					return false
				}
				b.observeVirtualClickResult(nil)
				return true
			}
			return b.pressCombatKey(skill.VK)
		},
	)
}

func (b *BotController) castClickSkillDirect(skill SkillConfig, stop <-chan struct{}) bool {
	select {
	case <-stop:
		return false
	default:
	}
	if !b.IsRunning() || b.IsChatPaused() || !b.canRunSkill(skill) || b.normalSkillsBlocked() {
		return false
	}
	if !skill.ClickAreaSet {
		fmt.Printf("[Bot] Skill %s -> FAILED click area is not set\n", skill.Name)
		return false
	}
	if err := clickWindowClientPointReferencePriority(
		stop,
		b.normalSkillsBlocked,
		b.hwnd,
		skill.ClickX,
		skill.ClickY,
		skill.ClickReferenceWidth,
		skill.ClickReferenceHeight,
	); err != nil {
		if errors.Is(err, errClickCanceled) || errors.Is(err, errClickPreempted) {
			return false
		}
		b.observeVirtualClickResult(err)
		fmt.Printf("[Bot] Skill %s -> FAILED click: %v\n", skill.Name, err)
		appendOCRLog("VIRTUAL CLICK FAILED | Skill %s | Saved=%d,%d | Reference=%dx%d | %v", skill.Name, skill.ClickX, skill.ClickY, skill.ClickReferenceWidth, skill.ClickReferenceHeight, err)
		return false
	}
	b.observeVirtualClickResult(nil)
	return true
}

func (b *BotController) CastEmergencySkill(skill WebEmergencySkillConfig) bool {
	name := fmt.Sprintf("Emergency Skill %d", skill.Index)
	// Signal that an emergency wants to fire immediately. This interrupts any
	// regular skill that is mid-spacing-wait so the emergency click is not
	// delayed by attack skills saturating the input channel.
	stop := b.currentStopChannel()
	atomic.AddInt32(&b.emergencyPending, 1)
	defer atomic.AddInt32(&b.emergencyPending, -1)
	sent := b.castInputWithTargetGateForRunPriority(name, true, stop, inputPriorityEmergency, func() bool {
		if skill.Click {
			if !skill.ClickAreaSet {
				fmt.Printf("[Bot] %s -> FAILED click area is not set\n", name)
				return false
			}
			if err := b.clickPointForRun(
				stop,
				false,
				func() bool { return atomic.LoadInt32(&b.popupPending) > 0 },
				b.hwnd,
				skill.ClickX,
				skill.ClickY,
				skill.ClickReferenceWidth,
				skill.ClickReferenceHeight,
			); err != nil {
				if errors.Is(err, errClickCanceled) || errors.Is(err, errClickPreempted) {
					return false
				}
				b.observeVirtualClickResult(err)
				fmt.Printf("[Bot] %s -> FAILED click: %v\n", name, err)
				appendOCRLog("VIRTUAL CLICK FAILED | %s | Saved=%d,%d | Reference=%dx%d | %v", name, skill.ClickX, skill.ClickY, skill.ClickReferenceWidth, skill.ClickReferenceHeight, err)
				return false
			}
			b.observeVirtualClickResult(nil)
			return true
		}
		return b.pressCombatKey(skill.VK)
	})
	if sent {
		b.holdNormalSkillsFor(emergencyAttackHold)
	}
	return sent
}

// CastPriorityKeyAction routes keyboard-mode AutoPot and similar survival
// actions through the same preemptive lane as click-mode AutoPot/Emergency.
func (b *BotController) CastPriorityKeyAction(name string, vk uintptr) bool {
	if !b.IsRunning() || b.IsChatPaused() {
		return false
	}
	stop := b.currentStopChannel()
	atomic.AddInt32(&b.emergencyPending, 1)
	defer atomic.AddInt32(&b.emergencyPending, -1)

	sent := b.castInputWithTargetGateForRunPriority(name, true, stop, inputPriorityEmergency, func() bool {
		return b.pressCombatKey(vk)
	})
	if sent {
		b.holdNormalSkillsFor(emergencyAttackHold)
	}
	return sent
}

// CastPriorityClickAction is the click-mode counterpart used by AutoPot. It
// outranks Buff/Attack but yields to the short popup/party transaction.
func (b *BotController) CastPriorityClickAction(name string, hwnd uintptr, x, y, refW, refH int) bool {
	if !b.IsRunning() || b.IsChatPaused() {
		return false
	}
	stop := b.currentStopChannel()
	atomic.AddInt32(&b.emergencyPending, 1)
	defer atomic.AddInt32(&b.emergencyPending, -1)

	sent := b.castInputWithTargetGateForRunPriority(name, true, stop, inputPriorityEmergency, func() bool {
		err := b.clickPointForRun(
			stop,
			false,
			func() bool { return atomic.LoadInt32(&b.popupPending) > 0 },
			hwnd,
			x,
			y,
			refW,
			refH,
		)
		if err == nil {
			b.observeVirtualClickResult(nil)
			return true
		}
		if !errors.Is(err, errClickCanceled) && !errors.Is(err, errClickPreempted) {
			b.observeVirtualClickResult(err)
			appendOCRLog("VIRTUAL CLICK FAILED | %s | Saved=%d,%d | Reference=%dx%d | %v", name, x, y, refW, refH, err)
		}
		return false
	})
	if sent {
		b.holdNormalSkillsFor(emergencyAttackHold)
	}
	return sent
}

// lockInputAfterSpacing returns with inputMu held. It never sleeps while
// holding the lane, and every return after locking releases it. This is
// important during Stop: an old spacing wait must not leave inputMu locked for
// the next runtime start.
//
// abortOnStop must be false only for an action deliberately allowed after the
// bot has stopped (Auto Resu). In that state b.stop is already closed, so
// selecting on it while waiting would win immediately on every iteration and
// spin forever. That loop kept runPicker alive and made a later Alt+F12 Stop
// appear to hang.
func (b *BotController) lockInputAfterSpacingForRun(stop <-chan struct{}, spacing time.Duration, abortForPriority, abortOnStop bool) bool {
	for {
		b.inputMu.Lock()
		// With no previous input there is nothing to space against. Initializing
		// wait to spacing and leaving lastInputAt empty made the first popup/party
		// action sleep forever in this loop.
		if b.lastInputAt.IsZero() {
			return true
		}
		wait := spacing - time.Since(b.lastInputAt)
		if wait <= 0 {
			return true
		}
		b.inputMu.Unlock()

		// Normal input checks for a newly pending popup at least every 10 ms.
		// The popup action itself keeps the full spacing so the game receives a
		// clean click, but it should never wait behind a skill's whole cooldown.
		sleepFor := wait
		if abortForPriority && sleepFor > 10*time.Millisecond {
			sleepFor = 10 * time.Millisecond
		}
		// Normal bot actions abort promptly during Stop. An action explicitly
		// allowed after Stop waits only for its bounded spacing timer: b.stop is
		// permanently closed in that state and cannot be selected on.
		timer := time.NewTimer(sleepFor)
		if abortOnStop {
			select {
			case <-timer.C:
			case <-stop:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				return false
			}
		} else {
			<-timer.C
		}
		if abortForPriority && atomic.LoadInt32(&b.emergencyPending) != 0 {
			return false
		}
	}
}

func (b *BotController) lockInputAfterSpacing(spacing time.Duration, abortForPriority, abortOnStop bool) bool {
	return b.lockInputAfterSpacingForRun(b.currentStopChannel(), spacing, abortForPriority, abortOnStop)
}

// CastClickAction handles AutoAccept, AutoResu, and DCOk as an atomic popup
// transaction. The popupPending gate keeps recurring click skills and survival
// actions from entering between focus and confirmation.
//
// If secondClickDelay > 0, a second click is sent after that delay while
// inputMu is still held, so no skill click can slip in between the two clicks.
// This is needed for popups that require a first click to focus/activate them
// before the second click actually confirms the action.
func (b *BotController) CastClickAction(name string, hwnd uintptr, x, y, refW, refH int, secondClickDelay time.Duration, allowWhenStopped bool) bool {
	if (!allowWhenStopped && !b.IsRunning()) || b.IsChatPaused() {
		return false
	}

	stop := b.currentStopChannel()
	// Popup actions outrank every recurring automation path. The counter covers
	// the complete focus+confirm transaction, including its inter-click gap.
	atomic.AddInt32(&b.popupPending, 1)
	defer atomic.AddInt32(&b.popupPending, -1)

	if !b.lockInputAfterSpacingForRun(stop, clickInputSpacing, false, !allowWhenStopped) {
		return false
	}
	defer b.inputMu.Unlock()
	if (!allowWhenStopped && !b.IsRunning()) || b.IsChatPaused() {
		return false
	}

	// First click.
	err := b.clickPointForRun(stop, allowWhenStopped, nil, hwnd, x, y, refW, refH)
	if err != nil {
		if errors.Is(err, errClickCanceled) {
			return false
		}
		b.observeVirtualClickResult(err)
		appendOCRLog("VIRTUAL CLICK FAILED | %s | Saved=%d,%d | Reference=%dx%d | %v", name, x, y, refW, refH, err)
		return false
	}
	b.observeVirtualClickResult(nil)
	b.lastInputAt = time.Now()
	b.holdNormalSkillsFor(emergencyAttackHold)
	clickTotal := 1
	if secondClickDelay > 0 {
		clickTotal = 2
	}
	appendOCRLog("POPUP CLICK SENT | Action=%s | Click=1/%d | Saved=%d,%d | Reference=%dx%d", name, clickTotal, x, y, refW, refH)

	// Optional second click (e.g. to confirm a popup that needs focus first).
	// inputMu remains held throughout so skill clicks cannot interleave.
	if secondClickDelay > 0 {
		if !waitForInputGap(stop, secondClickDelay, !allowWhenStopped) {
			return false
		}
		err = b.clickPointForRun(stop, allowWhenStopped, nil, hwnd, x, y, refW, refH)
		if err != nil {
			if errors.Is(err, errClickCanceled) {
				return false
			}
			b.observeVirtualClickResult(err)
			appendOCRLog("VIRTUAL CLICK FAILED | %s (2nd) | Saved=%d,%d | Reference=%dx%d | %v", name, x, y, refW, refH, err)
			// The first click only focuses/activates the popup. Do not report the
			// action as accepted unless the second confirmation click lands.
			return false
		}
		b.observeVirtualClickResult(nil)
		b.lastInputAt = time.Now()
		appendOCRLog("POPUP CLICK SENT | Action=%s | Click=2/2 | Saved=%d,%d | Reference=%dx%d", name, x, y, refW, refH)
	}
	b.holdNormalSkillsFor(emergencyAttackHold)

	return true
}

// CastClickAction2 runs the complete ChatParty transaction: arm Party mode,
// wait for the game UI, click the character, then keep every other input tier
// blocked while the game consumes that click. A transient character-click
// failure is retried without releasing popupPending.
func (b *BotController) CastClickAction2(hwnd uintptr, x1, y1, refW1, refH1, x2, y2, refW2, refH2 int) bool {
	if !b.IsRunning() || b.IsChatPaused() {
		return false
	}

	stop := b.currentStopChannel()
	atomic.AddInt32(&b.popupPending, 1)
	defer atomic.AddInt32(&b.popupPending, -1)

	if !b.lockInputAfterSpacingForRun(stop, clickInputSpacing, false, true) {
		return false
	}
	defer b.inputMu.Unlock()
	if !b.IsRunning() || b.IsChatPaused() {
		return false
	}

	err := b.clickPointForRun(stop, false, nil, hwnd, x1, y1, refW1, refH1)
	if err != nil {
		if errors.Is(err, errClickCanceled) {
			return false
		}
		b.observeVirtualClickResult(err)
		appendOCRLog("CHAT PARTY CLICK FAILED | Saved=%d,%d | Reference=%dx%d | %v", x1, y1, refW1, refH1, err)
		return false
	}
	b.observeVirtualClickResult(nil)
	appendOCRLog("CHAT PARTY CLICK SENT | Saved=%d,%d | Reference=%dx%d | Waiting=%s for character click", x1, y1, refW1, refH1, partyInviteArmDelay)
	if !waitForInputGap(stop, partyInviteArmDelay, true) {
		appendOCRLog("CHAT CHARACTER CLICK CANCELED | Runtime stopped during Party activation delay")
		return false
	}

	characterSent := false
	for attempt := 1; attempt <= partyInviteCharacterTries; attempt++ {
		err = b.clickPointForRun(stop, false, nil, hwnd, x2, y2, refW2, refH2)
		if err == nil {
			b.observeVirtualClickResult(nil)
			characterSent = true
			appendOCRLog("CHAT CHARACTER CLICK SENT | Attempt=%d/%d | Saved=%d,%d | Reference=%dx%d", attempt, partyInviteCharacterTries, x2, y2, refW2, refH2)
			break
		}
		if errors.Is(err, errClickCanceled) {
			appendOCRLog("CHAT CHARACTER CLICK CANCELED | Attempt=%d/%d | Runtime stopped", attempt, partyInviteCharacterTries)
			return false
		}
		b.observeVirtualClickResult(err)
		appendOCRLog("CHAT CHARACTER CLICK FAILED | Attempt=%d/%d | Saved=%d,%d | Reference=%dx%d | %v", attempt, partyInviteCharacterTries, x2, y2, refW2, refH2, err)
		if attempt < partyInviteCharacterTries && !waitForInputGap(stop, partyInviteRetryGap, true) {
			return false
		}
	}

	// Even on a physical API failure, do not immediately release Buff/skill
	// input after Party mode was armed. This prevents the next timer from
	// consuming that mode while the invite flow is still settling/retrying.
	b.lastInputAt = time.Now()
	b.holdNormalSkillsFor(emergencyAttackHold)
	if !waitForInputGap(stop, partyInvitePostClickSettle, true) {
		return false
	}
	return characterSent
}

func (b *BotController) clickPoint(allowWhenStopped bool, hwnd uintptr, x, y, refW, refH int) error {
	return b.clickPointForRun(b.currentStopChannel(), allowWhenStopped, nil, hwnd, x, y, refW, refH)
}

func (b *BotController) clickPointForRun(stop <-chan struct{}, allowWhenStopped bool, preempted func() bool, hwnd uintptr, x, y, refW, refH int) error {
	if !allowWhenStopped {
		select {
		case <-stop:
			return errClickCanceled
		default:
		}
	}
	if preempted != nil && preempted() {
		return errClickPreempted
	}
	if b.clickPointOverride != nil {
		return b.clickPointOverride(allowWhenStopped, hwnd, x, y, refW, refH)
	}
	if allowWhenStopped {
		return clickWindowClientPointReference(hwnd, x, y, refW, refH)
	}
	return clickWindowClientPointReferencePriority(stop, preempted, hwnd, x, y, refW, refH)
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
	return b.castKeyForRun(name, vk, bypassTargetGate, b.currentStopChannel())
}

func (b *BotController) castKeyForRun(name string, vk uintptr, bypassTargetGate bool, stop <-chan struct{}) bool {
	return b.castInputWithTargetGateForRun(name, bypassTargetGate, stop, func() bool {
		return b.pressCombatKey(vk)
	})
}

func (b *BotController) castInputWithTargetGate(
	name string,
	bypassTargetGate bool,
	sendInput func() bool,
) bool {
	return b.castInputWithTargetGateForRun(name, bypassTargetGate, b.currentStopChannel(), sendInput)
}

func (b *BotController) castInputWithTargetGateForRun(
	name string,
	bypassTargetGate bool,
	stop <-chan struct{},
	sendInput func() bool,
) bool {
	return b.castInputWithTargetGateForRunPriority(name, bypassTargetGate, stop, inputPriorityNormal, sendInput)
}

func (b *BotController) inputPreempted(priority int) bool {
	switch priority {
	case inputPriorityPopup:
		return false
	case inputPriorityEmergency:
		return atomic.LoadInt32(&b.popupPending) > 0
	case inputPriorityBuff:
		return atomic.LoadInt32(&b.popupPending) > 0 || atomic.LoadInt32(&b.emergencyPending) > 0
	default:
		return b.normalSkillsBlocked()
	}
}

func (b *BotController) castInputWithTargetGateForRunPriority(
	name string,
	bypassTargetGate bool,
	stop <-chan struct{},
	priority int,
	sendInput func() bool,
) bool {
	select {
	case <-stop:
		return false
	default:
	}
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
			// Let emergency popup actions take precedence while a normal skill
			// is waiting for the game's input spacing.
			for time.Since(b.lastInputAt) < b.minimumInputSpacing(name) {
				b.inputMu.Unlock()
				select {
				case <-time.After(10 * time.Millisecond):
				case <-stop:
					return false
				}
				if b.inputPreempted(priority) {
					return false
				}
				b.inputMu.Lock()
				if !b.IsRunning() || b.IsChatPaused() {
					b.inputMu.Unlock()
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
	select {
	case <-stop:
		return false
	default:
	}
	if !b.IsRunning() || b.IsChatPaused() {
		return false
	}
	if b.inputPreempted(priority) {
		return false
	}

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

// RequestStop flips the running state and broadcasts cancellation without
// waiting for click/key workers to drain. RuntimeManager uses this for UI and
// global-hotkey Stop so those control paths can never sit behind an input
// operation. The detached runtime owns this BotController, so its workers can
// finish safely in the background.
func (b *BotController) RequestStop() bool {
	atomic.AddUint64(&b.stopGeneration, 1)
	return b.signalStop()
}

func (b *BotController) WaitForWorkers(timeout time.Duration) bool {
	run := b.lastStoppedRun.Load()
	if run == nil {
		run = b.activeRun.Load()
	}
	if run == nil {
		return true
	}
	done := make(chan struct{})
	go func() {
		run.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

func (b *BotController) stopForRecovery() bool {
	return b.stopBot(false)
}

func (b *BotController) signalStop() bool {
	run := b.activeRun.Load()
	b.runningFlag.Store(false)
	if run == nil {
		return false
	}
	b.lastStoppedRun.Store(run)
	return run.requestStop()
}

func (b *BotController) stopBot(userRequested bool) bool {
	b.lifecycleMu.Lock()
	defer b.lifecycleMu.Unlock()
	run := b.activeRun.Load()
	if userRequested {
		atomic.AddUint64(&b.stopGeneration, 1)
	}
	if !b.signalStop() {
		return false
	}

	fmt.Println("----------------------------------------")
	fmt.Println("[Bot] STOPPING...")
	fmt.Println("----------------------------------------")

	// --------------------------------------------------------
	// Close current stop channel.
	// --------------------------------------------------------

	// --------------------------------------------------------
	// Wait all goroutines.
	// Give goroutines up to 5 seconds to drain. If they are still
	// running (e.g. stuck in a retry loop), force-exit to unblock
	// the hotkey callback so the user can toggle Start/Stop again.
	// --------------------------------------------------------
	done := make(chan struct{})
	go func() {
		if run != nil {
			run.wg.Wait()
		}
		close(done)
	}()
	select {
	case <-done:
		fmt.Println("[Bot] All goroutines finished cleanly.")
	case <-time.After(5 * time.Second):
		fmt.Println("[Bot] WARNING: goroutines did not exit within 5s — forcing stop.")
		appendOCRLog("BOT STOP FORCED | Goroutines did not exit within 5s; forced stop")
	}

	fmt.Println("----------------------------------------")
	fmt.Println("[Bot] STOPPED")
	fmt.Println("----------------------------------------")
	return true
}

func (b *BotController) currentStopChannel() <-chan struct{} {
	if run := b.activeRun.Load(); run != nil {
		return run.stop
	}
	// A controller always has an initial run token, but keep a permanently
	// closed fallback so cancellation still wins if construction ever changes.
	closed := make(chan struct{})
	close(closed)
	return closed
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
		stopGeneration := atomic.LoadUint64(&b.stopGeneration)
		time.Sleep(1500 * time.Millisecond)
		cancelled := b.IsRunning() || atomic.LoadUint64(&b.stopGeneration) != stopGeneration
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
