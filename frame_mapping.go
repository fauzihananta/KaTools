package main

import (
	"fmt"
	"math"
)

// frameCoordinateMapping converts a saved picker coordinate (relative to the
// game client area) into a WGC-frame coordinate. WGC differs by Windows/GPU:
// it can expose either the client content or the full top-level window.
type frameCoordinateMapping struct {
	ScaleX          float64
	ScaleY          float64
	OriginX         int
	OriginY         int
	UsesClientFrame bool
}

func frameMappingForWindow(
	frameWidth, frameHeight int,
	windowWidth, windowHeight int,
	clientOffsetX, clientOffsetY int,
	clientWidth, clientHeight int,
) (frameCoordinateMapping, error) {
	if frameWidth <= 0 || frameHeight <= 0 ||
		windowWidth <= 0 || windowHeight <= 0 ||
		clientWidth <= 0 || clientHeight <= 0 {
		return frameCoordinateMapping{}, fmt.Errorf("invalid frame/window/client dimensions")
	}

	// Compare scale consistency, rather than raw dimensions: a DPI path can
	// make WGC output a proportional frame at a different resolution.
	clientScaleError := math.Abs(float64(frameWidth)/float64(clientWidth) - float64(frameHeight)/float64(clientHeight))
	windowScaleError := math.Abs(float64(frameWidth)/float64(windowWidth) - float64(frameHeight)/float64(windowHeight))

	if clientScaleError <= windowScaleError {
		return frameCoordinateMapping{
			ScaleX:          float64(frameWidth) / float64(clientWidth),
			ScaleY:          float64(frameHeight) / float64(clientHeight),
			UsesClientFrame: true,
		}, nil
	}

	scaleX := float64(frameWidth) / float64(windowWidth)
	scaleY := float64(frameHeight) / float64(windowHeight)
	return frameCoordinateMapping{
		ScaleX:  scaleX,
		ScaleY:  scaleY,
		OriginX: int(math.Round(float64(clientOffsetX) * scaleX)),
		OriginY: int(math.Round(float64(clientOffsetY) * scaleY)),
	}, nil
}
