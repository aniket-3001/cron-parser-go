package cron

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// isoMillis is JavaScript's Date.prototype.toISOString format, which the
// fixtures are recorded in.
const isoMillis = "2006-01-02T15:04:05.000Z"

func toISO(t time.Time) string { return t.UTC().Format(isoMillis) }

type includeProbe struct {
	Date   string `json:"date"`
	Result bool   `json:"result"`
}

type scheduleCase struct {
	Expression  string         `json:"expression"`
	TZ          string         `json:"tz"`
	CurrentDate string         `json:"currentDate"`
	Next        []string       `json:"next"`
	NextError   string         `json:"nextError"`
	Prev        []string       `json:"prev"`
	PrevError   string         `json:"prevError"`
	Includes    []includeProbe `json:"includes"`
}

type boundedCase struct {
	Expression  string   `json:"expression"`
	TZ          string   `json:"tz"`
	CurrentDate string   `json:"currentDate"`
	StartDate   string   `json:"startDate"`
	EndDate     string   `json:"endDate"`
	Next        []string `json:"next"`
	NextError   string   `json:"nextError"`
}

type scheduleFixtures struct {
	Iterations int            `json:"iterations"`
	Cases      []scheduleCase `json:"cases"`
	Bounded    []boundedCase  `json:"bounded"`
}

func loadScheduleFixtures(t *testing.T) scheduleFixtures {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "schedule-fixtures.json"))
	if err != nil {
		t.Fatalf("read fixtures: %v\nregenerate with: node scripts/probe/gen-schedule-fixtures.js", err)
	}
	var f scheduleFixtures
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("decode fixtures: %v", err)
	}
	return f
}

func mustParseISO(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return v
}

// TestScheduleMatchesReference is the engine's primary correctness check.
//
// Every expression is iterated forwards and backwards from each starting instant
// in each zone, and the resulting instants are compared exactly against the
// reference. Starting instants cluster around daylight-saving transitions and
// month boundaries, which is where the search loop's DST compensation and its
// day/month ordering actually matter.
func TestScheduleMatchesReference(t *testing.T) {
	f := loadScheduleFixtures(t)

	for _, tc := range f.Cases {
		name := tc.Expression + " | " + tc.TZ + " | " + tc.CurrentDate
		t.Run(name, func(t *testing.T) {
			loc, err := time.LoadLocation(tc.TZ)
			if err != nil {
				t.Skipf("zone %s unavailable: %v", tc.TZ, err)
			}
			start := mustParseISO(t, tc.CurrentDate)

			checkDirection(t, "next", tc.Expression, loc, start, tc.Next, tc.NextError, false)
			checkDirection(t, "prev", tc.Expression, loc, start, tc.Prev, tc.PrevError, true)

			for _, probe := range tc.Includes {
				e, err := Parse(tc.Expression, WithLocation(loc), WithCurrent(start))
				if err != nil {
					t.Fatalf("parse: %v", err)
				}
				got, err := e.Includes(mustParseISO(t, probe.Date))
				if err != nil {
					t.Fatalf("Includes(%s): %v", probe.Date, err)
				}
				if got != probe.Result {
					t.Errorf("Includes(%s) = %v, want %v", probe.Date, got, probe.Result)
				}
			}
		})
	}
}

// checkDirection walks one direction and compares against the recorded sequence.
//
// The reference records however many instants it produced before failing, so a
// partial sequence followed by an error is itself part of the contract.
func checkDirection(t *testing.T, label, expr string, loc *time.Location, start time.Time, want []string, wantErr string, reverse bool) {
	t.Helper()

	e, err := Parse(expr, WithLocation(loc), WithCurrent(start))
	if err != nil {
		// A parse failure must match whatever the reference reported.
		if wantErr == "" {
			t.Fatalf("%s: unexpected parse error: %v", label, err)
		}
		if err.Error() != wantErr {
			t.Errorf("%s: parse error\n  got  %q\n  want %q", label, err.Error(), wantErr)
		}
		return
	}

	step := e.Next
	if reverse {
		step = e.Prev
	}

	for i := 0; i < len(want); i++ {
		got, err := step()
		if err != nil {
			t.Fatalf("%s[%d]: unexpected error %v (reference produced %s)", label, i, err, want[i])
		}
		if iso := toISO(got); iso != want[i] {
			t.Fatalf("%s[%d]\n  got  %s\n  want %s", label, i, iso, want[i])
		}
	}

	// If the reference stopped short, the port must stop in the same place.
	if wantErr != "" {
		if _, err := step(); err == nil {
			t.Errorf("%s: expected error %q after %d values, got none", label, wantErr, len(want))
		} else if err.Error() != wantErr {
			t.Errorf("%s: error\n  got  %q\n  want %q", label, err.Error(), wantErr)
		}
	}
}

// TestBoundedIterationMatchesReference covers start and end windows, including
// the error raised on leaving them.
func TestBoundedIterationMatchesReference(t *testing.T) {
	f := loadScheduleFixtures(t)

	for _, tc := range f.Bounded {
		t.Run(tc.Expression+" | "+tc.CurrentDate, func(t *testing.T) {
			loc, err := time.LoadLocation(tc.TZ)
			if err != nil {
				t.Skipf("zone %s unavailable", tc.TZ)
			}

			opts := []Option{WithLocation(loc), WithCurrent(mustParseISO(t, tc.CurrentDate))}
			if tc.StartDate != "" {
				opts = append(opts, WithStart(mustParseISO(t, tc.StartDate)))
			}
			if tc.EndDate != "" {
				opts = append(opts, WithEnd(mustParseISO(t, tc.EndDate)))
			}

			e, err := Parse(tc.Expression, opts...)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}

			for i, want := range tc.Next {
				got, err := e.Next()
				if err != nil {
					t.Fatalf("next[%d]: unexpected error %v (reference produced %s)", i, err, want)
				}
				if iso := toISO(got); iso != want {
					t.Fatalf("next[%d]\n  got  %s\n  want %s", i, iso, want)
				}
			}

			if tc.NextError != "" {
				if _, err := e.Next(); err == nil {
					t.Errorf("expected error %q, got none", tc.NextError)
				} else if err.Error() != tc.NextError {
					t.Errorf("error\n  got  %q\n  want %q", err.Error(), tc.NextError)
				}
			}
		})
	}
}

// --- targeted behaviour tests ---------------------------------------------

// TestDayOfMonthDayOfWeekIsOr pins the rule that surprises everyone: when both
// day fields are restricted they combine with OR, so "0 0 13 * 5" fires on the
// 13th and on every Friday, not only on Friday the 13th.
func TestDayOfMonthDayOfWeekIsOr(t *testing.T) {
	e, err := Parse("0 0 13 * 5",
		WithLocation(time.UTC),
		WithCurrent(time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}

	got := e.Take(6)
	want := []string{
		"2026-02-06", // Friday
		"2026-02-13", // Friday AND the 13th
		"2026-02-20", // Friday
		"2026-02-27", // Friday
		"2026-03-06", // Friday
		"2026-03-13", // Friday AND the 13th
	}
	for i, w := range want {
		if got[i].UTC().Format("2006-01-02") != w {
			t.Errorf("take[%d] = %s, want %s", i, got[i].UTC().Format("2006-01-02"), w)
		}
	}
}

func TestDayOfMonthDayOfWeekAndWhenOneIsWildcard(t *testing.T) {
	// Only day-of-month restricted: the 13th regardless of weekday.
	e, err := Parse("0 0 13 * *",
		WithLocation(time.UTC),
		WithCurrent(time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	for i, w := range []string{"2026-02-13", "2026-03-13", "2026-04-13"} {
		got := e.Take(1)[0].UTC().Format("2006-01-02")
		if got != w {
			t.Errorf("occurrence %d = %s, want %s", i, got, w)
		}
	}
}

func TestLastDayAndLastWeekday(t *testing.T) {
	tests := []struct {
		expr string
		want []string
	}{
		{"0 0 L * *", []string{"2026-01-31", "2026-02-28", "2026-03-31"}},
		{"0 0 * * 5L", []string{"2026-01-30", "2026-02-27", "2026-03-27"}},
		{"0 0 * * MON#2", []string{"2026-01-12", "2026-02-09", "2026-03-09"}},
	}

	for _, tc := range tests {
		t.Run(tc.expr, func(t *testing.T) {
			e, err := Parse(tc.expr,
				WithLocation(time.UTC),
				WithCurrent(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)))
			if err != nil {
				t.Fatal(err)
			}
			for i, got := range e.Take(len(tc.want)) {
				if d := got.UTC().Format("2006-01-02"); d != tc.want[i] {
					t.Errorf("take[%d] = %s, want %s", i, d, tc.want[i])
				}
			}
		})
	}
}

// TestBareLDayOfWeekFailsAtIteration reproduces bug 4: "0 0 * * L" parses
// successfully and only fails when the schedule is walked, because the weekday
// is read from the first character and parseInt("L") is NaN.
func TestBareLDayOfWeekFailsAtIteration(t *testing.T) {
	e, err := Parse("0 0 * * L",
		WithLocation(time.UTC),
		WithCurrent(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("parse should succeed, reproducing the original: %v", err)
	}
	if _, err := e.Next(); err == nil {
		t.Fatal("Next() should fail for a bare L in day-of-week")
	} else if err.Error() != "Invalid last weekday of the month expression: L" {
		t.Errorf("got %q, want the invalid-last-weekday message", err.Error())
	}
}

// TestLoopLimitOnUnsatisfiableExpression covers the safety valve: the 30th of
// February can never occur, so the search gives up rather than spinning.
func TestLoopLimitOnUnsatisfiableExpression(t *testing.T) {
	// A restricted day-of-week rescues an unreachable day-of-month at parse
	// time, so this uses a form that survives validation and then never matches.
	e, err := Parse("0 0 30 2 *",
		WithLocation(time.UTC),
		WithCurrent(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		// The collection-level check rejects it up front, which is also correct.
		if err.Error() != "Invalid explicit day of month definition" {
			t.Fatalf("unexpected parse error: %v", err)
		}
		return
	}
	if _, err := e.Next(); err == nil {
		t.Fatal("expected the loop limit to trip")
	} else if err.Error() != "Invalid expression, loop limit exceeded" {
		t.Errorf("got %q, want the loop-limit message", err.Error())
	}
}

func TestIterationIsExclusiveOfTheStartingInstant(t *testing.T) {
	onSchedule := time.Date(2026, time.January, 1, 9, 0, 0, 0, time.UTC)

	e, err := Parse("0 0 9 * * *", WithLocation(time.UTC), WithCurrent(onSchedule))
	if err != nil {
		t.Fatal(err)
	}
	next, err := e.Next()
	if err != nil {
		t.Fatal(err)
	}
	if want := "2026-01-02T09:00:00.000Z"; toISO(next) != want {
		t.Errorf("Next() = %s, want %s", toISO(next), want)
	}

	e2, err := Parse("0 0 9 * * *", WithLocation(time.UTC), WithCurrent(onSchedule))
	if err != nil {
		t.Fatal(err)
	}
	prev, err := e2.Prev()
	if err != nil {
		t.Fatal(err)
	}
	if want := "2025-12-31T09:00:00.000Z"; toISO(prev) != want {
		t.Errorf("Prev() = %s, want %s", toISO(prev), want)
	}
}

func TestTakeNegativeWalksBackwards(t *testing.T) {
	e, err := Parse("0 0 * * *",
		WithLocation(time.UTC),
		WithCurrent(time.Date(2026, time.March, 10, 12, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	got := e.Take(-3)
	want := []string{"2026-03-10T00:00:00.000Z", "2026-03-09T00:00:00.000Z", "2026-03-08T00:00:00.000Z"}
	if len(got) != len(want) {
		t.Fatalf("got %d values, want %d", len(got), len(want))
	}
	for i, w := range want {
		if toISO(got[i]) != w {
			t.Errorf("take[%d] = %s, want %s", i, toISO(got[i]), w)
		}
	}
}

func TestResetReturnsTheCursor(t *testing.T) {
	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	e, err := Parse("0 0 * * *", WithLocation(time.UTC), WithCurrent(start))
	if err != nil {
		t.Fatal(err)
	}

	first := e.Take(3)
	e.Reset()
	again := e.Take(3)

	for i := range first {
		if !first[i].Equal(again[i]) {
			t.Errorf("after Reset, take[%d] = %s, want %s", i, toISO(again[i]), toISO(first[i]))
		}
	}

	e.Reset(time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC))
	if got := toISO(e.Take(1)[0]); got != "2026-06-02T00:00:00.000Z" {
		t.Errorf("after Reset(June 1), next = %s, want 2026-06-02T00:00:00.000Z", got)
	}
}

func TestHasNextAndHasPrevDoNotMoveTheCursor(t *testing.T) {
	e, err := Parse("0 0 * * *",
		WithLocation(time.UTC),
		WithCurrent(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}

	if !e.HasNext() {
		t.Error("HasNext() = false on an unbounded schedule")
	}
	if !e.HasPrev() {
		t.Error("HasPrev() = false on an unbounded schedule")
	}
	if got := toISO(e.Take(1)[0]); got != "2026-01-02T00:00:00.000Z" {
		t.Errorf("cursor moved: next = %s, want 2026-01-02T00:00:00.000Z", got)
	}
}

func TestHasNextIsFalseAtTheEndBound(t *testing.T) {
	e, err := Parse("0 0 * * *",
		WithLocation(time.UTC),
		WithCurrent(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
		WithEnd(time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	if !e.HasNext() {
		t.Fatal("the first occurrence is within the window")
	}
	if _, err := e.Next(); err != nil {
		t.Fatal(err)
	}
	if e.HasNext() {
		t.Error("HasNext() = true past the end bound")
	}
}

// TestAllIteratesAndStopsEarly checks the range-over-func iterator, including
// that breaking out leaves nothing running.
func TestAllIteratesAndStopsEarly(t *testing.T) {
	e, err := Parse("0 0 * * *",
		WithLocation(time.UTC),
		WithCurrent(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for tm := range e.All() {
		got = append(got, toISO(tm))
		if len(got) == 4 {
			break
		}
	}

	want := []string{
		"2026-01-02T00:00:00.000Z", "2026-01-03T00:00:00.000Z",
		"2026-01-04T00:00:00.000Z", "2026-01-05T00:00:00.000Z",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d values, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("all[%d] = %s, want %s", i, got[i], w)
		}
	}
}

func TestAllTerminatesAtTheEndBound(t *testing.T) {
	e, err := Parse("0 0 * * *",
		WithLocation(time.UTC),
		WithCurrent(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
		WithEnd(time.Date(2026, time.January, 4, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}

	count := 0
	for range e.All() {
		count++
		if count > 100 {
			t.Fatal("All() did not terminate at the end bound")
		}
	}
	if count != 3 {
		t.Errorf("produced %d occurrences, want 3", count)
	}
}

// TestSpringForwardCompensation records what happens to an expression scheduled
// inside a gap that does not exist on the transition day.
func TestSpringForwardCompensation(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("America/New_York unavailable")
	}

	e, err := Parse("30 2 * * *",
		WithLocation(ny),
		WithCurrent(time.Date(2026, time.March, 6, 12, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}

	var local []string
	for _, tm := range e.Take(4) {
		local = append(local, tm.In(ny).Format("2006-01-02 15:04"))
	}

	// 02:30 does not exist on 2026-03-08, so the occurrence lands after the gap.
	want := []string{"2026-03-07 02:30", "2026-03-08 03:30", "2026-03-09 02:30", "2026-03-10 02:30"}
	for i, w := range want {
		if local[i] != w {
			t.Errorf("occurrence %d = %s, want %s", i, local[i], w)
		}
	}
}

// TestOperationTrace covers the recording the test bridge relies on.
//
// The original's tests spy on its date-arithmetic method to check that
// iteration jumps to the next matching value rather than stepping towards it.
// The search runs here rather than in JavaScript, so the bridge replays this
// recording to make the same question answerable across the boundary. The
// recording is only worth anything if it reflects what the engine did, which is
// what these cases pin.
func TestOperationTrace(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		start   time.Time
		want    []traceOp
		wantISO string
	}{
		{
			// No later minute in the hour, so the search rolls forward an hour
			// and then sets the minute directly. One operation, not sixty.
			name:  "rolling into the next hour costs one operation",
			expr:  "0 10 * * * *",
			start: time.Date(2023, time.January, 1, 0, 59, 30, 0, time.UTC),
			want: []traceOp{
				{op: opAdd, unit: unitHour, hoursLen: 24},
			},
			wantISO: "2023-01-01T01:10:00.000Z",
		},
		{
			// Past the last scheduled hour, so the search skips a whole day
			// rather than stepping through the remaining hours.
			name:  "skipping to the next day costs one operation",
			expr:  "0 0 9 * * *",
			start: time.Date(2023, time.January, 1, 10, 0, 0, 0, time.UTC),
			want: []traceOp{
				{op: opAdd, unit: unitDay, hoursLen: 1},
			},
			wantISO: "2023-01-02T09:00:00.000Z",
		},
		{
			// A later matching second exists, so the search sets it directly and
			// performs no date operation at all.
			name:    "jumping within the minute costs nothing",
			expr:    "10,20 * * * * *",
			start:   time.Date(2023, time.January, 1, 0, 0, 12, 0, time.UTC),
			want:    nil,
			wantISO: "2023-01-01T00:00:20.000Z",
		},
		{
			name:    "jumping to a later hour costs nothing",
			expr:    "0 0 5,10 * * *",
			start:   time.Date(2023, time.January, 1, 6, 0, 0, 0, time.UTC),
			want:    nil,
			wantISO: "2023-01-01T10:00:00.000Z",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e, err := Parse(tc.expr, WithLocation(time.UTC), WithCurrent(tc.start))
			if err != nil {
				t.Fatal(err)
			}
			e.tracing = true

			got, err := e.Next()
			if err != nil {
				t.Fatal(err)
			}
			if toISO(got) != tc.wantISO {
				t.Fatalf("Next() = %s, want %s", toISO(got), tc.wantISO)
			}

			if len(e.trace) != len(tc.want) {
				t.Fatalf("recorded %d operations, want %d\n  got %+v", len(e.trace), len(tc.want), e.trace)
			}
			for i, w := range tc.want {
				if e.trace[i] != w {
					t.Errorf("operation %d = %+v, want %+v", i, e.trace[i], w)
				}
			}
		})
	}
}

// TestTracingIsOffByDefault records that the recording costs nothing unless it
// is asked for.
func TestTracingIsOffByDefault(t *testing.T) {
	e, err := Parse("0 10 * * * *",
		WithLocation(time.UTC),
		WithCurrent(time.Date(2023, time.January, 1, 0, 59, 30, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Next(); err != nil {
		t.Fatal(err)
	}
	if len(e.trace) != 0 {
		t.Errorf("recorded %d operations with tracing off", len(e.trace))
	}
}
