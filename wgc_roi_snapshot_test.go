package main

import "testing"

func TestWGCSnapshotLabels(t *testing.T) {
	if labels := wgcSnapshotLabels("status"); len(labels) != 1 || labels[0] != "HP Area" {
		t.Fatalf("status labels = %v", labels)
	}
	if labels := wgcSnapshotLabels("death"); len(labels) != 2 || labels[0] != "Death Area" || labels[1] != "Resu Area" {
		t.Fatalf("death labels = %v", labels)
	}
	if labels := wgcSnapshotLabels("unknown"); labels != nil {
		t.Fatalf("unknown labels = %v, want nil", labels)
	}
}
