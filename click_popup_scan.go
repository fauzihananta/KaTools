package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"KaTools/tools/ocrworker"
)

const clickPopupScanFile = "click_popup_scan_areas.json"

type ClickPopupScanArea struct {
	ROI          ocrworker.PartyROIConfig `json:"roi"`
	ClientWidth  int                      `json:"clientWidth"`
	ClientHeight int                      `json:"clientHeight"`
	Custom       bool                     `json:"custom"`
}

var clickPopupScanMu sync.Mutex

func validClickPopupScanKind(kind string) bool {
	return kind == "party" || kind == "death" || kind == "dc"
}

func defaultClickPopupScanAreas() map[string]ClickPopupScanArea {
	defaultArea := automaticPopupROI(1920, 1440)
	defaultArea.Selected = true
	area := ClickPopupScanArea{ROI: defaultArea, ClientWidth: 1920, ClientHeight: 1440}
	return map[string]ClickPopupScanArea{
		"party": area,
		"death": area,
		"dc":    area,
	}
}

func loadClickPopupScanAreasLocked() map[string]ClickPopupScanArea {
	areas := defaultClickPopupScanAreas()
	data, err := os.ReadFile(clickPopupScanFile)
	if err != nil {
		return areas
	}
	var stored map[string]ClickPopupScanArea
	if err := json.Unmarshal(data, &stored); err != nil {
		return areas
	}
	for kind, area := range stored {
		if !validClickPopupScanKind(kind) || area.ClientWidth <= 0 || area.ClientHeight <= 0 {
			continue
		}
		if area.ROI.Selected && (area.ROI.X < 0 || area.ROI.Y < 0 || area.ROI.Width <= 0 || area.ROI.Height <= 0) {
			continue
		}
		areas[kind] = area
	}
	return areas
}

func LoadClickPopupScanAreas() map[string]ClickPopupScanArea {
	clickPopupScanMu.Lock()
	defer clickPopupScanMu.Unlock()
	return loadClickPopupScanAreasLocked()
}

func LoadClickPopupScanAreaForClient(kind string, clientWidth, clientHeight int) (ClickPopupScanArea, bool) {
	if !validClickPopupScanKind(kind) || clientWidth <= 0 || clientHeight <= 0 {
		return ClickPopupScanArea{}, false
	}
	area := LoadClickPopupScanAreas()[kind]
	if area.ROI.Selected && (area.ClientWidth != clientWidth || area.ClientHeight != clientHeight) {
		area.ROI.X = area.ROI.X * clientWidth / area.ClientWidth
		area.ROI.Y = area.ROI.Y * clientHeight / area.ClientHeight
		area.ROI.Width = max(1, area.ROI.Width*clientWidth/area.ClientWidth)
		area.ROI.Height = max(1, area.ROI.Height*clientHeight/area.ClientHeight)
		area.ClientWidth = clientWidth
		area.ClientHeight = clientHeight
	}
	return area, true
}

func SavePickedClickPopupScanArea(kind string, roi ocrworker.PartyROIConfig, clientWidth, clientHeight int) error {
	if !validClickPopupScanKind(kind) {
		return fmt.Errorf("invalid click popup scan area %q", kind)
	}
	if clientWidth <= 0 || clientHeight <= 0 || roi.X < 0 || roi.Y < 0 || roi.Width < 5 || roi.Height < 5 ||
		roi.X+roi.Width > clientWidth || roi.Y+roi.Height > clientHeight {
		return fmt.Errorf("invalid click popup scan area bounds")
	}
	clickPopupScanMu.Lock()
	defer clickPopupScanMu.Unlock()
	areas := loadClickPopupScanAreasLocked()
	roi.Selected = true
	areas[kind] = ClickPopupScanArea{ROI: roi, ClientWidth: clientWidth, ClientHeight: clientHeight, Custom: true}
	data, err := json.MarshalIndent(areas, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(clickPopupScanFile, data, 0644)
}

func ResetClickPopupScanArea(kind string) error {
	if !validClickPopupScanKind(kind) {
		return fmt.Errorf("invalid click popup scan area %q", kind)
	}
	clickPopupScanMu.Lock()
	defer clickPopupScanMu.Unlock()
	areas := loadClickPopupScanAreasLocked()
	areas[kind] = ClickPopupScanArea{
		ROI:          ocrworker.PartyROIConfig{},
		ClientWidth:  1920,
		ClientHeight: 1440,
		Custom:       false,
	}
	data, err := json.MarshalIndent(areas, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(clickPopupScanFile, data, 0644)
}
