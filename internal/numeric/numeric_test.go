package numeric

import (
	"math"
	"testing"
)

func TestRoundHalfAwayFromZero(t *testing.T) {
	cases := []struct {
		in       float64
		decimals int
		want     float64
	}{
		{1.2345, 3, 1.235},
		{-1.2345, 3, -1.235},
		{2.5, 0, 3},
		{-2.5, 0, -3},
		{0.0004999, 3, 0},
		{123.456, -2, 123},
	}
	for _, c := range cases {
		if got := Round(c.in, c.decimals); math.Abs(got-c.want) > 1e-12 {
			t.Fatalf("Round(%v,%d) = %v, want %v", c.in, c.decimals, got, c.want)
		}
	}
	if got := Round(math.NaN(), 3); !math.IsNaN(got) {
		t.Fatal("NaN must pass through unchanged")
	}
	if got := Round(math.Inf(1), 3); !math.IsInf(got, 1) {
		t.Fatal("infinity must pass through unchanged")
	}
	if got := Round(1.23456789012345678, 40); got == 0 {
		t.Fatal("an excessive decimal count must be clamped, not zeroed")
	}
}

func TestRoundSignificant(t *testing.T) {
	if got := RoundSignificant(0.000123456, 3); math.Abs(got-0.000123) > 1e-12 {
		t.Fatalf("got %v", got)
	}
	if got := RoundSignificant(123456, 2); got != 120000 {
		t.Fatalf("got %v", got)
	}
	if got := RoundSignificant(0, 4); got != 0 {
		t.Fatalf("zero must stay zero, got %v", got)
	}
	if got := RoundSignificant(1.5, 0); math.Abs(got-2) > 1e-12 {
		t.Fatalf("digit counts below one must clamp to one, got %v", got)
	}
	if got := RoundSignificant(math.Inf(-1), 3); !math.IsInf(got, -1) {
		t.Fatal("infinity must pass through")
	}
}

func TestFixedAndSciFormatting(t *testing.T) {
	if got := Fixed(1.23456, 3); got != "1.235" {
		t.Fatalf("Fixed = %q", got)
	}
	if got := Fixed(-0.0004, 3); got != "-0.000" {
		t.Fatalf("Fixed = %q", got)
	}
	if got := Fixed(math.NaN(), 2); got != "NaN" {
		t.Fatalf("Fixed(NaN) = %q", got)
	}
	if got := Fixed(math.Inf(1), 2); got != "+Inf" {
		t.Fatalf("Fixed(+Inf) = %q", got)
	}
	if got := Fixed(math.Inf(-1), 2); got != "-Inf" {
		t.Fatalf("Fixed(-Inf) = %q", got)
	}
	if got := Sci(0, 4); got != "0.0000e+00" {
		t.Fatalf("Sci(0) = %q", got)
	}
	if got := Sci(1234.5, 3); got != "1.234e+03" && got != "1.235e+03" {
		t.Fatalf("Sci = %q", got)
	}
	if got := Sci(math.NaN(), 3); got != "NaN" {
		t.Fatalf("Sci(NaN) = %q", got)
	}
	if got := Sci(math.Inf(1), 3); got != "+Inf" {
		t.Fatalf("Sci(+Inf) = %q", got)
	}
	if got := Sci(math.Inf(-1), 3); got != "-Inf" {
		t.Fatalf("Sci(-Inf) = %q", got)
	}
}

func TestClampAndRatio(t *testing.T) {
	if got := Clamp(5, 0, 1); got != 1 {
		t.Fatalf("Clamp high = %v", got)
	}
	if got := Clamp(-5, 0, 1); got != 0 {
		t.Fatalf("Clamp low = %v", got)
	}
	if got := Clamp(0.5, 0, 1); got != 0.5 {
		t.Fatalf("Clamp mid = %v", got)
	}
	if got := Ratio(1, 0); got != 0 {
		t.Fatalf("Ratio by zero = %v", got)
	}
	if got := Ratio(3, 4); got != 0.75 {
		t.Fatalf("Ratio = %v", got)
	}
}

func TestSumFloatsIsOrderIndependent(t *testing.T) {
	values := []float64{1e16, 1, -1e16, 2.5, 0.25}
	forward := SumFloats(values)
	reversed := make([]float64, len(values))
	for i := range values {
		reversed[i] = values[len(values)-1-i]
	}
	if backward := SumFloats(reversed); backward != forward {
		t.Fatalf("sum depends on input order: %v vs %v", forward, backward)
	}
	if SumFloats(nil) != 0 {
		t.Fatal("the empty sum must be zero")
	}
	// The input slice must not be mutated.
	if values[0] != 1e16 {
		t.Fatal("SumFloats must not reorder its argument")
	}
}

func TestMaxFloat(t *testing.T) {
	if got := MaxFloat([]float64{1, 7, 3}); got != 7 {
		t.Fatalf("MaxFloat = %v", got)
	}
	if got := MaxFloat(nil); got != 0 {
		t.Fatalf("MaxFloat(nil) = %v", got)
	}
}

func TestDurationText(t *testing.T) {
	cases := map[int64]string{
		0:      "0s",
		45:     "45s",
		90:     "1m30s",
		3661:   "1h1m1s",
		90061:  "1d1h1m1s",
		-3661:  "-1h1m1s",
		259200: "3d0h0m0s",
	}
	for in, want := range cases {
		if got := DurationText(in); got != want {
			t.Fatalf("DurationText(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestSecondsToWholeSeconds(t *testing.T) {
	cases := map[float64]int64{
		0: 0, 1.4: 1, 1.5: 2, -1.5: -2, -0.4: 0, 43199.6: 43200,
	}
	for in, want := range cases {
		if got := SecondsToWholeSeconds(in); got != want {
			t.Fatalf("SecondsToWholeSeconds(%v) = %d, want %d", in, got, want)
		}
	}
	if got := SecondsToWholeSeconds(math.NaN()); got != 0 {
		t.Fatalf("NaN must map to zero, got %d", got)
	}
	if got := SecondsToWholeSeconds(math.Inf(1)); got != 0 {
		t.Fatalf("infinity must map to zero, got %d", got)
	}
}

func TestPadding(t *testing.T) {
	if got := Pad("ab", 5); got != "ab   " {
		t.Fatalf("Pad = %q", got)
	}
	if got := PadLeft("ab", 5); got != "   ab" {
		t.Fatalf("PadLeft = %q", got)
	}
	if got := Pad("abcdef", 3); got != "abcdef" {
		t.Fatalf("Pad must not truncate, got %q", got)
	}
	if got := PadLeft("abcdef", 3); got != "abcdef" {
		t.Fatalf("PadLeft must not truncate, got %q", got)
	}
}
