package main

// captureOffset maps picker coordinates (the game client area) to the WGC
// frame. Windows 10/11 and different GPU drivers can disagree about both the
// non-client border and title-bar height, so calibration must probe X and Y.
type captureOffset struct {
	X int
	Y int
}

// statusCaptureOffsetCandidates starts close to the selected pixels, then
// expands vertically. Horizontal and vertical values are combined because a
// DPI/client-frame mismatch is commonly present on both axes at once.
func statusCaptureOffsetCandidates() []captureOffset {
	horizontal := []int{0, 4, -4, 8, -8, 12, -12}
	vertical := []int{0, 4, -4, 8, -8, 12, -12, 16, -16, 24, -24, 32, -32, 40, -40}
	offsets := make([]captureOffset, 0, len(horizontal)*len(vertical))
	for _, y := range vertical {
		for _, x := range horizontal {
			offsets = append(offsets, captureOffset{X: x, Y: y})
		}
	}
	return offsets
}
