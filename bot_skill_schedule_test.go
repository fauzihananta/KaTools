package main

import (
	"testing"
	"time"
)

func TestMakeSkillSchedulesIncludesOnlyEnabledSlotsAndStaggersClick(t *testing.T) {
	skills := []SkillConfig{
		{Name: "1", Enabled: true, Delay: time.Second},
		{Name: "2", Enabled: false, Delay: time.Second},
		{Name: "3", Enabled: true, Delay: 2 * time.Second},
		{Name: "4", Enabled: true}, // invalid intervals remain ignored
	}

	schedules := makeSkillSchedules(skills, true)
	if len(schedules) != 2 {
		t.Fatalf("expected 2 enabled schedules, got %d", len(schedules))
	}
	if schedules[0].skill.Name != "1" || schedules[1].skill.Name != "3" {
		t.Fatalf("unexpected scheduled slots: %q, %q", schedules[0].skill.Name, schedules[1].skill.Name)
	}
	if gap := schedules[1].nextCast.Sub(schedules[0].nextCast); gap != clickInputSpacing {
		t.Fatalf("expected click stagger %s, got %s", clickInputSpacing, gap)
	}
}
