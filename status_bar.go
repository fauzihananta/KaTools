package main

import (
	"image"
	"math"
)

// detectStatusBarPercents estimates HP and TP from the filled red/blue bar
// width. The selected status ROI should include the entire horizontal bars.
// It performs no OCR and is intentionally cheap enough to run frequently.
func detectStatusBarPercents(img image.Image) (hpPercent float64, hpOK bool, tpPercent float64, tpOK bool) {
	if img == nil {
		return 0, false, 0, false
	}

	hp := detectFilledBar(img, isHPBarPixel)
	tp := detectFilledBar(img, isTPBarPixel)

	// The picker normally includes a character name and an uneven decorative
	// frame around the bars. Inferring a full bar as "ROI width minus two equal
	// margins" overstates a partly filled TP bar when those margins differ. If
	// either line visibly reaches the bar's right edge, its coloured span is a
	// reliable full-width reference for both aligned bars.
	if hp.ok && tp.ok && statusBarsAligned(hp, tp, img.Bounds().Dx()) {
		if fullWidth := visibleFullStatusBarWidth(hp, tp, img.Bounds()); fullWidth > 0 {
			hp.percent = statusBarPercent(hp.width, fullWidth)
			tp.percent = statusBarPercent(tp.width, fullWidth)
		}
	}

	return hp.percent, hp.ok, tp.percent, tp.ok
}

func detectFilledBarPercent(img image.Image, matches func(r, g, b uint8) bool) (float64, bool) {
	bar := detectFilledBar(img, matches)
	return bar.percent, bar.ok
}

type filledBar struct {
	start   int
	end     int
	width   int
	percent float64
	ok      bool
}

func detectFilledBar(img image.Image, matches func(r, g, b uint8) bool) filledBar {
	bounds := img.Bounds()
	width := bounds.Dx()
	if width < 16 || bounds.Dy() < 2 {
		return filledBar{}
	}

	bestCount := 0
	bestStart := 0
	bestEnd := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		count := 0
		start := -1
		end := -1
		gap := 0
		commitRun := func() {
			if count > bestCount || (count == bestCount && start >= 0 && (bestStart == 0 || start < bestStart)) {
				bestCount = count
				bestStart = start
				bestEnd = end
			}
		}
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if matches(uint8(r>>8), uint8(g>>8), uint8(b>>8)) {
				if start < 0 {
					start = x
				}
				end = x
				count++
				gap = 0
				continue
			}
			if start < 0 {
				continue
			}
			gap++
			// A real bar can have a one-pixel highlight/aliasing gap. Any longer
			// gap ends this run so unrelated blue effects cannot inflate TP.
			if gap <= 1 {
				continue
			}
			commitRun()
			count = 0
			start = -1
			end = -1
			gap = 0
		}
		commitRun()
	}

	// A bar row has many colored pixels; isolated HUD icons must not count.
	if bestCount < 8 || bestStart < bounds.Min.X || bestEnd <= bestStart {
		return filledBar{}
	}

	leftMargin := bestStart - bounds.Min.X
	fullWidth := width - 2*leftMargin
	if fullWidth < 12 {
		return filledBar{}
	}

	filledWidth := bestEnd - bestStart + 1
	return filledBar{
		start:   bestStart,
		end:     bestEnd,
		width:   filledWidth,
		percent: statusBarPercent(filledWidth, fullWidth),
		ok:      true,
	}
}

func statusBarsAligned(hp, tp filledBar, imageWidth int) bool {
	// The red and blue lines in Kathana's character panel have the same left
	// edge. A generous allowance keeps antialiasing from rejecting a real pair,
	// while avoiding calibration from an unrelated blue effect elsewhere.
	return absoluteInt(hp.start-tp.start) <= max(8, imageWidth/12)
}

func visibleFullStatusBarWidth(hp, tp filledBar, bounds image.Rectangle) int {
	// A full bar ends just before its thin right border. The picker may contain
	// a little frame beyond it, but the allowance must remain tight: a 96%-full
	// bar is not a full-width reference.
	nearRightEdge := func(bar filledBar) bool {
		return bar.end >= bounds.Max.X-8
	}

	fullWidth := 0
	if nearRightEdge(hp) {
		fullWidth = hp.width
	}
	if nearRightEdge(tp) && tp.width > fullWidth {
		fullWidth = tp.width
	}
	return fullWidth
}

func statusBarPercent(filledWidth, fullWidth int) float64 {
	if fullWidth <= 0 {
		return 0
	}
	percent := float64(filledWidth) * 100.0 / float64(fullWidth)
	// Borders/highlights often consume one or two pixels at the right edge of
	// a completely full game bar. Treat that visual rounding as full so a
	// 100%-HP character does not misleadingly appear as 99.2% in the UI.
	if percent >= 98.0 {
		percent = 100
	}
	return math.Max(0, math.Min(100, percent))
}

func isHPBarPixel(r, g, b uint8) bool {
	// Convert before adding the colour margin.  uint8 arithmetic wraps at 255,
	// which previously made a white HUD glyph (255,255,255) look like a red
	// bar pixel because 255+45 became 44.
	return int(r) >= 100 && int(r) >= int(g)+45 && int(r) >= int(b)+45
}

func isTPBarPixel(r, g, b uint8) bool {
	return int(b) >= 100 && int(b) >= int(r)+35 && int(b) >= int(g)+20
}
