package cron

import (
	"errors"
	"testing"
	"time"
)

func TestAccessors(t *testing.T) {
	e, err := Parse("0 30 9 15 6 1", WithLocation(time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	if got := e.String(); got != "0 30 9 15 6 1" {
		t.Errorf("String() = %q, want the expression as written", got)
	}

	f := e.Fields()
	if f == nil {
		t.Fatal("Fields() returned nil")
	}

	tests := []struct {
		name     string
		field    *Field
		unit     Unit
		min, max int
		raw      string
	}{
		{"second", f.Second, UnitSecond, 0, 59, "0"},
		{"minute", f.Minute, UnitMinute, 0, 59, "30"},
		{"hour", f.Hour, UnitHour, 0, 23, "9"},
		{"dayOfMonth", f.DayOfMonth, UnitDayOfMonth, 1, 31, "15"},
		{"month", f.Month, UnitMonth, 1, 12, "6"},
		{"dayOfWeek", f.DayOfWeek, UnitDayOfWeek, 0, 7, "1"},
	}

	for _, tc := range tests {
		if got := tc.field.Unit(); got != tc.unit {
			t.Errorf("%s Unit() = %v, want %v", tc.name, got, tc.unit)
		}
		if got := tc.field.Min(); got != tc.min {
			t.Errorf("%s Min() = %d, want %d", tc.name, got, tc.min)
		}
		if got := tc.field.Max(); got != tc.max {
			t.Errorf("%s Max() = %d, want %d", tc.name, got, tc.max)
		}
		if got := tc.field.Raw(); got != tc.raw {
			t.Errorf("%s Raw() = %q, want %q", tc.name, got, tc.raw)
		}
	}
}

func TestUnitAndTimeUnitNames(t *testing.T) {
	units := map[Unit]string{
		UnitSecond: "Second", UnitMinute: "Minute", UnitHour: "Hour",
		UnitDayOfMonth: "DayOfMonth", UnitMonth: "Month", UnitDayOfWeek: "DayOfWeek",
		Unit(99): "Unknown",
	}
	for u, want := range units {
		if got := u.String(); got != want {
			t.Errorf("Unit(%d).String() = %q, want %q", u, got, want)
		}
	}

	timeUnits := map[timeUnit]string{
		unitSecond: "Second", unitMinute: "Minute", unitHour: "Hour",
		unitDay: "Day", unitMonth: "Month", unitYear: "Year",
		timeUnit(99): "Unknown",
	}
	for u, want := range timeUnits {
		if got := u.String(); got != want {
			t.Errorf("timeUnit(%d).String() = %q, want %q", u, got, want)
		}
	}

	if got := opAdd.String(); got != "Add" {
		t.Errorf("opAdd.String() = %q, want Add", got)
	}
	if got := opSubtract.String(); got != "Subtract" {
		t.Errorf("opSubtract.String() = %q, want Subtract", got)
	}
}

func TestFromFieldsBuildsAWorkingExpression(t *testing.T) {
	parsed, err := parseFields("0 0 12 * * *", parseOptions{})
	if err != nil {
		t.Fatal(err)
	}

	e := FromFields(parsed,
		WithLocation(time.UTC),
		WithCurrent(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)))

	got, err := e.Next()
	if err != nil {
		t.Fatal(err)
	}
	if want := "2026-01-01T12:00:00.000Z"; toISO(got) != want {
		t.Errorf("Next() = %s, want %s", toISO(got), want)
	}

	// An expression built from fields carries no source text.
	if s := e.String(); s != "" {
		t.Errorf("String() = %q, want empty for a field-built expression", s)
	}
}

func TestWithHashSeedIsDeterministic(t *testing.T) {
	parseWith := func(seed string) []Value {
		e, err := Parse("H * * * *", WithLocation(time.UTC), WithHashSeed(seed))
		if err != nil {
			t.Fatal(err)
		}
		return e.Fields().Minute.Values()
	}

	a, b := parseWith("fixed"), parseWith("fixed")
	if len(a) != 1 || len(b) != 1 || a[0] != b[0] {
		t.Errorf("same seed gave %v and %v, want identical single values", a, b)
	}
}

func TestWithStrict(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		wantErr string
	}{
		{"six fields are accepted", "0 * * * * *", ""},
		{"five fields are rejected", "* * * * *", "Invalid cron expression, expected 6 fields"},
		{"empty is rejected", "", "Invalid cron expression"},
		{"both day fields restricted", "0 0 0 1 * 1",
			"Cannot use both dayOfMonth and dayOfWeek together in strict mode!"},
		{"only day of month restricted", "0 0 0 1 * *", ""},
		{"only day of week restricted", "0 0 0 * * 1", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.expr, WithLocation(time.UTC), WithStrict())
			switch {
			case tc.wantErr == "" && err != nil:
				t.Errorf("unexpected error: %v", err)
			case tc.wantErr != "" && err == nil:
				t.Errorf("expected error %q, got none", tc.wantErr)
			case tc.wantErr != "" && err.Error() != tc.wantErr:
				t.Errorf("got %q, want %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// TestStartBoundIsEnforcedWhenWalkingBackwards covers the lower half of the
// window check, which forward iteration never reaches.
func TestStartBoundIsEnforcedWhenWalkingBackwards(t *testing.T) {
	e, err := Parse("0 0 * * *",
		WithLocation(time.UTC),
		WithCurrent(time.Date(2026, time.January, 5, 0, 0, 0, 0, time.UTC)),
		WithStart(time.Date(2026, time.January, 3, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}

	// 4 Jan and 3 Jan are inside the window; the next step back is not.
	for i := 0; i < 2; i++ {
		if _, err := e.Prev(); err != nil {
			t.Fatalf("occurrence %d should be inside the window: %v", i, err)
		}
	}
	_, err = e.Prev()
	if err == nil {
		t.Fatal("expected the start bound to be enforced")
	}
	if err.Error() != "Out of the time span range" {
		t.Errorf("got %q, want the out-of-range message", err.Error())
	}
	if !errors.Is(err, ErrOutOfBounds) {
		t.Error("error should wrap ErrOutOfBounds")
	}
}

// TestCursorClampsIntoTheWindow records that asking to start outside the bounds
// moves the cursor to the nearer bound rather than failing immediately.
func TestCursorClampsIntoTheWindow(t *testing.T) {
	e, err := Parse("0 0 * * *",
		WithLocation(time.UTC),
		WithCurrent(time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC)),
		WithStart(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
		WithEnd(time.Date(2026, time.January, 10, 0, 0, 0, 0, time.UTC)))
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

func TestTakeStopsEarlyWhenTheScheduleRunsOut(t *testing.T) {
	e, err := Parse("0 0 * * *",
		WithLocation(time.UTC),
		WithCurrent(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
		WithEnd(time.Date(2026, time.January, 4, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	if got := e.Take(10); len(got) != 3 {
		t.Errorf("Take(10) returned %d values, want 3 before the window closes", len(got))
	}
}

func TestTakeZeroReturnsNothing(t *testing.T) {
	e, err := Parse("* * * * *", WithLocation(time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if got := e.Take(0); len(got) != 0 {
		t.Errorf("Take(0) returned %d values, want 0", len(got))
	}
}

func TestParseDefaultsToLocalTime(t *testing.T) {
	// No WithLocation: the expression should still parse and iterate.
	e, err := Parse("0 0 * * *")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Next(); err != nil {
		t.Fatalf("iteration in the default location failed: %v", err)
	}
}

func TestValueStringAndComparison(t *testing.T) {
	if got := num(42).String(); got != "42" {
		t.Errorf("num(42).String() = %q, want \"42\"", got)
	}
	if got := text("5L").String(); got != "5L" {
		t.Errorf("text(\"5L\").String() = %q, want \"5L\"", got)
	}

	// Token ordering is lexical, the branch that mixed numeric input never
	// reaches.
	if compareValues(text("L"), text("W")) >= 0 {
		t.Error("compareValues(L, W) should order L first")
	}
	if compareValues(text("W"), text("L")) <= 0 {
		t.Error("compareValues(W, L) should order W second")
	}
	if compareValues(text("L"), text("L")) != 0 {
		t.Error("compareValues of equal tokens should be 0")
	}
}

func TestFieldErrorRendersEveryKind(t *testing.T) {
	tests := []struct {
		err  *FieldError
		want string
	}{
		{&FieldError{Field: "CronSecond", Kind: errKindEmpty},
			"CronSecond Validation error, values contains no values"},
		{&FieldError{Field: "CronSecond", Kind: errKindDuplicate, Value: num(7)},
			"CronSecond Validation error, duplicate values found: 7"},
		{&FieldError{Field: "CronHour", Kind: errKindRange, Value: num(99), Min: 0, Max: 23},
			"CronHour Validation error, got value 99 expected range 0-23"},
		{&FieldError{Field: "CronDayOfMonth", Kind: errKindRange, Value: num(99), Min: 1, Max: 31, Chars: []rune{'L'}},
			"CronDayOfMonth Validation error, got value 99 expected range 1-31 or chars L"},
	}

	for _, tc := range tests {
		if got := tc.err.Error(); got != tc.want {
			t.Errorf("got  %q\nwant %q", got, tc.want)
		}
		if !errors.Is(tc.err, ErrValidation) {
			t.Error("FieldError should wrap ErrValidation")
		}
	}
}

func TestExpressionErrorSentinels(t *testing.T) {
	tests := []struct {
		err      error
		sentinel error
	}{
		{syntaxError("boom"), ErrSyntax},
		{constraintError("boom"), ErrConstraint},
		{validationError("boom"), ErrValidation},
	}
	for _, tc := range tests {
		if !errors.Is(tc.err, tc.sentinel) {
			t.Errorf("%v should wrap %v", tc.err, tc.sentinel)
		}
		if tc.err.Error() != "boom" {
			t.Errorf("Error() = %q, want \"boom\"", tc.err.Error())
		}
	}
}

func TestJSNumberCoercion(t *testing.T) {
	tests := []struct {
		in     string
		want   float64
		wantOK bool
	}{
		{"5", 5, true},
		{"", 0, true},      // Number("") is 0, unlike parseInt
		{"   ", 0, true},   // whitespace only is also 0
		{" 12 ", 12, true}, // surrounding whitespace is trimmed
		{"5L", 0, false},   // trailing garbage makes the whole thing NaN
		{"L", 0, false},
		{"-3", -3, true},
		{"1e3", 1000, true},
	}
	for _, tc := range tests {
		got, ok := jsNumber(tc.in)
		if ok != tc.wantOK || (ok && got != tc.want) {
			t.Errorf("jsNumber(%q) = (%v, %v), want (%v, %v)", tc.in, got, ok, tc.want, tc.wantOK)
		}
	}
}

func TestJSParseIntCoercion(t *testing.T) {
	tests := []struct {
		in     string
		want   int
		wantOK bool
	}{
		{"5", 5, true},
		{"5abc", 5, true}, // trailing garbage is discarded, unlike Number
		{"", 0, false},    // no digits is NaN, unlike Number
		{"abc", 0, false},
		{" 12", 12, true},
		{"\t\n 7", 7, true},
		{"+8", 8, true},
		{"-8", -8, true},
		{"-", 0, false},
		{"L", 0, false},
	}
	for _, tc := range tests {
		got, ok := jsParseInt(tc.in)
		if ok != tc.wantOK || (ok && got != tc.want) {
			t.Errorf("jsParseInt(%q) = (%d, %v), want (%d, %v)", tc.in, got, ok, tc.want, tc.wantOK)
		}
	}

	if got := numText(0, false); got != "NaN" {
		t.Errorf("numText(_, false) = %q, want NaN", got)
	}
	if got := numText(5, true); got != "5" {
		t.Errorf("numText(5, true) = %q, want 5", got)
	}
}

func TestIncludesRejectsNonMatchingFields(t *testing.T) {
	e, err := Parse("0 30 12 15 6 *", WithLocation(time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		when time.Time
		want bool
	}{
		{"exact match", time.Date(2026, time.June, 15, 12, 30, 0, 0, time.UTC), true},
		{"wrong second", time.Date(2026, time.June, 15, 12, 30, 1, 0, time.UTC), false},
		{"wrong minute", time.Date(2026, time.June, 15, 12, 31, 0, 0, time.UTC), false},
		{"wrong hour", time.Date(2026, time.June, 15, 13, 30, 0, 0, time.UTC), false},
		{"wrong month", time.Date(2026, time.July, 15, 12, 30, 0, 0, time.UTC), false},
		{"wrong day", time.Date(2026, time.June, 16, 12, 30, 0, 0, time.UTC), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := e.Includes(tc.when)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("Includes(%s) = %v, want %v", toISO(tc.when), got, tc.want)
			}
		})
	}
}

func TestIncludesPropagatesTheBareLError(t *testing.T) {
	e, err := Parse("0 0 * * L", WithLocation(time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Includes(time.Date(2026, time.January, 31, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Error("Includes should surface the malformed day-of-week error")
	}
}
