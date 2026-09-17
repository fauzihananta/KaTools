package main

import (
	"math"
	"regexp"
	"strconv"
	"sync"
	"time"
)

const autoPotCooldown = 1200 * time.Millisecond

// Low-level characters can legitimately have small HP/TP maxima (for example
// 50/50 HP and 30/30 TP). Keep a floor to reject OCR noise while allowing
// those real early-game values.
const minimumStatusMaximum = 20
const statusOCRBarTolerance = 4.0
const statusOCRBarOverestimateTolerance = 12.0

type AutoPotRule struct {
	Enabled   bool
	Threshold float64
	SlotVK    uintptr
	lastUsed  time.Time
	wasBelow  bool
}

type AutoPotEvent struct {
	Resource  string
	Current   int
	Maximum   int
	Threshold float64
	SlotVK    uintptr
	Pressed   bool
}

type AutoPotController struct {
	mu sync.Mutex
	hp AutoPotRule
	tp AutoPotRule

	hpCurrent int
	hpMax     int
	tpCurrent int
	tpMax     int
	lastRead  time.Time

	// A game HUD can render the current number unreliably over moving terrain
	// while still reading its fixed maximum correctly (for example 6334/6334
	// when the coloured bar is actually at 91%). Keep a short, separate
	// confirmation path for those full-value maximum candidates. It never uses
	// the OCR current value to press a potion.
	hpFullMaxCandidate      int
	hpFullMaxCandidateScans int
	tpFullMaxCandidate      int
	tpFullMaxCandidateScans int

	// The status picker can include asymmetric ornamental borders. The raw
	// colour-width estimate is therefore calibrated against a verified numeric
	// OCR pair before it is used for live potion decisions.
	hpBarPercentScale    float64
	tpBarPercentScale    float64
	hpBarPercentScaleSet bool
	tpBarPercentScaleSet bool

	hwnd uintptr
}

type AutoPotStatus struct {
	HPCurrent int   `json:"hpCurrent"`
	HPMax     int   `json:"hpMax"`
	TPCurrent int   `json:"tpCurrent"`
	TPMax     int   `json:"tpMax"`
	LastRead  int64 `json:"lastRead"`
}

func NewAutoPotController(hwnd uintptr, cfg WebBotConfig) *AutoPotController {
	p := &AutoPotController{hwnd: hwnd}
	p.Update(cfg)
	return p
}

func (p *AutoPotController) Update(cfg WebBotConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.hp.Enabled != cfg.AutoPotHPEnabled || p.hp.Threshold != cfg.AutoPotHPPercent || p.hp.SlotVK != cfg.AutoPotHPSlotVK {
		p.hp.wasBelow = false
	}
	if p.tp.Enabled != cfg.AutoPotTPEnabled || p.tp.Threshold != cfg.AutoPotTPPercent || p.tp.SlotVK != cfg.AutoPotTPSlotVK {
		p.tp.wasBelow = false
	}
	p.hp.Enabled, p.hp.Threshold, p.hp.SlotVK = cfg.AutoPotHPEnabled, cfg.AutoPotHPPercent, cfg.AutoPotHPSlotVK
	p.tp.Enabled, p.tp.Threshold, p.tp.SlotVK = cfg.AutoPotTPEnabled, cfg.AutoPotTPPercent, cfg.AutoPotTPSlotVK
}

func (p *AutoPotController) IsAnyEnabled() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.hp.Enabled || p.tp.Enabled
}

func (p *AutoPotController) HPThreshold() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.hp.Enabled {
		return 0
	}
	return p.hp.Threshold
}

func (p *AutoPotController) HPEnabled() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.hp.Enabled
}

// ConfiguredHPThreshold returns the value shown in the HP Pot threshold
// field even when HP Pot itself is off. Death recovery uses this as a safe
// resume threshold without ever pressing a potion slot.
func (p *AutoPotController) ConfiguredHPThreshold() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.hp.Threshold
}

func (p *AutoPotController) Observe(hpCurrent, hpMax, tpCurrent, tpMax int) []AutoPotEvent {
	p.mu.Lock()
	defer p.mu.Unlock()

	events := make([]AutoPotEvent, 0, 2)
	updated := false
	if statusMaximumPlausible(p.hpMax, hpMax) {
		p.hpCurrent, p.hpMax = hpCurrent, hpMax
		updated = true
		if event, ok := p.observeRule("HP", &p.hp, hpCurrent, hpMax); ok {
			events = append(events, event)
		}
	}
	if statusMaximumPlausible(p.tpMax, tpMax) {
		p.tpCurrent, p.tpMax = tpCurrent, tpMax
		updated = true
		if event, ok := p.observeRule("TP", &p.tp, tpCurrent, tpMax); ok {
			events = append(events, event)
		}
	}
	if updated {
		p.lastRead = time.Now()
	}
	return events
}

func statusMaximumPlausible(previous, next int) bool {
	if next < minimumStatusMaximum {
		return false
	}
	return maximumIsCloseEnough(previous, next)
}

func maximumIsCloseEnough(previous, next int) bool {
	if previous <= 0 {
		return true
	}
	return next >= previous/2 && next <= previous*3/2
}

// ObserveBarPercents updates the cached values from the red/blue bar fill
// without starting Tesseract. Maximum values come from the initial OCR read.
func (p *AutoPotController) ObserveBarPercents(hpPercent float64, hpOK bool, tpPercent float64, tpOK bool) []AutoPotEvent {
	p.mu.Lock()
	defer p.mu.Unlock()

	hpPercent = scaledStatusBarPercent(hpPercent, hpOK, p.hpBarPercentScale, p.hpBarPercentScaleSet)
	tpPercent = scaledStatusBarPercent(tpPercent, tpOK, p.tpBarPercentScale, p.tpBarPercentScaleSet)

	events := make([]AutoPotEvent, 0, 2)
	if hpOK && p.hpMax > 0 {
		p.hpCurrent = valueFromPercent(hpPercent, p.hpMax)
		if event, ok := p.observeRule("HP", &p.hp, p.hpCurrent, p.hpMax); ok {
			events = append(events, event)
		}
	} else if !hpOK && tpOK && p.hpMax > 0 {
		// At exactly zero HP there are no red fill pixels, so the color scan
		// cannot find an HP run. A valid TP bar in the same combined status ROI
		// proves that this is the HUD rather than an unrelated/invalid crop.
		p.hpCurrent = 0
		if event, ok := p.observeRule("HP", &p.hp, p.hpCurrent, p.hpMax); ok {
			events = append(events, event)
		}
	}
	if tpOK && p.tpMax > 0 {
		p.tpCurrent = valueFromPercent(tpPercent, p.tpMax)
		if event, ok := p.observeRule("TP", &p.tp, p.tpCurrent, p.tpMax); ok {
			events = append(events, event)
		}
	} else if !tpOK && hpOK && p.tpMax > 0 {
		// Symmetric handling for an empty TP bar. Require the HP bar to be
		// present so a failed/incorrect crop never becomes a false 0% reading.
		p.tpCurrent = 0
		if event, ok := p.observeRule("TP", &p.tp, p.tpCurrent, p.tpMax); ok {
			events = append(events, event)
		}
	}
	if hpOK || tpOK {
		p.lastRead = time.Now()
	}

	return events
}

// CalibrateBarPercents learns the small, stable difference between an OCR
// value and a colour-width estimate from the same frame. This keeps fast
// colour scans accurate even when the picked ROI has uneven HUD borders.
func (p *AutoPotController) CalibrateBarPercents(
	hpCurrent, hpMax int,
	hpPercent float64, hpOK bool,
	tpCurrent, tpMax int,
	tpPercent float64, tpOK bool,
) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.hpBarPercentScale, p.hpBarPercentScaleSet = calibrateStatusBarScale(
		p.hpBarPercentScale, p.hpBarPercentScaleSet,
		hpCurrent, hpMax, hpPercent, hpOK,
	)
	p.tpBarPercentScale, p.tpBarPercentScaleSet = calibrateStatusBarScale(
		p.tpBarPercentScale, p.tpBarPercentScaleSet,
		tpCurrent, tpMax, tpPercent, tpOK,
	)
}

func calibrateStatusBarScale(
	previous float64,
	wasSet bool,
	current, maximum int,
	rawPercent float64,
	rawOK bool,
) (float64, bool) {
	if !rawOK || maximum < minimumStatusMaximum || current < 0 || current > maximum || rawPercent < 5 {
		return previous, wasSet
	}

	actualPercent := float64(current) * 100.0 / float64(maximum)
	candidate := actualPercent / rawPercent
	// Numeric OCR is accepted only after bar validation. Keep an additional
	// conservative bound here so one malformed frame cannot poison live scans.
	if candidate < 0.80 || candidate > 1.10 {
		return previous, wasSet
	}
	if !wasSet {
		return candidate, true
	}
	return previous*0.70 + candidate*0.30, true
}

func scaledStatusBarPercent(percent float64, ok bool, scale float64, scaleSet bool) float64 {
	if !ok || !scaleSet {
		return percent
	}
	return math.Max(0, math.Min(100, percent*scale))
}

func (p *AutoPotController) HasStatusMaxima() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.hpMax > 0 && p.tpMax > 0
}

// ResetStatus discards values from a previous HUD crop. It is used when the
// user chooses a new Status HP / TP area, so a stale maximum can never drive
// a potion keypress while the new area is being calibrated.
func (p *AutoPotController) ResetStatus() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.hpCurrent, p.hpMax = 0, 0
	p.tpCurrent, p.tpMax = 0, 0
	p.hpFullMaxCandidate, p.hpFullMaxCandidateScans = 0, 0
	p.tpFullMaxCandidate, p.tpFullMaxCandidateScans = 0, 0
	p.lastRead = time.Time{}
	p.hpBarPercentScale = 0
	p.tpBarPercentScale = 0
	p.hpBarPercentScaleSet = false
	p.tpBarPercentScaleSet = false
	p.hp.wasBelow = false
	p.tp.wasBelow = false
}

// LearnFullStatusMaximums learns a fixed resource maximum only after the
// exact full form (N/N) is seen twice. It is intentionally independent from
// the coloured-bar/current validation: a bad OCR current must not discard a
// clearly repeated maximum, while the live current value continues to come
// from the red/blue fill width.
func (p *AutoPotController) LearnFullStatusMaximums(
	hpCurrent, hpMaximum int,
	tpCurrent, tpMaximum int,
) (hpLearned, tpLearned bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	hpLearned = learnFullStatusMaximum(
		&p.hpMax,
		&p.hpFullMaxCandidate,
		&p.hpFullMaxCandidateScans,
		hpCurrent,
		hpMaximum,
	)
	tpLearned = learnFullStatusMaximum(
		&p.tpMax,
		&p.tpFullMaxCandidate,
		&p.tpFullMaxCandidateScans,
		tpCurrent,
		tpMaximum,
	)
	if hpLearned || tpLearned {
		p.lastRead = time.Now()
	}
	return hpLearned, tpLearned
}

func learnFullStatusMaximum(
	stored, candidate, scans *int,
	current, maximum int,
) bool {
	if *stored > 0 || maximum < minimumStatusMaximum || current != maximum {
		return false
	}
	if *candidate != maximum {
		*candidate = maximum
		*scans = 1
		return false
	}
	*scans++
	if *scans < 2 {
		return false
	}
	*stored = maximum
	*candidate = 0
	*scans = 0
	return true
}

func (p *AutoPotController) Status() AutoPotStatus {
	p.mu.Lock()
	defer p.mu.Unlock()

	return AutoPotStatus{
		HPCurrent: p.hpCurrent,
		HPMax:     p.hpMax,
		TPCurrent: p.tpCurrent,
		TPMax:     p.tpMax,
		LastRead:  p.lastRead.Unix(),
	}
}

func (p *AutoPotController) observeRule(resource string, rule *AutoPotRule, current, maximum int) (AutoPotEvent, bool) {
	if !rule.Enabled || rule.SlotVK == 0 || rule.Threshold <= 0 || maximum <= 0 || current < 0 {
		rule.wasBelow = false
		return AutoPotEvent{}, false
	}

	below := float64(current)*100.0/float64(maximum) <= rule.Threshold
	if !below {
		rule.wasBelow = false
		return AutoPotEvent{}, false
	}

	firstBelow := !rule.wasBelow
	rule.wasBelow = true
	pressed := p.useIfLow(rule, current, maximum)
	if !firstBelow {
		return AutoPotEvent{}, false
	}

	return AutoPotEvent{
		Resource:  resource,
		Current:   current,
		Maximum:   maximum,
		Threshold: rule.Threshold,
		SlotVK:    rule.SlotVK,
		Pressed:   pressed,
	}, true
}

func valueFromPercent(percent float64, maximum int) int {
	percent = math.Max(0, math.Min(100, percent))
	return int(math.Round(percent * float64(maximum) / 100.0))
}

// statusPairMatchesBar rejects OCR values that do not agree with the colored
// fill in the same frame. It catches digit-merge errors such as 23964/23964
// when the blue bar is visibly below full.
func statusPairMatchesBar(current, maximum int, barPercent float64, barOK bool) bool {
	if current < 0 || maximum < minimumStatusMaximum || current > maximum {
		return false
	}
	if !barOK {
		return true
	}
	ocrPercent := float64(current) * 100.0 / float64(maximum)
	difference := ocrPercent - barPercent
	if difference <= 0 {
		// A slightly lower OCR value is credible when the selected HUD crop has
		// unequal borders: the colour-width method can overestimate its percent.
		// The inverse is kept strict because a merged OCR value such as full HP
		// against a visibly depleted bar must still be rejected.
		return -difference <= statusOCRBarOverestimateTolerance
	}
	return difference <= statusOCRBarTolerance
}

func (p *AutoPotController) useIfLow(rule *AutoPotRule, current, maximum int) bool {
	if time.Since(rule.lastUsed) < autoPotCooldown {
		return false
	}
	if pressKeyToWindow(p.hwnd, rule.SlotVK) {
		rule.lastUsed = time.Now()
		return true
	}
	return false
}

var statusPairPattern = regexp.MustCompile(`(\d{1,6})\s*/\s*(\d{1,6})`)

func parseStatusPairs(text string) (hpCurrent, hpMax, tpCurrent, tpMax int, ok bool) {
	pairs := statusPairPattern.FindAllStringSubmatch(text, -1)
	if len(pairs) < 2 {
		return 0, 0, 0, 0, false
	}
	hpCurrent, hpMax, ok = parseStatusPair(pairs[0][0])
	if !ok {
		return 0, 0, 0, 0, false
	}
	tpCurrent, tpMax, ok = parseStatusPair(pairs[1][0])
	if !ok {
		return 0, 0, 0, 0, false
	}
	return hpCurrent, hpMax, tpCurrent, tpMax, true
}

// parseStatusPair is the safe one-row counterpart to parseStatusPairs. It is
// used when Tesseract recognizes HP and TP using different image variants.
func parseStatusPair(text string) (current, maximum int, ok bool) {
	pair := statusPairPattern.FindStringSubmatch(text)
	if len(pair) != 3 {
		return 0, 0, false
	}
	current, err1 := strconv.Atoi(pair[1])
	maximum, err2 := strconv.Atoi(pair[2])
	if err1 != nil || err2 != nil || maximum <= 0 {
		return 0, 0, false
	}
	if current > maximum {
		// The small HUD font can make the first digit of a full value read as
		// another digit (for example 3414/2414). Repair only this narrow,
		// unambiguous OCR typo; other malformed values remain rejected.
		var repaired bool
		current, repaired = repairFullStatusValue(current, maximum)
		if !repaired || current > maximum {
			return 0, 0, false
		}
	}
	return current, maximum, true
}

func repairFullStatusValue(current, maximum int) (int, bool) {
	currentText := strconv.Itoa(current)
	maximumText := strconv.Itoa(maximum)
	if len(currentText) < 2 || len(currentText) != len(maximumText) {
		return current, false
	}
	if currentText[1:] != maximumText[1:] {
		return current, false
	}
	return maximum, true
}
