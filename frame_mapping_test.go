package main

import "testing"

func TestFrameMappingUsesClientCoordinatesForClientCapture(t *testing.T) {
	mapping, err := frameMappingForWindow(1440, 990, 1456, 1028, 8, 30, 1440, 990)
	if err != nil {
		t.Fatal(err)
	}
	if !mapping.UsesClientFrame || mapping.ScaleX != 1 || mapping.ScaleY != 1 || mapping.OriginX != 0 || mapping.OriginY != 0 {
		t.Fatalf("unexpected client mapping: %+v", mapping)
	}
}

func TestFrameMappingAddsClientOriginForWindowCapture(t *testing.T) {
	mapping, err := frameMappingForWindow(1456, 1028, 1456, 1028, 8, 30, 1440, 990)
	if err != nil {
		t.Fatal(err)
	}
	if mapping.UsesClientFrame || mapping.ScaleX != 1 || mapping.ScaleY != 1 || mapping.OriginX != 8 || mapping.OriginY != 30 {
		t.Fatalf("unexpected window mapping: %+v", mapping)
	}
}
