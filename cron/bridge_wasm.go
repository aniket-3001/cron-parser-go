//go:build js && wasm

package cron

import "time"

// This file exists only in WebAssembly builds. It exposes internals that the
// test bridge needs in order to run the original TypeScript suite unmodified,
// and it is absent from every ordinary build of the library.
//
// The original's CronDate is part of its public API and its tests drive it
// directly, so the bridge needs a date object with roughly forty methods. The Go
// library deliberately does not expose one — its public surface uses time.Time,
// which is what a Go caller expects — so the compatibility shape lives here
// instead of widening the real API.
//
// Everything below is a thin forwarder. There is no logic here, which is what
// keeps the behaviour under test in the library rather than in the bridge.

// Date is the wall-clock type the original exposes as CronDate.
type Date struct{ t *cronTime }

// NewDateFromMillis builds a Date from epoch milliseconds observed in loc.
func NewDateFromMillis(ms int64, loc *time.Location) *Date {
	return &Date{t: newCronTime(time.UnixMilli(ms), loc)}
}

// NewDateFromWallClock builds a Date from a wall-clock reading in loc,
// resolving impossible and ambiguous readings the way luxon does.
func NewDateFromWallClock(y, mo, d, h, mi, s, ms int, loc *time.Location) *Date {
	pivot := time.Date(y, time.Month(mo), d, h, mi, s, ms*int(time.Millisecond), time.UTC)
	return &Date{t: newCronTime(
		fromWallClock(y, time.Month(mo), d, h, mi, s, ms, zoneOffset(pivot, loc), loc), loc)}
}

// dateLayouts are tried in the order luxon tries its parsers: ISO first, then
// RFC 2822, then SQL, then the explicit "EEE, d MMM yyyy HH:mm:ss" format.
//
// A layout carrying a zone offset is parsed as an absolute instant and then
// viewed in loc. A layout without one is read as a wall-clock time already in
// loc, which is how luxon treats a zone-less string when given a zone.
var dateLayouts = []struct {
	layout   string
	hasOffet bool
}{
	// ISO 8601.
	{"2006-01-02T15:04:05.999999999Z07:00", true},
	{"2006-01-02T15:04:05Z07:00", true},
	// An offset may be written as hours alone, as in "+03".
	{"2006-01-02T15:04:05.999999999Z07", true},
	{"2006-01-02T15:04:05Z07", true},
	{"2006-01-02T15:04:05.999999999", false},
	{"2006-01-02T15:04:05", false},
	{"2006-01-02T15:04", false},
	{"2006-01-02", false},
	{"2006-01", false},

	// RFC 2822, with and without an offset.
	{"Mon, 2 Jan 2006 15:04:05 -0700", true},
	{"Mon, 2 Jan 2006 15:04:05 MST", true},
	{"2 Jan 2006 15:04:05 -0700", true},

	// SQL. The day is written "02" rather than "2" because Go's "2" would also
	// accept a single digit, where luxon's SQL parser requires zero padding.
	// "2021-01-4 10:00:00" therefore matches nothing and is rejected, which is
	// what the original does.
	{"2006-01-02 15:04:05.999999999", false},
	{"2006-01-02 15:04:05", false},
	{"2006-01-02 15:04", false},

	// The explicit format the original falls back to last.
	{"Mon, 2 Jan 2006 15:04:05", false},
	{"2 Jan 2006 15:04:05", false},
}

// NewDateFromString parses the timestamp formats the original accepts.
//
// It reports false when no layout matches, which the bridge turns into the
// original's "CronDate: unhandled timestamp" error.
func NewDateFromString(s string, loc *time.Location) (*Date, bool) {
	for _, l := range dateLayouts {
		if l.hasOffet {
			if t, err := time.Parse(l.layout, s); err == nil {
				return &Date{t: newCronTime(t, loc)}, true
			}
			continue
		}
		if t, err := time.ParseInLocation(l.layout, s, loc); err == nil {
			return &Date{t: newCronTime(t, loc)}, true
		}
	}
	return nil, false
}

// NewDateNow is the zero-argument constructor: the current instant, viewed in
// loc.
func NewDateNow(loc *time.Location) *Date {
	return &Date{t: newCronTime(time.Now(), loc)}
}

// Clone returns an independent copy carrying the same DST bookkeeping.
func (d *Date) Clone() *Date { return &Date{t: d.t.clone()} }

// WithLocation returns the same instant observed in another zone.
func (d *Date) WithLocation(loc *time.Location) *Date {
	c := d.t.clone()
	c.loc = loc
	c.t = c.t.In(loc)
	return &Date{t: c}
}

// Local component accessors.
func (d *Date) Year() int        { return d.t.Year() }
func (d *Date) Month() int       { return int(d.t.Month()) - 1 } // the original is zero-based
func (d *Date) Day() int         { return d.t.Day() }
func (d *Date) Weekday() int     { return d.t.Weekday() }
func (d *Date) Hour() int        { return d.t.Hour() }
func (d *Date) Minute() int      { return d.t.Minute() }
func (d *Date) Second() int      { return d.t.Second() }
func (d *Date) Millisecond() int { return d.t.Millisecond() }

// UTC component accessors.
func (d *Date) UTCYear() int    { return d.t.t.UTC().Year() }
func (d *Date) UTCMonth() int   { return int(d.t.t.UTC().Month()) - 1 }
func (d *Date) UTCDay() int     { return d.t.t.UTC().Day() }
func (d *Date) UTCWeekday() int { return int(d.t.t.UTC().Weekday()) }
func (d *Date) UTCHour() int    { return d.t.t.UTC().Hour() }
func (d *Date) UTCMinute() int  { return d.t.t.UTC().Minute() }
func (d *Date) UTCSecond() int  { return d.t.t.UTC().Second() }

// ZoneName identifies the date's timezone.
//
// An expression whose currentDate is a CronDate but which names no timezone of
// its own inherits the date's, because the original's copy constructor keeps
// the zone. The name has to cross the boundary for that to work.
func (d *Date) ZoneName() string { return d.t.loc.String() }

// OffsetMinutes is the offset from UTC in minutes, as getUTCOffset reports it.
func (d *Date) OffsetMinutes() int { return d.t.OffsetMinutes() }

// UnixMilli is the instant in epoch milliseconds.
func (d *Date) UnixMilli() int64 { return d.t.UnixMilli() }

// ISOString renders the instant in UTC, as Date.prototype.toISOString does.
func (d *Date) ISOString() string {
	return d.t.t.UTC().Format("2006-01-02T15:04:05.000Z")
}

// LocalString renders the instant in its own zone, as luxon's toISO does.
//
// The Z07:00 form is what makes UTC render as a trailing Z rather than as
// +00:00, which is the distinction toJSON's tests assert on.
func (d *Date) LocalString() string {
	return d.t.t.Format("2006-01-02T15:04:05.000Z07:00")
}

// DST bookkeeping. The bridge reports an unset value as null.
func (d *Date) DSTStart() int     { return d.t.dstStart }
func (d *Date) DSTEnd() int       { return d.t.dstEnd }
func (d *Date) SetDSTStart(v int) { d.t.dstStart = v }
func (d *Date) SetDSTEnd(v int)   { d.t.dstEnd = v }

// Setters.
func (d *Date) SetYear(y int)        { d.t.setYear(y) }
func (d *Date) SetMonth(m int)       { d.t.setMonth(time.Month(m + 1)) } // zero-based in
func (d *Date) SetDay(v int)         { d.t.setDayOfMonth(v) }
func (d *Date) SetWeekday(v int)     { d.t.setWeekday(v) }
func (d *Date) SetHour(v int)        { d.t.setHour(v) }
func (d *Date) SetMinute(v int)      { d.t.setMinute(v) }
func (d *Date) SetSecond(v int)      { d.t.setSecond(v) }
func (d *Date) SetMillisecond(v int) { d.t.setMillisecond(v) }

// Boundary helpers.
func (d *Date) StartOfDay() { d.t.startOfDay() }
func (d *Date) EndOfDay()   { d.t.endOfDay() }

// Arithmetic.
func (d *Date) AddYear()        { d.t.addYear() }
func (d *Date) AddMonth()       { d.t.addMonth() }
func (d *Date) AddDay()         { d.t.addDay() }
func (d *Date) AddHour()        { d.t.addHour() }
func (d *Date) AddMinute()      { d.t.addMinute() }
func (d *Date) AddSecond()      { d.t.addSecond() }
func (d *Date) SubtractYear()   { d.t.subtractYear() }
func (d *Date) SubtractMonth()  { d.t.subtractMonth() }
func (d *Date) SubtractDay()    { d.t.subtractDay() }
func (d *Date) SubtractHour()   { d.t.subtractHour() }
func (d *Date) SubtractMinute() { d.t.subtractMinute() }
func (d *Date) SubtractSecond() { d.t.subtractSecond() }

func (d *Date) IsLastDayOfMonth() bool     { return d.t.isLastDayOfMonth() }
func (d *Date) IsLastWeekdayOfMonth() bool { return d.t.isLastWeekdayOfMonth() }

// unitByName maps the original's TimeUnit strings onto the internal enum.
func unitByName(name string) (timeUnit, bool) {
	switch name {
	case "Second":
		return unitSecond, true
	case "Minute":
		return unitMinute, true
	case "Hour":
		return unitHour, true
	case "Day":
		return unitDay, true
	case "Month":
		return unitMonth, true
	case "Year":
		return unitYear, true
	}
	return 0, false
}

// AddUnit applies the add operation named by the original's TimeUnit enum.
func (d *Date) AddUnit(unit string) bool {
	u, ok := unitByName(unit)
	if ok {
		d.t.addUnit(u)
	}
	return ok
}

// SubtractUnit applies the subtract operation named by the original's TimeUnit.
func (d *Date) SubtractUnit(unit string) bool {
	u, ok := unitByName(unit)
	if ok {
		d.t.subtractUnit(u)
	}
	return ok
}

// InvokeDateOperation dispatches on the original's DateMathOp and TimeUnit
// names. It reports false for an unrecognised verb, which the bridge turns into
// the original's "Invalid verb" error.
func (d *Date) InvokeDateOperation(verb, unit string) bool {
	u, ok := unitByName(unit)
	if !ok {
		return false
	}
	switch verb {
	case "Add":
		d.t.addUnit(u)
	case "Subtract":
		d.t.subtractUnit(u)
	default:
		return false
	}
	return true
}

// ApplyDateOperation performs a date operation and records any daylight-saving
// transition it crossed.
func (d *Date) ApplyDateOperation(verb, unit string, hoursLength int) bool {
	u, ok := unitByName(unit)
	if !ok {
		return false
	}
	op := opAdd
	switch verb {
	case "Add":
		op = opAdd
	case "Subtract":
		op = opSubtract
	default:
		return false
	}
	d.t.applyDateOperation(op, u, hoursLength)
	return true
}

// CompactField exposes the range compaction the original publishes as a static
// method on CronFieldCollection. It is internal to the library proper.
func CompactField(values []Value) []CompactRange {
	ranges := compactField(values)
	out := make([]CompactRange, len(ranges))
	for i, r := range ranges {
		out[i] = CompactRange{
			Start: r.start, Count: r.count,
			End: r.end, HasEnd: r.hasEnd,
			Step: r.step, HasStep: r.hasStep,
		}
	}
	return out
}

// CompactRange is one run produced by CompactField. HasEnd and HasStep
// distinguish an absent value from a zero one, which the original expresses as
// undefined and which several of its checks treat alike.
type CompactRange struct {
	Start   Value
	Count   int
	End     int
	HasEnd  bool
	Step    int
	HasStep bool
}

// ExpressionFields exposes the fields an Expression was built from.
func ExpressionFields(e *Expression) *Fields { return e.Fields() }

// NewExpressionForBridge builds an Expression from fields and raw text, which
// Parse does internally but which the bridge needs when the original's
// fieldsToExpression is called.
func NewExpressionForBridge(f *Fields, cfg BridgeConfig) *Expression {
	c := config{loc: cfg.Location, hashSeed: cfg.HashSeed, strict: cfg.Strict}
	if cfg.HasCurrent {
		t := time.UnixMilli(cfg.CurrentMillis)
		c.current = &t
	}
	if cfg.HasStart {
		t := time.UnixMilli(cfg.StartMillis)
		c.start = &t
	}
	if cfg.HasEnd {
		t := time.UnixMilli(cfg.EndMillis)
		c.end = &t
	}
	return newExpression(f, c, cfg.Raw)
}

// BridgeConfig mirrors the option set across the boundary, where functional
// options cannot travel.
type BridgeConfig struct {
	Location      *time.Location
	Raw           string
	HashSeed      string
	Strict        bool
	HasCurrent    bool
	CurrentMillis int64
	HasStart      bool
	StartMillis   int64
	HasEnd        bool
	EndMillis     int64
}

// ParseWithConfig is Parse driven by a BridgeConfig rather than by options.
func ParseWithConfig(expression string, cfg BridgeConfig) (*Expression, error) {
	fields, err := parseFields(expression, parseOptions{strict: cfg.Strict, hashSeed: cfg.HashSeed})
	if err != nil {
		return nil, err
	}
	cfg.Raw = expandPredefined(expression)
	return NewExpressionForBridge(fields, cfg), nil
}

// FormatField renders one named field of a collection. Rendering needs the
// collection because day-of-month is sized against the month the expression
// names.
func FormatField(f *Fields, name string) (string, error) {
	var target *Field
	switch name {
	case "second":
		target = f.Second
	case "minute":
		target = f.Minute
	case "hour":
		target = f.Hour
	case "dayOfMonth":
		target = f.DayOfMonth
	case "month":
		target = f.Month
	case "dayOfWeek":
		target = f.DayOfWeek
	default:
		return "", validationError("unknown field name %s", name)
	}
	return f.formatField(target)
}

// NewFieldForBridge builds a field of the named unit, carrying the raw text and
// nth-day suffix that the original's field constructors accept as options.
func NewFieldForBridge(unit string, values []Value, raw string, nth int, wildcard *bool) (*Field, error) {
	var spec *fieldSpec
	switch unit {
	case "second":
		spec = specSecond
	case "minute":
		spec = specMinute
	case "hour":
		spec = specHour
	case "dayOfMonth":
		spec = specDayOfMonth
	case "month":
		spec = specMonth
	case "dayOfWeek":
		spec = specDayOfWeek
	default:
		return nil, validationError("unknown field unit %s", unit)
	}
	return newField(spec, values, fieldOptions{raw: raw, nthDayOfWeek: nth, wildcard: wildcard})
}

// Random is a live seeded generator.
//
// The generator has to persist across calls rather than be redrawn: successive
// values come from evolving internal state, so re-seeding on every crossing
// would replay the same value forever.
type Random struct{ next prng }

// NewRandom builds the generator behind the original's seededRandom. An empty
// seed yields a non-deterministic sequence, matching the original's treatment
// of a falsy seed.
func NewRandom(seed string) *Random { return &Random{next: seededRandom(seed)} }

// Next draws the next value in [0, 1).
func (r *Random) Next() float64 { return r.next() }

// FindNearestValueInList is the free-standing form of the nearest-value search,
// which the original publishes as a static method on CronField.
func FindNearestValueInList(values []Value, current int, reverse bool) (int, bool) {
	return findNearestInValues(values, current, reverse)
}
