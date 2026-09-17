package main

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"sync"
	"unsafe"

	"KaTools/tools/ocrworker"

	"golang.org/x/sys/windows"
)

const (
	srccopy          = 0x00CC0020
	dibRGBColors     = 0
	biRGB            = 0
	bitsPerPixelBGRA = 32
)

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [1]uint32
}

type roiPreviewCache struct {
	sync.RWMutex
	png []byte
}

var partyROIPreview roiPreviewCache
var statusROIPreview roiPreviewCache
var deathROIPreview roiPreviewCache
var targetROIPreview roiPreviewCache

func getPartyROIPreview() ([]byte, bool) {
	return getROIPreview(&partyROIPreview, "party")
}

func getStatusROIPreview() ([]byte, bool) {
	return getROIPreview(&statusROIPreview, "status")
}

func getDeathROIPreview() ([]byte, bool) {
	return getROIPreview(&deathROIPreview, "death")
}

func getTargetROIPreview() ([]byte, bool) {
	return getROIPreview(&targetROIPreview, "target")
}

func clearROIPreview(kind string) {
	preview := roiPreviewForKind(kind)
	if preview == nil {
		return
	}
	preview.Lock()
	preview.png = nil
	preview.Unlock()
	_ = os.Remove(roiPreviewFile(kind))
}

func roiPreviewForKind(kind string) *roiPreviewCache {
	switch kind {
	case "party":
		return &partyROIPreview
	case "status":
		return &statusROIPreview
	case "death":
		return &deathROIPreview
	case "target":
		return &targetROIPreview
	default:
		return nil
	}
}

func roiPreviewFile(kind string) string {
	return kind + "_roi_preview.png"
}

func getROIPreview(preview *roiPreviewCache, kind string) ([]byte, bool) {
	preview.RLock()
	if len(preview.png) != 0 {
		png := append([]byte(nil), preview.png...)
		preview.RUnlock()
		return png, true
	}
	preview.RUnlock()

	png, err := os.ReadFile(roiPreviewFile(kind))
	if err != nil || len(png) == 0 {
		return nil, false
	}
	preview.Lock()
	preview.png = append([]byte(nil), png...)
	preview.Unlock()
	return png, true
}

func saveROIPreview(kind string, png []byte) error {
	preview := roiPreviewForKind(kind)
	if preview == nil {
		return fmt.Errorf("unknown preview kind %q", kind)
	}
	if err := os.WriteFile(roiPreviewFile(kind), png, 0644); err != nil {
		return err
	}
	_ = hideROIConfigFiles()
	preview.Lock()
	preview.png = append([]byte(nil), png...)
	preview.Unlock()
	return nil
}

func capturePartyROIPreview(rect pickerRECT, roi ocrworker.PartyROIConfig) error {
	return captureROIPreview(rect, roi, "party")
}

func captureStatusROIPreview(rect pickerRECT, roi ocrworker.PartyROIConfig) error {
	return captureROIPreview(rect, roi, "status")
}

func captureDeathROIPreview(rect pickerRECT, roi ocrworker.PartyROIConfig) error {
	return captureROIPreview(rect, roi, "death")
}

func captureTargetROIPreview(rect pickerRECT, roi ocrworker.PartyROIConfig) error {
	return captureROIPreview(rect, roi, "target")
}

func captureROIPreview(rect pickerRECT, roi ocrworker.PartyROIConfig, kind string) error {
	if roi.Width <= 0 || roi.Height <= 0 {
		return fmt.Errorf("invalid selected area")
	}

	user32 := windows.NewLazySystemDLL("user32.dll")
	gdi32 := windows.NewLazySystemDLL("gdi32.dll")
	getDC := user32.NewProc("GetDC")
	releaseDC := user32.NewProc("ReleaseDC")
	createCompatibleDC := gdi32.NewProc("CreateCompatibleDC")
	deleteDC := gdi32.NewProc("DeleteDC")
	createCompatibleBitmap := gdi32.NewProc("CreateCompatibleBitmap")
	selectObject := gdi32.NewProc("SelectObject")
	deleteObject := gdi32.NewProc("DeleteObject")
	bitBlt := gdi32.NewProc("BitBlt")
	getDIBits := gdi32.NewProc("GetDIBits")

	screenDC, _, _ := getDC.Call(0)
	if screenDC == 0 {
		return fmt.Errorf("GetDC failed")
	}
	defer releaseDC.Call(0, screenDC)

	memDC, _, _ := createCompatibleDC.Call(screenDC)
	if memDC == 0 {
		return fmt.Errorf("CreateCompatibleDC failed")
	}
	defer deleteDC.Call(memDC)

	bitmap, _, _ := createCompatibleBitmap.Call(
		screenDC, uintptr(roi.Width), uintptr(roi.Height),
	)
	if bitmap == 0 {
		return fmt.Errorf("CreateCompatibleBitmap failed")
	}
	defer deleteObject.Call(bitmap)

	oldObject, _, _ := selectObject.Call(memDC, bitmap)
	defer selectObject.Call(memDC, oldObject)

	if copied, _, _ := bitBlt.Call(
		memDC, 0, 0, uintptr(roi.Width), uintptr(roi.Height), screenDC,
		uintptr(int(rect.Left)+roi.X), uintptr(int(rect.Top)+roi.Y), srccopy,
	); copied == 0 {
		return fmt.Errorf("BitBlt failed")
	}

	info := bitmapInfo{Header: bitmapInfoHeader{
		Size:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		Width:       int32(roi.Width),
		Height:      -int32(roi.Height), // top-down pixels
		Planes:      1,
		BitCount:    bitsPerPixelBGRA,
		Compression: biRGB,
		SizeImage:   uint32(roi.Width * roi.Height * 4),
	}}
	pixels := make([]byte, roi.Width*roi.Height*4)
	read, _, _ := getDIBits.Call(
		memDC, bitmap, 0, uintptr(roi.Height), uintptr(unsafe.Pointer(&pixels[0])),
		uintptr(unsafe.Pointer(&info)), dibRGBColors,
	)
	if read == 0 {
		return fmt.Errorf("GetDIBits failed")
	}

	img := image.NewRGBA(image.Rect(0, 0, roi.Width, roi.Height))
	for y := 0; y < roi.Height; y++ {
		for x := 0; x < roi.Width; x++ {
			src := (y*roi.Width + x) * 4
			dst := y*img.Stride + x*4
			img.Pix[dst] = pixels[src+2]
			img.Pix[dst+1] = pixels[src+1]
			img.Pix[dst+2] = pixels[src]
			img.Pix[dst+3] = 255
		}
	}

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		return fmt.Errorf("encode preview: %w", err)
	}

	if err := saveROIPreview(kind, encoded.Bytes()); err != nil {
		return fmt.Errorf("save preview: %w", err)
	}
	return nil
}
