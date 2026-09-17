package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"KaTools/tools/ocrworker"
)

const targetROIFile = "target_roi.json"

var targetROIState = struct {
	sync.RWMutex
	roi      ocrworker.PartyROIConfig
	selected bool
}{}

// LoadTargetROI restores a previously selected target-panel area so it can be
// shared by Target Until Dead and Emergency Skill after a config load.
func LoadTargetROI() ocrworker.PartyROIConfig {
	targetROIState.RLock()
	current := targetROIState.roi
	selected := targetROIState.selected
	targetROIState.RUnlock()
	if current.Width > 0 && current.Height > 0 {
		current.Selected = selected
		return current
	}

	data, err := os.ReadFile(targetROIFile)
	if err != nil {
		return ocrworker.PartyROIConfig{}
	}

	var roi ocrworker.PartyROIConfig
	if err := json.Unmarshal(data, &roi); err != nil || roi.Width <= 0 || roi.Height <= 0 {
		return ocrworker.PartyROIConfig{}
	}

	targetROIState.Lock()
	targetROIState.roi = roi
	targetROIState.selected = roi.Selected
	targetROIState.Unlock()
	return roi
}

func SavePickedTargetROI(roi ocrworker.PartyROIConfig) error {
	return saveTargetROI(roi, true)
}

func ResetPickedTargetROI() error {
	targetROIState.RLock()
	roi := targetROIState.roi
	targetROIState.RUnlock()
	return saveTargetROI(roi, false)
}

func saveTargetROI(roi ocrworker.PartyROIConfig, selected bool) error {
	if roi.Width <= 0 || roi.Height <= 0 {
		return fmt.Errorf("invalid target HP bar ROI size")
	}

	roi.Selected = selected
	data, err := json.MarshalIndent(roi, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(targetROIFile, data, 0644); err != nil {
		return err
	}

	targetROIState.Lock()
	targetROIState.roi = roi
	targetROIState.selected = selected
	targetROIState.Unlock()
	return nil
}
