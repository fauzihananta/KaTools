package main

import (
	"sync"
	"time"
)

const emergencySkillSlotCount = 5
const emergencySkillRepeatCooldown = 750 * time.Millisecond

// A colour scan is much more reliable than the tiny HUD digits, but a single
// WGC frame can still contain an animation or a clipped crop that resembles a
// short red bar.  Emergency skills are high-impact actions, so require two
// consecutive low readings before opening an emergency episode.  Likewise,
// require a small, stable recovery before re-arming it; otherwise one noisy
// full-bar frame could turn a sustained low-HP state into repeated casts.
const emergencyLowHPConfirmations = 2
const emergencyRecoveryConfirmations = 2
const emergencyRecoveryMarginPercent = 3.0

type EmergencySkillController struct {
	mu sync.RWMutex

	hwnd              uintptr
	slots             []WebEmergencySkillConfig
	inputSender       func(WebEmergencySkillConfig) bool
	selfTargeting     bool
	assistPanicTarget bool
	targetKnown       bool
	targetPresent     bool
	targetActionReady bool
	panicTargetSent   bool
	lastAttempt       [emergencySkillSlotCount]time.Time
	lowHPReadings     int
	recoveredReadings int
	lowHPEpisode      bool
}

// EmergencySkillEvent records one skill-delivery attempt that passed the
// colour-bar confirmation gate. It is returned to the runtime so it can be
// written to katools_ocr.log without coupling this controller to file I/O.
type EmergencySkillEvent struct {
	Slot        int
	VK          uintptr
	NeedsTarget bool
	Sent        bool
}

func NewEmergencySkillController(hwnd uintptr, cfg WebBotConfig) *EmergencySkillController {
	c := &EmergencySkillController{hwnd: hwnd}
	c.Update(cfg)
	return c
}

func (c *EmergencySkillController) Update(cfg WebBotConfig) {
	slots := make([]WebEmergencySkillConfig, 0, emergencySkillSlotCount)
	for _, slot := range cfg.EmergencySkills {
		if len(slots) == emergencySkillSlotCount {
			break
		}
		if slot.Enabled && isEmergencySkillVK(slot.VK) {
			slots = append(slots, slot)
		}
	}
	c.mu.Lock()
	c.slots = slots
	// Click targeting acquires monsters only through its configured Target?
	// click slots. Emergency automation must not inject E behind that workflow.
	c.selfTargeting = !cfg.ClickMethod() && (cfg.TargetEnabled || cfg.TargetUntilDeadEnabled)
	c.assistPanicTarget = !cfg.ClickMethod() && cfg.AssistPanicTarget
	// A live configuration change begins a new decision cycle. This prevents a
	// previously sent panic E from suppressing the newly selected emergency
	// setup while HP is still low.
	c.panicTargetSent = false
	c.lastAttempt = [emergencySkillSlotCount]time.Time{}
	c.lowHPReadings = 0
	c.recoveredReadings = 0
	c.lowHPEpisode = false
	c.mu.Unlock()
}

func (c *EmergencySkillController) SetInputSender(sender func(WebEmergencySkillConfig) bool) {
	c.mu.Lock()
	c.inputSender = sender
	c.mu.Unlock()
}

func (c *EmergencySkillController) sendSkill(slot WebEmergencySkillConfig) (sent bool, attempted bool) {
	index := slot.Index - 1
	if index < 0 || index >= emergencySkillSlotCount {
		index = 0
	}

	c.mu.Lock()
	if time.Since(c.lastAttempt[index]) < emergencySkillRepeatCooldown {
		c.mu.Unlock()
		return false, false
	}
	attemptAt := time.Now()
	c.lastAttempt[index] = attemptAt
	sender := c.inputSender
	hwnd := c.hwnd
	c.mu.Unlock()

	if sender != nil {
		sent = sender(slot)
	} else if slot.Click {
		if slot.ClickAreaSet {
			sent = clickWindowClientPoint(hwnd, slot.ClickX, slot.ClickY) == nil
		}
	} else {
		sent = pressKeyToWindow(hwnd, slot.VK)
	}

	// Failed input also consumes the short cooldown; otherwise a denied cursor
	// API call would be retried on every detector frame and starve other work.
	return sent, true
}

func (c *EmergencySkillController) IsAnyEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.slots) > 0
}

// NeedsTargetMonitor reports whether the passive Target panel monitor is
// required. It is deliberately independent from the normal Target checkboxes:
// assist characters may need a target-only emergency skill without owning the
// party's targeting loop.
func (c *EmergencySkillController) NeedsTargetMonitor() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.assistPanicTarget {
		return true
	}
	for _, slot := range c.slots {
		if slot.NeedsTarget {
			return true
		}
	}
	return false
}

// ObserveTargetPanel is fed by the passive Target panel scanner. targetReady
// is false while a configured skip-target name is still being OCR-validated.
func (c *EmergencySkillController) ObserveTargetPanel(present, targetReady bool) {
	c.mu.Lock()
	c.targetKnown = true
	c.targetPresent = present
	c.targetActionReady = targetReady
	c.mu.Unlock()
}

// ObserveHPPercent opens one emergency episode only after a stable low HP-bar
// reading. Every eligible slot is attempted once in that episode, then stays
// latched until HP has recovered in two stable scans. Target-required slots
// are attempted only after the target panel is visibly active (and, when
// applicable, has passed the existing skip-target gate). The bool result asks
// the caller to send one E through BotController's normal target path; that
// preserves chat guarding and target-name validation.
func (c *EmergencySkillController) ObserveHPPercent(percent float64, barFound bool, threshold float64) (requestTarget bool, events []EmergencySkillEvent) {
	if !barFound || threshold <= 0 {
		// A missing colour bar is not a recovery. Retain the current episode so
		// a clipped WGC frame cannot re-arm casts while HP remains low.
		return false, nil
	}

	if percent > threshold+emergencyRecoveryMarginPercent {
		c.mu.Lock()
		c.lowHPReadings = 0
		c.recoveredReadings++
		if c.recoveredReadings >= emergencyRecoveryConfirmations {
			c.lowHPEpisode = false
			c.panicTargetSent = false
			c.lastAttempt = [emergencySkillSlotCount]time.Time{}
		}
		c.mu.Unlock()
		return false, nil
	}

	if percent > threshold {
		// Dead band: do not let one frame around the threshold either trigger
		// an emergency or re-arm the existing low-HP episode.
		c.mu.Lock()
		c.lowHPReadings = 0
		c.recoveredReadings = 0
		c.mu.Unlock()
		return false, nil
	}

	c.mu.Lock()
	c.recoveredReadings = 0
	if c.lowHPEpisode {
		c.mu.Unlock()
		return false, nil
	}
	c.lowHPReadings++
	if c.lowHPReadings < emergencyLowHPConfirmations {
		c.mu.Unlock()
		return false, nil
	}
	// Latch before sending input: a rapid following WGC sample must not issue
	// a second cast while the first delivery is still in progress.
	c.lowHPEpisode = true
	slots := append([]WebEmergencySkillConfig(nil), c.slots...)
	targetKnown := c.targetKnown
	targetPresent := c.targetPresent
	targetReady := c.targetActionReady
	canAcquireTarget := c.selfTargeting || c.assistPanicTarget
	panicTargetSent := c.panicTargetSent
	c.mu.Unlock()
	if len(slots) == 0 {
		return false, nil
	}

	hasTargetSlots := false
	for _, slot := range slots {
		if slot.NeedsTarget {
			hasTargetSlots = true
			continue
		}
		if sent, attempted := c.sendSkill(slot); attempted {
			events = append(events, EmergencySkillEvent{Slot: slot.Index, VK: slot.VK, NeedsTarget: false, Sent: sent})
		}
	}

	if !hasTargetSlots {
		return false, events
	}
	if targetKnown && targetPresent && targetReady {
		// The visible Target panel has already settled, so target-only skills can
		// share this same low-HP cycle without an artificial delay.
		for _, slot := range slots {
			if slot.NeedsTarget {
				if sent, attempted := c.sendSkill(slot); attempted {
					events = append(events, EmergencySkillEvent{Slot: slot.Index, VK: slot.VK, NeedsTarget: true, Sent: sent})
				}
			}
		}
		return false, events
	}

	// In assist mode, a missing target normally means "do not disturb the
	// leader". Assist Panic Target is the explicit opt-in exception. In either
	// solo mode or panic-assist mode, E is sent once per low-HP episode only.
	// A low-HP event can arrive one capture frame before the passive Target
	// monitor has produced its first result. Treat that as "no confirmed active
	// target" rather than silently doing nothing. Once a bar is confirmed
	// present, Assist Panic still leaves the leader's target untouched.
	if (!targetKnown || !targetPresent) && canAcquireTarget && !panicTargetSent {
		c.mu.Lock()
		if !c.panicTargetSent {
			c.panicTargetSent = true
			requestTarget = true
		}
		c.mu.Unlock()
	}
	return requestTarget, events
}

func (c *EmergencySkillController) MarkTargetRequestFailed() {
	c.mu.Lock()
	c.panicTargetSent = false
	c.mu.Unlock()
}

func isEmergencySkillVK(vk uintptr) bool {
	return (vk >= 0x30 && vk <= 0x39) || (vk >= 0x70 && vk <= 0x79)
}
