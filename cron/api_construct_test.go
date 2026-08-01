package cron

import (
	"slices"
	"testing"
	"time"
)

func TestFieldConstructors(t *testing.T) {
	tests := []struct {
		name string
		make func() (*Field, error)
		unit Unit
		bad  func() (*Field, error)
	}{
		{"second", func() (*Field, error) { return NewSecond(Nums(0, 30)) }, UnitSecond,
			func() (*Field, error) { return NewSecond(Nums(60)) }},
		{"minute", func() (*Field, error) { return NewMinute(Nums(0, 30)) }, UnitMinute,
			func() (*Field, error) { return NewMinute(Nums(60)) }},
		{"hour", func() (*Field, error) { return NewHour(Nums(9, 17)) }, UnitHour,
			func() (*Field, error) { return NewHour(Nums(24)) }},
		{"dayOfMonth", func() (*Field, error) { return NewDayOfMonth(Nums(1, 15)) }, UnitDayOfMonth,
			func() (*Field, error) { return NewDayOfMonth(Nums(32)) }},
		{"month", func() (*Field, error) { return NewMonth(Nums(1, 6)) }, UnitMonth,
			func() (*Field, error) { return NewMonth(Nums(13)) }},
		{"dayOfWeek", func() (*Field, error) { return NewDayOfWeek(Nums(1, 5)) }, UnitDayOfWeek,
			func() (*Field, error) { return NewDayOfWeek(Nums(8)) }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, err := tc.make()
			if err != nil {
				t.Fatalf("constructing: %v", err)
			}
			if f.Unit() != tc.unit {
				t.Errorf("Unit() = %v, want %v", f.Unit(), tc.unit)
			}
			if _, err := tc.bad(); err == nil {
				t.Error("out-of-range values should be rejected")
			}
		})
	}

	// Tokens are accepted where the field permits them.
	if _, err := NewDayOfMonth([]Value{Text("L")}); err != nil {
		t.Errorf("day-of-month should accept L: %v", err)
	}
	if _, err := NewDayOfWeek([]Value{Text("5L")}); err != nil {
		t.Errorf("day-of-week should accept 5L: %v", err)
	}
}

func TestValueHelpers(t *testing.T) {
	if got := Num(7); got.N != 7 || !got.IsNumeric() {
		t.Errorf("Num(7) = %+v", got)
	}
	if got := Text("L"); got.Text != "L" || got.IsNumeric() {
		t.Errorf("Text(L) = %+v", got)
	}
	if got := Nums(3, 1, 2); !slices.Equal(got, []Value{Num(3), Num(1), Num(2)}) {
		t.Errorf("Nums = %v, want the given order preserved", got)
	}
}

func TestNewFields(t *testing.T) {
	mk := func() (*Field, *Field, *Field, *Field, *Field, *Field) {
		s, _ := NewSecond(Nums(0))
		mi, _ := NewMinute(Nums(0))
		h, _ := NewHour(Nums(12))
		d, _ := NewDayOfMonth(Nums(1))
		mo, _ := NewMonth(Nums(1))
		w, _ := NewDayOfWeek(Nums(0, 1, 2, 3, 4, 5, 6, 7))
		return s, mi, h, d, mo, w
	}

	s, mi, h, d, mo, w := mk()
	fields, err := NewFields(s, mi, h, d, mo, w)
	if err != nil {
		t.Fatalf("NewFields: %v", err)
	}

	e := FromFields(fields,
		WithLocation(time.UTC),
		WithCurrent(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)))
	got, err := e.Next()
	if err != nil {
		t.Fatal(err)
	}
	if want := "2026-01-01T12:00:00.000Z"; toISO(got) != want {
		t.Errorf("Next() = %s, want %s", toISO(got), want)
	}
}

func TestNewFieldsRejectsMissingFields(t *testing.T) {
	s, mi, h, d, mo, w := func() (*Field, *Field, *Field, *Field, *Field, *Field) {
		a, _ := NewSecond(Nums(0))
		b, _ := NewMinute(Nums(0))
		c, _ := NewHour(Nums(12))
		e, _ := NewDayOfMonth(Nums(1))
		f, _ := NewMonth(Nums(1))
		g, _ := NewDayOfWeek(Nums(1))
		return a, b, c, e, f, g
	}()

	tests := []struct {
		name string
		args [6]*Field
		want string
	}{
		{"second", [6]*Field{nil, mi, h, d, mo, w}, "Validation error, Field second is missing"},
		{"minute", [6]*Field{s, nil, h, d, mo, w}, "Validation error, Field minute is missing"},
		{"hour", [6]*Field{s, mi, nil, d, mo, w}, "Validation error, Field hour is missing"},
		{"dayOfMonth", [6]*Field{s, mi, h, nil, mo, w}, "Validation error, Field dayOfMonth is missing"},
		{"month", [6]*Field{s, mi, h, d, nil, w}, "Validation error, Field month is missing"},
		{"dayOfWeek", [6]*Field{s, mi, h, d, mo, nil}, "Validation error, Field dayOfWeek is missing"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewFields(tc.args[0], tc.args[1], tc.args[2], tc.args[3], tc.args[4], tc.args[5])
			if err == nil {
				t.Fatalf("expected %q", tc.want)
			}
			if err.Error() != tc.want {
				t.Errorf("got %q, want %q", err.Error(), tc.want)
			}
		})
	}
}

func TestNewFieldsRejectsMismatchedUnits(t *testing.T) {
	s, _ := NewSecond(Nums(0))
	mi, _ := NewMinute(Nums(0))
	h, _ := NewHour(Nums(12))
	d, _ := NewDayOfMonth(Nums(1))
	mo, _ := NewMonth(Nums(1))

	// An hour field passed where day-of-week belongs.
	_, err := NewFields(s, mi, h, d, mo, h)
	if err == nil {
		t.Fatal("expected a unit mismatch to be rejected")
	}
	if want := "Validation error, Field dayOfWeek has unit Hour"; err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}

func TestNewFieldsRejectsImpossibleDayOfMonth(t *testing.T) {
	s, _ := NewSecond(Nums(0))
	mi, _ := NewMinute(Nums(0))
	h, _ := NewHour(Nums(0))
	d, _ := NewDayOfMonth(Nums(30))
	mo, _ := NewMonth(Nums(2)) // February, at most 29 days
	w, _ := NewDayOfWeek(Nums(0, 1, 2, 3, 4, 5, 6, 7))

	_, err := NewFields(s, mi, h, d, mo, w)
	if err == nil {
		t.Fatal("expected 30 February to be rejected")
	}
	if want := "Invalid explicit day of month definition"; err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}
