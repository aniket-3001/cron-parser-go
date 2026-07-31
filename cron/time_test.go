package cron

import (
	"testing"
	"time"

	// The zone database is embedded in tests so results do not depend on what the
	// host machine happens to have installed. The library itself does not import
	// this — that would add roughly 450 KB to every binary that links it — but the
	// wasm bridge does, since js/wasm has no system zoneinfo at all.
	_ "time/tzdata"
)

const isoFormat = "2006-01-02T15:04:05.000-07:00"

func mustLoad(t *testing.T, zone string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(zone)
	if err != nil {
		t.Fatalf("LoadLocation(%q): %v", zone, err)
	}
	return loc
}

// at builds a cronTime from a wall-clock reading in the named zone.
//
// The offset search is seeded from the reading itself because there is no prior
// instance to take one from — the situation luxon is in when parsing an ISO
// string. Operations on an existing cronTime seed from that instance instead;
// see setWall.
func at(t *testing.T, zone string, y int, mo time.Month, d, h, mi, s, ms int) *cronTime {
	t.Helper()
	loc := mustLoad(t, zone)
	pivot := time.Date(y, mo, d, h, mi, s, ms*int(time.Millisecond), time.UTC)
	return newCronTime(fromWallClock(y, mo, d, h, mi, s, ms, zoneOffset(pivot, loc), loc), loc)
}

func (c *cronTime) iso() string { return c.t.Format(isoFormat) }

// TestLuxonDivergenceTable locks in every row of the measured luxon-vs-Go
// comparison in DESIGN.md section 4.1. Each expectation is what luxon produces;
// the "go naive" column records what the obvious Go translation would have given,
// and is present so a future reader can see why the code is written the way it is.
//
// These are the cases where a reasonable port silently diverges, so they are the
// first thing built and the first thing tested.
func TestLuxonDivergenceTable(t *testing.T) {
	tests := []struct {
		name    string
		run     func(t *testing.T) string
		want    string
		goNaive string // what time.Date / AddDate would have produced
	}{
		{
			name:    "add one year to 29 February clamps",
			want:    "2025-02-28T12:00:00.000+00:00",
			goNaive: "2025-03-01 (AddDate overflows)",
			run: func(t *testing.T) string {
				c := at(t, "UTC", 2024, time.February, 29, 12, 0, 0, 0)
				c.addYear()
				return c.iso()
			},
		},
		{
			name:    "subtract one year from 29 February clamps",
			want:    "2023-02-28T12:00:00.000+00:00",
			goNaive: "2023-03-01 (AddDate overflows)",
			run: func(t *testing.T) string {
				c := at(t, "UTC", 2024, time.February, 29, 12, 0, 0, 0)
				c.subtractYear()
				return c.iso()
			},
		},
		{
			name:    "setting month clamps the day",
			want:    "2024-02-29T12:00:00.000+00:00",
			goNaive: "2024-03-02 (time.Date normalises forward)",
			run: func(t *testing.T) string {
				c := at(t, "UTC", 2024, time.January, 31, 12, 0, 0, 0)
				c.setMonth(time.February)
				return c.iso()
			},
		},
		{
			name:    "setting year clamps the day",
			want:    "2025-02-28T12:00:00.000+00:00",
			goNaive: "2025-03-01 (time.Date normalises forward)",
			run: func(t *testing.T) string {
				c := at(t, "UTC", 2024, time.February, 29, 12, 0, 0, 0)
				c.setYear(2025)
				return c.iso()
			},
		},
		{
			name:    "setting day does NOT clamp, it overflows",
			want:    "2024-03-02T12:00:00.000+00:00",
			goNaive: "2024-03-02 (agrees)",
			run: func(t *testing.T) string {
				c := at(t, "UTC", 2024, time.February, 10, 12, 0, 0, 0)
				c.setDayOfMonth(31)
				return c.iso()
			},
		},
		{
			name:    "startOfDay where local midnight does not exist resolves forward",
			want:    "2026-09-06T01:00:00.000-03:00",
			goNaive: "2026-09-05T23:00-04:00 (lands on the PREVIOUS day)",
			run: func(t *testing.T) string {
				c := at(t, "America/Santiago", 2026, time.September, 6, 12, 0, 0, 0)
				c.startOfDay()
				return c.iso()
			},
		},
		{
			name:    "startOfDay in Beirut, midnight transition",
			want:    "2026-03-29T01:00:00.000+03:00",
			goNaive: "2026-03-29T01:00+03:00 (agrees)",
			run: func(t *testing.T) string {
				c := at(t, "Asia/Beirut", 2026, time.March, 29, 12, 0, 0, 0)
				c.startOfDay()
				return c.iso()
			},
		},
		{
			name:    "non-existent local time resolves forward past the gap",
			want:    "2026-03-08T03:30:00.000-04:00",
			goNaive: "2026-03-08T01:30-05:00 (resolves BACKWARD)",
			run: func(t *testing.T) string {
				return at(t, "America/New_York", 2026, time.March, 8, 2, 30, 0, 0).iso()
			},
		},
		{
			name:    "ambiguous local time resolves to the first occurrence",
			want:    "2026-11-01T01:30:00.000-04:00",
			goNaive: "2026-11-01T01:30-04:00 (agrees)",
			run: func(t *testing.T) string {
				return at(t, "America/New_York", 2026, time.November, 1, 1, 30, 0, 0).iso()
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.run(t)
			if got != tc.want {
				t.Errorf("got %s, want %s\n  (naive Go would give: %s)", got, tc.want, tc.goNaive)
			}
		})
	}
}

// TestStartOfHourInFractionalOffsetZones guards the Truncate trap: rounding
// against absolute time gives the wrong answer wherever the offset is not a whole
// number of hours.
func TestStartOfHourInFractionalOffsetZones(t *testing.T) {
	tests := []struct {
		zone string
		want string
	}{
		{"Asia/Kolkata", "2026-07-15T09:00:00.000+05:30"},        // +05:30
		{"Australia/Lord_Howe", "2026-07-15T09:00:00.000+10:30"}, // +10:30
		{"Pacific/Chatham", "2026-07-15T09:00:00.000+12:45"},     // +12:45
	}

	for _, tc := range tests {
		t.Run(tc.zone, func(t *testing.T) {
			c := at(t, tc.zone, 2026, time.July, 15, 9, 37, 42, 123)
			c.startOfHour()
			if got := c.iso(); got != tc.want {
				t.Errorf("startOfHour() = %s, want %s", got, tc.want)
			}
		})
	}
}

// TestHourSteppingAcrossTransitions records how stepping an hour behaves across
// gaps of differing widths. The Troll case is the one the original mishandles.
func TestHourSteppingAcrossTransitions(t *testing.T) {
	tests := []struct {
		name     string
		zone     string
		y        int
		mo       time.Month
		d, h     int
		wantHour int
		wantDiff int
		gapNote  string
	}{
		{
			name: "New York, one hour gap", zone: "America/New_York",
			y: 2026, mo: time.March, d: 8, h: 1,
			wantHour: 3, wantDiff: 2, gapNote: "1h gap: diff is 2, the original's check fires",
		},
		{
			name: "Antarctica/Troll, two hour gap", zone: "Antarctica/Troll",
			y: 2026, mo: time.March, d: 29, h: 0,
			wantHour: 3, wantDiff: 3, gapNote: "2h gap: diff is 3, the original's check does NOT fire",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := at(t, tc.zone, tc.y, tc.mo, tc.d, tc.h, 0, 0, 0)
			before := c.Hour()
			c.addHour()
			after := c.Hour()

			if after != tc.wantHour {
				t.Errorf("hour after addHour() = %d, want %d (%s)", after, tc.wantHour, c.iso())
			}
			if diff := after - before; diff != tc.wantDiff {
				t.Errorf("hour delta = %d, want %d — %s", diff, tc.wantDiff, tc.gapNote)
			}
		})
	}
}

// TestApplyDateOperationReproducesDSTBug pins the original's behaviour: the
// spring-forward branch keys off a wall-clock delta of exactly 2, so it fires for
// a one-hour gap and misses a two-hour one. Reproducing this is required for the
// reference suite to pass; the corrected behaviour is selected elsewhere.
func TestApplyDateOperationReproducesDSTBug(t *testing.T) {
	t.Run("one hour gap records dstStart", func(t *testing.T) {
		c := at(t, "America/New_York", 2026, time.March, 8, 1, 0, 0, 0)
		c.applyDateOperation(opAdd, unitHour, 1)
		if c.dstStart != 2 {
			t.Errorf("dstStart = %d, want 2 (the skipped hour)", c.dstStart)
		}
	})

	t.Run("two hour gap misses it - the bug", func(t *testing.T) {
		c := at(t, "Antarctica/Troll", 2026, time.March, 29, 0, 0, 0, 0)
		c.applyDateOperation(opAdd, unitHour, 1)
		if c.dstStart != -1 {
			t.Errorf("dstStart = %d, want -1; reproducing the original's bug requires "+
				"that a two-hour gap go unrecorded", c.dstStart)
		}
	})

	t.Run("hoursLength of 24 skips bookkeeping", func(t *testing.T) {
		c := at(t, "America/New_York", 2026, time.March, 8, 1, 0, 0, 0)
		c.applyDateOperation(opAdd, unitHour, 24)
		if c.dstStart != -1 {
			t.Errorf("dstStart = %d, want -1 when every hour matches", c.dstStart)
		}
	})
}

func TestArithmeticLandsOnUnitBoundaries(t *testing.T) {
	tests := []struct {
		name string
		op   func(c *cronTime)
		want string
	}{
		{"addMonth starts the month", (*cronTime).addMonth, "2026-08-01T00:00:00.000+00:00"},
		{"addDay starts the day", (*cronTime).addDay, "2026-07-16T00:00:00.000+00:00"},
		{"addHour starts the hour", (*cronTime).addHour, "2026-07-15T10:00:00.000+00:00"},
		{"addMinute starts the minute", (*cronTime).addMinute, "2026-07-15T09:38:00.000+00:00"},
		{"addSecond keeps milliseconds", (*cronTime).addSecond, "2026-07-15T09:37:43.123+00:00"},
		{"subtractMonth ends the previous month", (*cronTime).subtractMonth, "2026-06-30T23:59:59.000+00:00"},
		{"subtractDay ends the previous day", (*cronTime).subtractDay, "2026-07-14T23:59:59.000+00:00"},
		{"subtractHour ends the previous hour", (*cronTime).subtractHour, "2026-07-15T08:59:59.000+00:00"},
		{"subtractMinute ends the previous minute", (*cronTime).subtractMinute, "2026-07-15T09:36:59.000+00:00"},
		{"subtractSecond keeps milliseconds", (*cronTime).subtractSecond, "2026-07-15T09:37:41.123+00:00"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := at(t, "UTC", 2026, time.July, 15, 9, 37, 42, 123)
			tc.op(c)
			if got := c.iso(); got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestSetWeekdayMovesWithinISOWeek(t *testing.T) {
	// 2026-07-31 is a Friday; its ISO week runs Mon 07-27 to Sun 08-02.
	tests := []struct {
		target int
		want   string
	}{
		{1, "2026-07-27T12:00:00.000+00:00"}, // Monday
		{5, "2026-07-31T12:00:00.000+00:00"}, // Friday, unchanged
		{7, "2026-08-02T12:00:00.000+00:00"}, // Sunday
		{0, "2026-07-26T12:00:00.000+00:00"}, // extends below Monday
	}

	for _, tc := range tests {
		c := at(t, "UTC", 2026, time.July, 31, 12, 0, 0, 0)
		c.setWeekday(tc.target)
		if got := c.iso(); got != tc.want {
			t.Errorf("setWeekday(%d) = %s, want %s", tc.target, got, tc.want)
		}
	}
}

func TestLastDayHelpers(t *testing.T) {
	tests := []struct {
		name           string
		y              int
		mo             time.Month
		d              int
		lastDay        bool
		lastWeekdayWin bool
	}{
		{"29 Feb in a leap year is the last day", 2024, time.February, 29, true, true},
		{"28 Feb in a leap year is not", 2024, time.February, 28, false, true},
		{"28 Feb in a common year is the last day", 2026, time.February, 28, true, true},
		{"31 July is the last day", 2026, time.July, 31, true, true},
		{"24 July is inside the final week", 2026, time.July, 25, false, true},
		{"15 July is not", 2026, time.July, 15, false, false},
		{"31 Dec is the last day", 2026, time.December, 31, true, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := at(t, "UTC", tc.y, tc.mo, tc.d, 12, 0, 0, 0)
			if got := c.isLastDayOfMonth(); got != tc.lastDay {
				t.Errorf("isLastDayOfMonth() = %v, want %v", got, tc.lastDay)
			}
			if got := c.isLastWeekdayOfMonth(); got != tc.lastWeekdayWin {
				t.Errorf("isLastWeekdayOfMonth() = %v, want %v", got, tc.lastWeekdayWin)
			}
		})
	}
}

func TestFloorDiv(t *testing.T) {
	tests := []struct{ a, b, want int }{
		{13, 12, 1}, {12, 12, 1}, {11, 12, 0},
		{-1, 12, -1}, {-12, 12, -1}, {-13, 12, -2},
	}
	for _, tc := range tests {
		if got := floorDiv(tc.a, tc.b); got != tc.want {
			t.Errorf("floorDiv(%d, %d) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
