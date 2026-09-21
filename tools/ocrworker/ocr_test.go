package ocrworker

import (
	"image"
	"image/color"
	"testing"
)

func TestStatusNumbersImageRemovesHeaderThird(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 20, 12))
	got := statusNumbersImage(src)
	if got.Bounds().Dx() != 20 || got.Bounds().Dy() != 8 {
		t.Fatalf("status crop = %v, want 20x8 after header removal", got.Bounds())
	}
}

func TestStatusRowImagesReturnsOverlappingHPAndTPRows(t *testing.T) {
	rows := statusRowImages(image.NewRGBA(image.Rect(0, 0, 20, 30)))
	if len(rows) != 2 {
		t.Fatalf("row count = %d, want 2", len(rows))
	}
	if rows[0].Bounds().Dy() <= 15 || rows[1].Bounds().Dy() <= 15 {
		t.Fatalf("rows too short for a HUD line: %dx%d and %dx%d", rows[0].Bounds().Dx(), rows[0].Bounds().Dy(), rows[1].Bounds().Dx(), rows[1].Bounds().Dy())
	}
}

func TestStatusRowPairTextExtractsOneNumericPair(t *testing.T) {
	if got := statusRowPairText("Lv.48  6334 / 6334"); got != "6334 / 6334" {
		t.Fatalf("status row pair = %q, want numeric pair", got)
	}
	if got := statusRowPairText("4228 4228"); got != "" {
		t.Fatalf("row without slash = %q, want empty", got)
	}
}

func TestTargetNameBrightGlyphImageRejectsColoredBackdrop(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2, 1))
	src.Set(0, 0, color.RGBA{R: 240, G: 238, B: 242, A: 255})
	src.Set(1, 0, color.RGBA{R: 240, G: 120, B: 120, A: 255})

	got := targetNameBrightGlyphImage(src)
	if value := color.GrayModel.Convert(got.At(0, 0)).(color.Gray).Y; value != 0 {
		t.Fatalf("near-white glyph = %d, want black foreground", value)
	}
	if value := color.GrayModel.Convert(got.At(1, 0)).(color.Gray).Y; value != 255 {
		t.Fatalf("coloured backdrop = %d, want white background", value)
	}
}
