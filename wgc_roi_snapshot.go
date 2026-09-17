package main

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"time"

	"KaTools/tools/ocrworker"
)

const wgcSnapshotDirectory = "ScreenshotWGC"

func wgcSnapshotLabels(kind string) []string {
	switch kind {
	case "status":
		return []string{"HP Area"}
	case "party":
		return []string{"Party Area"}
	case "death":
		// Death and resurrection intentionally share one selected dialog ROI.
		return []string{"Death Area", "Resu Area"}
	case "target":
		return []string{"Target Area"}
	default:
		return nil
	}
}

func roiPreviewForSnapshot(kind string) ([]byte, bool) {
	switch kind {
	case "status":
		return getStatusROIPreview()
	case "party":
		return getPartyROIPreview()
	case "death":
		return getDeathROIPreview()
	case "target":
		return getTargetROIPreview()
	default:
		return nil, false
	}
}

// CaptureWGCROISnapshot writes a one-time, runtime WGC crop for a newly
// selected ROI. It never starts a recording and each label is overwritten on
// the next SET AREA, keeping only the current evidence for comparison.
func (m *RuntimeManager) CaptureWGCROISnapshot(kind string, roi ocrworker.PartyROIConfig) error {
	labels := wgcSnapshotLabels(kind)
	if len(labels) == 0 {
		return fmt.Errorf("unknown WGC snapshot kind %q", kind)
	}
	if !roi.Selected || roi.Width <= 0 || roi.Height <= 0 {
		return fmt.Errorf("%s has no selected area", kind)
	}

	m.mu.RLock()
	if !m.running || m.reader == nil || m.memory == 0 {
		m.mu.RUnlock()
		return fmt.Errorf("WGC is not running; start the bot once to create the %s debug crop", kind)
	}

	wgcROI := partyROIToFrame(roi, m.scaleX, m.scaleY, m.frameOriginX, m.frameOriginY)
	wgcROI.X += m.captureOffsetX
	wgcROI.Y += m.captureOffsetY
	img, ok := m.reader.ToImageRect(wgcROI.X, wgcROI.Y, wgcROI.Width, wgcROI.Height)
	frameNumber := readHeader(m.memory).FrameNumber
	frameWidth, frameHeight := m.reader.width, m.reader.height
	scaleX, scaleY := m.scaleX, m.scaleY
	originX, originY := m.frameOriginX, m.frameOriginY
	offsetX, offsetY := m.captureOffsetX, m.captureOffsetY
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("WGC crop is outside frame: X=%d Y=%d W=%d H=%d; frame=%dx%d", wgcROI.X, wgcROI.Y, wgcROI.Width, wgcROI.Height, frameWidth, frameHeight)
	}

	if err := os.MkdirAll(wgcSnapshotDirectory, 0755); err != nil {
		return fmt.Errorf("create %s: %w", wgcSnapshotDirectory, err)
	}
	setAreaPreview, hasSetAreaPreview := roiPreviewForSnapshot(kind)
	for _, label := range labels {
		if err := saveSnapshotPNG(filepath.Join(wgcSnapshotDirectory, label+".png"), img); err != nil {
			return err
		}
		if hasSetAreaPreview {
			if err := os.WriteFile(filepath.Join(wgcSnapshotDirectory, label+" - SET AREA.png"), setAreaPreview, 0644); err != nil {
				return fmt.Errorf("save SET AREA preview: %w", err)
			}
		}

		info := fmt.Sprintf(
			"Captured once: %s\nFrame number: %d (%dx%d)\n\nSET AREA (client coordinates)\nX=%d Y=%d W=%d H=%d\n\nWGC runtime crop (actual scanner coordinates)\nX=%d Y=%d W=%d H=%d\n\nMapping\nScaleX=%.6f ScaleY=%.6f\nFrameOriginX=%d FrameOriginY=%d\nCalibrationOffsetX=%d CalibrationOffsetY=%d\n",
			time.Now().Format(time.RFC3339), frameNumber, frameWidth, frameHeight,
			roi.X, roi.Y, roi.Width, roi.Height,
			wgcROI.X, wgcROI.Y, wgcROI.Width, wgcROI.Height,
			scaleX, scaleY, originX, originY, offsetX, offsetY,
		)
		if err := os.WriteFile(filepath.Join(wgcSnapshotDirectory, label+".txt"), []byte(info), 0644); err != nil {
			return fmt.Errorf("save snapshot coordinates: %w", err)
		}
		appendOCRLog("WGC SNAPSHOT | %s | SET AREA=%d,%d %dx%d | WGC=%d,%d %dx%d | Frame=%d", label, roi.X, roi.Y, roi.Width, roi.Height, wgcROI.X, wgcROI.Y, wgcROI.Width, wgcROI.Height, frameNumber)
	}

	return nil
}

func saveSnapshotPNG(path string, img image.Image) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create WGC snapshot: %w", err)
	}
	defer file.Close()
	if err := png.Encode(file, img); err != nil {
		return fmt.Errorf("encode WGC snapshot: %w", err)
	}
	return nil
}

// CaptureConfiguredWGCROISnapshots runs once just after WGC starts. It covers
// areas selected before Start; selections made while running are captured by
// the picker itself.
func (m *RuntimeManager) CaptureConfiguredWGCROISnapshots() {
	for _, item := range []struct {
		kind string
		roi  ocrworker.PartyROIConfig
	}{
		{kind: "status", roi: LoadStatusROI()},
		{kind: "party", roi: ocrworker.LoadPartyROI()},
		{kind: "death", roi: LoadDeathROI()},
		{kind: "target", roi: LoadTargetROI()},
	} {
		if !item.roi.Selected {
			continue
		}
		if err := m.CaptureWGCROISnapshot(item.kind, item.roi); err != nil {
			appendOCRLog("WGC SNAPSHOT | %s skipped | %v", item.kind, err)
		}
	}
}
