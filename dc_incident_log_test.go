package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteDCIncidentLogAtUsesTimestampFileNameAndContents(t *testing.T) {
	detectedAt := time.Date(2026, time.October, 20, 15, 4, 5, 123000000, time.Local)
	path, err := writeDCIncidentLogAt(t.TempDir(), detectedAt)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := filepath.Base(path), "2026-10-20_15-04-05.123.txt"; got != want {
		t.Fatalf("log name = %q, want %q", got, want)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "DC detected: 2026-10-20 15:04:05.123\n"; got != want {
		t.Fatalf("log contents = %q, want %q", got, want)
	}
	if !strings.HasSuffix(path, ".txt") {
		t.Fatalf("log path has no .txt suffix: %q", path)
	}
}
