package main

import (
	"testing"
	"time"
)

func TestCaptureFPSForConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  WebBotConfig
		want int
	}{
		{name: "key only", cfg: WebBotConfig{TargetEnabled: true}, want: 1},
		{name: "party scanner", cfg: WebBotConfig{AutoAcceptEnabled: true}, want: 10},
		{name: "status scanner", cfg: WebBotConfig{AutoPotHPEnabled: true}, want: 10},
		{name: "death scanner", cfg: WebBotConfig{AutoPauseDeathEnabled: true}, want: 10},
		{name: "target name scanner", cfg: WebBotConfig{TargetEnabled: true, TargetUntilDeadCharacterName: "Monster"}, want: 10},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := captureFPSForConfig(test.cfg); got != test.want {
				t.Fatalf("captureFPSForConfig() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestCapturePollingInterval(t *testing.T) {
	if got := capturePollingInterval(1); got != 50*time.Millisecond {
		t.Fatalf("1 FPS poll interval = %s, want 50ms", got)
	}
	if got := capturePollingInterval(10); got != 10*time.Millisecond {
		t.Fatalf("10 FPS poll interval = %s, want 10ms", got)
	}
}
