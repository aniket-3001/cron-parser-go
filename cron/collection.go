package cron

import "strings"

// fieldRange is a run of values recovered from an expanded field: a starting
// value, how many values the run covers, and optionally where it ends and what
// stride it uses.
//
// end and step are optional in the original, where absent means undefined. Both
// the absence and the value zero are meaningful — several checks in the original
// test them for truthiness, so zero and undefined take the same branch — which is
// why each carries its own presence flag rather than relying on a zero value.
type fieldRange struct {
	start   Value
	count   int
	end     int
	hasEnd  bool
	step    int
	hasStep bool
}

// truthyEnd reports whether the original's `range.end && ...` guard would pass.
// A zero end is falsy in JavaScript and so behaves as though it were absent.
func (r fieldRange) truthyEnd() bool { return r.hasEnd && r.end != 0 }

// truthyStep is the same test for `step`.
func (r fieldRange) truthyStep() bool { return r.hasStep && r.step != 0 }

// compactField folds a sorted list of values back into runs, so that a field
// expanded to [0 15 30 45] can be rendered as */15 rather than as a list.
//
// This is a close port of the original's algorithm, including the parts that are
// only correct by accident. It looks one element ahead to decide a run's stride,
// commits to that stride, and starts a new run as soon as an element breaks it.
func compactField(input []Value) []fieldRange {
	if len(input) == 0 {
		return nil
	}

	var output []fieldRange
	var current fieldRange
	haveCurrent := false

	for i, item := range input {
		if !haveCurrent {
			current = fieldRange{start: item, count: 1}
			haveCurrent = true
			continue
		}

		// The original reads `arr[i - 1] || current.start`, so a previous item
		// of zero is falsy and the run's start stands in for it. Values are
		// sorted ascending, so this only bites when the field permits 0.
		prevItem := input[i-1]
		if prevItem.IsNumeric() && prevItem.N == 0 {
			prevItem = current.start
		}

		var nextItem Value
		haveNext := i+1 < len(input)
		if haveNext {
			nextItem = input[i+1]
		}

		// Tokens terminate whatever run is open and stand alone. Sorting places
		// them last, so in practice this fires at most once, at the end.
		if !item.IsNumeric() && (item.Text == "L" || item.Text == "W") {
			output = append(output, current)
			output = append(output, fieldRange{start: item, count: 1})
			haveCurrent = false
			continue
		}

		if !current.hasStep && haveNext {
			// A non-numeric predecessor makes the stride NaN in the original,
			// and every comparison against NaN is false, so the else branch is
			// taken. Reproduced by treating it as "not less than or equal".
			stepDefined := prevItem.IsNumeric()
			step := 0
			if stepDefined {
				step = item.N - prevItem.N
			}
			nextStep := nextItem.N - item.N

			if stepDefined && step <= nextStep {
				current.count = 2
				current.end = item.N
				current.hasEnd = true
				current.step = step
				current.hasStep = true
				continue
			}
			current.step = 1
			current.hasStep = true
		}

		// `current.end ?? 0` keeps a genuine zero rather than substituting the
		// start, unlike the `||` above.
		end := 0
		if current.hasEnd {
			end = current.end
		}

		// The stride must have been established for the run to continue. In the
		// original this falls out of comparing against undefined, which is never
		// equal to a number; here it needs saying, because Go's zero value for
		// step is 0 and would wrongly extend a run of repeated zeros.
		if current.hasStep && item.N-end == current.step {
			current.count++
			current.end = item.N
			current.hasEnd = true
			continue
		}

		switch current.count {
		case 1:
			output = append(output, fieldRange{start: current.start, count: 1})
		case 2:
			// A run of two is emitted as two singletons rather than a range.
			output = append(output, fieldRange{start: current.start, count: 1})
			output = append(output, fieldRange{start: num(current.end), count: 1})
		default:
			output = append(output, current)
		}
		current = fieldRange{start: item, count: 1}
	}

	if haveCurrent {
		output = append(output, current)
	}
	return output
}

// formatSingleRange renders a field that compacted to exactly one run, when that
// run spans the field's whole range. It reports false when the run does not
// qualify and the general path should handle it.
func formatSingleRange(f *Field, r fieldRange, max int) (string, bool) {
	if !r.truthyStep() {
		return "", false
	}

	if r.step == 1 && r.start.IsNumeric() && r.start.N == f.Min() && r.truthyEnd() && r.end >= max {
		// A field written as ? round-trips back to ?, not to *.
		if f.HasQuestion() {
			return "?", true
		}
		return "*", true
	}

	if r.step != 1 && r.start.IsNumeric() && r.start.N == f.Min() && r.truthyEnd() && r.end >= max-r.step+1 {
		return "*/" + itoa(r.step), true
	}

	return "", false
}

// formatMultiRange renders a run covering more than one value.
//
// It can fail. The original guards these two cases with errors its author marked
// as unreachable, but they are reachable from ordinary input: a field holding
// repeated zeros compacts to a run with a stride of zero, because duplicate
// zeros escape validation. See upstream-issues/07.
func formatMultiRange(r fieldRange, max int) (string, error) {
	if r.step == 1 {
		return r.start.String() + "-" + itoa(r.end), nil
	}

	// A run starting at zero covers one more value than its count suggests,
	// because the first stride begins from the start itself. Computed before the
	// guards below, matching the original's ordering.
	multiplier := r.count
	if r.start.IsNumeric() && r.start.N == 0 {
		multiplier = r.count - 1
	}

	if !r.truthyStep() {
		return "", validationError("Unexpected range step")
	}
	// The original also guards against a falsy end here. That case cannot arise:
	// values are sorted ascending, so a run ending at zero holds nothing but
	// zeros, which forces a stride of zero and trips the guard above first.

	// When the stride would carry past the run's end, the run is too short to
	// be worth expressing as a stride and is listed explicitly instead.
	if r.step*multiplier > r.end {
		var parts []string
		for index := 0; index < r.end-r.start.N+1; index++ {
			if index%r.step == 0 {
				parts = append(parts, itoa(r.start.N+index))
			}
		}
		return strings.Join(parts, ","), nil
	}

	// A run reaching the last stride-aligned value in the field needs no end.
	if r.end == max-r.step+1 {
		return r.start.String() + "/" + itoa(r.step), nil
	}
	return r.start.String() + "-" + itoa(r.end) + "/" + itoa(r.step), nil
}

// formatField renders one field back to expression text.
//
// Two fields need context that the field itself does not carry, which is why
// this hangs off Fields rather than off Field.
func (f *Fields) formatField(field *Field) (string, error) {
	max := field.Max()
	values := field.values

	if field.Unit() == UnitDayOfWeek {
		// Day-of-week renders against 0-6. A trailing 7 is dropped because a
		// range ending on a multiple of 7 also injected a leading 0, so Sunday
		// is present twice and would otherwise be rendered twice.
		max = 6
		if n := len(values); n > 0 && values[n-1].IsNumeric() && values[n-1].N == 7 {
			values = values[:n-1]
		}
	}

	if field.Unit() == UnitDayOfMonth {
		// With exactly one month named, the field's maximum is that month's
		// length, so "1-30" in April renders as * rather than as a range.
		if len(f.Month.values) == 1 {
			max = daysInMonthTable[f.Month.values[0].N-1]
		}
	}

	ranges := compactField(values)

	if len(ranges) == 1 {
		if s, ok := formatSingleRange(field, ranges[0], max); ok {
			return s, nil
		}
	}

	parts := make([]string, len(ranges))
	for i, r := range ranges {
		value := r.start.String()
		if r.count != 1 {
			var err error
			if value, err = formatMultiRange(r, max); err != nil {
				return "", err
			}
		}
		if field.Unit() == UnitDayOfWeek && field.NthDayOfWeek() > 0 {
			value += "#" + itoa(field.NthDayOfWeek())
		}
		parts[i] = value
	}
	return strings.Join(parts, ","), nil
}

// Format renders the fields back to expression text.
//
// Seconds are omitted unless asked for, so the default output is the five-field
// form even when the expression was parsed from six.
//
// Formatting is very nearly the inverse of parsing, which makes for a property
// worth testing directly: parsing the output of Format should describe the same
// schedule. Two documented exceptions are in DECISIONS.md.
//
// It returns an error for fields that cannot be rendered, which a field holding
// repeated zeros cannot. The original throws in the same situation.
func (f *Fields) Format(includeSeconds bool) (string, error) {
	order := []*Field{f.Minute, f.Hour, f.DayOfMonth, f.Month, f.DayOfWeek}
	if includeSeconds {
		order = append([]*Field{f.Second}, order...)
	}

	parts := make([]string, len(order))
	for i, field := range order {
		s, err := f.formatField(field)
		if err != nil {
			return "", err
		}
		parts[i] = s
	}
	return strings.Join(parts, " "), nil
}

// SerializedField is the exported form of a single field.
type SerializedField struct {
	Wildcard bool    `json:"wildcard"`
	Values   []Value `json:"values"`
}

// SerializedFields is the exported form of a whole expression's fields.
type SerializedFields struct {
	Second     SerializedField `json:"second"`
	Minute     SerializedField `json:"minute"`
	Hour       SerializedField `json:"hour"`
	DayOfMonth SerializedField `json:"dayOfMonth"`
	Month      SerializedField `json:"month"`
	DayOfWeek  SerializedField `json:"dayOfWeek"`
}

// Serialize exposes the fields in a form suitable for transport.
func (f *Fields) Serialize() SerializedFields {
	one := func(field *Field) SerializedField {
		return SerializedField{Wildcard: field.IsWildcard(), Values: field.Values()}
	}
	return SerializedFields{
		Second:     one(f.Second),
		Minute:     one(f.Minute),
		Hour:       one(f.Hour),
		DayOfMonth: one(f.DayOfMonth),
		Month:      one(f.Month),
		DayOfWeek:  one(f.DayOfWeek),
	}
}

// Format renders the expression's fields back to text.
func (e *Expression) Format(includeSeconds bool) (string, error) {
	return e.fields.Format(includeSeconds)
}
