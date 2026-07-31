// Package cron parses cron expressions and calculates the instants at which they
// fire. It is a port of harrisiirak/cron-parser v5.6.2 from TypeScript to Go.
//
// The library is a calculator, not a scheduler: it answers "when does this
// expression next fire?" and never runs anything on a timer.
package cron

import "time"

// timeUnit identifies the unit a date operation acts on.
type timeUnit int

const (
	unitSecond timeUnit = iota
	unitMinute
	unitHour
	unitDay
	unitMonth
	unitYear
)

// String returns the name the original TypeScript enum uses. The test bridge
// compares against these strings, so they are part of the observable contract.
func (u timeUnit) String() string {
	switch u {
	case unitSecond:
		return "Second"
	case unitMinute:
		return "Minute"
	case unitHour:
		return "Hour"
	case unitDay:
		return "Day"
	case unitMonth:
		return "Month"
	case unitYear:
		return "Year"
	}
	return "Unknown"
}

// dateMathOp selects the direction of a date operation.
type dateMathOp int

const (
	opAdd dateMathOp = iota
	opSubtract
)

func (o dateMathOp) String() string {
	if o == opSubtract {
		return "Subtract"
	}
	return "Add"
}

// daysInMonthTable mirrors the original's DAYS_IN_MONTH constant, which stores 29
// for February regardless of the year. Its leap-permissiveness is observable —
// CronFieldCollection's validation reads it directly to decide whether an explicit
// day of month can ever occur — so it is reproduced rather than corrected. Code
// that needs a month's true length must call daysIn.
var daysInMonthTable = [12]int{31, 29, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}

func isLeapYear(y int) bool {
	return (y%4 == 0 && y%100 != 0) || y%400 == 0
}

// daysIn reports the true number of days in a month.
func daysIn(y int, mo time.Month) int {
	if mo == time.February && !isLeapYear(y) {
		return 28
	}
	return daysInMonthTable[int(mo)-1]
}

// floorDiv divides rounding toward negative infinity, unlike Go's / which
// truncates toward zero. Month arithmetic needs floor semantics to cross year
// boundaries correctly when subtracting.
func floorDiv(a, b int) int {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

// zoneOffset returns loc's offset from UTC, in seconds, at the instant t.
func zoneOffset(t time.Time, loc *time.Location) int {
	_, off := t.In(loc).Zone()
	return off
}

// fromWallClock builds an instant from a wall-clock reading in loc.
//
// This is a faithful port of luxon's fixOffset and is the only place in the
// package that converts wall-clock components into an instant. It exists because
// Go and luxon resolve impossible and ambiguous local times in opposite
// directions:
//
//	non-existent 2026-03-08T02:30 America/New_York
//	    luxon     -> 2026-03-08T03:30-04:00   (forward, past the gap)
//	    time.Date -> 2026-03-08T01:30-05:00   (backward, before the gap)
//
//	midnight on 2026-09-06 in America/Santiago, which does not exist
//	    luxon     -> 2026-09-06T01:00-03:00
//	    time.Date -> 2026-09-05T23:00-04:00   (the previous calendar day)
//
// The Santiago case is why time.Date cannot be used directly: it silently moves
// the calendar date, which would corrupt day-of-month matching.
//
// provisional is the offset, in seconds, that seeds the search. luxon always
// passes the offset of the DateTime being operated on, and that seed decides
// which side of an ambiguous reading is chosen: during a fall-back the hour
// 01:00–01:59 occurs twice, and seeding with the pre-transition offset selects
// the first occurrence while seeding with the post-transition offset selects the
// second. Computing the seed from the reading itself instead — the obvious
// shortcut — disagrees with luxon on roughly one reading in 300 near a
// transition, always by exactly the width of the offset change.
func fromWallClock(y int, mo time.Month, d, h, mi, s, ms int, provisional int, loc *time.Location) time.Time {
	// Interpret the reading as though it were UTC. luxon calls this objToLocalTS.
	// The result is a pivot, not a meaningful instant.
	local := time.Date(y, mo, d, h, mi, s, ms*int(time.Millisecond), time.UTC)

	o := provisional
	guess := local.Add(-time.Duration(o) * time.Second)

	o2 := zoneOffset(guess, loc)
	if o == o2 {
		return guess.In(loc)
	}

	guess = guess.Add(-time.Duration(o2-o) * time.Second)
	o3 := zoneOffset(guess, loc)
	if o2 == o3 {
		return guess.In(loc)
	}

	// The reading falls in a DST gap. luxon subtracts the smaller offset, which
	// yields the later instant — resolving forward, past the gap.
	lo := min(o2, o3)
	return local.Add(-time.Duration(lo) * time.Second).In(loc)
}

// addMonthsCivil shifts a civil date by n months, clamping the day to the target
// month's length.
//
// luxon clamps here — 2024-01-31 plus one month is 2024-02-29 — whereas
// time.Time.AddDate overflows to 2024-03-02. Note the asymmetry documented in
// SEMANTICS.md: luxon clamps for month and year arithmetic but overflows when
// setting a day directly, so this must not be applied uniformly.
func addMonthsCivil(y int, mo time.Month, d, n int) (int, time.Month, int) {
	total := y*12 + (int(mo) - 1) + n
	ny := floorDiv(total, 12)
	nmo := time.Month(total - ny*12 + 1)
	if dim := daysIn(ny, nmo); d > dim {
		d = dim
	}
	return ny, nmo, d
}

// addDaysCivil shifts a civil date by n days, normalising across month and year
// boundaries. Noon is used as the pivot so the calculation cannot be perturbed by
// a DST transition near midnight.
func addDaysCivil(y int, mo time.Month, d, n int) (int, time.Month, int) {
	t := time.Date(y, mo, d+n, 12, 0, 0, 0, time.UTC)
	return t.Year(), t.Month(), t.Day()
}

// cronTime is a wall-clock instant in a location together with the daylight
// saving bookkeeping the search loop depends on. It is the port of the original's
// CronDate.
type cronTime struct {
	t   time.Time
	loc *time.Location

	// dstStart and dstEnd record the hour affected by a daylight saving
	// transition, or -1 when unset. The original uses null; -1 stands in for it.
	dstStart int
	dstEnd   int
}

// newCronTime returns a cronTime for the instant t observed in loc.
//
// The instant is truncated to millisecond precision because the original operates
// in milliseconds throughout; leaving Go's nanoseconds in place would make
// UnixMilli comparisons drift. Truncating to a millisecond is epoch-aligned and so
// is safe in any zone — unlike truncating to an hour, which this package never
// does (see startOfHour).
func newCronTime(t time.Time, loc *time.Location) *cronTime {
	if loc == nil {
		loc = time.Local
	}
	return &cronTime{
		t:        t.In(loc).Truncate(time.Millisecond),
		loc:      loc,
		dstStart: -1,
		dstEnd:   -1,
	}
}

// clone returns a copy carrying the same DST bookkeeping, mirroring the
// original's copy constructor.
func (c *cronTime) clone() *cronTime {
	cp := *c
	return &cp
}

func (c *cronTime) Year() int         { return c.t.Year() }
func (c *cronTime) Month() time.Month { return c.t.Month() }
func (c *cronTime) Day() int          { return c.t.Day() }
func (c *cronTime) Hour() int         { return c.t.Hour() }
func (c *cronTime) Minute() int       { return c.t.Minute() }
func (c *cronTime) Second() int       { return c.t.Second() }
func (c *cronTime) Millisecond() int  { return c.t.Nanosecond() / int(time.Millisecond) }
func (c *cronTime) UnixMilli() int64  { return c.t.UnixMilli() }
func (c *cronTime) Time() time.Time   { return c.t }

// Weekday reports the day of the week with Sunday as 0, matching the original's
// getDay rather than Go's time.Weekday ordering, which also starts at Sunday but
// is a distinct type.
func (c *cronTime) Weekday() int { return int(c.t.Weekday()) }

// OffsetMinutes returns the offset from UTC in minutes, as the original's
// getUTCOffset does. It is how DST transition days are detected.
func (c *cronTime) OffsetMinutes() int { return zoneOffset(c.t, c.loc) / 60 }

// wall decomposes the instant into wall-clock components in c's location.
func (c *cronTime) wall() (y int, mo time.Month, d, h, mi, s, ms int) {
	y, mo, d = c.t.Date()
	h, mi, s = c.t.Clock()
	return y, mo, d, h, mi, s, c.t.Nanosecond() / int(time.Millisecond)
}

// setWall rebuilds the instant from wall-clock components in c's location,
// seeding the offset search with c's current offset exactly as luxon's set(),
// plus(), minus() and startOf() all do.
func (c *cronTime) setWall(y int, mo time.Month, d, h, mi, s, ms int) {
	c.t = fromWallClock(y, mo, d, h, mi, s, ms, zoneOffset(c.t, c.loc), c.loc)
}

// --- startOf / endOf -------------------------------------------------------
//
// endOf is computed as "the start of the next unit, minus one millisecond",
// which is how luxon defines it. Deriving it from components instead would give
// the wrong answer whenever the final second of the period does not exist.

// startOfMonth is not used by the engine, which reaches the first of a month
// through addMonth. It exists because the differential corpus exercises every
// luxon startOf/endOf pairing, and dropping it would leave that comparison
// incomplete.
func (c *cronTime) startOfMonth() {
	y, mo, _, _, _, _, _ := c.wall()
	c.setWall(y, mo, 1, 0, 0, 0, 0)
}

func (c *cronTime) startOfDay() {
	y, mo, d, _, _, _, _ := c.wall()
	c.setWall(y, mo, d, 0, 0, 0, 0)
}

// startOfHour zeroes the minutes and below. It rebuilds from wall-clock
// components rather than calling Truncate, which rounds against absolute time and
// so would be wrong in any zone whose offset is not a whole hour — India (+05:30),
// Australia/Lord_Howe (+10:30), Pacific/Chatham (+12:45).
func (c *cronTime) startOfHour() {
	y, mo, d, h, _, _, _ := c.wall()
	c.setWall(y, mo, d, h, 0, 0, 0)
}

func (c *cronTime) startOfMinute() {
	y, mo, d, h, mi, _, _ := c.wall()
	c.setWall(y, mo, d, h, mi, 0, 0)
}

func (c *cronTime) startOfSecond() {
	y, mo, d, h, mi, s, _ := c.wall()
	c.setWall(y, mo, d, h, mi, s, 0)
}

func (c *cronTime) endOfMonth() {
	y, mo, _, _, _, _, _ := c.wall()
	ny, nmo, _ := addMonthsCivil(y, mo, 1, 1)
	c.t = fromWallClock(ny, nmo, 1, 0, 0, 0, 0, zoneOffset(c.t, c.loc), c.loc).Add(-time.Millisecond)
}

func (c *cronTime) endOfDay() {
	y, mo, d, _, _, _, _ := c.wall()
	ny, nmo, nd := addDaysCivil(y, mo, d, 1)
	c.t = fromWallClock(ny, nmo, nd, 0, 0, 0, 0, zoneOffset(c.t, c.loc), c.loc).Add(-time.Millisecond)
}

// endOfHour advances a whole hour, snaps back to the hour boundary, then steps
// back a millisecond.
//
// The order is load-bearing and matches luxon's endOf, which is
// plus(1 unit).startOf(unit).minus(1). Snapping to the boundary first and then
// advancing — the intuitive reading — gives a different answer whenever the hour
// is repeated by a fall-back, because the snap resolves against the wrong
// offset. Australia/Lord_Howe, whose transition is 30 minutes, disagreed on 92
// readings when the order was reversed.
func (c *cronTime) endOfHour() {
	c.t = c.t.Add(time.Hour)
	c.startOfHour()
	c.t = c.t.Add(-time.Millisecond)
}

func (c *cronTime) endOfMinute() {
	c.t = c.t.Add(time.Minute)
	c.startOfMinute()
	c.t = c.t.Add(-time.Millisecond)
}

// --- arithmetic ------------------------------------------------------------
//
// Each method mirrors one of the original's, including the trailing startOf/endOf
// calls, because those are what make the results land on unit boundaries.
//
// Hours, minutes and seconds are added as exact durations; days, months and years
// are calendar-aware. That split matches luxon, where time units add elapsed time
// and calendar units add civil time.

// addYear mirrors plus({years: 1}), which clamps: 2024-02-29 becomes 2025-02-28.
// Go's AddDate would overflow to 2025-03-01. There is deliberately no startOf
// call — the original has none either.
func (c *cronTime) addYear() {
	y, mo, d, h, mi, s, ms := c.wall()
	ny, nmo, nd := addMonthsCivil(y, mo, d, 12)
	c.setWall(ny, nmo, nd, h, mi, s, ms)
}

// addMonth mirrors plus({months: 1}).startOf('month').
func (c *cronTime) addMonth() {
	y, mo, d, _, _, _, _ := c.wall()
	ny, nmo, _ := addMonthsCivil(y, mo, d, 1)
	c.setWall(ny, nmo, 1, 0, 0, 0, 0)
}

// addDay mirrors plus({days: 1}).startOf('day').
func (c *cronTime) addDay() {
	y, mo, d, _, _, _, _ := c.wall()
	ny, nmo, nd := addDaysCivil(y, mo, d, 1)
	c.setWall(ny, nmo, nd, 0, 0, 0, 0)
}

// addHour mirrors plus({hours: 1}).startOf('hour').
func (c *cronTime) addHour() {
	c.t = c.t.Add(time.Hour)
	c.startOfHour()
}

// addMinute mirrors plus({minutes: 1}).startOf('minute').
func (c *cronTime) addMinute() {
	c.t = c.t.Add(time.Minute)
	c.startOfMinute()
}

// addSecond mirrors plus({seconds: 1}). The original applies no startOf here, so
// sub-second components survive.
func (c *cronTime) addSecond() {
	c.t = c.t.Add(time.Second)
}

// subtractYear mirrors minus({years: 1}), which clamps like its counterpart.
func (c *cronTime) subtractYear() {
	y, mo, d, h, mi, s, ms := c.wall()
	ny, nmo, nd := addMonthsCivil(y, mo, d, -12)
	c.setWall(ny, nmo, nd, h, mi, s, ms)
}

// subtractMonth mirrors minus({months: 1}).endOf('month').startOf('second'),
// landing on the final whole second of the previous month.
func (c *cronTime) subtractMonth() {
	y, mo, d, _, _, _, _ := c.wall()
	ny, nmo, nd := addMonthsCivil(y, mo, d, -1)
	c.setWall(ny, nmo, nd, 0, 0, 0, 0)
	c.endOfMonth()
	c.startOfSecond()
}

// subtractDay mirrors minus({days: 1}).endOf('day').startOf('second').
func (c *cronTime) subtractDay() {
	y, mo, d, h, mi, s, ms := c.wall()
	ny, nmo, nd := addDaysCivil(y, mo, d, -1)
	c.setWall(ny, nmo, nd, h, mi, s, ms)
	c.endOfDay()
	c.startOfSecond()
}

// subtractHour mirrors minus({hours: 1}).endOf('hour').startOf('second').
func (c *cronTime) subtractHour() {
	c.t = c.t.Add(-time.Hour)
	c.endOfHour()
	c.startOfSecond()
}

// subtractMinute mirrors minus({minutes: 1}).endOf('minute').startOf('second').
func (c *cronTime) subtractMinute() {
	c.t = c.t.Add(-time.Minute)
	c.endOfMinute()
	c.startOfSecond()
}

// subtractSecond mirrors minus({seconds: 1}).
func (c *cronTime) subtractSecond() {
	c.t = c.t.Add(-time.Second)
}

// addUnit applies the add operation for the given unit.
func (c *cronTime) addUnit(u timeUnit) {
	switch u {
	case unitYear:
		c.addYear()
	case unitMonth:
		c.addMonth()
	case unitDay:
		c.addDay()
	case unitHour:
		c.addHour()
	case unitMinute:
		c.addMinute()
	case unitSecond:
		c.addSecond()
	}
}

// subtractUnit applies the subtract operation for the given unit.
func (c *cronTime) subtractUnit(u timeUnit) {
	switch u {
	case unitYear:
		c.subtractYear()
	case unitMonth:
		c.subtractMonth()
	case unitDay:
		c.subtractDay()
	case unitHour:
		c.subtractHour()
	case unitMinute:
		c.subtractMinute()
	case unitSecond:
		c.subtractSecond()
	}
}

// invokeDateOperation dispatches on direction.
func (c *cronTime) invokeDateOperation(op dateMathOp, u timeUnit) {
	if op == opAdd {
		c.addUnit(u)
		return
	}
	c.subtractUnit(u)
}

// --- setters ---------------------------------------------------------------
//
// luxon's set() clamps the day to the target month's length only when the day was
// not itself part of the update. Setting the month or year therefore clamps, while
// setting the day directly overflows:
//
//	2024-01-31 set month=2   -> 2024-02-29   (clamped)
//	2024-02-29 set year=2025 -> 2025-02-28   (clamped)
//	2024-02-10 set day=31    -> 2024-03-02   (overflowed)
//
// Applying either rule uniformly would break the other, so the split is
// deliberate. See DECISIONS.md D7.

// setDayOfMonth sets the day without clamping, so out-of-range values roll into
// the following month exactly as luxon's set({day}) does.
func (c *cronTime) setDayOfMonth(d int) {
	y, mo, _, h, mi, s, ms := c.wall()
	ny, nmo, nd := addDaysCivil(y, mo, 1, d-1)
	c.setWall(ny, nmo, nd, h, mi, s, ms)
}

// setMonth sets the month, clamping the day to the target month's length.
func (c *cronTime) setMonth(mo time.Month) {
	y, _, d, h, mi, s, ms := c.wall()
	if dim := daysIn(y, mo); d > dim {
		d = dim
	}
	c.setWall(y, mo, d, h, mi, s, ms)
}

// setYear sets the year, clamping the day so that 29 February survives a move to
// a non-leap year as 28 February.
func (c *cronTime) setYear(y int) {
	_, mo, d, h, mi, s, ms := c.wall()
	if dim := daysIn(y, mo); d > dim {
		d = dim
	}
	c.setWall(y, mo, d, h, mi, s, ms)
}

// setWeekday moves the instant within its ISO week, matching luxon's
// set({weekday}). Monday is 1 and Sunday is 7; values outside that range extend
// into the adjacent weeks, so 0 is the Sunday before.
func (c *cronTime) setWeekday(target int) {
	y, mo, d, h, mi, s, ms := c.wall()
	iso := int(c.t.Weekday())
	if iso == 0 {
		iso = 7
	}
	ny, nmo, nd := addDaysCivil(y, mo, d, target-iso)
	c.setWall(ny, nmo, nd, h, mi, s, ms)
}

func (c *cronTime) setHour(h int) {
	y, mo, d, _, mi, s, ms := c.wall()
	c.setWall(y, mo, d, h, mi, s, ms)
}

func (c *cronTime) setMinute(mi int) {
	y, mo, d, h, _, s, ms := c.wall()
	c.setWall(y, mo, d, h, mi, s, ms)
}

func (c *cronTime) setSecond(s int) {
	y, mo, d, h, mi, _, ms := c.wall()
	c.setWall(y, mo, d, h, mi, s, ms)
}

func (c *cronTime) setMillisecond(ms int) {
	y, mo, d, h, mi, s, _ := c.wall()
	c.setWall(y, mo, d, h, mi, s, ms)
}

// isLastDayOfMonth reports whether the instant falls on the month's final day.
//
// The original consults its leap-permissive DAYS_IN_MONTH table and subtracts one
// for a non-leap February; the result is the same as daysIn, and it is expressed
// this way to keep the correspondence obvious.
func (c *cronTime) isLastDayOfMonth() bool {
	y, mo, d, _, _, _, _ := c.wall()
	return d == daysIn(y, mo)
}

// isLastWeekdayOfMonth reports whether the instant falls within the final seven
// days of its month. Combined with a weekday match that identifies the last
// occurrence of that weekday, which is what the L suffix means.
func (c *cronTime) isLastWeekdayOfMonth() bool {
	y, mo, d, _, _, _, _ := c.wall()
	return d > daysIn(y, mo)-7
}

// applyDateOperation performs a date operation and records any daylight saving
// transition it crossed.
//
// This reproduces the original's logic, including its assumption that every
// transition is exactly one hour wide — see the diff == 2 test below. That
// assumption is wrong for Antarctica/Troll, whose spring-forward gap is two hours
// and therefore produces a difference of three, so the branch never fires. The bug
// is preserved here because the reference suite depends on it; the corrected
// behaviour is selected separately by the expression layer.
//
// hoursLength is the number of hour values the expression allows. When every hour
// matches there is nothing to compensate for, so the bookkeeping is skipped.
func (c *cronTime) applyDateOperation(op dateMathOp, u timeUnit, hoursLength int) {
	if u == unitMonth || u == unitDay {
		c.invokeDateOperation(op, u)
		return
	}

	previousHour := c.Hour()
	c.invokeDateOperation(op, u)
	currentHour := c.Hour()

	switch diff := currentHour - previousHour; {
	case diff == 2:
		if hoursLength != 24 {
			// The hour between the two readings was skipped by a spring-forward.
			c.dstStart = previousHour + 1
		}
	case diff == 0 && c.Minute() == 0 && c.Second() == 0:
		if hoursLength != 24 {
			c.dstEnd = currentHour
		}
	}
}
