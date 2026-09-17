package main

import "testing"

func TestOCRLogBackupPath(t *testing.T) {
	if got, want := ocrLogBackupPath("katools_ocr.log"), "katools_ocr.previous.log"; got != want {
		t.Fatalf("backup path = %q, want %q", got, want)
	}
}
