package main

import (
	"image"
	"image/color"
	"testing"
)

func TestDCNotificationRequiresTwoExactReadings(t *testing.T) {
	c := &DCNotificationController{}
	text := "Sorry connection to server has failed. Please try again"
	if c.Observe([]string{text}) {
		t.Fatal("one DC OCR reading sent Enter")
	}
	if !c.Observe([]string{text}) {
		t.Fatal("second DC OCR reading did not confirm")
	}
}

func TestDCNotificationRejectsIncompleteText(t *testing.T) {
	c := &DCNotificationController{}
	if c.Observe([]string{"connection to server is unstable"}) {
		t.Fatal("incomplete connection text was treated as a DC dialog")
	}
}

func TestDCNotificationROIStaysCenteredAndInBounds(t *testing.T) {
	roi := dcNotificationROI(1920, 1440)
	if !roi.In(image.Rect(0, 0, 1920, 1440)) {
		t.Fatalf("DC ROI %v is outside the frame", roi)
	}
	if roi.Min.X >= 960 || roi.Max.X <= 960 || roi.Min.Y >= 720 || roi.Max.Y <= 720 {
		t.Fatalf("DC ROI %v does not cover frame center", roi)
	}
}

func TestDCDialogLooksPresentNeedsDarkPanelAndBrightGlyphs(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 160, 80))
	for y := 12; y < 68; y++ {
		for x := 16; x < 144; x++ {
			img.Set(x, y, color.RGBA{R: 20, G: 20, B: 20, A: 255})
		}
	}
	for x := 35; x < 95; x++ {
		img.Set(x, 38, color.RGBA{R: 230, G: 230, B: 230, A: 255})
	}
	if !dcDialogLooksPresent(img) {
		t.Fatal("dark Message-style panel was not a DC OCR candidate")
	}
}
