package main

import (
	"fmt"
	"sync"
)

type PartyROIConfig struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

var partyROIConfig = struct {
	sync.RWMutex

	config PartyROIConfig
}{
	config: PartyROIConfig{
		X: 770,
		Y: 670,
		W: 350,
		H: 65,
	},
}

func getPartyROIConfig() PartyROIConfig {

	partyROIConfig.RLock()
	defer partyROIConfig.RUnlock()

	return partyROIConfig.config
}

func setPartyROIConfig(
	cfg PartyROIConfig,
) error {

	if cfg.X < 0 {
		return fmt.Errorf("ROI X cannot be negative")
	}

	if cfg.Y < 0 {
		return fmt.Errorf("ROI Y cannot be negative")
	}

	if cfg.W <= 0 {
		return fmt.Errorf("ROI width must be greater than 0")
	}

	if cfg.H <= 0 {
		return fmt.Errorf("ROI height must be greater than 0")
	}

	partyROIConfig.Lock()
	partyROIConfig.config = cfg
	partyROIConfig.Unlock()

	return nil
}
