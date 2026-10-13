package main

import "testing"

func TestAutoAcceptControllerKeepsOnePartyInviteAction(t *testing.T) {
	controller := NewAutoAcceptController(true, partyInviteReject)
	if !controller.IsEnabled() {
		t.Fatal("controller should preserve enabled state")
	}
	if got := controller.Action(); got != partyInviteReject {
		t.Fatalf("Action() = %q, want %q", got, partyInviteReject)
	}

	// Missing or malformed values from an older config must remain safe and
	// deterministic: they fall back to the normal Accept action.
	controller.Set(true, "both")
	if got := controller.Action(); got != partyInviteAccept {
		t.Fatalf("Action() after invalid action = %q, want %q", got, partyInviteAccept)
	}
}
