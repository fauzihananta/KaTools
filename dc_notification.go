package main

import (
	"image"
	"strings"
)

const dcConfirmationScans = 2

// DCNotificationController is deliberately independent of death, resurrection,
// and party state. A confirmed disconnect ends the whole runtime after it
// acknowledges the game's Message dialog.
type DCNotificationController struct {
	confirmationScans int
	triggered         bool
}

func (c *DCNotificationController) Observe(texts []string) bool {
	if c.triggered {
		return false
	}
	for _, text := range texts {
		if isDCNotificationText(text) {
			c.confirmationScans++
			if c.confirmationScans >= dcConfirmationScans {
				c.triggered = true
				return true
			}
			return false
		}
	}
	c.confirmationScans = 0
	return false
}

func isDCNotificationText(text string) bool {
	text = strings.ToLower(text)
	return strings.Contains(text, "connection") &&
		strings.Contains(text, "server") &&
		(strings.Contains(text, "failed") || strings.Contains(text, "fail")) &&
		strings.Contains(text, "try")
}

// dcNotificationROI is a fixed, resolution-relative center crop. The Kathana
// disconnect Message dialog is centered, so this requires no user picker and
// does not share the Death Area's coordinates or controller.
func dcNotificationROI(width, height int) image.Rectangle {
	x := width * 34 / 100
	y := height * 40 / 100
	w := max(1, width*32/100)
	h := max(1, height*16/100)
	return image.Rect(x, y, minInt(x+w, width), minInt(y+h, height))
}

// dcDialogLooksPresent is the cheap gate before starting Tesseract. The opaque
// dark Message panel plus its neutral-white glyphs distinguish the dialog from
// normal terrain; OCR still has to verify the full disconnect phrase.
func dcDialogLooksPresent(img image.Image) bool {
	if img == nil {
		return false
	}
	bounds := img.Bounds()
	if bounds.Dx() < 100 || bounds.Dy() < 60 {
		return false
	}
	dark, bright, samples := 0, 0, 0
	for y := bounds.Min.Y + 2; y < bounds.Max.Y-2; y += 4 {
		for x := bounds.Min.X + 2; x < bounds.Max.X-2; x += 4 {
			r, g, b, _ := img.At(x, y).RGBA()
			r8, g8, b8 := int(r>>8), int(g>>8), int(b>>8)
			samples++
			if r8 <= 48 && g8 <= 48 && b8 <= 48 {
				dark++
			}
			if r8 >= 170 && g8 >= 170 && b8 >= 170 &&
				maxInt(r8, g8, b8)-minInt(r8, g8, b8) <= 45 {
				bright++
			}
		}
	}
	return samples > 0 && dark*100 >= samples*12 && bright >= 10
}
