//go:build js && wasm

// Command bridge exposes the cron package to JavaScript so that the original
// TypeScript test suite can run, unmodified, against the Go port.
//
// It exists only for that purpose. The library itself has no JavaScript
// dependency, and benchmarks measure the Go package directly rather than
// anything here.
//
// The protocol is deliberately small: JavaScript holds opaque integer handles
// to Go objects and makes calls through a single dispatch function. Handles are
// released explicitly, or by a FinalizationRegistry on the JavaScript side.
package main

import (
	"fmt"
	"strings"
	"syscall/js"
	"time"

	// js/wasm has no system zone database, so it must be embedded. This is why
	// the import lives here rather than in the library, where it would add
	// roughly 450 KB to every binary that links it.
	_ "time/tzdata"

	"github.com/aniket-3001/cron-parser-go/cron"
)

// registry maps handles to the Go objects JavaScript is holding.
//
// A plain map with no mutex is correct here: js/wasm runs the Go scheduler on a
// single OS thread and every call arrives from the JavaScript event loop, so
// there is no concurrent access to guard against.
var (
	registry = map[int]any{}
	nextID   = 1
)

func store(v any) int {
	id := nextID
	nextID++
	registry[id] = v
	return id
}

func lookup[T any](h int) (T, bool) {
	v, ok := registry[h].(T)
	return v, ok
}

// result wraps every return value so that a failure is unambiguous. Returning
// the value directly would leave no way to distinguish a legitimate null from
// an error.
func ok(v any) any      { return map[string]any{"v": v} }
func fail(m string) any { return map[string]any{"e": m} }

func failErr(err error) any { return fail(err.Error()) }

// arg readers. The dispatcher validates arity before these run.
func argInt(args []js.Value, i int) int    { return args[i].Int() }
func argStr(args []js.Value, i int) string { return args[i].String() }
func argBool(args []js.Value, i int) bool  { return args[i].Bool() }

// argMillis reads an epoch-millisecond timestamp. JavaScript numbers are
// float64, so a plain Int() would overflow for instants beyond 2038.
func argMillis(args []js.Value, i int) int64 { return int64(args[i].Float()) }

// argValues reads a cron field value list, where each element is either a
// number or a token string.
func argValues(v js.Value) []cron.Value {
	n := v.Length()
	out := make([]cron.Value, n)
	for i := 0; i < n; i++ {
		item := v.Index(i)
		if item.Type() == js.TypeString {
			out[i] = cron.Text(item.String())
		} else {
			out[i] = cron.Num(item.Int())
		}
	}
	return out
}

// jsValues renders a field value list back to JavaScript, preserving the
// number-or-string distinction the original exposes.
func jsValues(vs []cron.Value) []any {
	out := make([]any, len(vs))
	for i, v := range vs {
		if v.IsNumeric() {
			out[i] = v.N
		} else {
			out[i] = v.Text
		}
	}
	return out
}

func loadLocation(name string) (*time.Location, error) {
	if name == "" {
		return time.UTC, nil
	}
	return time.LoadLocation(name)
}

// configFrom reads the option bag the shim passes as a plain object.
func configFrom(o js.Value) (cron.BridgeConfig, error) {
	cfg := cron.BridgeConfig{Location: time.UTC}

	if v := o.Get("tz"); v.Type() == js.TypeString {
		loc, err := loadLocation(v.String())
		if err != nil {
			return cfg, err
		}
		cfg.Location = loc
	}
	if v := o.Get("hashSeed"); v.Type() == js.TypeString {
		cfg.HashSeed = v.String()
	}
	if v := o.Get("strict"); v.Type() == js.TypeBoolean {
		cfg.Strict = v.Bool()
	}
	if v := o.Get("currentDate"); v.Type() == js.TypeNumber {
		cfg.HasCurrent, cfg.CurrentMillis = true, int64(v.Float())
	}
	if v := o.Get("startDate"); v.Type() == js.TypeNumber {
		cfg.HasStart, cfg.StartMillis = true, int64(v.Float())
	}
	if v := o.Get("endDate"); v.Type() == js.TypeNumber {
		cfg.HasEnd, cfg.EndMillis = true, int64(v.Float())
	}
	return cfg, nil
}

// dispatch routes one call. Operations are grouped by the object they act on.
func dispatch(op string, args []js.Value) any {
	switch {
	case strings.HasPrefix(op, "date."):
		return dispatchDate(op, args)
	case strings.HasPrefix(op, "field."):
		return dispatchField(op, args)
	case strings.HasPrefix(op, "fields."):
		return dispatchFields(op, args)
	case strings.HasPrefix(op, "expr."):
		return dispatchExpression(op, args)
	}

	switch op {
	case "release":
		delete(registry, argInt(args, 0))
		return ok(nil)

	case "random.new":
		return ok(store(cron.NewRandom(argStr(args, 0))))

	case "random.next":
		r, found := lookup[*cron.Random](argInt(args, 0))
		if !found {
			return fail("bridge: no such random handle")
		}
		return ok(r.Next())

	case "findNearestValueInList":
		v, found := cron.FindNearestValueInList(argValues(args[0]), argInt(args, 1), argBool(args, 2))
		if !found {
			return ok(nil)
		}
		return ok(v)

	case "compactField":
		ranges := cron.CompactField(argValues(args[0]))
		out := make([]any, len(ranges))
		for i, r := range ranges {
			m := map[string]any{"count": r.Count}
			if r.Start.IsNumeric() {
				m["start"] = r.Start.N
			} else {
				m["start"] = r.Start.Text
			}
			// Absent end and step are omitted rather than zeroed, so the shim
			// can reproduce the original's undefined.
			if r.HasEnd {
				m["end"] = r.End
			}
			if r.HasStep {
				m["step"] = r.Step
			}
			out[i] = m
		}
		return ok(out)

	case "parse":
		cfg, err := configFrom(args[1])
		if err != nil {
			return failErr(err)
		}
		e, err := cron.ParseWithConfig(argStr(args, 0), cfg)
		if err != nil {
			return failErr(err)
		}
		cron.EnableTrace(e)
		return ok(store(e))
	}

	return fail("unknown bridge operation: " + op)
}

func dispatchDate(op string, args []js.Value) any {
	// Construction takes no handle.
	switch op {
	case "date.fromMillis":
		loc, err := loadLocation(argStr(args, 1))
		if err != nil {
			return failErr(err)
		}
		return ok(store(cron.NewDateFromMillis(argMillis(args, 0), loc)))

	case "date.fromWallClock":
		loc, err := loadLocation(argStr(args, 7))
		if err != nil {
			return failErr(err)
		}
		return ok(store(cron.NewDateFromWallClock(
			argInt(args, 0), argInt(args, 1), argInt(args, 2), argInt(args, 3),
			argInt(args, 4), argInt(args, 5), argInt(args, 6), loc)))

	case "date.fromString":
		loc, err := loadLocation(argStr(args, 1))
		if err != nil {
			return failErr(err)
		}
		d, parsed := cron.NewDateFromString(argStr(args, 0), loc)
		if !parsed {
			return fail("CronDate: unhandled timestamp: " + argStr(args, 0))
		}
		return ok(store(d))

	case "date.now":
		loc, err := loadLocation(argStr(args, 0))
		if err != nil {
			return failErr(err)
		}
		return ok(store(cron.NewDateNow(loc)))
	}

	d, found := lookup[*cron.Date](argInt(args, 0))
	if !found {
		return fail("bridge: no such date handle")
	}

	switch op {
	case "date.clone":
		return ok(store(d.Clone()))

	case "date.withLocation":
		loc, err := loadLocation(argStr(args, 1))
		if err != nil {
			return failErr(err)
		}
		return ok(store(d.WithLocation(loc)))

	case "date.get":
		switch argStr(args, 1) {
		case "year":
			return ok(d.Year())
		case "month":
			return ok(d.Month())
		case "day":
			return ok(d.Day())
		case "weekday":
			return ok(d.Weekday())
		case "hour":
			return ok(d.Hour())
		case "minute":
			return ok(d.Minute())
		case "second":
			return ok(d.Second())
		case "millisecond":
			return ok(d.Millisecond())
		case "utcYear":
			return ok(d.UTCYear())
		case "utcMonth":
			return ok(d.UTCMonth())
		case "utcDay":
			return ok(d.UTCDay())
		case "utcWeekday":
			return ok(d.UTCWeekday())
		case "utcHour":
			return ok(d.UTCHour())
		case "utcMinute":
			return ok(d.UTCMinute())
		case "utcSecond":
			return ok(d.UTCSecond())
		case "zone":
			return ok(d.ZoneName())
		case "offsetMinutes":
			return ok(d.OffsetMinutes())
		case "time":
			return ok(float64(d.UnixMilli()))
		case "iso":
			return ok(d.ISOString())
		case "local":
			return ok(d.LocalString())
		case "isLastDayOfMonth":
			return ok(d.IsLastDayOfMonth())
		case "isLastWeekdayOfMonth":
			return ok(d.IsLastWeekdayOfMonth())
		case "dstStart":
			if v := d.DSTStart(); v >= 0 {
				return ok(v)
			}
			return ok(nil)
		case "dstEnd":
			if v := d.DSTEnd(); v >= 0 {
				return ok(v)
			}
			return ok(nil)
		}
		return fail("bridge: unknown date property " + argStr(args, 1))

	case "date.set":
		v := argInt(args, 2)
		switch argStr(args, 1) {
		case "year":
			d.SetYear(v)
		case "month":
			d.SetMonth(v)
		case "day":
			d.SetDay(v)
		case "weekday":
			d.SetWeekday(v)
		case "hour":
			d.SetHour(v)
		case "minute":
			d.SetMinute(v)
		case "second":
			d.SetSecond(v)
		case "millisecond":
			d.SetMillisecond(v)
		case "dstStart":
			d.SetDSTStart(v)
		case "dstEnd":
			d.SetDSTEnd(v)
		default:
			return fail("bridge: unknown date property " + argStr(args, 1))
		}
		return ok(nil)

	case "date.startOfDay":
		d.StartOfDay()
		return ok(nil)
	case "date.endOfDay":
		d.EndOfDay()
		return ok(nil)

	case "date.arith":
		switch argStr(args, 1) {
		case "addYear":
			d.AddYear()
		case "addMonth":
			d.AddMonth()
		case "addDay":
			d.AddDay()
		case "addHour":
			d.AddHour()
		case "addMinute":
			d.AddMinute()
		case "addSecond":
			d.AddSecond()
		case "subtractYear":
			d.SubtractYear()
		case "subtractMonth":
			d.SubtractMonth()
		case "subtractDay":
			d.SubtractDay()
		case "subtractHour":
			d.SubtractHour()
		case "subtractMinute":
			d.SubtractMinute()
		case "subtractSecond":
			d.SubtractSecond()
		default:
			return fail("bridge: unknown date operation " + argStr(args, 1))
		}
		return ok(nil)

	case "date.addUnit":
		if !d.AddUnit(argStr(args, 1)) {
			return fail("bridge: unknown time unit " + argStr(args, 1))
		}
		return ok(nil)

	case "date.subtractUnit":
		if !d.SubtractUnit(argStr(args, 1)) {
			return fail("bridge: unknown time unit " + argStr(args, 1))
		}
		return ok(nil)

	case "date.invoke":
		verb := argStr(args, 1)
		if !d.InvokeDateOperation(verb, argStr(args, 2)) {
			// The original reports the verb it could not understand.
			return fail("Invalid verb: " + verb)
		}
		return ok(nil)

	case "date.apply":
		verb := argStr(args, 1)
		if !d.ApplyDateOperation(verb, argStr(args, 2), argInt(args, 3)) {
			return fail("Invalid verb: " + verb)
		}
		return ok(nil)
	}

	return fail("unknown bridge operation: " + op)
}

func dispatchField(op string, args []js.Value) any {
	if op == "field.new" {
		raw, nth := "", 0
		var wildcard *bool
		if o := args[2]; o.Type() == js.TypeObject {
			if v := o.Get("rawValue"); v.Type() == js.TypeString {
				raw = v.String()
			}
			if v := o.Get("nthDayOfWeek"); v.Type() == js.TypeNumber {
				nth = v.Int()
			}
			if v := o.Get("wildcard"); v.Type() == js.TypeBoolean {
				b := v.Bool()
				wildcard = &b
			}
		}
		f, err := cron.NewFieldForBridge(argStr(args, 0), argValues(args[1]), raw, nth, wildcard)
		if err != nil {
			return failErr(err)
		}
		return ok(store(f))
	}

	f, found := lookup[*cron.Field](argInt(args, 0))
	if !found {
		return fail("bridge: no such field handle")
	}

	switch op {
	case "field.values":
		return ok(jsValues(f.Values()))
	case "field.get":
		switch argStr(args, 1) {
		case "isWildcard":
			return ok(f.IsWildcard())
		case "hasLast":
			return ok(f.HasLast())
		case "hasQuestion":
			return ok(f.HasQuestion())
		case "nthDay":
			return ok(f.NthDayOfWeek())
		case "min":
			return ok(f.Min())
		case "max":
			return ok(f.Max())
		case "raw":
			return ok(f.Raw())
		}
		return fail("bridge: unknown field property " + argStr(args, 1))
	}

	return fail("unknown bridge operation: " + op)
}

func dispatchFields(op string, args []js.Value) any {
	if op == "fields.new" {
		handles := make([]*cron.Field, 6)
		for i := 0; i < 6; i++ {
			item := args[0].Index(i)
			if item.Type() != js.TypeNumber {
				handles[i] = nil
				continue
			}
			f, found := lookup[*cron.Field](item.Int())
			if !found {
				return fail("bridge: no such field handle")
			}
			handles[i] = f
		}
		f, err := cron.NewFields(handles[0], handles[1], handles[2], handles[3], handles[4], handles[5])
		if err != nil {
			return failErr(err)
		}
		return ok(store(f))
	}

	f, found := lookup[*cron.Fields](argInt(args, 0))
	if !found {
		return fail("bridge: no such fields handle")
	}

	switch op {
	case "fields.field":
		var target *cron.Field
		switch argStr(args, 1) {
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
			return fail("bridge: unknown field name " + argStr(args, 1))
		}
		return ok(store(target))

	case "fields.format":
		s, err := f.Format(argBool(args, 1))
		if err != nil {
			return failErr(err)
		}
		return ok(s)

	case "fields.formatField":
		s, err := cron.FormatField(f, argStr(args, 1))
		if err != nil {
			return failErr(err)
		}
		return ok(s)

	case "fields.serialize":
		s := f.Serialize()
		one := func(sf cron.SerializedField) any {
			return map[string]any{"wildcard": sf.Wildcard, "values": jsValues(sf.Values)}
		}
		return ok(map[string]any{
			"second":     one(s.Second),
			"minute":     one(s.Minute),
			"hour":       one(s.Hour),
			"dayOfMonth": one(s.DayOfMonth),
			"month":      one(s.Month),
			"dayOfWeek":  one(s.DayOfWeek),
		})

	case "fields.toExpression":
		cfg, err := configFrom(args[1])
		if err != nil {
			return failErr(err)
		}
		e := cron.NewExpressionForBridge(f, cfg)
		cron.EnableTrace(e)
		return ok(store(e))
	}

	return fail("unknown bridge operation: " + op)
}

func dispatchExpression(op string, args []js.Value) any {
	e, found := lookup[*cron.Expression](argInt(args, 0))
	if !found {
		return fail("bridge: no such expression handle")
	}

	switch op {
	case "expr.next", "expr.prev":
		var (
			t   time.Time
			err error
		)
		if op == "expr.next" {
			t, err = e.Next()
		} else {
			t, err = e.Prev()
		}
		if err != nil {
			return failErr(err)
		}
		return ok(float64(t.UnixMilli()))

	case "expr.hasNext":
		return ok(e.HasNext())
	case "expr.hasPrev":
		return ok(e.HasPrev())

	case "expr.take":
		times := e.Take(argInt(args, 1))
		out := make([]any, len(times))
		for i, t := range times {
			out[i] = float64(t.UnixMilli())
		}
		return ok(out)

	case "expr.reset":
		if args[1].Type() == js.TypeNumber {
			e.Reset(time.UnixMilli(argMillis(args, 1)))
		} else {
			e.Reset()
		}
		return ok(nil)

	case "expr.format":
		s, err := e.Format(argBool(args, 1))
		if err != nil {
			return failErr(err)
		}
		return ok(s)

	case "expr.string":
		return ok(e.String())

	case "expr.includes":
		matched, err := e.Includes(time.UnixMilli(argMillis(args, 1)))
		if err != nil {
			return failErr(err)
		}
		return ok(matched)

	case "expr.fields":
		return ok(store(e.Fields()))

	case "expr.enableTrace":
		cron.EnableTrace(e)
		return ok(nil)

	case "expr.takeTrace":
		entries := cron.TakeTrace(e)
		out := make([]any, len(entries))
		for i, t := range entries {
			out[i] = map[string]any{"verb": t.Verb, "unit": t.Unit, "hoursLength": t.HoursLength}
		}
		return ok(out)
	}

	return fail("unknown bridge operation: " + op)
}

// safeDispatch turns a panic into an ordinary error.
//
// This matters more than it looks. syscall/js panics when a value is read as
// the wrong type, and a panic that escapes the callback tears down the whole Go
// runtime, so one malformed call would take every later call with it, and a
// test run would report hundreds of unrelated failures after the first genuine
// one. Converting to an error keeps a mistake local to the call that made it.
func safeDispatch(op string, args []js.Value) (result any) {
	defer func() {
		if r := recover(); r != nil {
			result = fail(fmt.Sprintf("bridge: %s failed: %v", op, r))
		}
	}()
	return dispatch(op, args)
}

func main() {
	js.Global().Set("__cronBridge", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return fail("bridge: missing operation")
		}
		return safeDispatch(args[0].String(), args[1:])
	}))

	// Signal readiness so the shim need not poll for the export to appear.
	js.Global().Set("__cronBridgeReady", true)

	// Keep the runtime alive; without this main returns and the exported
	// function stops working.
	select {}
}
