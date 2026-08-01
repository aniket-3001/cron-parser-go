package cron

import (
	"testing"
	"time"
)

// benchPatterns are the expressions the original library benchmarks itself on,
// copied from its benchmarks/benchmark-inputs.ts.
//
// Using the author's own corpus rather than one chosen here removes the
// temptation to pick expressions that flatter the port, and makes the two sides
// directly comparable.
var benchPatterns = []struct {
	pattern     string
	description string
}{
	{"* * * * * *", "every second"},
	{"0 15 */5 5 *", "every 5 days at 15:00 in May"},
	{"10-30/2 2 12 8 0", "every 2 minutes from 10-30 at 2am on Aug 12th, Sunday"},
	{"10 2 12 8 7", "02:10 on 12 August and every Sunday"},
	{"0 12 */5 6 *", "12:00 on every 5th day of June"},
	{"0 * * 1,4-10,L * *", "hourly on the 1st, 4th-10th, and last day"},
	{"0 0 0 * * 4,6L", "midnight every Thursday and last Saturday"},
	{"0 0 0 * * 1L,5L", "midnight on the last Monday and last Friday"},
	{"0 0 6-20/2,L 2 *", "midnight every 2nd hour 6-20 and last day in February"},
	{"0 H * * *", "hashed hour"},
	{"0 H/3 * * *", "hashed, every 3 hours"},
	{"H H H(9-20)/3 1-11 *", "hashed across three fields"},
	{"0 0 0 * * 5#3", "midnight on the 3rd Friday"},
	{"0 0 0 8 * 5#3", "midnight on the 8th or the 3rd Friday"},
	{"0 0 0 15 * 5#3", "midnight on the 15th or the 3rd Friday"},
}

var benchStart = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

// BenchmarkParse measures parsing alone.
func BenchmarkParse(b *testing.B) {
	for _, p := range benchPatterns {
		b.Run(p.pattern, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := Parse(p.pattern, WithLocation(time.UTC), WithHashSeed("bench")); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkNext measures iteration, with parsing hoisted out so the number is
// the cost of finding one occurrence.
func BenchmarkNext(b *testing.B) {
	for _, p := range benchPatterns {
		b.Run(p.pattern, func(b *testing.B) {
			e, err := Parse(p.pattern,
				WithLocation(time.UTC), WithHashSeed("bench"), WithCurrent(benchStart))
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := e.Next(); err != nil {
					// Some patterns run out of reachable occurrences; restarting
					// keeps the measurement about iteration rather than errors.
					e.Reset(benchStart)
				}
			}
		})
	}
}

// BenchmarkParseAndIterate is the combined figure, which is what the original's
// own harness reports: parse an expression and take ten occurrences from it.
func BenchmarkParseAndIterate(b *testing.B) {
	for _, p := range benchPatterns {
		b.Run(p.pattern, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				e, err := Parse(p.pattern,
					WithLocation(time.UTC), WithHashSeed("bench"), WithCurrent(benchStart))
				if err != nil {
					b.Fatal(err)
				}
				e.Take(10)
			}
		})
	}
}

// BenchmarkNextInZone measures iteration in a zone with daylight saving, where
// the search consults the transition table.
func BenchmarkNextInZone(b *testing.B) {
	zones := []string{"UTC", "America/New_York", "Australia/Lord_Howe", "Antarctica/Troll"}
	for _, zone := range zones {
		loc, err := time.LoadLocation(zone)
		if err != nil {
			b.Skipf("zone %s unavailable", zone)
		}
		b.Run(zone, func(b *testing.B) {
			e, err := Parse("0 30 2 * * *", WithLocation(loc), WithCurrent(benchStart))
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := e.Next(); err != nil {
					e.Reset(benchStart)
				}
			}
		})
	}
}

// BenchmarkFormat measures rendering the parsed fields back to text.
func BenchmarkFormat(b *testing.B) {
	e, err := Parse("*/15 9-17 * * 1-5", WithLocation(time.UTC))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := e.Format(true); err != nil {
			b.Fatal(err)
		}
	}
}
