package main

import "testing"

func TestDetectPartyAutomaticState(t *testing.T) {
	tests := []struct {
		text string
		want partyAutomaticState
	}{
		{"Automatic", partyAutomaticEnabled},
		{"party is NORMAL", partyAutomaticNormal},
		{"Master Loot", partyAutomaticUnknown},
		{"abnormal", partyAutomaticUnknown},
		{"Leave", partyAutomaticUnknown},
		{"", partyAutomaticUnknown},
	}
	for _, test := range tests {
		if got := detectPartyAutomaticState(test.text); got != test.want {
			t.Errorf("detectPartyAutomaticState(%q) = %d, want %d", test.text, got, test.want)
		}
	}
}
