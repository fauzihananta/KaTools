package main

import (
	"errors"
	"testing"
)

func TestFocusInfoTargetWindowLost(t *testing.T) {
	if !focusInfoTargetWindowLost(errors.New("GetWindowThreadProcessId failed")) {
		t.Fatal("expected invalid target-window error to stop the runtime")
	}
	if focusInfoTargetWindowLost(errors.New("GetGUIThreadInfo failed")) {
		t.Fatal("unrelated focus errors must not stop the runtime")
	}
	if focusInfoTargetWindowLost(nil) {
		t.Fatal("nil error must not stop the runtime")
	}
}
