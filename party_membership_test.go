package main

import "testing"

func TestHasPartyPanelBars(t *testing.T) {
	reader := newPartyTestFrame(480, 360)
	drawPartyTestBar(reader, 250, 430, 70, 76, Pixel{R: 220, G: 25, B: 25, A: 255})
	drawPartyTestBar(reader, 250, 430, 88, 94, Pixel{R: 25, G: 65, B: 220, A: 255})
	drawPartyTestBar(reader, 250, 430, 125, 131, Pixel{R: 220, G: 25, B: 25, A: 255})
	drawPartyTestBar(reader, 250, 430, 143, 149, Pixel{R: 25, G: 65, B: 220, A: 255})

	if !hasPartyPanelBars(reader) {
		t.Fatal("party HP/TP bars were not detected")
	}
}

func TestPartyMembershipMonitorResumesAfterThreeMissingChecks(t *testing.T) {
	monitor := &PartyMembershipMonitor{visible: true}
	reader := newPartyTestFrame(480, 360)
	for i := 0; i < 2; i++ {
		visible, changed := monitor.Update(reader)
		if !visible || changed {
			t.Fatalf("panel should remain visible on miss %d", i+1)
		}
	}
	visible, changed := monitor.Update(reader)
	if visible || !changed {
		t.Fatal("panel should clear after third missing check")
	}
}

func newPartyTestFrame(width, height int) *FrameReader {
	return &FrameReader{
		pixels: make([]byte, width*height*4),
		width:  width,
		height: height,
		stride: width * 4,
	}
}

func drawPartyTestBar(reader *FrameReader, startX, endX, startY, endY int, pixel Pixel) {
	for y := startY; y <= endY; y++ {
		for x := startX; x <= endX; x++ {
			offset := y*reader.stride + x*4
			reader.pixels[offset] = pixel.B
			reader.pixels[offset+1] = pixel.G
			reader.pixels[offset+2] = pixel.R
			reader.pixels[offset+3] = pixel.A
		}
	}
}
