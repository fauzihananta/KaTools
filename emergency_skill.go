package main

import (
	"sync"
	"time"
)

const emergencySkillSlotCount = 5
const emergencySkillRepeatCooldown = 750 * time.Millisecond

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
	c.mu.Unlock()
}

func (c *EmergencySkillController) SetInputSender(sender func(WebEmergencySkillConfig) bool) {
	c.mu.Lock()
	c.inputSender = sender
	c.mu.Unlock()
}

func (c *EmergencySkillController) sendSkill(slot WebEmergencySkillConfig) bool {
	index := slot.Index - 1
	if index < 0 || index >= emergencySkillSlotCount {
		index = 0
	}

	c.mu.Lock()
	if time.Since(c.lastAttempt[index]) < emergencySkillRepeatCooldown {
		c.mu.Unlock()
		return false
	}
	attemptAt := time.Now()
	c.lastAttempt[index] = attemptAt
	sender := c.inputSender
	hwnd := c.hwnd
	c.mu.Unlock()

	sent := false
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
	return sent
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

// ObserveHPPercent attempts every eligible non-target emergency slot while HP
// is low. Each slot has its own short cooldown, so a fast detector loop cannot
// flood the input lane. Target-required slots are attempted only after the
// target panel is visibly active (and, when applicable, has passed the existing
// skip-target gate). The bool result asks the caller to send one E through
// BotController's normal target path; that preserves chat guarding and
// target-name validation.
func (c *EmergencySkillController) ObserveHPPercent(percent float64, barFound bool, threshold float64) (requestTarget bool) {
	if !barFound || threshold <= 0 {
		return false
	}

	if percent > threshold {
		c.mu.Lock()
		c.panicTargetSent = false
		c.lastAttempt = [emergencySkillSlotCount]time.Time{}
		c.mu.Unlock()
		return false
	}

	c.mu.RLock()
	slots := append([]WebEmergencySkillConfig(nil), c.slots...)
	targetKnown := c.targetKnown
	targetPresent := c.targetPresent
	targetReady := c.targetActionReady
	canAcquireTarget := c.selfTargeting || c.assistPanicTarget
	panicTargetSent := c.panicTargetSent
	c.mu.RUnlock()
	if len(slots) == 0 {
		return false
	}

	hasTargetSlots := false
	for _, slot := range slots {
		if slot.NeedsTarget {
			hasTargetSlots = true
			continue
		}
		c.sendSkill(slot)
	}

	if !hasTargetSlots {
		return false
	}
	if targetKnown && targetPresent && targetReady {
		// The visible Target panel has already settled, so target-only skills can
		// share this same low-HP cycle without an artificial delay.
		for _, slot := range slots {
			if slot.NeedsTarget {
				c.sendSkill(slot)
			}
		}
		return false
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
	return requestTarget
}

func (c *EmergencySkillController) MarkTargetRequestFailed() {
	c.mu.Lock()
	c.panicTargetSent = false
	c.mu.Unlock()
}

func isEmergencySkillVK(vk uintptr) bool {
	return (vk >= 0x30 && vk <= 0x39) || (vk >= 0x70 && vk <= 0x79)
}
