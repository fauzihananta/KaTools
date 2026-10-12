package main

import (
	"testing"

	"KaTools/tools/ocrworker"
)

func TestChatTextMatchClientPointUsesOneCoordinateSpace(t *testing.T) {
	roi := ocrworker.PartyROIConfig{X: 521, Y: 237, Width: 1022, Height: 816, Selected: true}
	match := &ocrworker.TextMatch{
		X: 80, Y: 120, Width: 80, Height: 24,
		FollowingLine: &ocrworker.TextMatch{Text: "CharacterName", X: 120, Y: 160, Width: 120, Height: 20},
	}
	x, y, anchor, ok := chatTextMatchClientPoint(roi, match, 0.8, 0.6, 100, 1920, 1440)
	if !ok {
		t.Fatal("valid chat text point was rejected")
	}
	// X follows the centered character-name line. Y intentionally remains the
	// configured offset below the matched chat phrase.
	if x != 746 || y != 557 {
		t.Fatalf("client point = %d,%d, want 746,557", x, y)
	}
	if anchor != "CharacterName" {
		t.Fatalf("anchor = %q, want CharacterName", anchor)
	}
	fallbackX, fallbackY, fallbackOK := chatTextFollowingLineClientPoint(roi, match, 0.8, 0.6, 1920, 1440)
	if !fallbackOK || fallbackX != 746 || fallbackY != 520 {
		t.Fatalf("following-line fallback = %d,%d,%t, want 746,520,true", fallbackX, fallbackY, fallbackOK)
	}
}

func TestChatTextCombatPointCollisionBlocksSkillSlot(t *testing.T) {
	areas := map[string]ClickSkillROI{
		"1":         {X: 1220, Y: 1196, Width: 1, Height: 1, Selected: true, ClientWidth: 1920, ClientHeight: 1440},
		"ChatParty": {X: 1220, Y: 1196, Width: 1, Height: 1, Selected: true, ClientWidth: 1920, ClientHeight: 1440},
	}
	if slot, collision := chatTextCombatPointCollision(1210, 1185, 1920, 1440, areas); !collision || slot != "1" {
		t.Fatalf("skill collision = %q,%t, want slot 1", slot, collision)
	}
	if slot, collision := chatTextCombatPointCollision(900, 700, 1920, 1440, areas); collision {
		t.Fatalf("safe character point collided with %q", slot)
	}
}

func TestChatTextNameFallbackAvoidsOffsetPointOnSkillSlot(t *testing.T) {
	areas := map[string]ClickSkillROI{
		"F1": {X: 746, Y: 557, Width: 1, Height: 1, Selected: true, ClientWidth: 1920, ClientHeight: 1440},
	}
	if slot, collision := chatTextCombatPointCollision(746, 557, 1920, 1440, areas); !collision || slot != "F1" {
		t.Fatal("configured below-text point was not recognized as unsafe")
	}
	if slot, collision := chatTextCombatPointCollision(746, 520, 1920, 1440, areas); collision {
		t.Fatalf("character-name fallback unexpectedly collided with %q", slot)
	}
}
