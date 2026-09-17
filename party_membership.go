package main

import "time"

// PartyMembershipMonitor detects the party list visually. The list contains
// multiple aligned horizontal red HP / blue TP bar pairs, so this avoids
// full-screen OCR for the "Automatic / Leave" labels even when the player has
// moved the party UI elsewhere on the screen.
type PartyMembershipMonitor struct {
	visible      bool
	absentChecks int
	joiningUntil time.Time
}

func (m *PartyMembershipMonitor) ExpectPanel() {
	// After ENTER, let the game render its party panel before resuming invite
	// scans. This also prevents the still-closing invite dialog being OCR'd.
	m.joiningUntil = time.Now().Add(3 * time.Second)
}

func (m *PartyMembershipMonitor) ShouldPause() bool {
	return m.visible || time.Now().Before(m.joiningUntil)
}

func (m *PartyMembershipMonitor) Update(reader *FrameReader) (visible bool, changed bool) {
	found := hasPartyPanelBars(reader)
	if found {
		m.absentChecks = 0
		if !m.visible {
			m.visible = true
			return true, true
		}
		return true, false
	}

	if !m.visible {
		return false, false
	}

	// A panel can briefly disappear while UI animations or loading occur. Only
	// resume invite OCR after several misses so re-party still works reliably.
	m.absentChecks++
	if m.absentChecks < 3 {
		return true, false
	}

	m.visible = false
	m.absentChecks = 0
	return false, true
}

func hasPartyPanelBars(reader *FrameReader) bool {
	if reader == nil || reader.width < 80 || reader.height < 80 {
		return false
	}

	scanWidth := reader.width
	minRun := max(24, min(80, scanWidth/12))
	lastPairY := -100
	type partyBarPair struct {
		start int
		end   int
	}
	knownPairs := make([]partyBarPair, 0, 4)

	for y := 0; y < reader.height-8; y += 2 {
		if y-lastPairY < 16 {
			continue
		}

		redStart, redEnd, redRun := longestPartyBarRun(reader, y, scanWidth, isPartyRed)
		if redRun < minRun {
			continue
		}

		blueFound := false
		for blueY := y + 8; blueY <= min(reader.height-1, y+48); blueY += 2 {
			blueStart, blueEnd, blueRun := longestPartyBarRun(reader, blueY, scanWidth, isPartyBlue)
			if blueRun < minRun {
				continue
			}
			if min(redEnd, blueEnd)-max(redStart, blueStart)+1 >= minRun/2 {
				blueFound = true
				break
			}
		}

		if blueFound {
			currentPair := partyBarPair{start: redStart, end: redEnd}
			for _, knownPair := range knownPairs {
				if partyBarPairsAligned(knownPair.start, knownPair.end, currentPair.start, currentPair.end) {
					return true
				}
			}
			knownPairs = append(knownPairs, currentPair)
			lastPairY = y
		}
	}

	return false
}

func partyBarPairsAligned(startA, endA, startB, endB int) bool {
	widthA := endA - startA + 1
	widthB := endB - startB + 1
	tolerance := max(20, min(widthA, widthB)/5)
	return absoluteInt(startA-startB) <= tolerance && absoluteInt(widthA-widthB) <= tolerance
}

func absoluteInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func longestPartyBarRun(reader *FrameReader, y, maxX int, matches func(Pixel) bool) (start, end, longest int) {
	start, end = -1, -1
	runStart := -1
	for x := 0; x < maxX; x++ {
		pixel, ok := reader.GetPixel(x, y)
		if ok && matches(pixel) {
			if runStart < 0 {
				runStart = x
			}
			if x-runStart+1 > longest {
				start, end, longest = runStart, x, x-runStart+1
			}
			continue
		}
		runStart = -1
	}
	return start, end, longest
}

func isPartyRed(pixel Pixel) bool {
	return pixel.R >= 105 && pixel.R >= pixel.G+50 && pixel.R >= pixel.B+50
}

func isPartyBlue(pixel Pixel) bool {
	return pixel.B >= 105 && pixel.B >= pixel.R+35 && pixel.B >= pixel.G+20
}
