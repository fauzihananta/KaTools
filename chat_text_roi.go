package main

import (
	"encoding/json"
	"fmt"
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
