package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sync"

	"KaTools/tools/ocrworker"
)

const chatTextROIFile = "chat_text_roi.json"

type ChatTextROI struct {
	ROI          ocrworker.PartyROIConfig `json:"roi"`
	ClientWidth  int                      `json:"clientWidth"`
	ClientHeight int                      `json:"clientHeight"`
}

var chatTextROIState struct {
	sync.RWMutex
	value ChatTextROI
}

func LoadChatTextROI() ChatTextROI {
	chatTextROIState.RLock()
	value := chatTextROIState.value
	chatTextROIState.RUnlock()
	if value.ROI.Width > 0 && value.ROI.Height > 0 {
		return value
	}
	data, err := os.ReadFile(chatTextROIFile)
	if err != nil {
		return ChatTextROI{}
	}
	if json.Unmarshal(data, &value) != nil || value.ROI.Width <= 0 || value.ROI.Height <= 0 {
		return ChatTextROI{}
	}
	chatTextROIState.Lock()
	chatTextROIState.value = value
	chatTextROIState.Unlock()
	return value
}

func SavePickedChatTextROI(value ChatTextROI) error { return saveChatTextROI(value, true) }
func ResetPickedChatTextROI() error                 { value := LoadChatTextROI(); return saveChatTextROI(value, false) }
func saveChatTextROI(value ChatTextROI, selected bool) error {
	if value.ROI.Width <= 0 || value.ROI.Height <= 0 {
		return fmt.Errorf("invalid chat text ROI size")
	}
	value.ROI.Selected = selected
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(chatTextROIFile, data, 0644); err != nil {
		return err
	}
	chatTextROIState.Lock()
	chatTextROIState.value = value
	chatTextROIState.Unlock()
	return nil
}

type ChatTextClickSettings struct {
	mu      sync.RWMutex
	enabled bool
	keyword string
	yOffset int
}

func (s *ChatTextClickSettings) Update(enabled bool, keyword string, yOffset int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.enabled = enabled
	s.keyword = keyword
	s.yOffset = yOffset
	s.mu.Unlock()
}
func (s *ChatTextClickSettings) Snapshot() (bool, string, int) {
	if s == nil {
		return false, "", 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.enabled, s.keyword, s.yOffset
}

// chatTextMatchClientPoint converts a match inside the WGC crop back into the
// saved client-coordinate reference used by virtual clicks. The crop's local
// match coordinates are frame pixels, but roi.X/Y are already client pixels;
// only the local offset may be divided by the WGC scale. Capture calibration
// does not appear here because it shifts the crop to the correct content while
// leaving positions inside that crop unchanged.
func chatTextMatchClientPoint(roi ocrworker.PartyROIConfig, match *ocrworker.TextMatch, scaleX, scaleY float64, yOffset, referenceWidth, referenceHeight int) (x, y int, anchorText string, ok bool) {
	if !roi.Selected || match == nil || scaleX <= 0 || scaleY <= 0 || referenceWidth <= 0 || referenceHeight <= 0 {
		return 0, 0, "", false
	}
	anchorX := match.X + match.Width/2
	anchorText = "bubble"
	if match.FollowingLine != nil {
		anchorX = match.FollowingLine.X + match.FollowingLine.Width/2
		anchorText = match.FollowingLine.Text
	}
	anchorY := match.Y + match.Height/2
	x = roi.X + int(math.Round(float64(anchorX)/scaleX))
	y = roi.Y + int(math.Round(float64(anchorY)/scaleY)) + yOffset
	if x < 0 || y < 0 || x >= referenceWidth || y >= referenceHeight {
		return 0, 0, anchorText, false
	}
	return x, y, anchorText, true
}

// chatTextFollowingLineClientPoint is a safe fallback when the configured
// below-text offset would land on the action bar. Tesseract's FollowingLine is
// already constrained to the closest centered line underneath the chat phrase,
// which is normally the character name.
func chatTextFollowingLineClientPoint(roi ocrworker.PartyROIConfig, match *ocrworker.TextMatch, scaleX, scaleY float64, referenceWidth, referenceHeight int) (x, y int, ok bool) {
	if !roi.Selected || match == nil || match.FollowingLine == nil || scaleX <= 0 || scaleY <= 0 || referenceWidth <= 0 || referenceHeight <= 0 {
		return 0, 0, false
	}
	line := match.FollowingLine
	x = roi.X + int(math.Round(float64(line.X+line.Width/2)/scaleX))
	y = roi.Y + int(math.Round(float64(line.Y+line.Height/2)/scaleY))
	if x < 0 || y < 0 || x >= referenceWidth || y >= referenceHeight {
		return 0, 0, false
	}
	return x, y, true
}

// chatTextCombatPointCollision prevents a bad OCR/DPI conversion from clicking
// a configured skill, potion, Buff, or Emergency point. Popup action points are
// intentionally excluded because they are not combat slots.
func chatTextCombatPointCollision(x, y, referenceWidth, referenceHeight int, areas map[string]ClickSkillROI) (string, bool) {
	if referenceWidth <= 0 || referenceHeight <= 0 {
		return "", true
	}
	marginX := max(12, int(math.Round(32*float64(referenceWidth)/1920)))
	marginY := max(12, int(math.Round(32*float64(referenceHeight)/1440)))
	for slot, area := range areas {
		if !area.Selected || slot == "AutoAccept" || slot == "AutoResu" || slot == "DCOk" || slot == "ChatParty" {
			continue
		}
		areaRefW, areaRefH := area.ClientWidth, area.ClientHeight
		if areaRefW <= 0 || areaRefH <= 0 {
			areaRefW, areaRefH = referenceWidth, referenceHeight
		}
		areaX := int(math.Round(float64(area.X+area.Width/2) * float64(referenceWidth) / float64(areaRefW)))
		areaY := int(math.Round(float64(area.Y+area.Height/2) * float64(referenceHeight) / float64(areaRefH)))
		if absInt(x-areaX) <= marginX && absInt(y-areaY) <= marginY {
			return slot, true
		}
	}
	return "", false
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
