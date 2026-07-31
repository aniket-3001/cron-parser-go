package cron

import (
	"strconv"
	"strings"
)

// This file reproduces the two JavaScript number coercions the parser depends
// on. They differ from each other and from Go's strconv in ways that change
// which inputs are rejected and what the resulting error message says, so they
// are modelled explicitly rather than approximated.

// jsNumber mirrors unary plus — Number(s).
//
// It differs from parseInt in two ways that matter: an empty or all-whitespace
// string converts to 0 rather than NaN, and any trailing garbage makes the whole
// conversion NaN rather than being ignored. The parser relies on the second
// behaviour to tell "5" from "5L": the former becomes a number and the latter
// stays a token.
//
// The second return reports whether the conversion produced a number; false
// stands for NaN.
func jsNumber(s string) (float64, bool) {
	t := strings.TrimSpace(s)
	if t == "" {
		return 0, true
	}
	f, err := strconv.ParseFloat(t, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

// jsParseInt mirrors parseInt(s, 10).
//
// Leading whitespace is skipped, an optional sign is consumed, and digits are
// read until the first non-digit; anything after that is discarded, so "5abc"
// is 5. With no leading digits the result is NaN, reported as false.
func jsParseInt(s string) (int, bool) {
	i := 0
	for i < len(s) && isJSSpace(s[i]) {
		i++
	}

	start := i
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}

	digitsStart := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == digitsStart {
		return 0, false // no digits: NaN
	}

	n, err := strconv.Atoi(s[start:i])
	if err != nil {
		return 0, false
	}
	return n, true
}

// itoa is a local shorthand; the parser converts small integers constantly.
func itoa(n int) string { return strconv.Itoa(n) }

func isJSSpace(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\v', '\f', '\r':
		return true
	}
	return false
}

// numText renders a possibly-NaN number the way JavaScript string interpolation
// would, so error messages match the original exactly. A failed conversion
// interpolates as "NaN".
func numText(n int, ok bool) string {
	if !ok {
		return "NaN"
	}
	return strconv.Itoa(n)
}
