package main

import (
	"fmt"

	"KaTools/tools/ocrworker"
)

const (
	maxDetectorSampleWidth  = 160
	maxDetectorSampleHeight = 100
)

// ============================================================
// PartyDetector v3
// ============================================================
//
// Strategy:
//   1. Compare ROI against baseline.
//   2. Detect visual difference.
//   3. Reject broad/global screen darkening.
//   4. Require stronger change in the CENTER popup area.
//   5. Require several consecutive frames.
//
// This is intended to separate:
//   - skill / screen darkening  -> broad change
//   - party popup              -> localized center change
//

type PartyDetector struct {
	roi ocrworker.PartyROIConfig

	sampleWidth  int
	sampleHeight int

	baseline []float64

	initialized bool

	warmup   int
	hitCount int
	detected bool

	lastDiff       float64
	lastChanged    float64
	lastCenterDiff float64
	lastOuterDiff  float64
	lastDarkPct    float64
	lastBrightPct  float64
}

func newPartyDetector(roi ocrworker.PartyROIConfig) *PartyDetector {
	detector := &PartyDetector{}
	detector.setROI(roi)
	return detector
}

// setROI changes the visual detector to the current user-selected OCR region.
// The region is downsampled to keep detector work cheap even when a large
// selection is used. A changed region must build a fresh baseline; comparing
// it to pixels from the previous region would create a false candidate.
func (d *PartyDetector) setROI(roi ocrworker.PartyROIConfig) {
	if roi == d.roi {
		return
	}

	d.roi = roi
	d.sampleWidth = min(maxDetectorSampleWidth, max(1, roi.Width))
	d.sampleHeight = min(maxDetectorSampleHeight, max(1, roi.Height))
	d.baseline = make([]float64, d.sampleWidth*d.sampleHeight)
	d.initialized = false
	d.warmup = 0
	d.hitCount = 0
	d.detected = false
}

// ------------------------------------------------------------
// Sample ROI
// ------------------------------------------------------------

func (d *PartyDetector) samplePartyROI(r *FrameReader) ([]float64, int) {

	values := make(
		[]float64,
		d.sampleWidth*d.sampleHeight,
	)

	count := 0

	for y := 0; y < d.sampleHeight; y++ {
		frameY := d.roi.Y + y*d.roi.Height/d.sampleHeight

		for x := 0; x < d.sampleWidth; x++ {
			frameX := d.roi.X + x*d.roi.Width/d.sampleWidth

			pixel, ok := r.GetPixel(frameX, frameY)

			if !ok {
				continue
			}

			luma :=
				0.299*float64(pixel.R) +
					0.587*float64(pixel.G) +
					0.114*float64(pixel.B)

			values[count] = luma

			count++
		}
	}

	return values, count
}

// ------------------------------------------------------------
// Update detector
// ------------------------------------------------------------

func (d *PartyDetector) update(
	r *FrameReader,
	roi ocrworker.PartyROIConfig,
) bool {
	if roi.Width <= 0 ||
		roi.Height <= 0 ||
		roi.X < 0 ||
		roi.Y < 0 ||
		roi.X+roi.Width > r.width ||
		roi.Y+roi.Height > r.height {
		return false
	}

	d.setROI(roi)

	current, count :=
		d.samplePartyROI(r)

	if count == 0 {
		return d.detected
	}

	// --------------------------------------------------------
	// Build baseline
	// --------------------------------------------------------

	if !d.initialized {

		copy(
			d.baseline,
			current,
		)

		d.warmup++

		if d.warmup >= partyWarmupFrames {

			d.initialized = true

			fmt.Println()
			fmt.Println(
				"[PartyDetector] Baseline ready.",
			)
		}

		return false
	}

	// --------------------------------------------------------
	// Analyze differences
	// --------------------------------------------------------

	var diffSum float64
	var changed int

	var darkCount int
	var brightCount int

	// Center area.
	//
	// ROI = 400x250
	// Center area = 280x170
	//
	// This covers the popup region while leaving an outer
	// border that can be used to detect global screen changes.

	centerX1 := d.sampleWidth / 4
	centerX2 := d.sampleWidth - centerX1

	centerY1 := d.sampleHeight / 4
	centerY2 := d.sampleHeight - centerY1

	var centerDiffSum float64
	var centerCount int

	var outerDiffSum float64
	var outerCount int

	for y := 0; y < d.sampleHeight; y++ {

		for x := 0; x < d.sampleWidth; x++ {

			i :=
				y*d.sampleWidth +
					x

			diffSigned :=
				current[i] -
					d.baseline[i]

			diff :=
				diffSigned

			if diff < 0 {
				diff = -diff
			}

			diffSum += diff

			if diff >= partyDiffThreshold {
				changed++
			}

			// --------------------------------------------
			// Direction of brightness change
			// --------------------------------------------

			if diffSigned <= -partyDiffThreshold {
				darkCount++
			}

			if diffSigned >= partyDiffThreshold {
				brightCount++
			}

			// --------------------------------------------
			// Center vs outer region
			// --------------------------------------------

			if x >= centerX1 &&
				x < centerX2 &&
				y >= centerY1 &&
				y < centerY2 {

				centerDiffSum += diff
				centerCount++

			} else {

				outerDiffSum += diff
				outerCount++
			}
		}
	}

	meanDiff :=
		diffSum /
			float64(count)

	changedPct :=
		float64(changed) *
			100.0 /
			float64(count)

	darkPct :=
		float64(darkCount) *
			100.0 /
			float64(count)

	brightPct :=
		float64(brightCount) *
			100.0 /
			float64(count)

	centerDiff := 0.0

	if centerCount > 0 {
		centerDiff =
			centerDiffSum /
				float64(centerCount)
	}

	outerDiff := 0.0

	if outerCount > 0 {
		outerDiff =
			outerDiffSum /
				float64(outerCount)
	}

	d.lastDiff =
		meanDiff

	d.lastChanged =
		changedPct

	d.lastCenterDiff =
		centerDiff

	d.lastOuterDiff =
		outerDiff

	d.lastDarkPct =
		darkPct

	d.lastBrightPct =
		brightPct

		// --------------------------------------------------------
		// Candidate
		// --------------------------------------------------------
	// --------------------------------------------------------
	// Party shape detection
	// --------------------------------------------------------
	//
	// Berdasarkan hasil testing:
	//
	// PARTY:
	// diff=23.4 changed=59.9 center=35.4 outer=12.6
	// dark=51.8 bright=8.1
	//
	// SKILL:
	// diff=23.1 changed=36.2 center=25.3 outer=21.0
	// dark=2.7 bright=33.5
	//

	partyShape :=
		meanDiff >= 18.0 &&
			changedPct >= 45.0 &&
			centerDiff >= 28.0 &&
			outerDiff <= 16.0

	partyDarkPattern :=
		darkPct >= 35.0 &&
			darkPct >= brightPct*2.0

	candidate :=
		partyShape &&
			partyDarkPattern

	// --------------------------------------------------------
	// Hysteresis
	// --------------------------------------------------------

	if candidate {

		if !d.detected &&
			d.hitCount < partyConfirmFrames {

			d.hitCount++
		}

	} else if meanDiff < partyClearThreshold {

		d.hitCount = 0

	} else if d.hitCount > 0 {

		d.hitCount--
	}

	// --------------------------------------------------------
	// Confirm popup
	// --------------------------------------------------------

	if !d.detected &&
		d.hitCount >= partyConfirmFrames {

		d.detected = true

		return true
	}

	// --------------------------------------------------------
	// Clear popup
	// --------------------------------------------------------

	if d.detected &&
		meanDiff < partyClearThreshold &&
		changedPct < 4.0 {

		d.detected = false
		d.hitCount = 0

		copy(
			d.baseline,
			current,
		)

		return false
	}

	// --------------------------------------------------------
	// Slowly adapt baseline
	// --------------------------------------------------------

	if !d.detected &&
		!candidate {

		for i := 0; i < count; i++ {

			d.baseline[i] +=
				(current[i] -
					d.baseline[i]) *
					partyBaselineAlpha
		}
	}

	return d.detected
}

// ------------------------------------------------------------
// Debug
// ------------------------------------------------------------

func (d *PartyDetector) debugLine() string {

	return fmt.Sprintf(
		"[PartyDetector] diff=%5.1f changed=%5.1f%% center=%5.1f outer=%5.1f dark=%5.1f%% bright=%5.1f%% hits=%d/%d detected=%v",
		d.lastDiff,
		d.lastChanged,
		d.lastCenterDiff,
		d.lastOuterDiff,
		d.lastDarkPct,
		d.lastBrightPct,
		d.hitCount,
		partyConfirmFrames,
		d.detected,
	)
}

// ============================================================
// Party Auto Accept
// ============================================================
