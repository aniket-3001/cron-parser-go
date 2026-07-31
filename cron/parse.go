package cron

import (
	"regexp"
	"slices"
	"strings"
)

// PredefinedExpressions are the @-prefixed shorthands, stored in six-field form.
//
// The last four are extensions rather than standard cron: no Unix implementation
// has @minutely, @secondly, @weekdays or @weekends.
var PredefinedExpressions = map[string]string{
	"@yearly":   "0 0 0 1 1 *",
	"@annually": "0 0 0 1 1 *",
	"@monthly":  "0 0 0 1 * *",
	"@weekly":   "0 0 0 * * 0",
	"@daily":    "0 0 0 * * *",
	"@hourly":   "0 0 * * * *",
	"@minutely": "0 * * * * *",
	"@secondly": "* * * * * *",
	"@weekdays": "0 0 0 * * 1-5",
	"@weekends": "0 0 0 * * 0,6",
}

// Month and day-of-week names, lowercase because the parser lowercases before
// looking up.
var (
	monthAliases = map[string]int{
		"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
		"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12,
	}
	dayOfWeekAliases = map[string]int{
		"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6,
	}
)

var (
	whitespaceRe = regexp.MustCompile(`\s+`)
	aliasRe      = regexp.MustCompile(`(?i)[a-z]{3}`)
	wildcardRe   = regexp.MustCompile(`[*?]`)
	hashedRe     = regexp.MustCompile(`H(?:\((\d+)-(\d+)\))?(?:/(\d+))?`)

	// nthIncompatibleRe reproduces the original's /([,-/])/. Inside a character
	// class `,-/` is a RANGE from 0x2C to 0x2F, which covers , - . and / — the
	// dot is unintended. Written out here so the four characters are visible;
	// reported upstream as bug 3.
	nthIncompatibleRe = regexp.MustCompile(`[,\-./]`)
)

// expandPredefined resolves an @-alias to its six-field form, leaving anything
// else untouched.
//
// The expansion happens before the text is retained, so an expression parsed
// from "@yearly" reports itself as "0 0 0 1 1 *" rather than as the alias. That
// is what the original does: it reassigns its local before passing it on.
func expandPredefined(expression string) string {
	if expanded, ok := PredefinedExpressions[expression]; ok {
		return expanded
	}
	return expression
}

// rawFields holds the six field strings before expansion.
type rawFields struct {
	second, minute, hour, dayOfMonth, month, dayOfWeek string
}

// Fields is the parsed form of an expression: six validated fields.
type Fields struct {
	Second     *Field
	Minute     *Field
	Hour       *Field
	DayOfMonth *Field
	Month      *Field
	DayOfWeek  *Field
}

// parseOptions carries what parsing needs from the caller.
type parseOptions struct {
	strict   bool
	hashSeed string
}

// parseFields turns an expression into six validated fields.
//
// The order of operations matters and is preserved from the original: a single
// PRNG is created up front and threaded through every field, so H values in
// different fields are correlated by position, and the fields are parsed in the
// order second, minute, hour, month, dayOfMonth, dayOfWeek. Note that month
// comes before dayOfMonth, which is not the order they appear in an expression;
// changing it would change every hashed expression's output.
func parseFields(expression string, opts parseOptions) (*Fields, error) {
	rand := seededRandom(opts.hashSeed)
	expression = expandPredefined(expression)

	raw, err := getRawFields(expression, opts.strict)
	if err != nil {
		return nil, err
	}

	if opts.strict && raw.dayOfMonth != "*" && raw.dayOfWeek != "*" {
		return nil, syntaxError("Cannot use both dayOfMonth and dayOfWeek together in strict mode!")
	}

	second, err := parseField(UnitSecond, raw.second, rand)
	if err != nil {
		return nil, err
	}
	minute, err := parseField(UnitMinute, raw.minute, rand)
	if err != nil {
		return nil, err
	}
	hour, err := parseField(UnitHour, raw.hour, rand)
	if err != nil {
		return nil, err
	}
	month, err := parseField(UnitMonth, raw.month, rand)
	if err != nil {
		return nil, err
	}
	dayOfMonth, err := parseField(UnitDayOfMonth, raw.dayOfMonth, rand)
	if err != nil {
		return nil, err
	}

	dowText, nth, err := parseNthDay(raw.dayOfWeek)
	if err != nil {
		return nil, err
	}
	dayOfWeek, err := parseField(UnitDayOfWeek, dowText, rand)
	if err != nil {
		return nil, err
	}

	build := func(u Unit, vals []Value, rawText string, nth int) (*Field, error) {
		return newField(specFor(u), vals, fieldOptions{raw: rawText, nthDayOfWeek: nth})
	}

	f := &Fields{}
	if f.Second, err = build(UnitSecond, second, raw.second, 0); err != nil {
		return nil, err
	}
	if f.Minute, err = build(UnitMinute, minute, raw.minute, 0); err != nil {
		return nil, err
	}
	if f.Hour, err = build(UnitHour, hour, raw.hour, 0); err != nil {
		return nil, err
	}
	if f.DayOfMonth, err = build(UnitDayOfMonth, dayOfMonth, raw.dayOfMonth, 0); err != nil {
		return nil, err
	}
	if f.Month, err = build(UnitMonth, month, raw.month, 0); err != nil {
		return nil, err
	}
	// The day-of-week field keeps the original raw text, including any `#N`
	// suffix, because hasLastChar inspects it.
	if f.DayOfWeek, err = build(UnitDayOfWeek, dayOfWeek, raw.dayOfWeek, nth); err != nil {
		return nil, err
	}

	if err := validateFields(f); err != nil {
		return nil, err
	}
	return f, nil
}

// validateFields reproduces the collection-level check from
// CronFieldCollection's constructor.
//
// Only an explicit day of month that can never occur is rejected, and only when
// exactly one month is named and day of week is unrestricted — a restricted day
// of week rescues the expression, since the two combine with OR. Note that just
// the FIRST day-of-month value is inspected, so "0 0 30,31 2 *" is rejected
// while "0 0 1,31 2 *" is accepted and simply never matches.
func validateFields(f *Fields) error {
	months := f.Month.values
	if len(months) != 1 || f.DayOfMonth.HasLast() || !f.DayOfWeek.IsWildcard() {
		return nil
	}
	first := f.DayOfMonth.values[0]
	day, ok := jsParseInt(first.String())
	// daysInMonthTable is the original's leap-permissive table, so February
	// admits 29 regardless of year.
	if !ok || day > daysInMonthTable[months[0].N-1] {
		return validationError("Invalid explicit day of month definition")
	}
	return nil
}

// getRawFields splits an expression into its six fields, padding short forms.
func getRawFields(expression string, strict bool) (rawFields, error) {
	if strict && expression == "" {
		return rawFields{}, syntaxError("Invalid cron expression")
	}
	if expression == "" {
		expression = "0 * * * * *"
	}

	trimmed := strings.TrimSpace(expression)
	var atoms []string
	if trimmed == "" {
		// "".split(/\s+/) yields [""] in JavaScript, not an empty array. Go's
		// Split on an empty string would drop the element and change how many
		// defaults get prepended.
		atoms = []string{""}
	} else {
		atoms = whitespaceRe.Split(trimmed, -1)
	}

	if strict && len(atoms) < 6 {
		return rawFields{}, syntaxError("Invalid cron expression, expected 6 fields")
	}
	if len(atoms) > 6 {
		return rawFields{}, syntaxError("Invalid cron expression, too many fields")
	}

	// Defaults are taken from the END of this list and prepended, so a
	// five-field expression gains a "0" seconds field. Shorter forms take more
	// defaults and end up misaligned — a one-atom expression puts that atom in
	// dayOfWeek and "0" in month — which is faithful to the original.
	defaults := []string{"*", "*", "*", "*", "*", "0"}
	if len(atoms) < len(defaults) {
		atoms = append(slices.Clone(defaults[len(atoms):]), atoms...)
	}

	return rawFields{
		second: atoms[0], minute: atoms[1], hour: atoms[2],
		dayOfMonth: atoms[3], month: atoms[4], dayOfWeek: atoms[5],
	}, nil
}

// parseField expands one field into the explicit list of values it permits.
func parseField(u Unit, value string, rand prng) ([]Value, error) {
	spec := specFor(u)

	if u == UnitMonth || u == UnitDayOfWeek {
		var aliasErr error
		value = aliasRe.ReplaceAllStringFunc(value, func(match string) string {
			lower := strings.ToLower(match)
			if n, ok := monthAliases[lower]; ok {
				return itoa(n)
			}
			if n, ok := dayOfWeekAliases[lower]; ok {
				return itoa(n)
			}
			if aliasErr == nil {
				aliasErr = validationError("Validation error, cannot resolve alias %q", lower)
			}
			return match
		})
		if aliasErr != nil {
			return nil, aliasErr
		}
	}

	if !spec.validChars.MatchString(value) {
		return nil, syntaxError("Invalid characters, got value: %s", value)
	}

	value = wildcardRe.ReplaceAllString(value, itoa(spec.min)+"-"+itoa(spec.max))

	value, err := parseHashed(value, spec, rand)
	if err != nil {
		return nil, err
	}
	return parseSequence(u, value, spec)
}

// parseHashed resolves H tokens into concrete values.
//
// The generator is advanced exactly once per field, before any replacement, and
// unconditionally — the original draws its random value at the top of the
// function, whether or not the field contains an H. Skipping the draw for fields
// without an H would desynchronise the shared generator and change the values
// every later field resolves to.
func parseHashed(value string, spec *fieldSpec, rand prng) (string, error) {
	randomValue := rand()

	matches := hashedRe.FindAllStringSubmatchIndex(value, -1)
	if len(matches) == 0 {
		return value, nil
	}

	var b strings.Builder
	last := 0
	for _, m := range matches {
		b.WriteString(value[last:m[0]])

		// Optional groups report -1 when they did not participate.
		group := func(i int) string {
			if m[2*i] < 0 {
				return ""
			}
			return value[m[2*i]:m[2*i+1]]
		}

		replacement, err := hashReplacement(group(1), group(2), group(3), spec, randomValue)
		if err != nil {
			return "", err
		}
		b.WriteString(replacement)
		last = m[1]
	}
	b.WriteString(value[last:])
	return b.String(), nil
}

// hashReplacement produces the text a single H token expands to.
func hashReplacement(minTxt, maxTxt, stepTxt string, spec *fieldSpec, r float64) (string, error) {
	switch {
	case minTxt != "" && maxTxt != "" && stepTxt != "": // H(a-b)/step
		lo, _ := jsParseInt(minTxt)
		hi, _ := jsParseInt(maxTxt)
		step, _ := jsParseInt(stepTxt)
		if lo > hi {
			return "", constraintError("Invalid range: %d-%d, min > max", lo, hi)
		}
		if step <= 0 {
			return "", constraintError("Invalid step: %d, must be positive", step)
		}
		start := max(lo, spec.min)
		return joinStepped(start/step*step+int(r*float64(step)), hi, step, start), nil

	case minTxt != "" && maxTxt != "": // H(a-b)
		lo, _ := jsParseInt(minTxt)
		hi, _ := jsParseInt(maxTxt)
		if lo > hi {
			return "", constraintError("Invalid range: %d-%d, min > max", lo, hi)
		}
		return itoa(int(r*float64(hi-lo+1)) + lo), nil

	case stepTxt != "": // H/step
		step, _ := jsParseInt(stepTxt)
		if step <= 0 {
			return "", constraintError("Invalid step: %d, must be positive", step)
		}
		return joinStepped(spec.min/step*step+int(r*float64(step)), spec.max, step, spec.min), nil

	default: // bare H
		return itoa(int(r*float64(spec.max-spec.min+1) + float64(spec.min))), nil
	}
}

// joinStepped walks from start to limit by step, keeping values at or above
// floor, and joins them with commas.
func joinStepped(start, limit, step, floor int) string {
	var parts []string
	for i := start; i <= limit; i += step {
		if i >= floor {
			parts = append(parts, itoa(i))
		}
	}
	return strings.Join(parts, ",")
}

// parseSequence expands a comma-separated list into concrete values.
func parseSequence(u Unit, val string, spec *fieldSpec) ([]Value, error) {
	var stack []Value

	for _, atom := range strings.Split(val, ",") {
		if atom == "" {
			return nil, syntaxError("Invalid list value format")
		}

		list, single, isList, err := parseRepeat(u, atom, spec)
		if err != nil {
			return nil, err
		}

		if isList {
			// Ranges are appended verbatim. The %7 normalisation below is NOT
			// applied here, which is why a day-of-week range keeps its 7 while
			// a bare 7 becomes 0. See SEMANTICS.md 2.2.
			for _, n := range list {
				stack = append(stack, num(n))
			}
			continue
		}

		if isConstraintToken(spec, single) {
			stack = append(stack, single)
			continue
		}

		v, ok := jsParseInt(single.String())
		if !ok || v < spec.min || v > spec.max {
			return nil, constraintError("Constraint error, got value %s expected range %d-%d",
				single, spec.min, spec.max)
		}
		if u == UnitDayOfWeek {
			stack = append(stack, num(v%7))
		} else {
			stack = append(stack, single)
		}
	}
	return stack, nil
}

// isConstraintToken reports whether a value contains one of the field's special
// characters, mirroring #isValidConstraintChar. Note it tests containment, not
// equality, so "5L" qualifies.
func isConstraintToken(spec *fieldSpec, v Value) bool {
	s := v.String()
	for _, c := range spec.chars {
		if strings.ContainsRune(s, c) {
			return true
		}
	}
	return false
}

// parseRepeat handles the `/step` suffix, returning either an expanded list or a
// single value.
func parseRepeat(u Unit, val string, spec *fieldSpec) (list []int, single Value, isList bool, err error) {
	atoms := strings.Split(val, "/")
	if len(atoms) > 2 {
		return nil, Value{}, false, syntaxError("Invalid repeat: %s", val)
	}

	if len(atoms) == 2 {
		lhs := atoms[0]
		if !strings.Contains(lhs, "-") {
			// A bare start with a step runs to the field's maximum, so "5/10"
			// means "5-59/10" in a minute field.
			lhs = lhs + "-" + itoa(spec.max)
		}
		step, stepOK := jsParseInt(atoms[1])
		return parseRange(u, lhs, step, stepOK, spec)
	}

	return parseRange(u, val, 1, true, spec)
}

// parseRange expands "a-b" into values, or passes a single value through.
func parseRange(u Unit, val string, step int, stepOK bool, spec *fieldSpec) (list []int, single Value, isList bool, err error) {
	atoms := strings.Split(val, "-")
	if len(atoms) <= 1 {
		// Not a range. Numeric text becomes a number; anything else stays a
		// token, which is how L and 5L survive parsing.
		if f, ok := jsNumber(val); ok {
			return nil, num(int(f)), false, nil
		}
		return nil, text(val), false, nil
	}

	lo, loOK := jsParseInt(atoms[0])
	hi, hiOK := jsParseInt(atoms[1])

	if !loOK || !hiOK || lo < spec.min || hi > spec.max {
		return nil, Value{}, false, constraintError(
			"Constraint error, got range %s-%s expected range %d-%d",
			numText(lo, loOK), numText(hi, hiOK), spec.min, spec.max)
	}
	if lo > hi {
		return nil, Value{}, false, constraintError("Invalid range: %d-%d, min(%d) > max(%d)", lo, hi, lo, hi)
	}
	if !stepOK || step <= 0 {
		return nil, Value{}, false, constraintError("Constraint error, cannot repeat at every %s time.", numText(step, stepOK))
	}

	return createRange(u, lo, hi, step), Value{}, true, nil
}

// createRange materialises a range.
//
// The day-of-week special case is the source of the duplicate Sunday documented
// in SEMANTICS.md 2.2: when the range ends on a multiple of 7 a leading 0 is
// injected, while the 7 itself stays in the list. So "5-7" yields [0,5,6,7],
// meaning Sunday, Friday, Saturday, with Sunday represented twice.
func createRange(u Unit, lo, hi, step int) []int {
	var stack []int
	if u == UnitDayOfWeek && hi%7 == 0 {
		stack = append(stack, 0)
	}
	for i := lo; i <= hi; i += step {
		if !slices.Contains(stack, i) {
			stack = append(stack, i)
		}
	}
	return stack
}

// parseNthDay splits a `#N` suffix off the day-of-week field.
func parseNthDay(val string) (dayOfWeek string, nth int, err error) {
	atoms := strings.Split(val, "#")
	if len(atoms) <= 1 {
		return atoms[0], 0, nil
	}

	nthValue, nthOK := jsNumber(atoms[len(atoms)-1])

	// The list/range/step characters cannot combine with #. The character class
	// also matches "." by accident; see nthIncompatibleRe.
	if m := nthIncompatibleRe.FindString(val); m != "" {
		return "", 0, constraintError(
			"Constraint error, invalid dayOfWeek `#` and `%s` special characters are incompatible", m)
	}

	if len(atoms) > 2 || !nthOK || nthValue < 1 || nthValue > 5 {
		return "", 0, constraintError("Constraint error, invalid dayOfWeek occurrence number (#)")
	}
	return atoms[0], int(nthValue), nil
}
