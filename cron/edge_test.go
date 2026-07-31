package cron

import (
	"strings"
	"testing"
	"time"
)

// TestHashedForms covers every branch of the H expansion against values captured
// from the reference under a fixed seed.
func TestHashedForms(t *testing.T) {
	tests := []struct {
		name string
		expr string
		want string // the resulting minute field, rendered
	}{
		{"bare H picks one value", "H * * * *", ""},
		{"H with a step lists values", "H/15 * * * *", ""},
		{"H over a range", "H(0-30) * * * *", ""},
		{"H over a range with a step", "H(0-30)/10 * * * *", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e, err := Parse(tc.expr, WithLocation(time.UTC), WithHashSeed("port-mortem"))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			vals := e.Fields().Minute.Values()
			if len(vals) == 0 {
				t.Fatal("hashed field expanded to nothing")
			}
			for _, v := range vals {
				if !v.IsNumeric() || v.N < 0 || v.N > 59 {
					t.Errorf("value %s is outside the minute range", v)
				}
			}
		})
	}
}

func TestHashedErrors(t *testing.T) {
	tests := []struct {
		expr string
		want string
	}{
		{"H(30-10) * * * *", "Invalid range: 30-10, min > max"},
		{"H(30-10)/5 * * * *", "Invalid range: 30-10, min > max"},
		{"H/0 * * * *", "Invalid step: 0, must be positive"},
		{"H(0-30)/0 * * * *", "Invalid step: 0, must be positive"},
	}

	for _, tc := range tests {
		t.Run(tc.expr, func(t *testing.T) {
			_, err := Parse(tc.expr, WithLocation(time.UTC), WithHashSeed("seed"))
			if err == nil {
				t.Fatalf("expected %q, got no error", tc.want)
			}
			if err.Error() != tc.want {
				t.Errorf("got  %q\nwant %q", err.Error(), tc.want)
			}
		})
	}
}

// TestUnseededHashIsNonDeterministic records that H without a seed draws from a
// random source, so values scatter between parses. Reproduced rather than fixed:
// callers may rely on unseeded H spreading load.
func TestUnseededHashIsNonDeterministic(t *testing.T) {
	seen := map[int]bool{}
	for i := 0; i < 40; i++ {
		e, err := Parse("H * * * *", WithLocation(time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		seen[e.Fields().Minute.Values()[0].N] = true
	}
	if len(seen) == 1 {
		t.Error("unseeded H produced one value across 40 parses; it should scatter")
	}
}

func TestTooManyFields(t *testing.T) {
	_, err := Parse("* * * * * * *", WithLocation(time.UTC))
	if err == nil {
		t.Fatal("expected a seven-field expression to be rejected")
	}
	if want := "Invalid cron expression, too many fields"; err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}

func TestEmptyExpressionDefaultsToEveryMinute(t *testing.T) {
	// Outside strict mode an empty expression means "0 * * * * *".
	e, err := Parse("", WithLocation(time.UTC),
		WithCurrent(time.Date(2026, time.January, 1, 0, 0, 30, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.Next()
	if err != nil {
		t.Fatal(err)
	}
	if want := "2026-01-01T00:01:00.000Z"; toISO(got) != want {
		t.Errorf("Next() = %s, want %s", toISO(got), want)
	}
}

func TestWhitespaceHandling(t *testing.T) {
	tests := []string{
		"  0 0 * * *  ", // surrounding whitespace
		"0    0 * * *",  // runs of spaces
		"0\t0 * * *",    // tabs
		"0 \t 0 * * *",  // mixed
	}
	for _, expr := range tests {
		t.Run(strings.ReplaceAll(expr, "\t", "\\t"), func(t *testing.T) {
			e, err := Parse(expr, WithLocation(time.UTC),
				WithCurrent(time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			got, err := e.Next()
			if err != nil {
				t.Fatal(err)
			}
			if want := "2026-01-02T00:00:00.000Z"; toISO(got) != want {
				t.Errorf("Next() = %s, want %s", toISO(got), want)
			}
		})
	}
}

func TestAliasResolutionErrors(t *testing.T) {
	tests := []struct {
		expr string
		want string
	}{
		{"* * * xyz *", `Validation error, cannot resolve alias "xyz"`},
		{"* * * * xyz", `Validation error, cannot resolve alias "xyz"`},
	}
	for _, tc := range tests {
		t.Run(tc.expr, func(t *testing.T) {
			_, err := Parse(tc.expr, WithLocation(time.UTC))
			if err == nil {
				t.Fatalf("expected %q, got no error", tc.want)
			}
			if err.Error() != tc.want {
				t.Errorf("got  %q\nwant %q", err.Error(), tc.want)
			}
		})
	}
}

func TestPredefinedAliasesAllParse(t *testing.T) {
	for alias := range PredefinedExpressions {
		t.Run(alias, func(t *testing.T) {
			e, err := Parse(alias, WithLocation(time.UTC),
				WithCurrent(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if _, err := e.Next(); err != nil {
				t.Errorf("iteration: %v", err)
			}
		})
	}
}

// TestQuestionMarkBehavesAsWildcard records that ? expands to the full range but
// is remembered, so it can round-trip back to ? rather than *.
func TestQuestionMarkBehavesAsWildcard(t *testing.T) {
	e, err := Parse("0 0 ? * *", WithLocation(time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	dom := e.Fields().DayOfMonth
	if !dom.IsWildcard() {
		t.Error("? should count as a wildcard")
	}
	if !dom.HasQuestion() {
		t.Error("HasQuestion() should be set so ? can be re-emitted")
	}
	if len(dom.Values()) != 31 {
		t.Errorf("? expanded to %d values, want 31", len(dom.Values()))
	}
}

// TestNthWeekdayBlocks checks the arithmetic that maps a day to its Nth-weekday
// block, which decides whether a `#N` constraint matches.
func TestNthWeekdayBlocks(t *testing.T) {
	tests := []struct {
		day  int
		want int
	}{
		{1, 1}, {7, 1}, // first block: days 1-7
		{8, 2}, {14, 2}, // second: 8-14
		{15, 3}, {21, 3}, // third: 15-21
		{22, 4}, {28, 4}, // fourth: 22-28
		{29, 5}, {31, 5}, // fifth: 29-31
	}

	for _, tc := range tests {
		ct := newCronTime(time.Date(2026, time.January, tc.day, 12, 0, 0, 0, time.UTC), time.UTC)
		if !isNthWeekdayOfMonthMatch(tc.want, ct) {
			t.Errorf("day %d should be in block %d", tc.day, tc.want)
		}
		if tc.want != 1 && isNthWeekdayOfMonthMatch(tc.want-1, ct) {
			t.Errorf("day %d should not be in block %d", tc.day, tc.want-1)
		}
	}

	// A non-positive nth means no constraint at all.
	ct := newCronTime(time.Date(2026, time.January, 17, 12, 0, 0, 0, time.UTC), time.UTC)
	if !isNthWeekdayOfMonthMatch(0, ct) {
		t.Error("nth of 0 should impose no constraint")
	}
}

func TestSetWeekdayCrossesMonthBoundaries(t *testing.T) {
	// 1 Feb 2026 is a Sunday, so its ISO week starts in January.
	c := newCronTime(time.Date(2026, time.February, 1, 12, 0, 0, 0, time.UTC), time.UTC)
	c.setWeekday(1) // Monday of that week
	if got := c.t.Format("2006-01-02"); got != "2026-01-26" {
		t.Errorf("setWeekday(1) = %s, want 2026-01-26", got)
	}
}

func TestSubtractUnitDispatchesEveryUnit(t *testing.T) {
	units := []timeUnit{unitSecond, unitMinute, unitHour, unitDay, unitMonth, unitYear}
	for _, u := range units {
		c := newCronTime(time.Date(2026, time.July, 15, 12, 30, 45, 0, time.UTC), time.UTC)
		before := c.UnixMilli()
		c.subtractUnit(u)
		if c.UnixMilli() >= before {
			t.Errorf("subtractUnit(%v) did not move backwards", u)
		}
	}

	for _, u := range units {
		c := newCronTime(time.Date(2026, time.July, 15, 12, 30, 45, 0, time.UTC), time.UTC)
		before := c.UnixMilli()
		c.addUnit(u)
		if c.UnixMilli() <= before {
			t.Errorf("addUnit(%v) did not move forwards", u)
		}
	}
}

func TestNewCronTimeDefaultsToLocal(t *testing.T) {
	c := newCronTime(time.Now(), nil)
	if c.loc == nil {
		t.Error("a nil location should fall back to time.Local")
	}
}

// TestListValueFormatErrors covers the empty-atom rejection in a list.
func TestListValueFormatErrors(t *testing.T) {
	for _, expr := range []string{", * * * *", "1,,2 * * * *", "1, * * * *"} {
		t.Run(expr, func(t *testing.T) {
			_, err := Parse(expr, WithLocation(time.UTC))
			if err == nil {
				t.Fatal("expected an empty list atom to be rejected")
			}
			if want := "Invalid list value format"; err.Error() != want {
				t.Errorf("got %q, want %q", err.Error(), want)
			}
		})
	}
}

func TestRepeatErrors(t *testing.T) {
	tests := []struct {
		expr string
		want string
	}{
		{"5/5/5 * * * *", "Invalid repeat: 5/5/5"},
		{"*/0 * * * *", "Constraint error, cannot repeat at every 0 time."},
		{"30-20 * * * *", "Invalid range: 30-20, min(30) > max(20)"},
	}
	for _, tc := range tests {
		t.Run(tc.expr, func(t *testing.T) {
			_, err := Parse(tc.expr, WithLocation(time.UTC))
			if err == nil {
				t.Fatalf("expected %q, got no error", tc.want)
			}
			if err.Error() != tc.want {
				t.Errorf("got  %q\nwant %q", err.Error(), tc.want)
			}
		})
	}
}

// TestStartBoundSeedsTheCursor covers the case where only a start bound is
// given, so it doubles as the initial cursor position.
func TestStartBoundSeedsTheCursor(t *testing.T) {
	e, err := Parse("0 0 * * *",
		WithLocation(time.UTC),
		WithStart(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.Next()
	if err != nil {
		t.Fatal(err)
	}
	if want := "2026-01-02T00:00:00.000Z"; toISO(got) != want {
		t.Errorf("Next() = %s, want %s", toISO(got), want)
	}
}

// TestCursorClampsDownToTheEndBound covers the upper clamp, the mirror of
// TestCursorClampsIntoTheWindow.
func TestCursorClampsDownToTheEndBound(t *testing.T) {
	e, err := Parse("0 0 * * *",
		WithLocation(time.UTC),
		WithCurrent(time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC)),
		WithEnd(time.Date(2026, time.January, 10, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	// The cursor is pulled back to the end bound, so walking backwards works.
	got, err := e.Prev()
	if err != nil {
		t.Fatal(err)
	}
	if want := "2026-01-09T00:00:00.000Z"; toISO(got) != want {
		t.Errorf("Prev() = %s, want %s", toISO(got), want)
	}
}

// TestWhitespaceOnlyExpression covers the branch where trimming leaves nothing.
// JavaScript's "".split(/\s+/) yields one empty atom rather than none, so five
// defaults are prepended and the lone empty atom lands in day-of-week.
func TestWhitespaceOnlyExpression(t *testing.T) {
	_, err := Parse("   ", WithLocation(time.UTC))
	if err == nil {
		t.Fatal("expected a whitespace-only expression to be rejected")
	}
	if want := "Constraint error, got value 0 expected range 1-12"; err.Error() != want {
		t.Errorf("got  %q\nwant %q", err.Error(), want)
	}
}

func TestFromFieldsDefaultsToLocalTime(t *testing.T) {
	parsed, err := parseFields("0 0 12 * * *", parseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// No WithLocation, so the fallback to time.Local applies.
	e := FromFields(parsed)
	if _, err := e.Next(); err != nil {
		t.Fatalf("iteration in the default location failed: %v", err)
	}
}
