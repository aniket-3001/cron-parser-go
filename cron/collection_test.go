package cron

import (
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"
	"time"
)

type serializedFieldFixture struct {
	Wildcard bool        `json:"wildcard"`
	Values   []jsonValue `json:"values"`
}

type stringifyCase struct {
	Expression           string `json:"expression"`
	OK                   bool   `json:"ok"`
	Error                string `json:"error"`
	Stringify            string `json:"stringify"`
	StringifyWithSeconds string `json:"stringifyWithSeconds"`
	ToString             string `json:"toString"`
	RoundTrip            string `json:"roundTrip"`
	RoundTripStable      bool   `json:"roundTripStable"`
	Serialize            struct {
		Second     serializedFieldFixture `json:"second"`
		Minute     serializedFieldFixture `json:"minute"`
		Hour       serializedFieldFixture `json:"hour"`
		DayOfMonth serializedFieldFixture `json:"dayOfMonth"`
		Month      serializedFieldFixture `json:"month"`
		DayOfWeek  serializedFieldFixture `json:"dayOfWeek"`
	} `json:"serialize"`
}

type crontabCase struct {
	Name        string            `json:"name"`
	Content     string            `json:"content"`
	Variables   map[string]string `json:"variables"`
	Expressions []string          `json:"expressions"`
	Errors      []string          `json:"errors"`
}

type stringifyFixtures struct {
	HashSeed string          `json:"hashSeed"`
	Cases    []stringifyCase `json:"cases"`
	Crontabs []crontabCase   `json:"crontabs"`
}

func loadStringifyFixtures(t *testing.T) stringifyFixtures {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "stringify-fixtures.json"))
	if err != nil {
		t.Fatalf("read fixtures: %v\nregenerate with: node scripts/probe/gen-stringify-fixtures.js", err)
	}
	var f stringifyFixtures
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("decode fixtures: %v", err)
	}
	return f
}

// TestFormatMatchesReference checks rendering against text captured from the
// reference, covering the range compaction that turns an expanded field back
// into wildcards, steps, ranges and lists.
func TestFormatMatchesReference(t *testing.T) {
	f := loadStringifyFixtures(t)

	for _, tc := range f.Cases {
		t.Run(tc.Expression, func(t *testing.T) {
			e, err := Parse(tc.Expression, WithLocation(time.UTC), WithHashSeed(f.HashSeed))
			if !tc.OK {
				if err == nil {
					t.Fatalf("expected error %q, but parsing succeeded", tc.Error)
				}
				if err.Error() != tc.Error {
					t.Errorf("error\n  got  %q\n  want %q", err.Error(), tc.Error)
				}
				return
			}
			if err != nil {
				t.Fatalf("parse: %v", err)
			}

			if got := mustFormat(t, e, false); got != tc.Stringify {
				t.Errorf("Format(false)\n  got  %q\n  want %q", got, tc.Stringify)
			}
			if got := mustFormat(t, e, true); got != tc.StringifyWithSeconds {
				t.Errorf("Format(true)\n  got  %q\n  want %q", got, tc.StringifyWithSeconds)
			}
			if got := e.String(); got != tc.ToString {
				t.Errorf("String()\n  got  %q\n  want %q", got, tc.ToString)
			}
		})
	}
}

// TestRoundTripProperty checks that rendering is a true inverse of parsing:
// parsing the rendered text must produce the same rendering again.
//
// This is a property rather than an example, so it keeps its force under
// translation in a way a table of expected strings does not.
func TestRoundTripProperty(t *testing.T) {
	f := loadStringifyFixtures(t)

	for _, tc := range f.Cases {
		if !tc.OK {
			continue
		}
		t.Run(tc.Expression, func(t *testing.T) {
			first, err := Parse(tc.Expression, WithLocation(time.UTC), WithHashSeed(f.HashSeed))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			rendered := mustFormat(t, first, true)

			second, err := Parse(rendered, WithLocation(time.UTC), WithHashSeed(f.HashSeed))
			if err != nil {
				t.Fatalf("reparsing %q failed: %v", rendered, err)
			}

			if got := mustFormat(t, second, true); got != rendered {
				t.Errorf("round trip is not stable\n  %q\n  -> %q\n  -> %q", tc.Expression, rendered, got)
			}

			// The reference agrees on where the round trip lands.
			if tc.RoundTrip != "" && rendered != tc.RoundTrip {
				t.Errorf("round trip\n  got  %q\n  want %q", rendered, tc.RoundTrip)
			}
		})
	}
}

// TestRoundTripPreservesFields checks the stronger form of the property: the
// field values themselves survive rendering, not merely the text.
//
// Two fields are compared loosely, and both relaxations are properties of the
// original rather than concessions by the port:
//
//   - Day-of-week drops a trailing 7 when rendered, so a field holding Sunday as
//     both 0 and 7 comes back holding only 0.
//
//   - Day-of-month is rendered against the named month's length when exactly one
//     month is named, so "1-29" in February renders as "*" and widens to 1-31 on
//     reparse. No information is lost: the extra days do not occur in that month,
//     which TestRoundTripAgreesOnSchedules confirms by comparing instants.
//
// Verified against the reference, which widens identically.
func TestRoundTripPreservesFields(t *testing.T) {
	f := loadStringifyFixtures(t)

	for _, tc := range f.Cases {
		if !tc.OK {
			continue
		}
		t.Run(tc.Expression, func(t *testing.T) {
			first, err := Parse(tc.Expression, WithLocation(time.UTC), WithHashSeed(f.HashSeed))
			if err != nil {
				t.Fatal(err)
			}
			second, err := Parse(mustFormat(t, first, true), WithLocation(time.UTC), WithHashSeed(f.HashSeed))
			if err != nil {
				t.Fatal(err)
			}

			a, b := first.Fields(), second.Fields()
			pairs := []struct {
				name string
				x, y *Field
			}{
				{"second", a.Second, b.Second},
				{"minute", a.Minute, b.Minute},
				{"hour", a.Hour, b.Hour},
				{"month", a.Month, b.Month},
			}
			for _, p := range pairs {
				if !slices.Equal(p.x.Values(), p.y.Values()) {
					t.Errorf("%s values changed\n  %v\n  %v", p.name, p.x.Values(), p.y.Values())
				}
			}

			if !slices.Equal(withoutTrailingSeven(a.DayOfWeek.Values()), withoutTrailingSeven(b.DayOfWeek.Values())) {
				t.Errorf("dayOfWeek values changed\n  %v\n  %v", a.DayOfWeek.Values(), b.DayOfWeek.Values())
			}

			// Day-of-month may widen only when a single month narrowed it.
			if !slices.Equal(a.DayOfMonth.Values(), b.DayOfMonth.Values()) {
				if len(a.Month.values) != 1 {
					t.Errorf("dayOfMonth values changed without a single month to narrow them\n  %v\n  %v",
						a.DayOfMonth.Values(), b.DayOfMonth.Values())
					return
				}
				if !isSupersetOf(b.DayOfMonth.Values(), a.DayOfMonth.Values()) {
					t.Errorf("dayOfMonth did not widen, it changed\n  %v\n  %v",
						a.DayOfMonth.Values(), b.DayOfMonth.Values())
				}
			}
		})
	}
}

func isSupersetOf(super, sub []Value) bool {
	for _, v := range sub {
		if !slices.Contains(super, v) {
			return false
		}
	}
	return true
}

func withoutTrailingSeven(vs []Value) []Value {
	if n := len(vs); n > 0 && vs[n-1].IsNumeric() && vs[n-1].N == 7 {
		return vs[:n-1]
	}
	return vs
}

// TestRoundTripAgreesOnSchedules is the strongest form: an expression and its
// rendering must fire at exactly the same instants.
func TestRoundTripAgreesOnSchedules(t *testing.T) {
	f := loadStringifyFixtures(t)
	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

	for _, tc := range f.Cases {
		if !tc.OK {
			continue
		}
		t.Run(tc.Expression, func(t *testing.T) {
			first, err := Parse(tc.Expression,
				WithLocation(time.UTC), WithHashSeed(f.HashSeed), WithCurrent(start))
			if err != nil {
				t.Fatal(err)
			}
			rendered := mustFormat(t, first, true)

			second, err := Parse(rendered,
				WithLocation(time.UTC), WithHashSeed(f.HashSeed), WithCurrent(start))
			if err != nil {
				t.Fatal(err)
			}

			a, b := first.Take(12), second.Take(12)
			if len(a) != len(b) {
				t.Fatalf("produced %d and %d occurrences", len(a), len(b))
			}
			for i := range a {
				if !a[i].Equal(b[i]) {
					t.Fatalf("occurrence %d differs after rendering to %q\n  %s\n  %s",
						i, rendered, toISO(a[i]), toISO(b[i]))
				}
			}
		})
	}
}

func TestSerializeMatchesReference(t *testing.T) {
	f := loadStringifyFixtures(t)

	for _, tc := range f.Cases {
		if !tc.OK {
			continue
		}
		t.Run(tc.Expression, func(t *testing.T) {
			e, err := Parse(tc.Expression, WithLocation(time.UTC), WithHashSeed(f.HashSeed))
			if err != nil {
				t.Fatal(err)
			}
			got := e.Fields().Serialize()

			checks := []struct {
				name string
				got  SerializedField
				want serializedFieldFixture
			}{
				{"second", got.Second, tc.Serialize.Second},
				{"minute", got.Minute, tc.Serialize.Minute},
				{"hour", got.Hour, tc.Serialize.Hour},
				{"dayOfMonth", got.DayOfMonth, tc.Serialize.DayOfMonth},
				{"month", got.Month, tc.Serialize.Month},
				{"dayOfWeek", got.DayOfWeek, tc.Serialize.DayOfWeek},
			}
			for _, c := range checks {
				if c.got.Wildcard != c.want.Wildcard {
					t.Errorf("%s wildcard = %v, want %v", c.name, c.got.Wildcard, c.want.Wildcard)
				}
				if !slices.Equal(c.got.Values, unwrap(c.want.Values)) {
					t.Errorf("%s values\n  got  %v\n  want %v", c.name, c.got.Values, unwrap(c.want.Values))
				}
			}
		})
	}
}

func TestCrontabMatchesReference(t *testing.T) {
	f := loadStringifyFixtures(t)

	for _, tc := range f.Crontabs {
		t.Run(tc.Name, func(t *testing.T) {
			got := ParseCrontab(tc.Content, WithLocation(time.UTC))

			if len(got.Variables) != len(tc.Variables) {
				t.Errorf("variables\n  got  %v\n  want %v", got.Variables, tc.Variables)
			}
			for k, want := range tc.Variables {
				if got.Variables[k] != want {
					t.Errorf("variable %q = %q, want %q", k, got.Variables[k], want)
				}
			}

			if len(got.Entries) != len(tc.Expressions) {
				t.Fatalf("parsed %d schedules, want %d", len(got.Entries), len(tc.Expressions))
			}
			for i, want := range tc.Expressions {
				if rendered := mustFormat(t, got.Entries[i].Expression, true); rendered != want {
					t.Errorf("schedule %d\n  got  %q\n  want %q", i, rendered, want)
				}
			}

			gotErrs := make([]string, 0, len(got.Errors))
			for line := range got.Errors {
				gotErrs = append(gotErrs, line)
			}
			sort.Strings(gotErrs)
			wantErrs := slices.Clone(tc.Errors)
			sort.Strings(wantErrs)
			if !slices.Equal(gotErrs, wantErrs) {
				t.Errorf("unparseable lines\n  got  %v\n  want %v", gotErrs, wantErrs)
			}
		})
	}
}

func TestCrontabCapturesCommands(t *testing.T) {
	got := ParseCrontab("*/10 * * * * /path/to/exe --flag value\n0 0 * * *\n", WithLocation(time.UTC))

	if len(got.Entries) != 2 {
		t.Fatalf("parsed %d schedules, want 2", len(got.Entries))
	}
	if want := []string{"/path/to/exe", "--flag", "value"}; !slices.Equal(got.Entries[0].Command, want) {
		t.Errorf("command = %v, want %v", got.Entries[0].Command, want)
	}
	if len(got.Entries[1].Command) != 0 {
		t.Errorf("command = %v, want none", got.Entries[1].Command)
	}
}

func TestParseCrontabFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "crontab")
	content := "FOO=bar\n*/5 * * * * cmd\n"
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ParseCrontabFile(p, WithLocation(time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if got.Variables["FOO"] != "bar" {
		t.Errorf("variables = %v, want FOO=bar", got.Variables)
	}
	if len(got.Entries) != 1 {
		t.Errorf("parsed %d schedules, want 1", len(got.Entries))
	}

	if _, err := ParseCrontabFile(filepath.Join(dir, "missing"), WithLocation(time.UTC)); err == nil {
		t.Error("expected an error for a missing file")
	}
}

// TestCompactFieldRuns exercises the compaction directly, including the run of
// exactly two that is emitted as two singletons rather than as a range.
func TestCompactFieldRuns(t *testing.T) {
	vals := func(ns ...int) []Value {
		out := make([]Value, len(ns))
		for i, n := range ns {
			out[i] = num(n)
		}
		return out
	}

	tests := []struct {
		name  string
		input []Value
		want  []fieldRange
	}{
		{"empty", nil, nil},
		{"single value", vals(5), []fieldRange{{start: num(5), count: 1}}},
		{
			"two values become two singletons",
			vals(1, 2),
			[]fieldRange{{start: num(1), count: 1}, {start: num(2), count: 1}},
		},
		{
			"an even stride becomes one run",
			vals(0, 15, 30, 45),
			[]fieldRange{{start: num(0), count: 4, end: 45, hasEnd: true, step: 15, hasStep: true}},
		},
		{
			"a contiguous run",
			vals(1, 2, 3, 4, 5),
			[]fieldRange{{start: num(1), count: 5, end: 5, hasEnd: true, step: 1, hasStep: true}},
		},
		{
			"a token stands alone",
			[]Value{num(1), text("L")},
			[]fieldRange{{start: num(1), count: 1}, {start: text("L"), count: 1}},
		},
		{
			// A run of two is established by the lookahead and then broken, so
			// it is emitted as two singletons rather than as a range.
			"an established run of two is split when broken",
			vals(1, 2, 10),
			[]fieldRange{
				{start: num(1), count: 1},
				{start: num(2), count: 1},
				{start: num(10), count: 1},
			},
		},
		{
			// Repeated zeros compact to a run with a stride of zero, which
			// cannot be rendered. See upstream-issues/07.
			"repeated zeros yield a zero stride",
			vals(0, 0, 0),
			[]fieldRange{{start: num(0), count: 3, end: 0, hasEnd: true, step: 0, hasStep: true}},
		},
		{
			// A run of three or more survives as a range when it breaks.
			"a longer run is kept as a range",
			vals(1, 2, 3, 10),
			[]fieldRange{
				{start: num(1), count: 3, end: 3, hasEnd: true, step: 1, hasStep: true},
				{start: num(10), count: 1},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := compactField(tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d ranges, want %d\n  %+v", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("range %d\n  got  %+v\n  want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestDayOfMonthRendersAgainstASingleMonth records that naming exactly one month
// narrows the day-of-month maximum to that month's length, so a full span
// renders as a wildcard.
func TestDayOfMonthRendersAgainstASingleMonth(t *testing.T) {
	tests := []struct {
		expr string
		want string
	}{
		{"0 0 1-30 4 *", "0 0 * 4 *"},    // April has 30 days
		{"0 0 1-31 1 *", "0 0 * 1 *"},    // January has 31
		{"0 0 1-29 2 *", "0 0 * 2 *"},    // the table is leap-permissive
		{"0 0 1-30 * *", "0 0 1-30 * *"}, // several months, so no narrowing
	}

	for _, tc := range tests {
		t.Run(tc.expr, func(t *testing.T) {
			e, err := Parse(tc.expr, WithLocation(time.UTC))
			if err != nil {
				t.Fatal(err)
			}
			if got := mustFormat(t, e, false); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestDayOfWeekDropsTheTrailingSeven records the rendering half of the duplicate
// Sunday: a field holding both 0 and 7 renders Sunday once.
func TestDayOfWeekDropsTheTrailingSeven(t *testing.T) {
	tests := []struct {
		expr string
		want string
	}{
		{"* * * * 5-7", "* * * * 0,5,6"},
		{"* * * * 0-7", "* * * * *"},
		{"* * * * 1-7", "* * * * *"},
		{"* * * * 7", "* * * * 0"},
		{"* * * * 6,7", "* * * * 0,6"},
	}

	for _, tc := range tests {
		t.Run(tc.expr, func(t *testing.T) {
			e, err := Parse(tc.expr, WithLocation(time.UTC))
			if err != nil {
				t.Fatal(err)
			}
			if got := mustFormat(t, e, false); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNthDayIsAppendedWhenRendering(t *testing.T) {
	e, err := Parse("0 0 * * MON#2", WithLocation(time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if want := "0 0 * * 1#2"; mustFormat(t, e, false) != want {
		t.Errorf("got %q, want %q", mustFormat(t, e, false), want)
	}
}

func TestQuestionMarkRoundTripsAsQuestionMark(t *testing.T) {
	e, err := Parse("0 0 ? * *", WithLocation(time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if want := "0 0 ? * *"; mustFormat(t, e, false) != want {
		t.Errorf("got %q, want %q; ? must not render as *", mustFormat(t, e, false), want)
	}
}

// TestRoundTripPropertyOverRandomExpressions sweeps the grammar rather than a
// curated list, checking the three round-trip properties on every expression
// that parses: the rendering is stable, reparsing it succeeds, and both fire at
// identical instants.
//
// The fixtures above only prove what was chosen by hand; this is what gives the
// property real force.
func TestRoundTripPropertyOverRandomExpressions(t *testing.T) {
	rng := rand.New(rand.NewSource(20260801))
	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

	checked, unrenderable := 0, 0
	for i := 0; i < 20000; i++ {
		expr := randomExpression(rng)

		first, err := Parse(expr, WithLocation(time.UTC), WithCurrent(start))
		if err != nil {
			continue // invalid input is the parser's business, not this test's
		}

		rendered, err := first.Format(true)
		if err != nil {
			// A field of repeated zeros cannot be rendered, in this port or in
			// the original. TestFormatRejectsRepeatedZeros pins that; see
			// upstream-issues/07.
			unrenderable++
			continue
		}

		second, err := Parse(rendered, WithLocation(time.UTC), WithCurrent(start))
		if err != nil {
			t.Fatalf("%q rendered to %q, which failed to parse: %v", expr, rendered, err)
		}

		// Rendering is not a fixed point after one application: a day-of-month
		// rendered against a named month's length reparses to a wider set, which
		// then renders differently. It does settle, so the property checked here
		// is convergence rather than immediate stability. See
		// upstream-issues/08.
		if !convergesWithin(t, rendered, 4) {
			t.Fatalf("rendering never settled, starting from %q -> %q", expr, rendered)
		}

		// A day field covering its whole range without being written "*" or "?"
		// is not a wildcard, yet renders as "*". That flips day matching between
		// OR and AND, so the rendering can fire on entirely different days. The
		// behaviour is reproduced from the original and is reported upstream as
		// bug 6; TestKnownRoundTripScheduleDivergence pins it exactly. Schedules
		// are not comparable for this class of input.
		if wildcardFlagsChanged(first, second) {
			checked++
			continue
		}

		// Compare schedules. Expressions that cannot be satisfied report the
		// same failure on both sides, which is itself agreement.
		a, aErr := takeOrError(first, 8)
		b, bErr := takeOrError(second, 8)
		if aErr != bErr {
			t.Fatalf("%q and its rendering %q disagree on failure\n  %v\n  %v", expr, rendered, aErr, bErr)
		}
		if len(a) != len(b) {
			t.Fatalf("%q and its rendering %q produced %d and %d occurrences", expr, rendered, len(a), len(b))
		}
		for j := range a {
			if !a[j].Equal(b[j]) {
				t.Fatalf("occurrence %d differs\n  %q -> %s\n  %q -> %s",
					j, expr, toISO(a[j]), rendered, toISO(b[j]))
			}
		}
		checked++
	}

	if checked < 1000 {
		t.Fatalf("only %d expressions parsed; the generator is producing too little valid input", checked)
	}
	t.Logf("round trip held for %d expressions (%d unrenderable, see upstream-issues/07)", checked, unrenderable)
}

// convergesWithin reports whether repeatedly parsing and rendering reaches a
// fixed point within n applications.
func convergesWithin(t *testing.T, expr string, n int) bool {
	t.Helper()
	for i := 0; i < n; i++ {
		e, err := Parse(expr, WithLocation(time.UTC))
		if err != nil {
			t.Fatalf("rendered form %q failed to parse: %v", expr, err)
		}
		next, err := e.Format(true)
		if err != nil {
			t.Fatalf("rendered form %q failed to render: %v", expr, err)
		}
		if next == expr {
			return true
		}
		expr = next
	}
	return false
}

// wildcardFlagsChanged reports whether rendering altered either day field's
// wildcard flag, which is the precondition for the round-trip divergence
// described in upstream-issues/06.
func wildcardFlagsChanged(a, b *Expression) bool {
	return a.Fields().DayOfMonth.IsWildcard() != b.Fields().DayOfMonth.IsWildcard() ||
		a.Fields().DayOfWeek.IsWildcard() != b.Fields().DayOfWeek.IsWildcard()
}

func takeOrError(e *Expression, n int) ([]time.Time, string) {
	var out []time.Time
	for i := 0; i < n; i++ {
		tm, err := e.Next()
		if err != nil {
			return out, err.Error()
		}
		out = append(out, tm)
	}
	return out, ""
}

// TestKnownRoundTripScheduleDivergence pins the reference behaviour reported in
// upstream-issues/06: a day field covering its whole range but not written "*"
// or "?" is treated as restricted, yet renders as "*". Day matching switches
// between OR and AND on that flag, so the rendering fires on different days.
//
// Every expectation here was verified against v5.6.2. The port reproduces the
// behaviour deliberately; if a future change "fixes" it, this test fails and the
// divergence from the reference becomes visible rather than silent.
func TestKnownRoundTripScheduleDivergence(t *testing.T) {
	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		expr       string
		rendered   string
		beforeDays []string
		afterDays  []string
	}{
		{
			// Day-of-week spelled out in full: OR becomes AND, so a daily
			// schedule becomes monthly.
			expr: "0 0 16 * 0-6", rendered: "0 0 16 * *",
			beforeDays: []string{"2026-01-02", "2026-01-03"},
			afterDays:  []string{"2026-01-16", "2026-02-16"},
		},
		{
			expr: "0 0 16 * 0-7", rendered: "0 0 16 * *",
			beforeDays: []string{"2026-01-02", "2026-01-03"},
			afterDays:  []string{"2026-01-16", "2026-02-16"},
		},
		{
			expr: "0 0 16 * */1", rendered: "0 0 16 * *",
			beforeDays: []string{"2026-01-02", "2026-01-03"},
			afterDays:  []string{"2026-01-16", "2026-02-16"},
		},
		{
			// The same in reverse: an exhaustive day-of-month turns a daily
			// schedule into a weekly one.
			expr: "0 0 1-31 * 5", rendered: "0 0 * * 5",
			beforeDays: []string{"2026-01-02", "2026-01-03"},
			afterDays:  []string{"2026-01-02", "2026-01-09"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.expr, func(t *testing.T) {
			first, err := Parse(tc.expr, WithLocation(time.UTC), WithCurrent(start))
			if err != nil {
				t.Fatal(err)
			}
			if got := mustFormat(t, first, false); got != tc.rendered {
				t.Fatalf("Format(false) = %q, want %q", got, tc.rendered)
			}

			second, err := Parse(tc.rendered, WithLocation(time.UTC), WithCurrent(start))
			if err != nil {
				t.Fatal(err)
			}

			days := func(e *Expression) []string {
				out := []string{}
				for _, tm := range e.Take(2) {
					out = append(out, tm.UTC().Format("2006-01-02"))
				}
				return out
			}

			if got := days(first); !slices.Equal(got, tc.beforeDays) {
				t.Errorf("original fires on %v, want %v", got, tc.beforeDays)
			}
			if got := days(second); !slices.Equal(got, tc.afterDays) {
				t.Errorf("rendering fires on %v, want %v", got, tc.afterDays)
			}
		})
	}
}

// TestWildcardFlagFollowsRawTextNotValues isolates the root cause: the flag is
// derived from how the field was written, not from what it covers.
func TestWildcardFlagFollowsRawTextNotValues(t *testing.T) {
	tests := []struct {
		expr string
		want bool
	}{
		{"0 0 * * *", true},
		{"0 0 * * ?", true},
		{"0 0 * * 0-6", false}, // covers every day, but is not a wildcard
		{"0 0 * * 0-7", false},
		{"0 0 * * */1", false},
	}

	for _, tc := range tests {
		t.Run(tc.expr, func(t *testing.T) {
			e, err := Parse(tc.expr, WithLocation(time.UTC))
			if err != nil {
				t.Fatal(err)
			}
			if got := e.Fields().DayOfWeek.IsWildcard(); got != tc.want {
				t.Errorf("dayOfWeek IsWildcard() = %v, want %v", got, tc.want)
			}
		})
	}
}

// mustFormat renders an expression, failing the test if rendering errors.
func mustFormat(t *testing.T, e *Expression, includeSeconds bool) string {
	t.Helper()
	s, err := e.Format(includeSeconds)
	if err != nil {
		t.Fatalf("Format(%v): %v", includeSeconds, err)
	}
	return s
}

// TestFormatRejectsRepeatedZeros pins the failure reported in
// upstream-issues/07. Repeated zeros escape duplicate validation, compact to a
// run with a stride of zero, and then cannot be rendered. The original throws
// the same internal error, which its author had marked unreachable.
func TestFormatRejectsRepeatedZeros(t *testing.T) {
	tests := []string{
		"0 0 * * 7,0,7", // three values, all normalising to Sunday
		"0 0 * * 0,0,0",
	}

	for _, expr := range tests {
		t.Run(expr, func(t *testing.T) {
			e, err := Parse(expr, WithLocation(time.UTC))
			if err != nil {
				t.Fatalf("parsing should succeed, reproducing the original: %v", err)
			}
			if got := e.Fields().DayOfWeek.Values(); len(got) != 3 {
				t.Fatalf("dayOfWeek = %v, want three repeated zeros", got)
			}

			if _, err := e.Format(true); err == nil {
				t.Fatal("Format should fail for a field of repeated zeros")
			} else if err.Error() != "Unexpected range step" {
				t.Errorf("got %q, want %q", err.Error(), "Unexpected range step")
			}
		})
	}
}

// TestStringReportsEmptyWhenRenderingFails records how String behaves when it
// cannot render, since a Stringer has no way to report the error.
func TestStringReportsEmptyWhenRenderingFails(t *testing.T) {
	parsed, err := parseFields("0 0 * * * 0,0,0", parseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	e := FromFields(parsed, WithLocation(time.UTC))
	if got := e.String(); got != "" {
		t.Errorf("String() = %q, want empty when rendering fails", got)
	}
}

// TestRenderingConvergesButIsNotIdempotent pins the behaviour reported in
// upstream-issues/08. Rendering narrows day-of-month to the named month's
// length, but parsing expands a bare start against the field's own maximum of
// 31, so the first rendering reparses to a wider set and renders differently.
// It settles on the second application.
//
// The schedule never changes: the extra days do not occur in that month.
// Verified against v5.6.2, which behaves identically.
func TestRenderingConvergesButIsNotIdempotent(t *testing.T) {
	const expr = "* * 9-20/3 16-26/5 jun *"

	want := []string{
		"* * 9-18/3 16/5 6 *",
		"* * 9-18/3 16-31/5 6 *",
		"* * 9-18/3 16-31/5 6 *", // settled
	}

	current := expr
	for i, w := range want {
		e, err := Parse(current, WithLocation(time.UTC))
		if err != nil {
			t.Fatalf("step %d: parsing %q: %v", i, current, err)
		}
		got, err := e.Format(true)
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		if got != w {
			t.Fatalf("step %d\n  from %q\n  got  %q\n  want %q", i, current, got, w)
		}
		current = got
	}

	// The added day does not exist in June, so the schedule is unchanged.
	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	a, err := Parse(expr, WithLocation(time.UTC), WithCurrent(start))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Parse(want[1], WithLocation(time.UTC), WithCurrent(start))
	if err != nil {
		t.Fatal(err)
	}
	x, y := a.Take(20), b.Take(20)
	if len(x) != len(y) {
		t.Fatalf("produced %d and %d occurrences", len(x), len(y))
	}
	for i := range x {
		if !x[i].Equal(y[i]) {
			t.Errorf("occurrence %d differs: %s vs %s", i, toISO(x[i]), toISO(y[i]))
		}
	}
}
