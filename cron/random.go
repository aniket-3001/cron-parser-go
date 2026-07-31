package cron

import (
	"math/rand"
	"unicode/utf16"
)

// prng returns successive pseudo-random values in [0, 1), matching the shape of
// JavaScript's Math.random.
type prng func() float64

// xfnv1a is the FNV-1a string hash the original uses to derive a seed.
//
// The arithmetic is deliberately uint32 throughout. JavaScript's Math.imul is a
// 32-bit multiply that wraps, and `h >>> 0` coerces to unsigned 32-bit; Go's
// uint32 reproduces both exactly, whereas int would diverge once the product
// overflows.
//
// The input is walked as UTF-16 code units rather than bytes because
// String.prototype.charCodeAt yields code units. For ASCII seeds the two agree,
// but any non-ASCII character would hash differently if bytes were used.
func xfnv1a(s string) uint32 {
	h := uint32(2166136261)
	for _, u := range utf16.Encode([]rune(s)) {
		h ^= uint32(u)
		h *= 16777619
	}
	return h
}

// mulberry32 is the small PRNG the original seeds with xfnv1a.
//
// Every operation stays in uint32. In JavaScript the intermediate
// `t + Math.imul(...)` is evaluated as a float64 and can exceed 32 bits, but the
// enclosing XOR coerces it back via ToInt32 — that is, modulo 2^32 — which is
// what uint32 addition does natively, so the bit patterns agree.
func mulberry32(seed uint32) prng {
	return func() float64 {
		seed += 0x6d2b79f5
		t := seed
		t = (t ^ (t >> 15)) * (t | 1)
		t ^= t + (t^(t>>7))*(t|61)
		return float64(t^(t>>14)) / 4294967296
	}
}

// seededRandom builds the generator used to resolve H (hashed) field values.
//
// With a seed the sequence is deterministic, which is the point of H: a fleet of
// machines sharing a seed spreads its work without stampeding. Without one the
// original falls back to Math.random, so H values differ on every parse; that
// non-determinism is reproduced rather than fixed, since callers may rely on
// unseeded H being scattered.
//
// An empty seed counts as no seed. The original tests the string for truthiness
// — `str ? xfnv1a(str)() : ...` — so "" takes the random branch, and a caller
// passing an empty hashSeed gets scattered values rather than a fixed sequence.
func seededRandom(seed string) prng {
	if seed == "" {
		// The original picks Math.floor(Math.random() * 10_000_000_000), which
		// exceeds 32 bits; only the low 32 bits survive the first coercion, so
		// seeding directly from a uint32 is equivalent.
		return mulberry32(rand.Uint32())
	}
	return mulberry32(xfnv1a(seed))
}
