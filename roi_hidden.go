package main

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

var roiConfigFiles = []string{
	"party_roi.json",
	clickSkillROIFile,
	clickPopupScanFile,
	statusROIFile,
	deathROIFile,
	targetROIFile,
	chatTextROIFile,
	"party_roi_preview.png",
	"status_roi_preview.png",
	"death_roi_preview.png",
	"target_roi_preview.png",
	"click-popup-party_roi_preview.png",
	"click-popup-death_roi_preview.png",
	"click-popup-dc_roi_preview.png",
}

// hideROIConfigFiles keeps remembered screen coordinates out of the normal
// folder view. Hidden is only a Windows display attribute: KaTools can still
// read and update these JSON files normally.
func hideROIConfigFiles() error {
	var firstErr error
	for _, path := range roiConfigFiles {
		if err := hideFile(path); err != nil && !errors.Is(err, os.ErrNotExist) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func hideFile(path string) error {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	attrs, err := windows.GetFileAttributes(name)
	if err != nil {
		return err
	}
	if attrs&windows.FILE_ATTRIBUTE_HIDDEN != 0 {
		return nil
	}
	return windows.SetFileAttributes(name, attrs|windows.FILE_ATTRIBUTE_HIDDEN)
}
