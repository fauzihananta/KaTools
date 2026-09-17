package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"KaTools/tools/ocrworker"
)

const deathROIFile = "death_roi.json"

var deathROIState = struct {
	sync.RWMutex
	roi      ocrworker.PartyROIConfig
	selected bool
}{}

// LoadDeathROI restores a previously selected death-dialog area along with a
// loaded configuration.
func LoadDeathROI() ocrworker.PartyROIConfig {
	deathROIState.RLock()
	current := deathROIState.roi
	selected := deathROIState.selected
	deathROIState.RUnlock()
	if current.Width > 0 && current.Height > 0 {
		current.Selected = selected
		return current
	}

	data, err := os.ReadFile(deathROIFile)
	if err != nil {
		return ocrworker.PartyROIConfig{}
	}
	var roi ocrworker.PartyROIConfig
	if err := json.Unmarshal(data, &roi); err != nil || roi.Width <= 0 || roi.Height <= 0 {
		return ocrworker.PartyROIConfig{}
	}

	deathROIState.Lock()
	deathROIState.roi = roi
	deathROIState.selected = roi.Selected
	deathROIState.Unlock()
	return roi
}

func SavePickedDeathROI(roi ocrworker.PartyROIConfig) error {
	return saveDeathROI(roi, true)
}

func ResetPickedDeathROI() error {
	deathROIState.RLock()
	roi := deathROIState.roi
	deathROIState.RUnlock()
	return saveDeathROI(roi, false)
}

func saveDeathROI(roi ocrworker.PartyROIConfig, selected bool) error {
	if roi.Width <= 0 || roi.Height <= 0 {
		return fmt.Errorf("invalid death ROI size")
	}

	roi.Selected = selected
	data, err := json.MarshalIndent(roi, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(deathROIFile, data, 0644); err != nil {
		return err
	}

	deathROIState.Lock()
	deathROIState.roi = roi
	deathROIState.selected = selected
	deathROIState.Unlock()
	return nil
}
