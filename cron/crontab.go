package cron

import (
	"os"
	"regexp"
	"strings"
)

// variableRe matches a crontab variable assignment. The left side is greedy, so
// a line containing several equals signs splits at the last one.
var variableRe = regexp.MustCompile(`^(.*)=(.*)$`)

// CrontabEntry is one schedule line from a crontab file.
type CrontabEntry struct {
	// Expression is the parsed schedule.
	Expression *Expression
	// Command is whatever followed the five schedule fields, split on spaces.
	Command []string
}

// Crontab is the result of reading a crontab file.
type Crontab struct {
	// Variables holds KEY=value assignments, with surrounding quotes stripped.
	Variables map[string]string
	// Entries holds the lines that parsed as schedules.
	Entries []CrontabEntry
	// Errors maps each unparseable line to the reason it failed, so a single
	// bad line does not discard the rest of the file.
	Errors map[string]error
}

// ParseCrontab reads crontab content.
//
// It takes content rather than a path because that is the more testable shape,
// and because the test bridge must perform its own file reads: the reference
// suite replaces the filesystem module and asserts on the calls made to it, so
// reads have to happen on the JavaScript side. ParseCrontabFile is the
// convenience wrapper for ordinary Go callers.
//
// Only the first five fields of a line are treated as the schedule, matching
// the original — so a crontab line always parses in the five-field form, and
// seconds default to zero.
func ParseCrontab(content string, opts ...Option) *Crontab {
	result := &Crontab{
		Variables: map[string]string{},
		Errors:    map[string]error{},
	}

	for _, line := range strings.Split(content, "\n") {
		entry := strings.TrimSpace(line)
		if entry == "" || strings.HasPrefix(entry, "#") {
			continue
		}

		if m := variableRe.FindStringSubmatch(entry); m != nil {
			result.Variables[m[1]] = strings.NewReplacer(`"`, "", "'", "").Replace(m[2])
			continue
		}

		// The original splits on a single space rather than on runs of
		// whitespace, so repeated spaces yield empty atoms that count toward
		// the five schedule fields.
		atoms := strings.Split(entry, " ")
		schedule := atoms
		if len(schedule) > 5 {
			schedule = schedule[:5]
		}

		expr, err := Parse(strings.Join(schedule, " "), opts...)
		if err != nil {
			result.Errors[entry] = err
			continue
		}

		var command []string
		if len(atoms) > 5 {
			command = atoms[5:]
		}
		result.Entries = append(result.Entries, CrontabEntry{Expression: expr, Command: command})
	}

	return result
}

// ParseCrontabFile reads and parses a crontab file.
func ParseCrontabFile(path string, opts ...Option) (*Crontab, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseCrontab(string(data), opts...), nil
}
