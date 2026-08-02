package cron

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Unit identifies one of the six fields of a cron expression.
type Unit int

const (
	UnitSecond Unit = iota
	UnitMinute
	UnitHour
	UnitDayOfMonth
	UnitMonth
	UnitDayOfWeek
)

// String returns the name the original TypeScript enum uses.
func (u Unit) String() string {
	switch u {
	case UnitSecond:
		return "Second"
	case UnitMinute:
		return "Minute"
	case UnitHour:
		return "Hour"
	case UnitDayOfMonth:
		return "DayOfMonth"
	case UnitMonth:
		return "Month"
	case UnitDayOfWeek:
		return "DayOfWeek"
	}
	return "Unknown"
}

// Value is a single value in a cron field: either a number, or a token such as
// "L" (last) or "5L" (last Friday).
//
// The original stores these as a `number | string` union. Go has no untagged
// unions, and `any` would forfeit type safety and require a type switch at every
// use, so the two cases are made explicit. Text is empty exactly when the value
// is numeric, which keeps a single ordered slice sortable by the original's
// mixed comparator. See DECISIONS.md D3.
type Value struct {
	N    int
	Text string
}

func num(n int) Value     { return Value{N: n} }
func text(s string) Value { return Value{Text: s} }

// IsNumeric reports whether the value is a plain number rather than a token.
func (v Value) IsNumeric() bool { return v.Text == "" }

// String renders the value as it appears in an expression.
func (v Value) String() string {
	if v.IsNumeric() {
		return strconv.Itoa(v.N)
	}
	return v.Text
}

// compareValues orders values the way the original's CronField.sorter does:
// numbers ascending, tokens lexically, and every number before every token.
//
// The original uses localeCompare for the token case. The only tokens that can
// occur are "L", "W" and the digit-prefixed forms such as "5L", for which ICU
// collation and byte order agree.
func compareValues(a, b Value) int {
	switch {
	case a.IsNumeric() && b.IsNumeric():
		return a.N - b.N
	case !a.IsNumeric() && !b.IsNumeric():
		return strings.Compare(a.Text, b.Text)
	case a.IsNumeric():
		return -1
	default:
		return 1
	}
}

// Field-level regular expressions, mirroring the static validChars getters.
// The alternations after the first accept the hashed (H) forms, which contain
// parentheses that the leading character class would otherwise reject.
var (
	validCharsBase = regexp.MustCompile(
		`^[?,*\dH/-]+$|^.*H\(\d+-\d+\)/\d+.*$|^.*H\(\d+-\d+\).*$|^.*H/\d+.*$`)
	validCharsDayOfMonth = regexp.MustCompile(
		`^[?,*\dLH/-]+$|^.*H\(\d+-\d+\)/\d+.*$|^.*H\(\d+-\d+\).*$|^.*H/\d+.*$`)
	validCharsDayOfWeek = regexp.MustCompile(
		`^[?,*\dLH#/-]+$|^.*H\(\d+-\d+\)/\d+.*$|^.*H\(\d+-\d+\).*$|^.*H/\d+.*$`)
)

// fieldSpec is the immutable description of one cron field.
//
// The original models this as an abstract class with six subclasses that
// override static getters. Go has embedding but not method overriding, and the
// subclasses differ only in data (never in behaviour) so a table of
// descriptors expresses the same thing without six near-identical types. See
// DECISIONS.md D2.
type fieldSpec struct {
	unit Unit
	// name is the originating class name, which appears in error messages.
	name       string
	min, max   int
	chars      []rune
	validChars *regexp.Regexp
}

var (
	specSecond     = &fieldSpec{UnitSecond, "CronSecond", 0, 59, nil, validCharsBase}
	specMinute     = &fieldSpec{UnitMinute, "CronMinute", 0, 59, nil, validCharsBase}
	specHour       = &fieldSpec{UnitHour, "CronHour", 0, 23, nil, validCharsBase}
	specDayOfMonth = &fieldSpec{UnitDayOfMonth, "CronDayOfMonth", 1, 31, []rune{'L'}, validCharsDayOfMonth}
	specMonth      = &fieldSpec{UnitMonth, "CronMonth", 1, 12, nil, validCharsBase}
	specDayOfWeek  = &fieldSpec{UnitDayOfWeek, "CronDayOfWeek", 0, 7, []rune{'L'}, validCharsDayOfWeek}
)

// specFor returns the descriptor for a unit.
func specFor(u Unit) *fieldSpec {
	switch u {
	case UnitSecond:
		return specSecond
	case UnitMinute:
		return specMinute
	case UnitHour:
		return specHour
	case UnitDayOfMonth:
		return specDayOfMonth
	case UnitMonth:
		return specMonth
	default:
		return specDayOfWeek
	}
}

// matchesToken reports whether s is one of the field's special-character forms:
// up to two leading digits followed by a permitted character, so "L", "5L" and
// "15L" qualify but "123L" does not. This mirrors the original's
// `new RegExp("^\\d{0,2}" + char + "$")`.
func (s *fieldSpec) matchesToken(v string) bool {
	r := []rune(v)
	if len(r) == 0 {
		return false
	}
	last := r[len(r)-1]
	if !slices.Contains(s.chars, last) {
		return false
	}
	digits := r[:len(r)-1]
	if len(digits) > 2 {
		return false
	}
	for _, d := range digits {
		if d < '0' || d > '9' {
			return false
		}
	}
	return true
}

// fieldOptions carries what the parser knows about a field beyond its values.
type fieldOptions struct {
	// raw is the field's text as written, before expansion. It decides
	// wildcard-ness and the L/? flags.
	raw string
	// wildcard overrides the derived flag when non-nil.
	//
	// The parser never sets it, but the field constructors are public API and
	// callers may, so the override has to be honoured rather than derived away.
	wildcard *bool
	// nthDayOfWeek is the N of a `#N` suffix, or 0 when absent.
	nthDayOfWeek int
}

// Field is one validated cron field: an ordered set of permitted values plus the
// flags the matcher consults.
type Field struct {
	spec         *fieldSpec
	values       []Value
	wildcard     bool
	hasLast      bool
	hasQuestion  bool
	nthDayOfWeek int
	raw          string
}

// newField validates values and returns the field.
//
// values is copied before sorting. The original sorts in place, which mutates
// the caller's array, a bug reproduced nowhere in this port because Go slices
// share backing arrays in exactly the same way, so the naive translation would
// inherit it. See DECISIONS.md D4.
func newField(spec *fieldSpec, values []Value, opts fieldOptions) (*Field, error) {
	if len(values) == 0 {
		return nil, &FieldError{Field: spec.name, Kind: errKindEmpty}
	}

	sorted := slices.Clone(values)
	slices.SortStableFunc(sorted, compareValues)

	f := &Field{
		spec:         spec,
		values:       sorted,
		nthDayOfWeek: opts.nthDayOfWeek,
		raw:          opts.raw,
	}

	// The L and ? flags test the raw text as well as the values, so a token
	// that survived expansion is caught either way.
	f.hasLast = strings.Contains(opts.raw, "L") || slices.Contains(values, text("L"))
	f.hasQuestion = strings.Contains(opts.raw, "?") || slices.Contains(values, text("?"))

	if opts.wildcard != nil {
		f.wildcard = *opts.wildcard
	} else {
		f.wildcard = f.derivedWildcard()
	}

	if err := f.validate(); err != nil {
		return nil, err
	}
	return f, nil
}

// derivedWildcard mirrors the original's #isWildcardValue: when a raw value is
// present it must literally be * or ?, otherwise the field counts as a wildcard
// only if it covers its whole range. The second branch matters for fields built
// programmatically, which carry no raw text.
func (f *Field) derivedWildcard() bool {
	if len(f.raw) > 0 {
		return f.raw == "*" || f.raw == "?"
	}
	for v := f.spec.min; v <= f.spec.max; v++ {
		if !slices.Contains(f.values, num(v)) {
			return false
		}
	}
	return true
}

// validate checks every value against the field's range or permitted tokens,
// then rejects duplicates.
func (f *Field) validate() error {
	for _, v := range f.values {
		ok := false
		if v.IsNumeric() {
			ok = v.N >= f.spec.min && v.N <= f.spec.max
		} else {
			ok = f.spec.matchesToken(v.Text)
		}
		if !ok {
			return rangeError(f.spec, v)
		}
	}

	// The original writes `if (duplicate)` against the value returned by
	// Array.prototype.find, which stops at the FIRST duplicate. When that value
	// is 0 the test is falsy and no error is raised, and because the search
	// already stopped, later duplicates go unreported as well. Values are sorted
	// ascending, so a duplicated 0 masks every other duplicate in the field.
	//
	// Reproduced deliberately; reported upstream as bug 5. Checking each value
	// instead, which is the obvious translation, rejects "0,7,4,4" where the
	// original accepts it.
	for i, v := range f.values {
		if slices.Index(f.values, v) == i {
			continue
		}
		if v.IsNumeric() && v.N == 0 {
			return nil
		}
		return &FieldError{Field: f.spec.name, Kind: errKindDuplicate, Value: v}
	}
	return nil
}

// Unit reports which field this is.
func (f *Field) Unit() Unit { return f.spec.unit }

// Values returns the permitted values in ascending order. The slice is a copy;
// callers cannot disturb the field.
func (f *Field) Values() []Value { return slices.Clone(f.values) }

// Min and Max are the field's inclusive bounds.
func (f *Field) Min() int { return f.spec.min }
func (f *Field) Max() int { return f.spec.max }

// IsWildcard reports whether the field matches every value.
func (f *Field) IsWildcard() bool { return f.wildcard }

// HasLast reports whether the field carries an L token.
func (f *Field) HasLast() bool { return f.hasLast }

// HasQuestion reports whether the field was written as ?, which round-trips back
// to ? rather than * when the expression is rendered.
func (f *Field) HasQuestion() bool { return f.hasQuestion }

// NthDayOfWeek returns the N of a `#N` suffix, or 0 when there is none.
func (f *Field) NthDayOfWeek() int { return f.nthDayOfWeek }

// Raw returns the field's text as originally written.
func (f *Field) Raw() string { return f.raw }

// contains reports whether n is permitted.
func (f *Field) contains(n int) bool {
	return slices.Contains(f.values, num(n))
}

// findNearestInValues returns the next value strictly after current, or
// strictly before it when reverse is set. Token values are skipped: in the
// original they compare as NaN, which is never greater or less than anything.
func findNearestInValues(values []Value, current int, reverse bool) (int, bool) {
	if reverse {
		for i := len(values) - 1; i >= 0; i-- {
			if v := values[i]; v.IsNumeric() && v.N < current {
				return v.N, true
			}
		}
		return 0, false
	}
	for _, v := range values {
		if v.IsNumeric() && v.N > current {
			return v.N, true
		}
	}
	return 0, false
}

// findNearestValue is findNearestInValues over this field's own values.
func (f *Field) findNearestValue(current int, reverse bool) (int, bool) {
	return findNearestInValues(f.values, current, reverse)
}

// minOrMax returns the field's smallest permitted value, or its largest when
// reverse is set. It mirrors the original's #getMinOrMax, which indexes the
// sorted array directly.
func (f *Field) minOrMax(reverse bool) int {
	if reverse {
		return f.values[len(f.values)-1].N
	}
	return f.values[0].N
}
