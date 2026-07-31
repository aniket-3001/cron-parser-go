package cron

import (
	"errors"
	"slices"
	"testing"
)

// mkField is a shorthand for building a field from plain numbers.
func mkField(t *testing.T, spec *fieldSpec, ns ...int) *Field {
	t.Helper()
	vals := make([]Value, len(ns))
	for i, n := range ns {
		vals[i] = num(n)
	}
	f, err := newField(spec, vals, fieldOptions{})
	if err != nil {
		t.Fatalf("newField: %v", err)
	}
	return f
}

// TestFieldValidationMessages pins every validation message against text
// captured from the reference implementation. 42 of the original tests assert on
// these strings, so a drift here fails the suite in a way that looks unrelated.
func TestFieldValidationMessages(t *testing.T) {
	tests := []struct {
		name   string
		spec   *fieldSpec
		values []Value
		want   string // empty means construction should succeed
	}{
		{"empty values", specSecond, nil,
			"CronSecond Validation error, values contains no values"},
		{"second above range", specSecond, []Value{num(60)},
			"CronSecond Validation error, got value 60 expected range 0-59"},
		{"second below range", specSecond, []Value{num(-1)},
			"CronSecond Validation error, got value -1 expected range 0-59"},
		{"duplicate value", specSecond, []Value{num(1), num(1)},
			"CronSecond Validation error, duplicate values found: 1"},
		{"minute above range", specMinute, []Value{num(60)},
			"CronMinute Validation error, got value 60 expected range 0-59"},
		{"hour above range", specHour, []Value{num(24)},
			"CronHour Validation error, got value 24 expected range 0-23"},
		{"month below range", specMonth, []Value{num(0)},
			"CronMonth Validation error, got value 0 expected range 1-12"},
		{"month above range", specMonth, []Value{num(13)},
			"CronMonth Validation error, got value 13 expected range 1-12"},

		// Fields with special characters append " or chars L" to the message.
		{"day of month above range", specDayOfMonth, []Value{num(32)},
			"CronDayOfMonth Validation error, got value 32 expected range 1-31 or chars L"},
		{"day of month below range", specDayOfMonth, []Value{num(0)},
			"CronDayOfMonth Validation error, got value 0 expected range 1-31 or chars L"},
		{"day of week above range", specDayOfWeek, []Value{num(8)},
			"CronDayOfWeek Validation error, got value 8 expected range 0-7 or chars L"},

		// W is rejected everywhere: it appears in the CronChars type and in the
		// stringify path, but no field lists it in chars. See SEMANTICS.md 7.
		{"W token is rejected", specDayOfMonth, []Value{text("W")},
			"CronDayOfMonth Validation error, got value W expected range 1-31 or chars L"},
		{"more than two digits before L", specDayOfMonth, []Value{text("123L")},
			"CronDayOfMonth Validation error, got value 123L expected range 1-31 or chars L"},

		{"bare L is accepted", specDayOfMonth, []Value{text("L")}, ""},
		{"digit-prefixed L is accepted", specDayOfMonth, []Value{text("15L")}, ""},
		{"day of week L is accepted", specDayOfWeek, []Value{text("5L")}, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newField(tc.spec, tc.values, fieldOptions{})
			switch {
			case tc.want == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tc.want == "":
				return
			case err == nil:
				t.Fatalf("expected error %q, got nil", tc.want)
			case err.Error() != tc.want:
				t.Errorf("got  %q\nwant %q", err.Error(), tc.want)
			}
			if !errors.Is(err, ErrValidation) {
				t.Errorf("error should wrap ErrValidation for errors.Is")
			}
		})
	}
}

// TestDuplicateZeroEscapesValidation reproduces bug 5 in the original: the
// duplicate check is `if (duplicate)` against the value returned by
// Array.prototype.find, which is falsy when that value is 0. Duplicate zeros are
// therefore accepted while every other duplicate is rejected.
//
// Verified against v5.6.2: new CronSecond([0,0]) succeeds with values [0,0],
// while new CronSecond([1,1]) throws.
func TestDuplicateZeroEscapesValidation(t *testing.T) {
	f, err := newField(specSecond, []Value{num(0), num(0)}, fieldOptions{})
	if err != nil {
		t.Fatalf("duplicate zeros should be accepted, reproducing the original: %v", err)
	}
	if got := f.Values(); len(got) != 2 {
		t.Errorf("values = %v, want both zeros retained", got)
	}

	if _, err := newField(specSecond, []Value{num(1), num(1)}, fieldOptions{}); err == nil {
		t.Error("duplicate non-zero values must still be rejected")
	}
}

// TestConstructorDoesNotMutateCaller guards against bug 2 in the original, where
// an in-place sort rewrites the caller's array. Go slices share backing arrays,
// so the naive translation would inherit the bug.
func TestConstructorDoesNotMutateCaller(t *testing.T) {
	caller := []Value{num(30), num(10), num(20)}
	before := slices.Clone(caller)

	if _, err := newField(specMinute, caller, fieldOptions{}); err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(caller, before) {
		t.Errorf("constructor mutated the caller's slice: %v, was %v", caller, before)
	}
}

func TestValuesAccessorReturnsACopy(t *testing.T) {
	f := mkField(t, specMinute, 5, 10)
	got := f.Values()
	got[0] = num(999)
	if f.Values()[0].N != 5 {
		t.Error("mutating the slice returned by Values() disturbed the field")
	}
}

// TestSortingOrdersNumbersBeforeTokens pins the mixed comparator. Verified
// against the original: new CronDayOfMonth([15,'L',3,1]).values is [1,3,15,'L'].
func TestSortingOrdersNumbersBeforeTokens(t *testing.T) {
	f, err := newField(specDayOfMonth,
		[]Value{num(15), text("L"), num(3), num(1)}, fieldOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := []Value{num(1), num(3), num(15), text("L")}
	if !slices.Equal(f.Values(), want) {
		t.Errorf("got %v, want %v", f.Values(), want)
	}
}

func TestWildcardDerivation(t *testing.T) {
	all := make([]Value, 24)
	for i := range all {
		all[i] = num(i)
	}

	tests := []struct {
		name   string
		values []Value
		opts   fieldOptions
		want   bool
	}{
		{"raw star is a wildcard", []Value{num(1)}, fieldOptions{raw: "*"}, true},
		{"raw question mark is a wildcard", []Value{num(1)}, fieldOptions{raw: "?"}, true},
		{"other raw text is not", []Value{num(1)}, fieldOptions{raw: "1"}, false},
		// Without raw text the field is a wildcard only if it spans its range.
		// This is the path fields built programmatically take.
		{"full range without raw is a wildcard", all, fieldOptions{}, true},
		{"partial range without raw is not", []Value{num(1), num(2)}, fieldOptions{}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, err := newField(specHour, tc.values, tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			if f.IsWildcard() != tc.want {
				t.Errorf("IsWildcard() = %v, want %v", f.IsWildcard(), tc.want)
			}
		})
	}
}

// TestHasLastRequiresExactToken records that the L flag tests for the exact
// string "L", so "15L" does not set it. Verified against the original:
// new CronDayOfMonth(['15L']).hasLastChar is false.
func TestHasLastRequiresExactToken(t *testing.T) {
	tests := []struct {
		name   string
		values []Value
		raw    string
		want   bool
	}{
		{"bare L value", []Value{text("L")}, "", true},
		{"digit-prefixed L value", []Value{text("15L")}, "", false},
		{"L anywhere in raw text", []Value{num(15)}, "15L", true},
		{"no L at all", []Value{num(15)}, "15", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, err := newField(specDayOfMonth, tc.values, fieldOptions{raw: tc.raw})
			if err != nil {
				t.Fatal(err)
			}
			if f.HasLast() != tc.want {
				t.Errorf("HasLast() = %v, want %v", f.HasLast(), tc.want)
			}
		})
	}
}

func TestFindNearestValue(t *testing.T) {
	f := mkField(t, specMinute, 0, 15, 30, 45)

	tests := []struct {
		current int
		reverse bool
		want    int
		wantOK  bool
	}{
		{0, false, 15, true},
		{14, false, 15, true},
		{15, false, 30, true},
		{37, false, 45, true},
		{45, false, 0, false}, // nothing later in the hour
		{59, false, 0, false},
		{46, true, 45, true},
		{45, true, 30, true},
		{15, true, 0, true},
		{0, true, 0, false}, // nothing earlier
	}

	for _, tc := range tests {
		got, ok := f.findNearestValue(tc.current, tc.reverse)
		if ok != tc.wantOK || (ok && got != tc.want) {
			t.Errorf("findNearestValue(%d, reverse=%v) = (%d, %v), want (%d, %v)",
				tc.current, tc.reverse, got, ok, tc.want, tc.wantOK)
		}
	}
}

// TestFindNearestValueSkipsTokens records that token values take no part in the
// search. In the original they parse as NaN, and every comparison against NaN is
// false, so they are silently ignored.
func TestFindNearestValueSkipsTokens(t *testing.T) {
	f, err := newField(specDayOfMonth,
		[]Value{num(5), num(20), text("L")}, fieldOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := f.findNearestValue(20, false); ok {
		t.Errorf("findNearestValue(20) = (%d, true); the L token must not be returned", got)
	}
	if got, ok := f.findNearestValue(6, false); !ok || got != 20 {
		t.Errorf("findNearestValue(6) = (%d, %v), want (20, true)", got, ok)
	}
}

func TestMinOrMax(t *testing.T) {
	f := mkField(t, specMinute, 5, 25, 55)
	if got := f.minOrMax(false); got != 5 {
		t.Errorf("minOrMax(false) = %d, want 5", got)
	}
	if got := f.minOrMax(true); got != 55 {
		t.Errorf("minOrMax(true) = %d, want 55", got)
	}
}

func TestSpecTokenMatching(t *testing.T) {
	tests := []struct {
		spec *fieldSpec
		in   string
		want bool
	}{
		{specDayOfMonth, "L", true},
		{specDayOfMonth, "5L", true},
		{specDayOfMonth, "15L", true},
		{specDayOfMonth, "123L", false}, // at most two leading digits
		{specDayOfMonth, "W", false},    // W is in no field's chars
		{specDayOfMonth, "aL", false},
		{specDayOfMonth, "", false},
		{specDayOfWeek, "5L", true},
		{specSecond, "L", false}, // second permits no tokens
	}

	for _, tc := range tests {
		if got := tc.spec.matchesToken(tc.in); got != tc.want {
			t.Errorf("%s.matchesToken(%q) = %v, want %v", tc.spec.name, tc.in, got, tc.want)
		}
	}
}
