package main

import (
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	// At 15 FPS, a target panel can miss a few capture frames while the game
	// animates or redraws. Six scans (about 1.5 seconds) avoids changing a
	// still-living target because of one brief visual dropout.
	targetEmptyConfirmScans = 6
	targetReacquireCooldown = 800 * time.Millisecond
	// Support Skills need one short, confirmed target-free window to dispatch
	// a held skill before Target Until Dead presses E for the next monster.
	targetSupportDispatchWindow = 80 * time.Millisecond
)

// TargetUntilDeadController turns a sequence of empty monster-HP scans into
// one target press. A visible filled bar always blocks new target presses.
type TargetUntilDeadController struct {
	mu            sync.Mutex
	enabled       bool // Target Until Dead mode: retarget when the bar disappears.
	filterEnabled bool // Name-based skill/attack gate for either target mode.
	supportMode   bool // Hold non-With Target support slots until target is clear.
	characterName string
	emptyScans    int
	lastTargetAt  time.Time
	// supportClearConfirmed is true only after targetEmptyConfirmScans absent
	// bars. In Support mode the following E is delayed briefly so the support
	// scheduler can safely flush any held skill while no monster is selected.
	supportClearConfirmed bool
	retargetNotBefore     time.Time
}

func NewTargetUntilDeadController(enabled bool, filterEnabled bool, supportMode bool, characterName string) *TargetUntilDeadController {
	return &TargetUntilDeadController{
		enabled:       enabled,
		filterEnabled: filterEnabled,
		supportMode:   supportMode,
		characterName: strings.TrimSpace(characterName),
	}
}

func (c *TargetUntilDeadController) Update(enabled bool, filterEnabled bool, supportMode bool, characterName string) {
	c.mu.Lock()
	characterName = strings.TrimSpace(characterName)
	if c.enabled != enabled || c.filterEnabled != filterEnabled || c.supportMode != supportMode || c.characterName != characterName {
		c.emptyScans = 0
		c.lastTargetAt = time.Time{}
		c.supportClearConfirmed = false
		c.retargetNotBefore = time.Time{}
	}
	c.enabled = enabled
	c.filterEnabled = filterEnabled
	c.supportMode = supportMode
	c.characterName = characterName
	c.mu.Unlock()
}

func (c *TargetUntilDeadController) ExcludedNames() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.characterName
}

func (c *TargetUntilDeadController) IsEnabled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.enabled
}

func (c *TargetUntilDeadController) IsFilterEnabled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.filterEnabled
}

func (c *TargetUntilDeadController) IsSupportMode() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.enabled && c.supportMode
}

// StartInitialTarget authorizes one immediate E when Target Until Dead starts.
// Name-based skipping cannot begin until a target HUD exists, and waiting for
// the empty-bar confirmation here made a fresh run appear idle until the user
// manually pressed E. Subsequent retargets still use the normal confirmation
// path in Observe.
func (c *TargetUntilDeadController) StartInitialTarget() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.enabled {
		return false
	}
	c.emptyScans = 0
	c.supportClearConfirmed = false
	c.retargetNotBefore = time.Time{}
	c.lastTargetAt = time.Now()
	return true
}

// TargetClearConfirmed is consumed by the Support scheduler. It is false in
// Attacker mode, while a target is visible, and immediately after E is sent.
func (c *TargetUntilDeadController) TargetClearConfirmed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.enabled && c.supportMode && c.supportClearConfirmed
}

// Observe returns true only when the monster bar has been absent repeatedly.
func (c *TargetUntilDeadController) Observe(barVisible bool) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.enabled {
		c.supportClearConfirmed = false
		c.retargetNotBefore = time.Time{}
		return false
	}
	if barVisible {
		c.emptyScans = 0
		c.supportClearConfirmed = false
		c.retargetNotBefore = time.Time{}
		return false
	}

	// Support mode has already confirmed that the previous monster died. The
	// BotController must first acknowledge that every held support skill has
	// actually been dispatched; RetargetAfterSupportDispatch performs E after
	// that handshake. This prevents a slow scheduler from casting after a new
	// full-HP monster has already been selected.
	if c.supportMode && c.supportClearConfirmed {
		return false
	}

	c.emptyScans++
	if c.emptyScans < targetEmptyConfirmScans || time.Since(c.lastTargetAt) < targetReacquireCooldown {
		return false
	}

	if c.supportMode {
		c.supportClearConfirmed = true
		c.retargetNotBefore = time.Now().Add(targetSupportDispatchWindow)
		return false
	}

	c.emptyScans = 0
	c.lastTargetAt = time.Now()
	return true
}

// RetargetAfterSupportDispatch emits the delayed Target Until Dead E only
// after the Support scheduler has drained the skills held for this target.
// It is separate from Observe so a late scheduler can never race the next E.
func (c *TargetUntilDeadController) RetargetAfterSupportDispatch(dispatchComplete bool) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.enabled || !c.supportMode || !c.supportClearConfirmed || !dispatchComplete {
		return false
	}
	if time.Now().Before(c.retargetNotBefore) || time.Since(c.lastTargetAt) < targetReacquireCooldown {
		return false
	}

	c.supportClearConfirmed = false
	c.retargetNotBefore = time.Time{}
	c.emptyScans = 0
	c.lastTargetAt = time.Now()
	return true
}

// ForceRetarget is used after the target HUD is verified as excluded. It must
// not reuse the empty-bar cooldown: the next skipped target can be identified
// in well under that cooldown, and should immediately advance to another E.
func (c *TargetUntilDeadController) ForceRetarget() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.filterEnabled {
		return false
	}
	c.emptyScans = 0
	c.supportClearConfirmed = false
	c.retargetNotBefore = time.Time{}
	c.lastTargetAt = time.Now()
	return true
}

func targetTextMatchesExcludedName(text, names string) bool {
	ocrWords := targetNameWords(text)
	for _, name := range strings.Split(names, ";") {
		nameWords := targetNameWords(name)
		if len(nameWords) == 0 {
			continue
		}
		compactName := strings.Join(nameWords, "")

		if len(nameWords) == 1 {
			// A one-word exclusion is an exact target name, not a prefix.
			// For example, excluding a player named "Vasabhum" must not
			// skip the monster "Vasabhum Caura".
			hasSecondNameWord := func(index int) bool {
				for otherIndex, other := range ocrWords {
					// "Lv" and short OCR fragments are HUD noise. A second
					// normal-length word is evidence of a multi-word target name.
					if otherIndex != index && len(other) >= 4 {
						return true
					}
				}
				return false
			}
			for index, word := range ocrWords {
				if word == compactName && !hasSecondNameWord(index) {
					return true
				}
			}
			// Target-name OCR can confuse a few glyphs (for example, Ghorayogi
			// may become "rharavani"). For a one-word exclusion, preserve the
			// tolerant match against one OCR word at a time. Short names need a
			// tighter guard: the old 44%% LCS threshold made Rasuna ("asa") match
			// the unrelated monster Kubasang (also "asa").
			if len(compactName) >= 6 {
				for index, word := range ocrWords {
					// An exact word embedded in a longer target is handled above;
					// do not let the fuzzy branch turn it back into a match.
					if word != compactName && !hasSecondNameWord(index) &&
						oneWordExcludedNameFuzzyMatch(compactName, word) {
						return true
					}
				}
			}
			continue
		}

		// Multi-word exclusions must match a multi-word target name. The old
		// fuzzy word comparison let "Vasabhum" match "Vasabhum Caura" merely
		// because the first word was similar. Compare only spans with the same
		// word count and reject candidates that are materially shorter.
		for start := 0; start+len(nameWords) <= len(ocrWords); start++ {
			candidate := strings.Join(ocrWords[start:start+len(nameWords)], "")
			if candidate == compactName {
				return true
			}
			if len(candidate)*100 < len(compactName)*80 {
				continue
			}
			if float64(longestCommonSubsequence(compactName, candidate))/float64(len(compactName)) >= 0.72 {
				return true
			}
		}
	}
	return false
}

// oneWordExcludedNameFuzzyMatch retains the OCR tolerance needed by longer
// names, but prevents a small shared subsequence from making short player
// names match an unrelated monster. A one-character OCR insertion/deletion or
// a near miss such as "rasua" is still accepted for a short configured name.
func oneWordExcludedNameFuzzyMatch(expected, candidate string) bool {
	if len(expected) < 6 || candidate == "" {
		return false
	}

	common := longestCommonSubsequence(expected, candidate)
	if len(expected) <= 7 {
		lengthDifference := len(expected) - len(candidate)
		if lengthDifference < 0 {
			lengthDifference = -lengthDifference
		}
		return lengthDifference <= 1 &&
			float64(common)/float64(len(expected)) >= 0.60
	}

	return float64(common)/float64(len(expected)) >= 0.44
}

// targetTextPotentiallyMatchesExcludedName is deliberately more conservative
// than targetTextMatchesExcludedName. It is used only while a multi-word
// excluded name is being read incompletely by OCR. For example, the target HUD
// can temporarily read "Vasabhum Lv" instead of "Vasabhum Caura Lv3". In that
// state the bot must not attack; it waits for another scan or retargets after
// the normal validation timeout.
//
// One-word exclusions are intentionally not included here. A user can exclude
// "Vasabhum" while still wanting to attack a monster named "Vasabhum Caura".
func targetTextPotentiallyMatchesExcludedName(text, names string) bool {
	ocrWords := targetNameWords(text)
	for _, name := range strings.Split(names, ";") {
		nameWords := targetNameWords(name)
		if len(nameWords) < 2 {
			continue
		}

		// The first word is the useful stable part when the target panel gets
		// clipped at the right edge. Ignore short fragments such as "lv".
		expected := nameWords[0]
		if len(expected) < 4 {
			continue
		}
		for _, word := range ocrWords {
			if len(word) < 4 {
				continue
			}
			if word == expected {
				return true
			}
			if len(word)*100 >= len(expected)*70 &&
				float64(longestCommonSubsequence(expected, word))/float64(len(expected)) >= 0.72 {
				return true
			}
		}
	}
	return false
}

func targetNameLettersOnly(text string) string {
	var out strings.Builder
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func targetNameWords(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r)
	})
}

func longestCommonSubsequence(a, b string) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			if a[i-1] == b[j-1] {
				current[j] = previous[j-1] + 1
			} else if previous[j] > current[j-1] {
				current[j] = previous[j]
			} else {
				current[j] = current[j-1]
			}
		}
		previous, current = current, previous
		for j := range current {
			current[j] = 0
		}
	}
	return previous[len(b)]
}

func targetTextHasReadableName(text string) bool {
	for _, word := range targetNameWords(text) {
		if len(word) >= 3 && word != "detected" && word != "diacritics" {
			return true
		}
	}
	return false
}
