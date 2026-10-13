package main

import (
	"strings"
	"unicode"
)

type partyAutomaticState uint8

const (
	partyAutomaticUnknown partyAutomaticState = iota
	partyAutomaticNormal
	partyAutomaticEnabled
)

// detectPartyAutomaticState is intentionally conservative. Only the exact
// Normal state may advance the party loot mode; Master Loot or an unclear OCR
// result must never click the compact panel control.
func detectPartyAutomaticState(text string) partyAutomaticState {
	for _, word := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r)
	}) {
		switch word {
		case "automatic":
			return partyAutomaticEnabled
		case "normal":
			return partyAutomaticNormal
		}
	}
	return partyAutomaticUnknown
}
