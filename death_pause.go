package main

import (
	"image"
	"math/bits"
	"strings"
	"sync"
)

type DeathPauseController struct {
	mu                      sync.Mutex
	enabled                 bool
	autoResurrectEnabled    bool
	triggered               bool
	waitingResurrection     bool
	resurrectionAccepted    bool
	resurrectionPromptScans int
	deathDialogCleared      bool
	dialogClearScans        int
	healthyScans            int
	dialogMask              deathDialogMask
	hasDialogMask           bool
}

const healthResumeConfirmScans = 2
const deathDialogClearConfirmScans = 2
const resurrectionPromptConfirmScans = 2
const deathDialogMaskSamplesX = 48
const deathDialogMaskSamplesY = 24

type deathDialogMask [deathDialogMaskSamplesX * deathDialogMaskSamplesY / 64]uint64

func NewDeathPauseController(enabled, autoResurrectEnabled bool) *DeathPauseController {
	return &DeathPauseController{
		enabled:              enabled,
		autoResurrectEnabled: autoResurrectEnabled,
	}
}

func (p *DeathPauseController) Update(enabled, autoResurrectEnabled bool) {
	p.mu.Lock()
	if p.enabled != enabled || p.autoResurrectEnabled != autoResurrectEnabled {
		p.resetLocked()
	}
	p.enabled = enabled
	p.autoResurrectEnabled = autoResurrectEnabled
	p.mu.Unlock()
}

func (p *DeathPauseController) IsEnabled() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.enabled
}

func (p *DeathPauseController) TriggerOnce() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.enabled || p.triggered {
		return false
	}
	p.triggered = true
	p.waitingResurrection = p.autoResurrectEnabled
	p.resurrectionAccepted = false
	p.resurrectionPromptScans = 0
	p.deathDialogCleared = false
	p.dialogClearScans = 0
	p.healthyScans = 0
	return true
}

func (p *DeathPauseController) AutoResurrectEnabled() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.enabled && p.autoResurrectEnabled
}

func (p *DeathPauseController) ShouldScanResurrection() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.enabled && p.waitingResurrection && !p.resurrectionAccepted
}

func (p *DeathPauseController) MarkResurrectionAccepted() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.enabled || !p.waitingResurrection || p.resurrectionAccepted {
		return false
	}
	p.resurrectionAccepted = true
	p.resurrectionPromptScans = 0
	p.healthyScans = 0
	return true
}

// ObserveResurrectionPrompt requires the exact resurrection prompt to remain
// visible in two consecutive checks. This prevents one noisy OCR result from
// sending ENTER to the game chat while the death screen is still changing.
func (p *DeathPauseController) ObserveResurrectionPrompt(visible bool) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.enabled || !p.waitingResurrection || p.resurrectionAccepted {
		return false
	}
	if !visible {
		p.resurrectionPromptScans = 0
		return false
	}
	p.resurrectionPromptScans++
	return p.resurrectionPromptScans >= resurrectionPromptConfirmScans
}

func (p *DeathPauseController) ShouldWatchHP() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	// A player can manually click the ordinary respawn OK button instead of
	// waiting for Auto Resu. In either path, only a confirmed healthy HP bar
	// may release the paused bot.
	return p.enabled && p.triggered && p.deathDialogCleared
}

func (p *DeathPauseController) IsTriggered() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.enabled && p.triggered
}

// ObserveDeathDialog prevents stale/full HP pixels behind the death Message
// dialog from resuming KaTools. The dialog must be absent in two OCR scans
// after a confirmed death before HP is allowed to release the pause.
func (p *DeathPauseController) ObserveDeathDialog(visible bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.enabled || !p.triggered {
		return
	}
	if visible {
		p.deathDialogCleared = false
		p.dialogClearScans = 0
		p.healthyScans = 0
		return
	}
	p.dialogClearScans++
	if p.dialogClearScans >= deathDialogClearConfirmScans {
		p.deathDialogCleared = true
	}
}

// RememberDeathDialog stores the visual fingerprint from the exact frame in
// which OCR confirmed the respawn message. Future scans compare against this
// fingerprint instead of trying to guess a dialog from generic dark pixels.
func (p *DeathPauseController) RememberDeathDialog(mask deathDialogMask) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.enabled || !p.triggered {
		return
	}
	p.dialogMask = mask
	p.hasDialogMask = true
}

func (p *DeathPauseController) DeathDialogMatches(mask deathDialogMask) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.enabled || !p.triggered || !p.hasDialogMask {
		return false
	}

	referenceDark := 0
	matchingDark := 0
	for i := range p.dialogMask {
		referenceDark += bits.OnesCount64(p.dialogMask[i])
		matchingDark += bits.OnesCount64(p.dialogMask[i] & mask[i])
	}
	// A real Message dialog has a substantial opaque interior. Requiring 90%
	// of those exact dark positions avoids treating ordinary dark terrain as
	// the still-open dialog.
	return referenceDark >= deathDialogMaskSamplesX*deathDialogMaskSamplesY/5 &&
		matchingDark*100 >= referenceDark*90
}

// ObserveHPPercent resumes the normal bot after a death only when the red HP
// bar is above the configured Auto Potion threshold for consecutive scans. It
// supports both automatic resurrection and a player manually clicking OK.
func (p *DeathPauseController) ObserveHPPercent(percent float64, barFound bool, threshold float64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.enabled || !p.triggered || !p.deathDialogCleared ||
		!barFound || threshold <= 0 || percent <= threshold {
		p.healthyScans = 0
		return false
	}
	p.healthyScans++
	if p.healthyScans < healthResumeConfirmScans {
		return false
	}
	p.resetLocked()
	return true
}

func (p *DeathPauseController) resetLocked() {
	p.triggered = false
	p.waitingResurrection = false
	p.resurrectionAccepted = false
	p.resurrectionPromptScans = 0
	p.deathDialogCleared = false
	p.dialogClearScans = 0
	p.healthyScans = 0
	p.dialogMask = deathDialogMask{}
	p.hasDialogMask = false
}

func isDeathRespawnMessage(text string) bool {
	text = strings.ToLower(text)
	return strings.Contains(text, "respawn at the last saved location") ||
		(strings.Contains(text, "respawn") && strings.Contains(text, "saved") && strings.Contains(text, "location"))
}

func isResurrectionPrompt(text string) bool {
	text = strings.ToLower(text)
	return strings.Contains(text, "lost prana") && strings.Contains(text, "resurrect")
}

// resurrectionDialogLooksForeground distinguishes bright Message-dialog text
// from the same dialog dimmed behind another game overlay. It deliberately
// errs on the safe side: if the dialog is dim, Auto Resu waits instead of
// risking ENTER being delivered to chat.
func resurrectionDialogLooksForeground(img image.Image) bool {
	if img == nil {
		return false
	}
	b := img.Bounds()
	if b.Dx() < 80 || b.Dy() < 40 {
		return false
	}

	// Ignore the outer dialog frame. Bright, near-neutral glyphs in the inner
	// area are the white Message text / active OK caption.
	minX := b.Min.X + b.Dx()/12
	maxX := b.Max.X - b.Dx()/12
	minY := b.Min.Y + b.Dy()/8
	maxY := b.Max.Y - b.Dy()/10
	bright := 0
	for y := minY; y < maxY; y += 2 {
		for x := minX; x < maxX; x += 2 {
			r, g, bl, _ := img.At(x, y).RGBA()
			r8, g8, b8 := int(r>>8), int(g>>8), int(bl>>8)
			if r8 >= 165 && g8 >= 165 && b8 >= 165 &&
				maxInt(r8, g8, b8)-minInt(r8, g8, b8) <= 42 {
				bright++
			}
		}
	}
	// At 2x sampling this is only a small cluster of glyph pixels. The low
	// threshold accommodates different resolutions, while the neutral-color
	// rule rejects most terrain, skill, and UI colours.
	return bright >= 12
}

func maxInt(values ...int) int {
	max := values[0]
	for _, value := range values[1:] {
		if value > max {
			max = value
		}
	}
	return max
}

func minInt(values ...int) int {
	min := values[0]
	for _, value := range values[1:] {
		if value < min {
			min = value
		}
	}
	return min
}

// deathDialogMaskFromImage creates a dark-pixel fingerprint of the selected
// Death Area. It is compared only against the exact frame that OCR confirmed
// as the respawn dialog.
func deathDialogMaskFromImage(img image.Image) deathDialogMask {
	var mask deathDialogMask
	if img == nil {
		return mask
	}
	bounds := img.Bounds()
	if bounds.Dx() < 20 || bounds.Dy() < 20 {
		return mask
	}

	for y := 0; y < deathDialogMaskSamplesY; y++ {
		py := bounds.Min.Y + (y*2+1)*bounds.Dy()/(deathDialogMaskSamplesY*2)
		for x := 0; x < deathDialogMaskSamplesX; x++ {
			px := bounds.Min.X + (x*2+1)*bounds.Dx()/(deathDialogMaskSamplesX*2)
			r, g, b, _ := img.At(px, py).RGBA()
			if r>>8 <= 40 && g>>8 <= 40 && b>>8 <= 40 {
				index := y*deathDialogMaskSamplesX + x
				mask[index/64] |= uint64(1) << (index % 64)
			}
		}
	}
	return mask
}
