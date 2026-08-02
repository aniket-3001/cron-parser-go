package cron

import (
	"iter"
	"time"
)

// loopLimit caps the search. An expression such as "0 0 30 2 *", the 30th of
// February, can never match, so without a cap the search would spin forever.
const loopLimit = 10000

// config collects the settings Parse accepts.
type config struct {
	loc      *time.Location
	current  *time.Time
	start    *time.Time
	end      *time.Time
	hashSeed string
	strict   bool
}

// Option configures Parse.
type Option func(*config)

// WithLocation evaluates the expression in loc. Defaults to time.Local.
func WithLocation(loc *time.Location) Option {
	return func(c *config) { c.loc = loc }
}

// WithCurrent sets the instant iteration starts from. Defaults to now.
func WithCurrent(t time.Time) Option {
	return func(c *config) { c.current = &t }
}

// WithStart refuses to produce any instant before t.
func WithStart(t time.Time) Option {
	return func(c *config) { c.start = &t }
}

// WithEnd refuses to produce any instant after t.
func WithEnd(t time.Time) Option {
	return func(c *config) { c.end = &t }
}

// WithHashSeed makes H field values deterministic. Without it they are drawn
// from a random source on every parse, so a fleet sharing a seed spreads its
// work while an unseeded caller gets scattered values.
func WithHashSeed(seed string) Option {
	return func(c *config) { c.hashSeed = seed }
}

// WithStrict requires six fields and forbids restricting day-of-month and
// day-of-week together.
func WithStrict() Option {
	return func(c *config) { c.strict = true }
}

// Expression is a parsed cron expression and a cursor into the schedule it
// describes.
//
// It is a calculator, not a scheduler: nothing here runs on a timer. Next and
// Prev advance an internal cursor, so repeated calls walk the schedule.
//
// An Expression is not safe for concurrent use; the cursor is mutable state.
type Expression struct {
	fields *Fields
	loc    *time.Location
	raw    string

	current    *cronTime
	initial    *cronTime
	start, end *cronTime

	// The DST-transition check is memoised per calendar day so the search loop
	// does not recompute it on every iteration.
	dstDayKey string
	dstDayHit bool

	// tracing records each date operation the search performs.
	//
	// It exists for the test bridge. The original exposes its date arithmetic as
	// a method and its tests spy on that method to check that iteration jumps to
	// the next matching value rather than stepping towards it one unit at a
	// time. With the search loop here rather than in JavaScript, the only way to
	// answer that question across the boundary is to report what actually
	// happened. Nothing else reads these fields, and they are off by default.
	tracing bool
	trace   []traceOp
}

// traceOp is one recorded date operation.
type traceOp struct {
	op       dateMathOp
	unit     timeUnit
	hoursLen int
}

// applyOp performs a date operation, recording it when tracing is on.
//
// Every date operation the search makes goes through here, so the recording is
// a log of what the engine did rather than a description of what it intends. An
// engine that started stepping hour by hour would produce a longer log, which
// is what keeps the original's tests meaningful when replayed against it.
func (e *Expression) applyOp(c *cronTime, op dateMathOp, u timeUnit, hoursLen int) {
	if e.tracing {
		e.trace = append(e.trace, traceOp{op: op, unit: u, hoursLen: hoursLen})
	}
	c.applyDateOperation(op, u, hoursLen)
}

// Parse compiles a cron expression.
//
// Five or six fields are accepted; with five, a "0" seconds field is prepended.
func Parse(expression string, opts ...Option) (*Expression, error) {
	cfg := config{loc: time.Local}
	for _, o := range opts {
		o(&cfg)
	}

	fields, err := parseFields(expression, parseOptions{
		strict:   cfg.strict,
		hashSeed: cfg.hashSeed,
	})
	if err != nil {
		return nil, err
	}
	return newExpression(fields, cfg, expandPredefined(expression)), nil
}

// FromFields builds an Expression from fields assembled directly, without
// parsing any text.
func FromFields(fields *Fields, opts ...Option) *Expression {
	cfg := config{loc: time.Local}
	for _, o := range opts {
		o(&cfg)
	}
	return newExpression(fields, cfg, "")
}

// newExpression assembles an Expression. Both entry points seed cfg.loc with
// time.Local before applying options, so a nil location cannot reach here.
func newExpression(fields *Fields, cfg config, raw string) *Expression {
	loc := cfg.loc
	e := &Expression{fields: fields, loc: loc, raw: raw}
	if cfg.start != nil {
		e.start = newCronTime(*cfg.start, loc)
	}
	if cfg.end != nil {
		e.end = newCronTime(*cfg.end, loc)
	}

	// The cursor starts at the requested instant, or at the start bound when
	// only that was given. Requesting an instant outside the bounds clamps to
	// the nearer bound rather than failing immediately.
	start := time.Now()
	switch {
	case cfg.current != nil:
		start = *cfg.current
	case cfg.start != nil:
		start = *cfg.start
	}
	cursor := newCronTime(start, loc)
	if cfg.current != nil || cfg.start != nil {
		switch {
		case e.start != nil && cursor.UnixMilli() < e.start.UnixMilli():
			cursor = e.start.clone()
		case e.end != nil && cursor.UnixMilli() > e.end.UnixMilli():
			cursor = e.end.clone()
		}
	}

	e.current = cursor
	e.initial = cursor.clone()
	return e
}

// Fields returns the parsed fields.
func (e *Expression) Fields() *Fields { return e.fields }

// String returns the expression as written, falling back to a rendering of the
// fields for expressions assembled without text.
//
// Rendering can fail for a field holding repeated zeros; String reports the
// empty string in that case, since a Stringer cannot return an error. Callers
// that need to know should use Format.
func (e *Expression) String() string {
	if e.raw != "" {
		return e.raw
	}
	s, err := e.fields.Format(true)
	if err != nil {
		return ""
	}
	return s
}

// Next advances the cursor and returns the next matching instant.
//
// The result is exclusive of the current cursor: calling Next when the cursor
// sits exactly on a matching instant returns the following one.
func (e *Expression) Next() (time.Time, error) {
	ct, err := e.findSchedule(false)
	if err != nil {
		return time.Time{}, err
	}
	return ct.Time(), nil
}

// Prev moves the cursor backwards and returns the previous matching instant.
func (e *Expression) Prev() (time.Time, error) {
	ct, err := e.findSchedule(true)
	if err != nil {
		return time.Time{}, err
	}
	return ct.Time(), nil
}

// HasNext reports whether a further instant exists, leaving the cursor where it
// was.
func (e *Expression) HasNext() bool {
	saved := e.current
	_, err := e.findSchedule(false)
	e.current = saved
	return err == nil
}

// HasPrev reports whether an earlier instant exists, leaving the cursor where it
// was.
func (e *Expression) HasPrev() bool {
	saved := e.current
	_, err := e.findSchedule(true)
	e.current = saved
	return err == nil
}

// Take returns the next n instants, or the previous |n| when n is negative.
//
// It stops early and returns what it has if the schedule runs out, matching the
// original, which swallows the error rather than propagating it.
func (e *Expression) Take(n int) []time.Time {
	var out []time.Time
	step := e.Next
	count := n
	if n < 0 {
		step = e.Prev
		count = -n
	}
	for i := 0; i < count; i++ {
		t, err := step()
		if err != nil {
			return out
		}
		out = append(out, t)
	}
	return out
}

// Reset returns the cursor to its initial position, or to t when given.
func (e *Expression) Reset(t ...time.Time) {
	if len(t) > 0 {
		e.current = newCronTime(t[0], e.loc)
		return
	}
	e.current = e.initial.clone()
}

// All iterates the schedule forwards from the current cursor, stopping when the
// schedule is exhausted.
//
// This replaces the original's Symbol.iterator. Range-over-func is the idiomatic
// Go equivalent: a channel-based iterator would leak a goroutine whenever the
// consumer stops early.
func (e *Expression) All() iter.Seq[time.Time] {
	return func(yield func(time.Time) bool) {
		for {
			t, err := e.Next()
			if err != nil || !yield(t) {
				return
			}
		}
	}
}

// Includes reports whether the expression fires at exactly t.
//
// It returns an error only for the malformed day-of-week form that the original
// also rejects at iteration time rather than at parse time.
func (e *Expression) Includes(t time.Time) (bool, error) {
	ct := newCronTime(t, e.loc)

	if !e.fields.Second.contains(ct.Second()) ||
		!e.fields.Minute.contains(ct.Minute()) ||
		!e.fields.Hour.contains(ct.Hour()) ||
		!e.fields.Month.contains(int(ct.Month())) {
		return false, nil
	}
	return e.matchDayOfMonth(ct)
}

// validateTimeSpan reports an error when the cursor has left the configured
// window.
func (e *Expression) validateTimeSpan(ct *cronTime) error {
	if e.start == nil && e.end == nil {
		return nil
	}
	now := ct.UnixMilli()
	if e.start != nil && now < e.start.UnixMilli() {
		return &ExpressionError{msg: "Out of the time span range", sentinel: ErrOutOfBounds}
	}
	if e.end != nil && now > e.end.UnixMilli() {
		return &ExpressionError{msg: "Out of the time span range", sentinel: ErrOutOfBounds}
	}
	return nil
}

// findSchedule walks time until every field matches.
//
// The order of the checks is load-bearing: day is tested before month, which is
// not the order a reader expects but is what the original does. Two of the
// reference tests observe the resulting sequence of date operations, and any
// reordering changes it even when the returned instants agree.
func (e *Expression) findSchedule(reverse bool) (*cronTime, error) {
	op := opAdd
	if reverse {
		op = opSubtract
	}

	current := e.current.clone()
	startedAt := current.UnixMilli()
	hoursLen := len(e.fields.Hour.values)

	stepCount := 0
	for {
		stepCount++
		if stepCount >= loopLimit {
			break
		}

		if err := e.validateTimeSpan(current); err != nil {
			return nil, err
		}

		ok, err := e.matchDayOfMonth(current)
		if err != nil {
			return nil, err
		}
		if !ok {
			e.applyOp(current, op, unitDay, hoursLen)
			continue
		}

		if !e.fields.Month.contains(int(current.Month())) {
			e.applyOp(current, op, unitMonth, hoursLen)
			continue
		}

		if !e.matchHour(current, op, reverse) {
			continue
		}

		if !e.fields.Minute.contains(current.Minute()) {
			e.moveToNextMinute(current, op, reverse)
			continue
		}

		if !e.fields.Second.contains(current.Second()) {
			e.moveToNextSecond(current, op, reverse)
			continue
		}

		if startedAt == current.UnixMilli() {
			// Landing back on the starting instant means the caller asked for
			// the *next* occurrence while sitting exactly on one. Step a second
			// and keep looking.
			//
			// A backwards search that began on a sub-second offset is the
			// exception: stripping the milliseconds below already yields an
			// earlier matching instant, so it is accepted instead of spinning
			// to the loop limit.
			if op == opAdd || current.Millisecond() == 0 {
				e.applyOp(current, op, unitSecond, hoursLen)
				continue
			}
		}
		break
	}

	if stepCount >= loopLimit {
		return nil, &ExpressionError{
			msg:      "Invalid expression, loop limit exceeded",
			sentinel: ErrLoopLimit,
		}
	}

	if current.Millisecond() != 0 {
		current.setMillisecond(0)
	}
	e.current = current
	return current, nil
}

// matchHour tests the hour and, when it does not match, moves the cursor toward
// one that might.
//
// It returns true only when the cursor may proceed to the minute check. The
// daylight-saving handling here is what makes schedules survive transitions.
func (e *Expression) matchHour(current *cronTime, op dateMathOp, reverse bool) bool {
	hour := e.fields.Hour
	hoursLen := len(hour.values)
	currentHour := current.Hour()

	isMatch := hour.contains(currentHour)
	isDstEnd := current.dstEnd == currentHour

	// Spring forward: the scheduled hour does not exist on this day, so the
	// first hour after the gap stands in for it.
	if current.dstStart != -1 && current.dstStart == currentHour-1 {
		if hour.contains(current.dstStart) {
			return true
		}
	}

	// Fall back: the hour repeats. Skip the second pass when moving forwards so
	// the same schedule is not produced twice.
	if isDstEnd && !reverse {
		current.dstEnd = -1
		e.applyOp(current, opAdd, unitHour, hoursLen)
		return false
	}

	if isMatch {
		return true
	}

	current.dstStart = -1
	nextHour, ok := hour.findNearestValue(currentHour, reverse)
	if !ok {
		// No matching hour remains today; skip the whole day rather than
		// stepping hour by hour to its end.
		e.applyOp(current, op, unitDay, hoursLen)
		return false
	}

	if e.checkDstTransition(current) {
		// Setting the hour directly could land on a time that does not exist or
		// occurs twice, which would bypass the dstStart/dstEnd bookkeeping that
		// applyDateOperation maintains. On transition days, step instead.
		steps := nextHour - currentHour
		if reverse {
			steps = currentHour - nextHour
		}
		for i := 0; i < steps; i++ {
			e.applyOp(current, op, unitHour, hoursLen)
			// A spring-forward step can cross two wall-clock hours, so stop as
			// soon as the target is reached or passed.
			if !reverse && current.Hour() >= nextHour {
				break
			}
			if reverse && current.Hour() <= nextHour {
				break
			}
		}
	} else {
		current.setHour(nextHour)
	}

	current.setMinute(e.fields.Minute.minOrMax(reverse))
	current.setSecond(e.fields.Second.minOrMax(reverse))
	return false
}

// moveToNextMinute advances to the next permitted minute, rolling into the next
// hour when none remains.
func (e *Expression) moveToNextMinute(current *cronTime, op dateMathOp, reverse bool) {
	if next, ok := e.fields.Minute.findNearestValue(current.Minute(), reverse); ok {
		current.setMinute(next)
		current.setSecond(e.fields.Second.minOrMax(reverse))
		return
	}
	e.applyOp(current, op, unitHour, len(e.fields.Hour.values))
	current.setMinute(e.fields.Minute.minOrMax(reverse))
	current.setSecond(e.fields.Second.minOrMax(reverse))
}

// moveToNextSecond advances to the next permitted second, rolling into the next
// minute when none remains.
func (e *Expression) moveToNextSecond(current *cronTime, op dateMathOp, reverse bool) {
	if next, ok := e.fields.Second.findNearestValue(current.Second(), reverse); ok {
		current.setSecond(next)
		return
	}
	e.applyOp(current, op, unitMinute, len(e.fields.Hour.values))
	current.setSecond(e.fields.Second.minOrMax(reverse))
}

// checkDstTransition reports whether the cursor's calendar day contains a
// daylight-saving transition, memoised per day.
func (e *Expression) checkDstTransition(current *cronTime) bool {
	key := itoa(current.Year()) + "-" + itoa(int(current.Month())) + "-" + itoa(current.Day())
	if e.dstDayKey == key {
		return e.dstDayHit
	}

	startOfDay := current.clone()
	startOfDay.startOfDay()
	endOfDay := current.clone()
	endOfDay.endOfDay()

	e.dstDayKey = key
	e.dstDayHit = startOfDay.OffsetMinutes() != endOfDay.OffsetMinutes()
	return e.dstDayHit
}

// matchDayOfMonth applies the three-rule day matching inherited from Unix cron.
//
//	Rule 1  both day-of-month and day-of-week restricted -> EITHER may match
//	Rule 2  day-of-month restricted, day-of-week open    -> day-of-month must match
//	Rule 3  day-of-month open, day-of-week restricted    -> day-of-week must match
//
// Rule 1 being OR rather than AND is the part that surprises people: "0 0 13 * 5"
// means the 13th *or* any Friday, not Friday the 13th.
func (e *Expression) matchDayOfMonth(current *cronTime) (bool, error) {
	domWildcard := e.fields.DayOfMonth.IsWildcard()
	dowWildcard := e.fields.DayOfWeek.IsWildcard()

	matchedDOM := e.fields.DayOfMonth.contains(current.Day()) ||
		(e.fields.DayOfMonth.HasLast() && current.isLastDayOfMonth())

	matchedDOW := e.fields.DayOfWeek.contains(current.Weekday()) &&
		isNthWeekdayOfMonthMatch(e.fields.DayOfWeek.NthDayOfWeek(), current)

	if !matchedDOW && e.fields.DayOfWeek.HasLast() {
		last, err := isLastWeekdayOfMonthMatch(e.fields.DayOfWeek.values, current)
		if err != nil {
			return false, err
		}
		matchedDOW = last
	}

	switch {
	case !domWildcard && !dowWildcard && (matchedDOM || matchedDOW):
		return true, nil
	case matchedDOM && dowWildcard:
		return true, nil
	case domWildcard && !dowWildcard && matchedDOW:
		return true, nil
	}
	return false, nil
}

// isNthWeekdayOfMonthMatch tests a `#N` constraint. A non-positive nth means no
// constraint at all.
func isNthWeekdayOfMonthMatch(nth int, current *cronTime) bool {
	if nth <= 0 {
		return true
	}
	// The Nth occurrence of a weekday always falls in the Nth block of seven days.
	return (current.Day()+6)/7 == nth
}

// isLastWeekdayOfMonthMatch tests an `L` suffix, such as 5L for the last Friday.
//
// The weekday is read from the first character of the value, which is how the
// original does it. A bare "L" therefore yields NaN there and produces an error,
// at iteration time rather than at parse time, which is bug 4 in the original and
// is reproduced here.
func isLastWeekdayOfMonthMatch(values []Value, current *cronTime) (bool, error) {
	inFinalWeek := current.isLastWeekdayOfMonth()

	for _, v := range values {
		// Value.String never yields an empty string: numeric values render at
		// least one digit, and a token reaching here came from non-empty input.
		s := v.String()
		weekday, ok := jsParseInt(s[:1])
		if !ok {
			return false, constraintError("Invalid last weekday of the month expression: %s", s)
		}
		if current.Weekday() == weekday%7 && inFinalWeek {
			return true, nil
		}
	}
	return false, nil
}
