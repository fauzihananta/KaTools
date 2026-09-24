package main

import "image"

const (
	targetPanelNameAreaPercent    = 60
	targetPanelMinNameGlyphPixels = 18
	// Kathana renders the compact target name immediately above its red HP
	// bar.  Keep a little vertical slack for font antialiasing and the gap
	// between the name and bar, but never feed another HUD line above it to
	// the single-line OCR pass.
	targetPanelNameMaxHeight = 24
)

// targetPanelNameBounds returns the header portion of a combined target-panel
// selection.  The picker intentionally asks for both the name and the red HP
// bar, because the latter is needed by the target monitor.  A fixed 60% header
// crop is not sufficient, though: with the HUD docked at the top of the game
// window, the bar (and its white numbers) can start well inside that 60%.
// Tesseract's single-line target pass then reads the name and bar as one line.
//
// Prefer the first wide red HP-bar row as the bottom edge of the name header.
// The name is directly above that row, so crop a short window ending at it.
// This matters when adjacent HUDs overlap a generously selected area: taking
// everything above the red bar can include the player's blue TP line and make
// Tesseract read its borders as a fake target name.  A nearly dead target can
// have only a short red fill, so the required run is deliberately small.  If
// the bar is empty or hidden, retain the historical 60% fallback rather than
// returning an unreliable tiny crop.
func targetPanelNameBounds(img image.Image) image.Rectangle {
	if img == nil {
		return image.Rectangle{}
	}
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 {
		return image.Rectangle{}
	}

	top := bounds.Min.Y
	bottom := bounds.Min.Y + height*targetPanelNameAreaPercent/100
	minimumBarPixels := max(6, width/30)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		redPixels := 0
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if isHPBarPixel(uint8(r>>8), uint8(g>>8), uint8(b>>8)) {
				redPixels++
			}
		}
		// Do not let a red glyph at the very top of an unusual target name
		// truncate the crop.  A HUD header always has several rows before its
		// HP bar.
		if y-bounds.Min.Y >= 8 && redPixels >= minimumBarPixels {
			bottom = y
			top = max(bounds.Min.Y, y-targetPanelNameMaxHeight)
			break
		}
	}

	if bottom <= top {
		bottom = min(bounds.Max.Y, top+1)
	}
	return image.Rect(bounds.Min.X, top, bounds.Max.X, bottom)
}

// targetPanelNameLooksPresent detects the actual name/level glyphs in the
// upper part of a selected Target panel. Support mode needs this stricter
// signal in addition to the red fill: at very low HP the red portion can be
// only a few pixels wide, but the target is still alive and attack-causing
// support skills must remain held.
func targetPanelNameLooksPresent(img image.Image) bool {
	if img == nil {
		return false
	}
	header := targetPanelNameBounds(img)
	width, height := header.Dx(), header.Dy()
	if width <= 0 || height <= 0 {
		return false
	}

	glyphs := 0
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, g, b, _ := img.At(header.Min.X+x, header.Min.Y+y).RGBA()
			r8, g8, b8 := uint8(r>>8), uint8(g>>8), uint8(b>>8)
			if r8 >= 205 && g8 >= 205 && b8 >= 205 {
				glyphs++
				if glyphs >= targetPanelMinNameGlyphPixels {
					return true
				}
			}
		}
	}
	return false
}

// targetPanelNameSignature is a tiny fingerprint of the upper part of the
// selected target panel, where the name and level are rendered. It excludes
// the red HP bar so ordinary damage does not repeatedly start name OCR.
func targetPanelNameSignature(img image.Image) uint64 {
	if img == nil {
		return 0
	}
	bounds := targetPanelNameBounds(img)
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 {
		return 0
	}

	const sampleX = 40
	const sampleY = 8
	hash := uint64(1469598103934665603)
	for y := 0; y < sampleY; y++ {
		py := bounds.Min.Y + y*height/sampleY
		for x := 0; x < sampleX; x++ {
			px := bounds.Min.X + x*width/sampleX
			r, g, b, _ := img.At(px, py).RGBA()
			// Hash only the bright UI glyphs. The selected panel can be partly
			// transparent, so hashing every background pixel made the signature
			// change with moving terrain even though the target name had not.
			r8, g8, b8 := uint8(r>>8), uint8(g>>8), uint8(b>>8)
			value := uint64(0)
			// Name and level use near-white glyphs. Requiring all RGB channels
			// to be bright rejects green terrain, red bars, and most spell effects
			// visible through the partially transparent target panel.
			if r8 >= 205 && g8 >= 205 && b8 >= 205 {
				value = 1
			}
			hash ^= value
			hash *= 1099511628211
		}
	}
	return hash
}

// targetPanelNameImage removes the red HP bar before OCR. The panel picker
// intentionally includes both the name and bar so death can be detected, but
// the bar's changing number is noise when validating a target name.
func targetPanelNameImage(img image.Image) image.Image {
	if img == nil {
		return nil
	}
	bounds := targetPanelNameBounds(img)
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 {
		return nil
	}

	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			dst.Set(x, y, img.At(bounds.Min.X+x, bounds.Min.Y+y))
		}
	}
	return dst
}
