package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

const clickSkillROIFile = "click_skill_rois.json"

var clickSkillROIMu sync.Mutex

var clickSkillSlots = []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "0", "F1", "F2", "F3", "F4", "F5", "F6", "F7", "F8", "F9", "F10"}

var clickActionSlots = []string{"AutoAccept", "AutoResu", "DCOk", "ChatParty"}

type ClickSkillROI struct {
	X            int  `json:"x"`
	Y            int  `json:"y"`
	Width        int  `json:"width"`
	Height       int  `json:"height"`
	Selected     bool `json:"selected"`
	ClientWidth  int  `json:"clientWidth,omitempty"`
	ClientHeight int  `json:"clientHeight,omitempty"`
}

func isClickSkillVK(vk uintptr) bool {
	return (vk >= 0x30 && vk <= 0x39) || (vk >= 0x70 && vk <= 0x79)
}

func isClickSkillSlot(slot string) bool {
	if slot == "HP" || slot == "TP" {
		return true
	}
	for _, candidate := range clickActionSlots {
		if slot == candidate {
			return true
		}
	}
	if len(slot) == len("Emergency1") && slot[:len("Emergency")] == "Emergency" && slot[len("Emergency")] >= '1' && slot[len("Emergency")] <= '5' {
		return true
	}
	for _, candidate := range clickSkillSlots {
		if candidate == slot {
			return true
		}
	}
	return false
}

func loadClickSkillROIsLocked() map[string]ClickSkillROI {
	areas := make(map[string]ClickSkillROI, len(clickSkillSlots))
	data, err := os.ReadFile(clickSkillROIFile)
	if err != nil {
		return areas
	}
	var stored map[string]ClickSkillROI
	if err := json.Unmarshal(data, &stored); err != nil {
		return areas
	}
	for slot, roi := range stored {
		referenceValid := (roi.ClientWidth == 0 && roi.ClientHeight == 0) || (roi.ClientWidth > 0 && roi.ClientHeight > 0)
		if isClickSkillSlot(slot) && referenceValid && roi.Selected && roi.X >= 0 && roi.Y >= 0 && roi.Width > 0 && roi.Height > 0 {
			areas[slot] = roi
		}
	}
	return areas
}

func LoadClickSkillROIs() map[string]ClickSkillROI {
	clickSkillROIMu.Lock()
	defer clickSkillROIMu.Unlock()
	return loadClickSkillROIsLocked()
}

func SavePickedClickSkillROI(slot string, roi ClickSkillROI) error {
	if !isClickSkillSlot(slot) {
		return fmt.Errorf("invalid click skill slot %q", slot)
	}
	referenceValid := (roi.ClientWidth == 0 && roi.ClientHeight == 0) || (roi.ClientWidth > 0 && roi.ClientHeight > 0)
	if !referenceValid || roi.X < 0 || roi.Y < 0 || roi.Width <= 0 || roi.Height <= 0 {
		return fmt.Errorf("invalid click skill area")
	}
	roi.Selected = true

	clickSkillROIMu.Lock()
	defer clickSkillROIMu.Unlock()
	stored := loadClickSkillROIsLocked()
	stored[slot] = roi
	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(clickSkillROIFile, data, 0644)
}

func ResetPickedClickSkillROI(slot string) error {
	if !isClickSkillSlot(slot) {
		return fmt.Errorf("invalid click skill slot %q", slot)
	}
	clickSkillROIMu.Lock()
	defer clickSkillROIMu.Unlock()
	stored := loadClickSkillROIsLocked()
	delete(stored, slot)
	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(clickSkillROIFile, data, 0644)
}

func clickConfiguredAction(hwnd uintptr, slot string) (bool, error) {
	area, ok := LoadClickSkillROIs()[slot]
	if !ok || !area.Selected {
		return false, fmt.Errorf("click area %q is not set", slot)
	}
	err := clickWindowClientPointReference(
		hwnd,
		area.X+area.Width/2,
		area.Y+area.Height/2,
		area.ClientWidth,
		area.ClientHeight,
	)
	return err == nil, err
}
