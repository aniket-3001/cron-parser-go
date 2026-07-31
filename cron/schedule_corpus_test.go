package cron

import (
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// randomExpression builds a syntactically plausible cron expression from the
// grammar the parser accepts. Some are invalid; those are kept, because how the
// two implementations disagree about rejecting an expression matters as much as
// how they agree about accepting one.
func randomExpression(rng *rand.Rand) string {
	fieldText := func(lo, hi int, extras []string) string {
		switch rng.Intn(10) {
		case 0, 1:
			return "*"
		case 2:
			return itoa(lo + rng.Intn(hi-lo+1))
		case 3: // list
			n := 1 + rng.Intn(3)
			parts := make([]string, n)
			for i := range parts {
				parts[i] = itoa(lo + rng.Intn(hi-lo+1))
			}
			return strings.Join(parts, ",")
		case 4: // range
			a := lo + rng.Intn(hi-lo+1)
			b := lo + rng.Intn(hi-lo+1)
			return itoa(a) + "-" + itoa(b)
		case 5: // step over everything
			return "*/" + itoa(1+rng.Intn(hi-lo+1))
		case 6: // stepped range
			a := lo + rng.Intn(hi-lo+1)
			b := lo + rng.Intn(hi-lo+1)
			return itoa(a) + "-" + itoa(b) + "/" + itoa(1+rng.Intn(5))
		case 7: // bare start with a step
			return itoa(lo+rng.Intn(hi-lo+1)) + "/" + itoa(1+rng.Intn(5))
		case 8:
			if len(extras) > 0 {
				return extras[rng.Intn(len(extras))]
			}
			return "*"
		default:
			return "*"
		}
	}

	fields := []string{
		fieldText(0, 59, nil),                           // second
		fieldText(0, 59, nil),                           // minute
		fieldText(0, 23, nil),                           // hour
		fieldText(1, 31, []string{"L"}),                 // day of month
		fieldText(1, 12, []string{"JAN", "jun", "DEC"}), // month
		fieldText(0, 7, []string{"L", "5L", "1L", "MON#2", "5#3", "MON-FRI", "SUN"}), // day of week
	}

	// Half the time drop the seconds field, exercising the five-field form.
	if rng.Intn(2) == 0 {
		fields = fields[1:]
	}
	return strings.Join(fields, " ")
}

type scheduleProbe struct {
	Expression string   `json:"expression"`
	TZ         string   `json:"tz"`
	StartMs    int64    `json:"startMs"`
	Next       []string `json:"next"`
	NextError  string   `json:"nextError"`
	Prev       []string `json:"prev"`
	PrevError  string   `json:"prevError"`
	ParseError string   `json:"parseError"`
}

// TestGenerateScheduleCorpus writes randomly generated expressions together with
// the schedules this implementation produces, for
// scripts/probe/verify-schedule.js to check against the reference.
//
// The committed fixtures cover cases chosen by hand, which by construction only
// prove what was already suspected. This sweeps the grammar instead, weighted
// toward daylight-saving transitions, and is the same machinery the differential
// fuzzer will use.
//
// Skipped unless CRON_GEN_CORPUS=1.
func TestGenerateScheduleCorpus(t *testing.T) {
	if os.Getenv("CRON_GEN_CORPUS") != "1" {
		t.Skip("set CRON_GEN_CORPUS=1 to regenerate the schedule corpus")
	}

	// The seed is settable so the sweep can be rerun over fresh ground; a single
	// seed only ever proves something about the expressions that seed happens to
	// produce.
	seed := int64(20260801)
	if s := os.Getenv("CRON_CORPUS_SEED"); s != "" {
		if n, ok := jsParseInt(s); ok {
			seed = int64(n)
		}
	}

	rng := rand.New(rand.NewSource(seed))
	zones := []string{
		"UTC", "America/New_York", "Europe/London", "Asia/Kolkata",
		"Australia/Lord_Howe", "Antarctica/Troll", "Pacific/Chatham",
		"America/Santiago", "Australia/Sydney", "Africa/Cairo",
	}

	var probes []scheduleProbe
	const iterations = 6

	for i := 0; i < 6000; i++ {
		expr := randomExpression(rng)
		zone := zones[rng.Intn(len(zones))]
		loc, err := time.LoadLocation(zone)
		if err != nil {
			t.Fatalf("zone %s: %v", zone, err)
		}

		// Bias starting instants toward transitions, where disagreement is
		// possible, but keep a spread of ordinary ones too.
		var start time.Time
		if trs := transitionInstants(loc, 2024, 2027); len(trs) > 0 && rng.Intn(2) == 0 {
			start = trs[rng.Intn(len(trs))].Add(time.Duration(rng.Intn(240)-120) * time.Minute)
		} else {
			start = time.Date(2020+rng.Intn(10), time.Month(1+rng.Intn(12)), 1+rng.Intn(28),
				rng.Intn(24), rng.Intn(60), rng.Intn(60), 0, loc)
		}

		p := scheduleProbe{Expression: expr, TZ: zone, StartMs: start.UnixMilli()}

		e, err := Parse(expr, WithLocation(loc), WithCurrent(start))
		if err != nil {
			p.ParseError = err.Error()
			probes = append(probes, p)
			continue
		}
		p.Next, p.NextError = walk(e, iterations, false)

		e2, _ := Parse(expr, WithLocation(loc), WithCurrent(start))
		p.Prev, p.PrevError = walk(e2, iterations, true)

		probes = append(probes, p)
	}

	out := filepath.Join("..", "scripts", "probe", "schedule-corpus.json")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(probes); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %d probes to %s", len(probes), out)
}

func walk(e *Expression, n int, reverse bool) ([]string, string) {
	step := e.Next
	if reverse {
		step = e.Prev
	}
	var out []string
	for i := 0; i < n; i++ {
		tm, err := step()
		if err != nil {
			return out, err.Error()
		}
		out = append(out, toISO(tm))
	}
	return out, ""
}
