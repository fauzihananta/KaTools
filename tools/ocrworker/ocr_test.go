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

func TestStatusNumbersImageLocatesColoredBarRows(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 100, 60))
	for y := 28; y <= 32; y++ {
		for x := 5; x < 95; x++ {
			src.Set(x, y, color.RGBA{R: 220, G: 25, B: 25, A: 255})
		}
	}
	for y := 44; y <= 48; y++ {
		for x := 5; x < 95; x++ {
			src.Set(x, y, color.RGBA{R: 25, G: 65, B: 220, A: 255})
		}
	}

	got := statusNumbersImage(src)
	if got.Bounds().Dx() != 100 || got.Bounds().Dy() != 31 {
		t.Fatalf("bar crop = %v, want 100x31", got.Bounds())
	}
}

func TestStatusBarColourChecksRejectWhiteHUDGlyphs(t *testing.T) {
	if statusHPBarPixel(255, 255, 255) {
		t.Fatal("white HUD glyph was treated as a red HP-bar pixel")
	}
	if statusTPBarPixel(255, 255, 255) {
		t.Fatal("white HUD glyph was treated as a blue TP-bar pixel")
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

func TestStatusRowImagesFollowColoredBars(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 100, 60))
	for y := 20; y <= 25; y++ {
		for x := 5; x < 95; x++ {
			src.Set(x, y, color.RGBA{R: 220, G: 25, B: 25, A: 255})
		}
	}
	for y := 42; y <= 47; y++ {
		for x := 5; x < 95; x++ {
			src.Set(x, y, color.RGBA{R: 25, G: 65, B: 220, A: 255})
		}
	}

	rows := statusRowImages(src)
	if len(rows) != 2 {
		t.Fatalf("row count = %d, want 2", len(rows))
	}
	if got, want := rows[0].Bounds().Dy(), 18; got != want {
		t.Fatalf("HP row height = %d, want %d", got, want)
	}
	if got, want := rows[1].Bounds().Dy(), 18; got != want {
		t.Fatalf("TP row height = %d, want %d", got, want)
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

func TestTargetNameLeftImageExcludesLevelColumn(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 100, 20))
	left := targetNameLeftImage(src)
	if left == nil {
		t.Fatal("name-only image is nil")
	}
	if got, want := left.Bounds().Dx(), 75; got != want {
		t.Fatalf("name-only width = %d, want %d", got, want)
	}
	if got, want := left.Bounds().Dy(), 20; got != want {
		t.Fatalf("name-only height = %d, want %d", got, want)
	}
}

func TestTargetNameOCRVariantsTryNameOnlyMaskFirst(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 100, 20))
	variants := targetNameOCRVariants(src)
	if len(variants) < 2 {
		t.Fatalf("variant count = %d, want at least name-only and full masks", len(variants))
	}
	// targetNameLeftImage uses 75%% of the input and each target OCR image is
	// upscaled 3x. Keeping this first is what lets a conclusive configured-name
	// match skip the slower fallback Tesseract processes.
	if got, want := variants[0].Bounds().Dx(), 75*3; got != want {
		t.Fatalf("first target OCR width = %d, want name-only %d", got, want)
	}
	if got, want := variants[1].Bounds().Dx(), 100*3; got != want {
		t.Fatalf("second target OCR width = %d, want full masked %d", got, want)
	}
}
