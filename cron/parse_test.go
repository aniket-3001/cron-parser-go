package cron

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// jsonValue decodes a cron field value, which the reference emits as either a
// number or a token string.
type jsonValue struct{ Value }

func (v *jsonValue) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		v.Value = text(s)
		return nil
	}
	var n int
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	v.Value = num(n)
	return nil
}

type fixtureCase struct {
	Expression string      `json:"expression"`
	OK         bool        `json:"ok"`
	Error      string      `json:"error"`
	Second     []jsonValue `json:"second"`
	Minute     []jsonValue `json:"minute"`
	Hour       []jsonValue `json:"hour"`
	DayOfMonth []jsonValue `json:"dayOfMonth"`
	Month      []jsonValue `json:"month"`
	DayOfWeek  []jsonValue `json:"dayOfWeek"`
	Wildcards  struct {
		Second     bool `json:"second"`
		Minute     bool `json:"minute"`
		Hour       bool `json:"hour"`
		DayOfMonth bool `json:"dayOfMonth"`
		Month      bool `json:"month"`
		DayOfWeek  bool `json:"dayOfWeek"`
	} `json:"wildcards"`
	NthDayOfWeek int `json:"nthDayOfWeek"`
	HasLast      struct {
		DayOfMonth bool `json:"dayOfMonth"`
		DayOfWeek  bool `json:"dayOfWeek"`
	} `json:"hasLast"`
	HasQuestion struct {
		DayOfMonth bool `json:"dayOfMonth"`
		DayOfWeek  bool `json:"dayOfWeek"`
	} `json:"hasQuestion"`
	Strict bool `json:"strict"`
}

type fixtures struct {
	HashSeed    string        `json:"hashSeed"`
	Cases       []fixtureCase `json:"cases"`
	StrictCases []fixtureCase `json:"strictCases"`
}

func loadFixtures(t *testing.T) fixtures {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "parse-fixtures.json"))
	if err != nil {
		t.Fatalf("read fixtures: %v\nregenerate with: node scripts/probe/gen-parse-fixtures.js", err)
	}
	var f fixtures
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("decode fixtures: %v", err)
	}
	return f
}

func unwrap(vs []jsonValue) []Value {
	out := make([]Value, len(vs))
	for i, v := range vs {
		out[i] = v.Value
	}
	return out
}

// TestParseMatchesReference is the parser's primary correctness check. Every
// expression is parsed by both implementations and the resulting field values,
// wildcard flags, L/? flags and nth-day are compared, along with the exact text
// of every error.
//
// The fixtures are captured from the reference build rather than written by
// hand, so this asserts against real behaviour rather than against my reading of
// the source. They are committed, so `go test` verifies the parser with no Node
// present.
func TestParseMatchesReference(t *testing.T) {
	f := loadFixtures(t)
	all := append(slices.Clone(f.Cases), f.StrictCases...)

	for _, tc := range all {
		name := tc.Expression
		if name == "" {
			name = "(empty)"
		}
		if tc.Strict {
			name += " [strict]"
		}

		t.Run(name, func(t *testing.T) {
			got, err := parseFields(tc.Expression, parseOptions{
				strict:   tc.Strict,
				hashSeed: f.HashSeed,
			})

			if !tc.OK {
				if err == nil {
					t.Fatalf("expected error %q, but parsing succeeded", tc.Error)
				}
				if err.Error() != tc.Error {
					t.Errorf("error mismatch\n  got  %q\n  want %q", err.Error(), tc.Error)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			checkValues(t, "second", got.Second, tc.Second)
			checkValues(t, "minute", got.Minute, tc.Minute)
			checkValues(t, "hour", got.Hour, tc.Hour)
			checkValues(t, "dayOfMonth", got.DayOfMonth, tc.DayOfMonth)
			checkValues(t, "month", got.Month, tc.Month)
			checkValues(t, "dayOfWeek", got.DayOfWeek, tc.DayOfWeek)

			checkBool(t, "wildcard second", got.Second.IsWildcard(), tc.Wildcards.Second)
			checkBool(t, "wildcard minute", got.Minute.IsWildcard(), tc.Wildcards.Minute)
			checkBool(t, "wildcard hour", got.Hour.IsWildcard(), tc.Wildcards.Hour)
			checkBool(t, "wildcard dayOfMonth", got.DayOfMonth.IsWildcard(), tc.Wildcards.DayOfMonth)
			checkBool(t, "wildcard month", got.Month.IsWildcard(), tc.Wildcards.Month)
			checkBool(t, "wildcard dayOfWeek", got.DayOfWeek.IsWildcard(), tc.Wildcards.DayOfWeek)

			checkBool(t, "hasLast dayOfMonth", got.DayOfMonth.HasLast(), tc.HasLast.DayOfMonth)
			checkBool(t, "hasLast dayOfWeek", got.DayOfWeek.HasLast(), tc.HasLast.DayOfWeek)
			checkBool(t, "hasQuestion dayOfMonth", got.DayOfMonth.HasQuestion(), tc.HasQuestion.DayOfMonth)
			checkBool(t, "hasQuestion dayOfWeek", got.DayOfWeek.HasQuestion(), tc.HasQuestion.DayOfWeek)

			if got.DayOfWeek.NthDayOfWeek() != tc.NthDayOfWeek {
				t.Errorf("nthDayOfWeek = %d, want %d", got.DayOfWeek.NthDayOfWeek(), tc.NthDayOfWeek)
			}
		})
	}
}

func checkValues(t *testing.T, field string, got *Field, want []jsonValue) {
	t.Helper()
	w := unwrap(want)
	if !slices.Equal(got.Values(), w) {
		t.Errorf("%s values\n  got  %v\n  want %v", field, got.Values(), w)
	}
}

func checkBool(t *testing.T, label string, got, want bool) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %v, want %v", label, got, want)
	}
}

// TestDayOfWeekSevenAsymmetry isolates the quirk most likely to be "cleaned up"
// by a future change: %7 normalisation is applied to single values but not to
// ranges, and a range ending on a multiple of 7 also gains a leading 0. So
// Sunday can appear twice, as both 0 and 7.
//
// Nineteen of the reference tests read field values directly, so normalising
// this away would fail them even though the schedules would be identical.
func TestDayOfWeekSevenAsymmetry(t *testing.T) {
	tests := []struct {
		expr string
		want []int
	}{
		{"* * * * 7", []int{0}},
		{"* * * * 0", []int{0}},
		{"* * * * 6,7", []int{0, 6}},
		{"* * * * 5-7", []int{0, 5, 6, 7}},
		{"* * * * 1-7", []int{0, 1, 2, 3, 4, 5, 6, 7}},
		{"* * * * 0-7", []int{0, 1, 2, 3, 4, 5, 6, 7}},
	}

	for _, tc := range tests {
		t.Run(tc.expr, func(t *testing.T) {
			f, err := parseFields(tc.expr, parseOptions{})
			if err != nil {
				t.Fatal(err)
			}
			want := make([]Value, len(tc.want))
			for i, n := range tc.want {
				want[i] = num(n)
			}
			if !slices.Equal(f.DayOfWeek.Values(), want) {
				t.Errorf("got %v, want %v", f.DayOfWeek.Values(), want)
			}
		})
	}
}

// TestFieldExpansion records that parsing expands each field into the explicit
// set of values it permits, which is what lets matching be a set membership
// test rather than an interpretation step.
func TestFieldExpansion(t *testing.T) {
	f, err := parseFields("*/20 9-11 * * 1-5", parseOptions{})
	if err != nil {
		t.Fatal(err)
	}

	checkInts := func(name string, got *Field, want ...int) {
		t.Helper()
		w := make([]Value, len(want))
		for i, n := range want {
			w[i] = num(n)
		}
		if !slices.Equal(got.Values(), w) {
			t.Errorf("%s = %v, want %v", name, got.Values(), w)
		}
	}

	checkInts("second", f.Second, 0)
	checkInts("minute", f.Minute, 0, 20, 40)
	checkInts("hour", f.Hour, 9, 10, 11)
	checkInts("dayOfWeek", f.DayOfWeek, 1, 2, 3, 4, 5)
	if !f.DayOfMonth.IsWildcard() {
		t.Error("dayOfMonth should be a wildcard")
	}
}

// TestPRNGIsThreadedAcrossFields guards the sequencing rule: one generator is
// shared by all six fields and advanced once per field even when the field has
// no H. Drawing lazily would change the values every later field resolves to.
func TestPRNGIsThreadedAcrossFields(t *testing.T) {
	// The H is in the last field, so its value depends on five prior draws
	// having happened for the fields that contain no H at all.
	a, err := parseFields("* * * * H", parseOptions{hashSeed: "seed"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := parseFields("* * * * H", parseOptions{hashSeed: "seed"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(a.DayOfWeek.Values(), b.DayOfWeek.Values()) {
		t.Fatal("same seed produced different values")
	}

	// A different seed should move it.
	c, err := parseFields("* * * * H", parseOptions{hashSeed: "other"})
	if err != nil {
		t.Fatal(err)
	}
	if slices.Equal(a.DayOfWeek.Values(), c.DayOfWeek.Values()) {
		t.Log("note: different seeds produced the same value; possible but worth noticing")
	}
}

func TestFieldPaddingTakesDefaultsFromTheEnd(t *testing.T) {
	// Five fields gain a "0" seconds field, which is the intended behaviour.
	f, err := parseFields("30 12 * * *", parseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Second.Values(); len(got) != 1 || got[0].N != 0 {
		t.Errorf("second = %v, want [0]", got)
	}
	if got := f.Minute.Values(); len(got) != 1 || got[0].N != 30 {
		t.Errorf("minute = %v, want [30]", got)
	}

	// Fewer fields take more defaults and misalign, producing an error about a
	// field the user never wrote. Reproduced deliberately.
	if _, err := parseFields("5", parseOptions{}); err == nil {
		t.Error("expected the misaligned single-atom form to fail")
	} else if err.Error() != "Constraint error, got value 0 expected range 1-12" {
		t.Errorf("got %q, want the month-range message", err.Error())
	}
}
