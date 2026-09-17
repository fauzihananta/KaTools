package main

import "testing"

func TestStatusCaptureOffsetCandidatesProbeBothAxes(t *testing.T) {
	offsets := statusCaptureOffsetCandidates()
	if len(offsets) == 0 || offsets[0] != (captureOffset{}) {
		t.Fatal("calibration must try the unshifted status ROI first")
	}

	foundDiagonal := false
	for _, offset := range offsets {
		if offset.X == 8 && offset.Y == 24 {
			foundDiagonal = true
			break
		}
	}
	if !foundDiagonal {
		t.Fatal("calibration does not include combined horizontal/vertical offsets")
	}
}
