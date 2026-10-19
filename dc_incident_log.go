package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const dcLogsDirectoryName = "DC Logs"

func dcLogsDirectory() string {
	return filepath.Join(filepath.Dir(savedConfigPath()), dcLogsDirectoryName)
}

// recordDCIncident writes one small, standalone record per confirmed DC. It is
// deliberately separate from katools_ocr.log so a player can find the exact
// disconnect time without searching through the runtime log.
func recordDCIncident(detectedAt time.Time) (string, error) {
	return writeDCIncidentLogAt(dcLogsDirectory(), detectedAt)
}

func writeDCIncidentLogAt(directory string, detectedAt time.Time) (string, error) {
	if err := os.MkdirAll(directory, 0755); err != nil {
		return "", fmt.Errorf("create DC Logs directory: %w", err)
	}

	baseName := detectedAt.Format("2006-01-02_15-04-05.000")
	contents := fmt.Sprintf("DC detected: %s\n", detectedAt.Format("2006-01-02 15:04:05.000"))
	for suffix := 0; suffix < 100; suffix++ {
		name := baseName
		if suffix > 0 {
			name = fmt.Sprintf("%s_%d", baseName, suffix)
		}
		path := filepath.Join(directory, name+".txt")
		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0644)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("create DC incident log: %w", err)
		}
		_, writeErr := file.WriteString(contents)
		closeErr := file.Close()
		if writeErr != nil {
			return "", fmt.Errorf("write DC incident log: %w", writeErr)
		}
		if closeErr != nil {
			return "", fmt.Errorf("close DC incident log: %w", closeErr)
		}
		return path, nil
	}
	return "", fmt.Errorf("create DC incident log: too many logs at the same timestamp")
}
