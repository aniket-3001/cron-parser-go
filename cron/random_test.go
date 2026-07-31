package cron

import (
	"math"
	"testing"
)

// TestSeededRandomMatchesOriginal pins the PRNG against values captured from the
// reference implementation by running its own dist/utils/random.js.
//
// The sequences must agree bit for bit, because H field values are derived from
// them: a divergence here would silently reschedule every hashed expression.
// The unicode seed is the one that matters most — it is what proves the hash
// walks UTF-16 code units, as charCodeAt does, rather than UTF-8 bytes.
func TestSeededRandomMatchesOriginal(t *testing.T) {
	tests := []struct {
		seed string
		want []float64
	}{
		{"a", []float64{
			0.62168568139895797, 0.30822347407229245, 0.36686075991019607,
			0.59513628249987960, 0.89098017965443432,
		}},
		{"hello", []float64{
			0.63119658012874424, 0.79834905150346458, 0.30862852558493614,
			0.45389872160740197, 0.04941447009332478,
		}},
		{"port-mortem", []float64{
			0.20640618237666786, 0.49566277605481446, 0.86496829940006137,
			0.38064925721846521, 0.39674381469376385,
		}},
		{"Ω-unicode-☃", []float64{
			0.29437708389014006, 0.73983847070485353, 0.58994506578892469,
			0.34390871250070632, 0.68991918419487774,
		}},
	}

	for _, tc := range tests {
		t.Run(tc.seed, func(t *testing.T) {
			r := seededRandom(tc.seed)
			for i, want := range tc.want {
				got := r()
				// The values are k/2^32 exactly, so equality is meaningful;
				// a tolerance guards only against formatting loss in the
				// captured literals.
				if math.Abs(got-want) > 1e-15 {
					t.Errorf("call %d: got %.17f, want %.17f", i+1, got, want)
				}
			}
		})
	}
}

// TestSeededRandomEmptySeedIsRandom documents that an empty seed is treated as
// no seed, matching the original's truthiness test.
func TestSeededRandomEmptySeedIsRandom(t *testing.T) {
	// Two generators built from the empty seed should almost certainly differ.
	// A shared first value across several attempts would mean "" was being
	// hashed rather than routed to the random branch.
	same := 0
	for i := 0; i < 8; i++ {
		if seededRandom("")() == seededRandom("")() {
			same++
		}
	}
	if same == 8 {
		t.Error("empty seed produced identical sequences; it should take the random branch")
	}
}

func TestSeededRandomIsDeterministic(t *testing.T) {
	a, b := seededRandom("same-seed"), seededRandom("same-seed")
	for i := 0; i < 100; i++ {
		if x, y := a(), b(); x != y {
			t.Fatalf("call %d diverged: %v vs %v", i, x, y)
		}
	}
}

func TestSeededRandomStaysInUnitInterval(t *testing.T) {
	r := seededRandom("range-check")
	for i := 0; i < 100000; i++ {
		if v := r(); v < 0 || v >= 1 {
			t.Fatalf("call %d produced %v, outside [0,1)", i, v)
		}
	}
}

func TestXfnv1aKnownValues(t *testing.T) {
	// FNV-1a 32-bit offset basis, unchanged by an empty input.
	if got := xfnv1a(""); got != 2166136261 {
		t.Errorf("xfnv1a(\"\") = %d, want 2166136261", got)
	}
	// Distinct inputs should not collide on these short seeds.
	seen := map[uint32]string{}
	for _, s := range []string{"a", "b", "hello", "world", "port-mortem", "0", "1"} {
		h := xfnv1a(s)
		if prev, dup := seen[h]; dup {
			t.Errorf("collision: %q and %q both hash to %d", prev, s, h)
		}
		seen[h] = s
	}
}
