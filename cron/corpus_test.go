package cron

import (
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// differentialZones spans the shapes of timezone behaviour that matter:
// whole-hour DST, a two-hour gap, half-hour and quarter-hour offsets, midnight
// transitions, southern-hemisphere ordering, and no DST at all.
var differentialZones = []string{
	"UTC",
	"America/New_York",    // 1h DST
	"Europe/London",       // 1h DST, different dates
	"Antarctica/Troll",    // 2h DST gap
	"Australia/Lord_Howe", // 30min DST
	"Pacific/Chatham",     // +12:45 / +13:45
	"Asia/Kolkata",        // +05:30, no DST
	"Asia/Beirut",         // midnight transition
	"America/Santiago",    // midnight transition, southern hemisphere
	"Africa/Cairo",        // midnight transition
	"Australia/Sydney",    // southern hemisphere
	"America/Sao_Paulo",   // DST abolished in 2019
}

// timeOps are the operations CronDate exposes, each paired with the luxon call
// chain it must match. The verifier reproduces these chains exactly; keeping the
// mapping in one place is what makes the comparison meaningful.
var timeOps = []struct {
	name  string
	apply func(c *cronTime, arg int)
}{
	{"startOfMonth", func(c *cronTime, _ int) { c.startOfMonth() }},
	{"startOfDay", func(c *cronTime, _ int) { c.startOfDay() }},
	{"startOfHour", func(c *cronTime, _ int) { c.startOfHour() }},
	{"startOfMinute", func(c *cronTime, _ int) { c.startOfMinute() }},
	{"startOfSecond", func(c *cronTime, _ int) { c.startOfSecond() }},
	{"endOfMonth", func(c *cronTime, _ int) { c.endOfMonth() }},
	{"endOfDay", func(c *cronTime, _ int) { c.endOfDay() }},
	{"endOfHour", func(c *cronTime, _ int) { c.endOfHour() }},
	{"endOfMinute", func(c *cronTime, _ int) { c.endOfMinute() }},

	{"addYear", func(c *cronTime, _ int) { c.addYear() }},
	{"addMonth", func(c *cronTime, _ int) { c.addMonth() }},
	{"addDay", func(c *cronTime, _ int) { c.addDay() }},
	{"addHour", func(c *cronTime, _ int) { c.addHour() }},
	{"addMinute", func(c *cronTime, _ int) { c.addMinute() }},
	{"addSecond", func(c *cronTime, _ int) { c.addSecond() }},

	{"subtractYear", func(c *cronTime, _ int) { c.subtractYear() }},
	{"subtractMonth", func(c *cronTime, _ int) { c.subtractMonth() }},
	{"subtractDay", func(c *cronTime, _ int) { c.subtractDay() }},
	{"subtractHour", func(c *cronTime, _ int) { c.subtractHour() }},
	{"subtractMinute", func(c *cronTime, _ int) { c.subtractMinute() }},
	{"subtractSecond", func(c *cronTime, _ int) { c.subtractSecond() }},

	{"setHour", func(c *cronTime, a int) { c.setHour(a % 24) }},
	{"setMinute", func(c *cronTime, a int) { c.setMinute(a % 60) }},
	{"setSecond", func(c *cronTime, a int) { c.setSecond(a % 60) }},
	{"setDayOfMonth", func(c *cronTime, a int) { c.setDayOfMonth(a%31 + 1) }},
	{"setMonth", func(c *cronTime, a int) { c.setMonth(time.Month(a%12 + 1)) }},
	{"setYear", func(c *cronTime, a int) { c.setYear(2020 + a%10) }},
	{"setWeekday", func(c *cronTime, a int) { c.setWeekday(a%7 + 1) }},
}

type opCase struct {
	Zone string `json:"zone"`
	// AnchorMs is the instant the operation starts from, as epoch milliseconds.
	// Absolute instants are unambiguous, so both implementations agree on the
	// starting point and only the operation itself is under test.
	AnchorMs int64  `json:"anchorMs"`
	Op       string `json:"op"`
	Arg      int    `json:"arg"`
	// ResultMs is what the Go implementation produced.
	ResultMs int64 `json:"resultMs"`
}

// TestGenerateTimeOpCorpus writes a corpus of (instant, operation) pairs and the
// results the Go time layer produces, for scripts/probe/verify-time-ops.js to
// check against luxon.
//
// The nine cases in TestLuxonDivergenceTable were chosen by hand and only prove
// what was already known to be interesting. This exercises every CronDate
// operation from tens of thousands of starting instants, weighted toward DST
// transitions where the implementations can actually disagree.
//
// It found a real bug on first run: seeding luxon's offset search from the
// reading itself rather than from the instance being operated on disagreed on
// 143 of 41,088 ambiguous fall-back readings.
//
// Skipped unless CRON_GEN_CORPUS=1, since it writes a file and is only useful
// alongside the Node verifier.
func TestGenerateTimeOpCorpus(t *testing.T) {
	if os.Getenv("CRON_GEN_CORPUS") != "1" {
		t.Skip("set CRON_GEN_CORPUS=1 to regenerate the differential corpus")
	}

	rng := rand.New(rand.NewSource(20260801))
	var cases []opCase

	for _, zone := range differentialZones {
		loc := mustLoad(t, zone)

		var anchors []int64

		// Dense sampling across each transition: every 10 minutes for the 3 hours
		// either side, which covers gaps and repeated hours of any width.
		for _, tr := range transitionInstants(loc, 2024, 2027) {
			for delta := -180; delta <= 180; delta += 10 {
				anchors = append(anchors, tr.Add(time.Duration(delta)*time.Minute).UnixMilli())
			}
		}

		// Month and year boundaries, where the clamping rules bite.
		for _, y := range []int{2023, 2024, 2025, 2026} {
			for _, mo := range []time.Month{time.January, time.February, time.March, time.December} {
				for _, d := range []int{1, 28, 29, 30, 31} {
					if d > daysIn(y, mo) {
						continue
					}
					anchors = append(anchors, time.Date(y, mo, d, 12, 30, 45, 0, loc).UnixMilli())
					anchors = append(anchors, time.Date(y, mo, d, 0, 0, 0, 0, loc).UnixMilli())
					anchors = append(anchors, time.Date(y, mo, d, 23, 59, 59, 0, loc).UnixMilli())
				}
			}
		}

		// Uniform sampling to catch anything the targeted anchors miss.
		for i := 0; i < 300; i++ {
			y := 2020 + rng.Intn(10)
			anchors = append(anchors, time.Date(y, time.Month(1+rng.Intn(12)), 1+rng.Intn(28),
				rng.Intn(24), rng.Intn(60), rng.Intn(60), rng.Intn(1000)*int(time.Millisecond), loc).UnixMilli())
		}

		for _, anchor := range anchors {
			for _, op := range timeOps {
				arg := rng.Intn(64)
				c := newCronTime(time.UnixMilli(anchor), loc)
				op.apply(c, arg)
				cases = append(cases, opCase{
					Zone: zone, AnchorMs: anchor, Op: op.name, Arg: arg, ResultMs: c.UnixMilli(),
				})
			}
		}
	}

	out := filepath.Join("..", "scripts", "probe", "time-op-corpus.json")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	if err := json.NewEncoder(f).Encode(cases); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %d cases to %s", len(cases), out)
}

// transitionInstants finds the exact moments loc's UTC offset changes, by
// scanning daily then bisecting to the minute.
func transitionInstants(loc *time.Location, fromYear, toYear int) []time.Time {
	var out []time.Time
	cur := time.Date(fromYear, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(toYear, 12, 31, 0, 0, 0, 0, time.UTC)

	prev := zoneOffset(cur, loc)
	for cur.Before(end) {
		next := cur.AddDate(0, 0, 1)
		if off := zoneOffset(next, loc); off != prev {
			lo, hi := cur, next
			for hi.Sub(lo) > time.Minute {
				mid := lo.Add(hi.Sub(lo) / 2)
				if zoneOffset(mid, loc) == prev {
					lo = mid
				} else {
					hi = mid
				}
			}
			out = append(out, hi)
			prev = off
		}
		cur = next
	}
	return out
}
