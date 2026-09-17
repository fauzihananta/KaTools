package main

import (
	"image"
	"image/color"
	"math"
	"testing"
)

func TestDetectStatusBarPercents(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 120, 30))
	fillBar(img, 4, 115, 4, 9, 70, color.RGBA{R: 220, G: 25, B: 25, A: 255})
	fillBar(img, 4, 115, 18, 23, 45, color.RGBA{R: 25, G: 65, B: 220, A: 255})

	hp, hpOK, tp, tpOK := detectStatusBarPercents(img)
	if !hpOK || !tpOK {
		t.Fatalf("bars not found: hpOK=%t tpOK=%t", hpOK, tpOK)
	}
	if math.Abs(hp-62.5) > 1.5 {
		t.Fatalf("unexpected HP percent: %.2f", hp)
	}
	if math.Abs(tp-40.2) > 1.5 {
		t.Fatalf("unexpected TP percent: %.2f", tp)
	}
}

func TestDetectStatusBarPercentsRoundsVisuallyFullBarToOneHundred(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 120, 20))
	// 110 px appears as a full 112-pixel game bar after its border/highlight.
	fillBar(img, 4, 115, 4, 9, 110, color.RGBA{R: 220, G: 25, B: 25, A: 255})

	hp, hpOK, _, _ := detectStatusBarPercents(img)
	if !hpOK || hp != 100 {
		t.Fatalf("visually full HP bar = %.2f%%, want 100%%", hp)
	}
}

func TestDetectStatusBarUsesFullHPLineAsTPWidthReference(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 160, 30))
	// This mimics a real selection with an asymmetric panel frame: the left
	// margin is 12 px but the right border is only a few pixels away. HP is
	// full, while TP is 125/142 = 88.0%. The old symmetric-margin formula
	// incorrectly reported TP as about 92%.
	fillBar(img, 12, 155, 4, 9, 142, color.RGBA{R: 220, G: 25, B: 25, A: 255})
	fillBar(img, 12, 155, 18, 23, 125, color.RGBA{R: 25, G: 65, B: 220, A: 255})

	hp, hpOK, tp, tpOK := detectStatusBarPercents(img)
	if !hpOK || !tpOK || hp != 100 {
		t.Fatalf("unexpected status detection: hp=%.2f ok=%t tp=%.2f ok=%t", hp, hpOK, tp, tpOK)
	}
	if math.Abs(tp-88.0) > 1.0 {
		t.Fatalf("TP percent = %.2f, want about 88%%", tp)
	}
}

func TestDetectStatusBarIgnoresSeparatedBlueNoise(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 120, 24))
	// The actual TP fill is 45 px. A separate blue effect appears near the
	// right edge; it must not stretch TP from 40% to nearly 100%.
	fillBar(img, 4, 115, 10, 15, 45, color.RGBA{R: 25, G: 65, B: 220, A: 255})
	for y := 10; y <= 15; y++ {
		for x := 95; x <= 114; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 25, G: 65, B: 220, A: 255})
		}
	}

	_, _, tp, tpOK := detectStatusBarPercents(img)
	if !tpOK {
		t.Fatal("TP bar was not found")
	}
	if math.Abs(tp-40.2) > 1.5 {
		t.Fatalf("separated blue pixels inflated TP to %.2f%%", tp)
	}
}

func fillBar(img *image.RGBA, startX, endX, startY, endY, fillWidth int, c color.RGBA) {
	for y := startY; y <= endY; y++ {
		for x := startX; x < startX+fillWidth && x <= endX; x++ {
			img.SetRGBA(x, y, c)
		}
	}
}
