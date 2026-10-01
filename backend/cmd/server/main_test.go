package main

import (
	"testing"
	"time"
)

func TestParseCheckTime(t *testing.T) {
	if hour, minute := parseCheckTime("03:17"); hour != 3 || minute != 17 {
		t.Fatalf("unexpected parsed time: %d:%02d", hour, minute)
	}
	if hour, minute := parseCheckTime("invalid"); hour != 3 || minute != 0 {
		t.Fatalf("invalid time should use 03:00, got %d:%02d", hour, minute)
	}
}

func TestNextDailyCheckUsesConfiguredTimezoneAndRollsOver(t *testing.T) {
	location := time.FixedZone("test", 8*60*60)
	before := time.Date(2026, 10, 2, 2, 59, 0, 0, location)
	next := nextDailyCheck(before, "03:00", location)
	expected := time.Date(2026, 10, 2, 3, 0, 0, 0, location)
	if !next.Equal(expected) {
		t.Fatalf("next=%s expected=%s", next, expected)
	}
	after := time.Date(2026, 10, 2, 3, 1, 0, 0, location)
	next = nextDailyCheck(after, "03:00", location)
	expected = time.Date(2026, 10, 3, 3, 0, 0, 0, location)
	if !next.Equal(expected) {
		t.Fatalf("rollover next=%s expected=%s", next, expected)
	}
}
