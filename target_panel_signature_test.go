package main

import (
	"image"
	"image/color"
	"testing"
)

func TestTargetPanelNameSignatureIgnoresLowerHPBar(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 40))
	for y := 20; y < 40; y++ {
		for x := 0; x < 100; x++ {
			img.Set(x, y, color.RGBA{R: 200, A: 255})
		}
	}
	before := targetPanelNameSignature(img)
	for y := 20; y < 40; y++ {
		for x := 0; x < 100; x++ {
			img.Set(x, y, color.RGBA{R: 100, A: 255})
		}
	}
	if before != targetPanelNameSignature(img) {
		t.Fatal("HP-bar-only change altered target name signature")
	}

	for y := 3; y <= 7; y++ {
		for x := 8; x <= 14; x++ {
			img.Set(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	if before == targetPanelNameSignature(img) {
		t.Fatal("name-area change did not alter target name signature")
	}
}

func TestTargetPanelNameImageKeepsFullHeader(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 60))
	name := targetPanelNameImage(img)
	if name == nil {
		t.Fatal("name crop is nil")
	}
	if got, want := name.Bounds().Dy(), 36; got != want {
		t.Fatalf("name crop height = %d, want %d", got, want)
	}
}

func TestTargetPanelNameLooksPresentRequiresHeaderGlyphs(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 40))
	if targetPanelNameLooksPresent(img) {
		t.Fatal("blank target panel was treated as a live target")
	}

	// Bright pixels in the lower HP-bar portion must not hold Support Skills.
	for y := 30; y < 40; y++ {
		for x := 0; x < 100; x++ {
			img.Set(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	if targetPanelNameLooksPresent(img) {
		t.Fatal("lower bar pixels were treated as target-name glyphs")
	}

	for index := 0; index < targetPanelMinNameGlyphPixels; index++ {
		img.Set(5+index, 4, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	}
	if !targetPanelNameLooksPresent(img) {
		t.Fatal("visible target header glyphs did not keep Support Skills held")
	}
}
