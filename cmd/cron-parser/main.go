// Command cron-parser answers questions about cron expressions from the shell.
//
// It exists so the port is a runnable artifact rather than only a library, and
// so the two implementations can be compared on identical input from a command
// line. The output shape is deliberately plain: one instant per line, or JSON
// when asked, so a diff against the original's output is meaningful.
//
//	cron-parser next  "*/15 9-17 * * 1-5" -n 5 -tz America/New_York
//	cron-parser prev  "0 0 * * *" -n 3
//	cron-parser parse "0 0 L * *" -json
//	cron-parser check "0 0 * * *" -at 2026-01-02T00:00:00Z
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	// The zone database is embedded so the binary behaves identically wherever
	// it runs, including in a scratch container with no system zoneinfo.
	_ "time/tzdata"

	"github.com/aniket-3001/cron-parser-go/cron"
)

const usage = `cron-parser, evaluate cron expressions

usage:
  cron-parser next  <expression> [-n count] [-tz zone] [-from time] [-json]
  cron-parser prev  <expression> [-n count] [-tz zone] [-from time] [-json]
  cron-parser parse <expression> [-tz zone] [-json]
  cron-parser check <expression> -at <time> [-tz zone]

commands:
  next    the next occurrences
  prev    the previous occurrences
  parse   the expression's fields and its canonical form
  check   whether the expression fires at a given instant

flags:
  -n      how many occurrences to print (default 1)
  -tz     IANA timezone, for example Europe/Sofia (default UTC)
  -from   starting instant, RFC 3339 (default now)
  -at     the instant to test, RFC 3339
  -seed   hash seed, making H fields deterministic
  -json   emit JSON instead of one instant per line

Five or six fields are accepted. With five, a zero seconds field is prepended.
`

type fieldOut struct {
	Values   []string `json:"values"`
	Wildcard bool     `json:"wildcard"`
}

func main() {
	if len(os.Args) < 3 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	command, expression := os.Args[1], os.Args[2]

	fs := flag.NewFlagSet(command, flag.ExitOnError)
	count := fs.Int("n", 1, "how many occurrences to print")
	zone := fs.String("tz", "UTC", "IANA timezone")
	from := fs.String("from", "", "starting instant, RFC 3339")
	at := fs.String("at", "", "instant to test, RFC 3339")
	seed := fs.String("seed", "", "hash seed for H fields")
	asJSON := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(os.Args[3:]); err != nil {
		os.Exit(2)
	}

	loc, err := time.LoadLocation(*zone)
	if err != nil {
		fail("unknown timezone %q: %v", *zone, err)
	}

	opts := []cron.Option{cron.WithLocation(loc)}
	if *seed != "" {
		opts = append(opts, cron.WithHashSeed(*seed))
	}
	if *from != "" {
		t, err := time.Parse(time.RFC3339, *from)
		if err != nil {
			fail("-from must be RFC 3339: %v", err)
		}
		opts = append(opts, cron.WithCurrent(t))
	}

	expr, err := cron.Parse(expression, opts...)
	if err != nil {
		// Parse failures go to stderr with a non-zero status, so a shell can
		// tell a rejected expression from one that simply never fires.
		fail("%v", err)
	}

	switch command {
	case "next", "prev":
		printOccurrences(expr, command, *count, loc, *asJSON)
	case "parse":
		printFields(expr, expression, *asJSON)
	case "check":
		if *at == "" {
			fail("check requires -at")
		}
		t, err := time.Parse(time.RFC3339, *at)
		if err != nil {
			fail("-at must be RFC 3339: %v", err)
		}
		matched, err := expr.Includes(t)
		if err != nil {
			fail("%v", err)
		}
		fmt.Println(matched)
		if !matched {
			// A shell can branch on this without parsing the output.
			os.Exit(1)
		}
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
}

func printOccurrences(expr *cron.Expression, direction string, count int, loc *time.Location, asJSON bool) {
	step := expr.Next
	if direction == "prev" {
		step = expr.Prev
	}

	out := make([]string, 0, count)
	for i := 0; i < count; i++ {
		t, err := step()
		if err != nil {
			// Running out of occurrences is not a failure: an expression bounded
			// by a window legitimately ends. Whatever was found is still printed.
			break
		}
		out = append(out, t.In(loc).Format(time.RFC3339))
	}

	if asJSON {
		emit(map[string]any{"occurrences": out})
		return
	}
	for _, s := range out {
		fmt.Println(s)
	}
}

func printFields(expr *cron.Expression, raw string, asJSON bool) {
	f := expr.Fields()
	canonical, err := expr.Format(true)
	if err != nil {
		fail("%v", err)
	}

	named := []struct {
		name  string
		field *cron.Field
	}{
		{"second", f.Second},
		{"minute", f.Minute},
		{"hour", f.Hour},
		{"dayOfMonth", f.DayOfMonth},
		{"month", f.Month},
		{"dayOfWeek", f.DayOfWeek},
	}

	if asJSON {
		fields := map[string]fieldOut{}
		for _, n := range named {
			fields[n.name] = fieldOut{Values: valueStrings(n.field), Wildcard: n.field.IsWildcard()}
		}
		emit(map[string]any{"expression": raw, "canonical": canonical, "fields": fields})
		return
	}

	fmt.Printf("expression  %s\n", raw)
	fmt.Printf("canonical   %s\n", canonical)
	for _, n := range named {
		mark := ""
		if n.field.IsWildcard() {
			mark = "  (wildcard)"
		}
		fmt.Printf("%-11s %s%s\n", n.name, strings.Join(valueStrings(n.field), ","), mark)
	}
}

func valueStrings(f *cron.Field) []string {
	values := f.Values()
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = v.String()
	}
	return out
}

func emit(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fail("%v", err)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "cron-parser: "+format+"\n", args...)
	os.Exit(1)
}
