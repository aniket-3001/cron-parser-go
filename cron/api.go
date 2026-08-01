package cron

// This file holds the constructors for assembling fields by hand, without
// parsing any text. They are what makes FromFields usable: without them a caller
// could hold a *Fields only by parsing an expression first.

// NewSecond returns a validated second field (0-59).
func NewSecond(values []Value) (*Field, error) {
	return newField(specSecond, values, fieldOptions{})
}

// NewMinute returns a validated minute field (0-59).
func NewMinute(values []Value) (*Field, error) {
	return newField(specMinute, values, fieldOptions{})
}

// NewHour returns a validated hour field (0-23).
func NewHour(values []Value) (*Field, error) {
	return newField(specHour, values, fieldOptions{})
}

// NewDayOfMonth returns a validated day-of-month field (1-31, or the token L).
func NewDayOfMonth(values []Value) (*Field, error) {
	return newField(specDayOfMonth, values, fieldOptions{})
}

// NewMonth returns a validated month field (1-12).
func NewMonth(values []Value) (*Field, error) {
	return newField(specMonth, values, fieldOptions{})
}

// NewDayOfWeek returns a validated day-of-week field (0-7, or a token such as
// 5L). Both 0 and 7 mean Sunday.
func NewDayOfWeek(values []Value) (*Field, error) {
	return newField(specDayOfWeek, values, fieldOptions{})
}

// Num builds a numeric field value.
func Num(n int) Value { return Value{N: n} }

// Text builds a token field value, such as "L" or "5L".
func Text(s string) Value { return Value{Text: s} }

// Nums builds a slice of numeric field values.
func Nums(ns ...int) []Value {
	out := make([]Value, len(ns))
	for i, n := range ns {
		out[i] = Value{N: n}
	}
	return out
}

// NewFields groups six fields into a validated set.
//
// Every field must be present and must be of the matching unit. The
// cross-field check rejects an explicit day of month that can never occur in
// the named month, but only when exactly one month is named and day-of-week is
// unrestricted, because otherwise the two day fields combine with OR and the
// unreachable day is harmless.
func NewFields(second, minute, hour, dayOfMonth, month, dayOfWeek *Field) (*Fields, error) {
	required := []struct {
		field *Field
		name  string
		unit  Unit
	}{
		{second, "second", UnitSecond},
		{minute, "minute", UnitMinute},
		{hour, "hour", UnitHour},
		{dayOfMonth, "dayOfMonth", UnitDayOfMonth},
		{month, "month", UnitMonth},
		{dayOfWeek, "dayOfWeek", UnitDayOfWeek},
	}

	for _, r := range required {
		if r.field == nil {
			return nil, validationError("Validation error, Field %s is missing", r.name)
		}
		if r.field.Unit() != r.unit {
			return nil, validationError("Validation error, Field %s has unit %s", r.name, r.field.Unit())
		}
	}

	f := &Fields{
		Second:     second,
		Minute:     minute,
		Hour:       hour,
		DayOfMonth: dayOfMonth,
		Month:      month,
		DayOfWeek:  dayOfWeek,
	}
	if err := validateFields(f); err != nil {
		return nil, err
	}
	return f, nil
}
