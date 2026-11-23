package bloomrules

import (
	"math"
	"testing"
	"time"
)

func TestCalendarAcrossDaylightSaving(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	sunday := time.Date(2026, 3, 9, 2, 0, 0, 0, time.UTC)
	periods := Calendar(sunday, location)
	if periods.Day.Start.Day() != 8 || periods.Day.End.Sub(periods.Day.Start) != 23*time.Hour {
		t.Fatalf("unexpected site day: %+v", periods.Day)
	}
	if periods.Week.Start.Weekday() != time.Monday || periods.Week.Start.Day() != 2 || periods.Week.End.Sub(periods.Week.Start) != 167*time.Hour {
		t.Fatalf("unexpected site week: %+v", periods.Week)
	}
	if !periods.PriorWeek.End.Equal(periods.Week.Start) || !periods.CompletedDays.End.Equal(periods.Day.Start) {
		t.Fatalf("non-contiguous period boundaries: %+v", periods)
	}
	monday := Calendar(periods.Week.End, location)
	if !monday.PriorWeek.Start.Equal(periods.Week.Start) || !monday.CompletedDays.Start.Equal(monday.CompletedDays.End) {
		t.Fatalf("unexpected Monday rollover: %+v", monday)
	}
}

func TestCalendarAtSkippedMidnight(t *testing.T) {
	location, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatal(err)
	}
	sunday := time.Date(2018, 11, 4, 15, 0, 0, 0, location)
	periods := Calendar(sunday, location)
	if periods.Day.Start.Day() != 4 || periods.Day.Start.Hour() != 1 || periods.Day.End.Hour() != 0 {
		t.Fatalf("unexpected shifted midnight: %+v", periods.Day)
	}
	if periods.Week.Start.Weekday() != time.Monday || periods.Week.Start.Day() != 29 {
		t.Fatalf("unexpected week around shifted midnight: %+v", periods.Week)
	}
}

func TestAggregateCountsOnlyProductiveLocalDays(t *testing.T) {
	location := time.FixedZone("site", 8*60*60)
	periods := Calendar(time.Date(2026, 11, 26, 12, 0, 0, 0, location), location)
	window := periods.CompletedDays
	stats := Aggregate(window, []Usage{
		{At: window.Start.Add(-time.Nanosecond), OutputTokens: 8000},
		{At: window.Start.UTC(), OutputTokens: 12, Images: -4},
		{At: window.Start.Add(23 * time.Hour).UTC(), Images: 2},
		{At: window.Start.Add(24 * time.Hour).UTC(), OutputTokens: 1, Images: 1},
		{At: window.End.Add(-time.Nanosecond)},
		{At: window.End, Images: 5},
	})
	want := Stats{Requests: 4, ProductiveRequests: 3, OutputTokens: 13, Images: 3, ActiveDays: 2}
	if stats != want {
		t.Fatalf("Aggregate = %+v, want %+v", stats, want)
	}
	overflow := Aggregate(window, []Usage{
		{At: window.Start, OutputTokens: math.MaxInt64, Images: math.MaxInt64},
		{At: window.Start, OutputTokens: 1, Images: 1},
	})
	if overflow.OutputTokens != math.MaxInt64 || overflow.Images != math.MaxInt64 {
		t.Fatalf("usage totals overflowed: %+v", overflow)
	}
}
