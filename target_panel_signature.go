package main

import "image"

const (
	targetPanelNameAreaPercent    = 60
	targetPanelMinNameGlyphPixels = 18
)

// targetPanelNameLooksPresent detects the actual name/level glyphs in the
// upper part of a selected Target panel. Support mode needs this stricter
// signal in addition to the red fill: at very low HP the red portion can be
// only a few pixels wide, but the target is still alive and attack-causing
// support skills must remain held.
func targetPanelNameLooksPresent(img image.Image) bool {
	if img == nil {
		return false
	}
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy() * targetPanelNameAreaPercent / 100
	if width <= 0 || height <= 0 {
		return false
	}

	glyphs := 0
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, g, b, _ := img.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
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
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy() * targetPanelNameAreaPercent / 100
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
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy() * targetPanelNameAreaPercent / 100
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
