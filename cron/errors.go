package cron

import (
	"errors"
	"fmt"
)

// Sentinel errors for classification with errors.Is. Callers that only need to
// know "was this a bad expression?" match on these; callers that need detail
// use errors.As with the concrete types below.
var (
	// ErrValidation covers field values that cannot be represented — out of
	// range, duplicated, or empty.
	ErrValidation = errors.New("cron: validation error")
	// ErrConstraint covers values that are syntactically fine but violate a
	// field's bounds, such as a range whose end exceeds the maximum.
	ErrConstraint = errors.New("cron: constraint error")
	// ErrSyntax covers expressions that cannot be parsed at all.
	ErrSyntax = errors.New("cron: syntax error")
	// ErrOutOfBounds is reported when iteration leaves the configured
	// start/end window.
	ErrOutOfBounds = errors.New("cron: out of the time span range")
	// ErrLoopLimit is reported when a search exceeds the iteration limit,
	// which means the expression can never match.
	ErrLoopLimit = errors.New("cron: loop limit exceeded")
)

// fieldErrorKind distinguishes the field validation failures.
type fieldErrorKind int

const (
	errKindEmpty fieldErrorKind = iota
	errKindRange
	errKindDuplicate
)

// The original also reports "values is not an array" when handed a non-array.
// That check has no counterpart here: the signature accepts []Value, so the
// failure is unrepresentable and the message is unreachable. The reference test
// that exercises it passes a bad type from JavaScript, which the test bridge
// rejects before it reaches this package.

// FieldError reports an invalid value in a cron field.
//
// Error renders the message the original TypeScript library produces, because 42
// of the reference tests assert on the exact text. Keeping that rendering here
// rather than in the test bridge means there is one source of truth: the bridge
// forwards the message instead of maintaining a translation table that could
// drift. Go convention would prefer lowercase, unpunctuated error strings; the
// deviation is deliberate and recorded in DECISIONS.md D6.
type FieldError struct {
	// Field is the originating class name — "CronSecond", "CronMinute" and so
	// on — because it appears verbatim in the message.
	Field string
	Kind  fieldErrorKind
	Value Value
	Min   int
	Max   int
	// Chars lists the field's permitted special characters, rendered as a
	// suffix on range errors when non-empty.
	Chars []rune
}

func (e *FieldError) Error() string {
	switch e.Kind {
	case errKindEmpty:
		return fmt.Sprintf("%s Validation error, values contains no values", e.Field)
	case errKindDuplicate:
		return fmt.Sprintf("%s Validation error, duplicate values found: %s", e.Field, e.Value)
	default:
		suffix := ""
		if len(e.Chars) > 0 {
			suffix = fmt.Sprintf(" or chars %s", string(e.Chars))
		}
		return fmt.Sprintf("%s Validation error, got value %s expected range %d-%d%s",
			e.Field, e.Value, e.Min, e.Max, suffix)
	}
}

func (e *FieldError) Unwrap() error { return ErrValidation }

// newFieldError is a convenience for the common range failure.
func rangeError(spec *fieldSpec, v Value) *FieldError {
	return &FieldError{
		Field: spec.name, Kind: errKindRange, Value: v,
		Min: spec.min, Max: spec.max, Chars: spec.chars,
	}
}

// ExpressionError reports a malformed expression. The message reproduces the
// original's wording; the wrapped sentinel supports errors.Is.
type ExpressionError struct {
	msg      string
	sentinel error
}

func (e *ExpressionError) Error() string { return e.msg }
func (e *ExpressionError) Unwrap() error { return e.sentinel }

func syntaxError(format string, args ...any) *ExpressionError {
	return &ExpressionError{msg: fmt.Sprintf(format, args...), sentinel: ErrSyntax}
}

func constraintError(format string, args ...any) *ExpressionError {
	return &ExpressionError{msg: fmt.Sprintf(format, args...), sentinel: ErrConstraint}
}

func validationError(format string, args ...any) *ExpressionError {
	return &ExpressionError{msg: fmt.Sprintf(format, args...), sentinel: ErrValidation}
}
