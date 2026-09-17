package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"KaTools/tools/ocrworker"
)

const statusROIFile = "status_roi.json"

var statusROIState = struct {
	sync.RWMutex
	roi      ocrworker.PartyROIConfig
	selected bool
}{}

// LoadStatusROI restores a previously selected status area so a saved KaTools
// configuration can be loaded without selecting the HP/TP panel again.
func LoadStatusROI() ocrworker.PartyROIConfig {
	statusROIState.RLock()
	current := statusROIState.roi
	selected := statusROIState.selected
	statusROIState.RUnlock()

	if current.Width > 0 && current.Height > 0 {
		current.Selected = selected
		return current
	}

	data, err := os.ReadFile(statusROIFile)
	if err != nil {
		return ocrworker.PartyROIConfig{}
	}

	var roi ocrworker.PartyROIConfig
	if err := json.Unmarshal(data, &roi); err != nil || roi.Width <= 0 || roi.Height <= 0 {
		return ocrworker.PartyROIConfig{}
	}

	statusROIState.Lock()
	statusROIState.roi = roi
	statusROIState.selected = roi.Selected
	statusROIState.Unlock()
	return roi
}

func SavePickedStatusROI(roi ocrworker.PartyROIConfig) error {
	return saveStatusROI(roi, true)
}

func ResetPickedStatusROI() error {
	statusROIState.RLock()
	roi := statusROIState.roi
	statusROIState.RUnlock()
	return saveStatusROI(roi, false)
}

func saveStatusROI(roi ocrworker.PartyROIConfig, selected bool) error {
	if roi.Width <= 0 || roi.Height <= 0 {
		return fmt.Errorf("invalid status ROI size")
	}

	roi.Selected = selected
	data, err := json.MarshalIndent(roi, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(statusROIFile, data, 0644); err != nil {
		return err
	}

	statusROIState.Lock()
	statusROIState.roi = roi
	statusROIState.selected = selected
	statusROIState.Unlock()
	return nil
}
