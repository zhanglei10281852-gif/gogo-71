// Package numeric holds the rounding and formatting helpers that make every
// DebrisLedger artefact byte-for-byte reproducible.
//
// Two rules are enforced through this package:
//
//  1. Every float that reaches a stored artefact is rounded to a fixed number
//     of decimals first, so that a difference below the reported precision can
//     never change a file.
//  2. Every float is rendered with strconv in a fixed notation, never with the
//     %v verb, so that platform differences in shortest-representation
//     printing cannot leak into output.
package numeric

import (
	"math"
	"sort"
	"strconv"
	"strings"
)

// Round rounds v half-away-from-zero to the given number of decimals.
// Non-finite values are returned unchanged so that a bug upstream stays
// visible instead of being silently converted to zero.
func Round(v float64, decimals int) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return v
	}
	if decimals < 0 {
		decimals = 0
	}
	if decimals > 15 {
		decimals = 15
	}
	scale := math.Pow(10, float64(decimals))
	scaled := v * scale
	if v < 0 {
		return -math.Floor(-scaled+0.5) / scale
	}
	return math.Floor(scaled+0.5) / scale
}

// RoundSignificant rounds v to the given number of significant digits, which is
// how probabilities are stored: their magnitude spans many decades.
func RoundSignificant(v float64, digits int) float64 {
	if v == 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return v
	}
	if digits < 1 {
		digits = 1
	}
	if digits > 15 {
		digits = 15
	}
	mag := math.Floor(math.Log10(math.Abs(v)))
	scale := math.Pow(10, float64(digits-1)-mag)
	return math.Floor(v*scale+0.5) / scale
}

// Fixed formats v with a fixed number of decimals.
func Fixed(v float64, decimals int) string {
	if math.IsNaN(v) {
		return "NaN"
	}
	if math.IsInf(v, 1) {
		return "+Inf"
	}
	if math.IsInf(v, -1) {
		return "-Inf"
	}
	return strconv.FormatFloat(Round(v, decimals), 'f', decimals, 64)
}

// Sci formats v in exponent notation with the given number of mantissa digits.
func Sci(v float64, digits int) string {
	if math.IsNaN(v) {
		return "NaN"
	}
	if math.IsInf(v, 0) {
		if v > 0 {
			return "+Inf"
		}
		return "-Inf"
	}
	if v == 0 {
		return "0." + strings.Repeat("0", digits) + "e+00"
	}
	return strconv.FormatFloat(v, 'e', digits, 64)
}

// Clamp constrains v to [lo, hi].
func Clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Ratio divides safely, returning 0 when the denominator is zero.
func Ratio(num, den float64) float64 {
	if den == 0 {
		return 0
	}
	return num / den
}

// SumFloats adds a slice using a sorted-magnitude order so that the result does
// not depend on the input ordering of equal-magnitude terms.
func SumFloats(values []float64) float64 {
	ordered := make([]float64, len(values))
	copy(ordered, values)
	sort.Float64s(ordered)
	total := 0.0
	for _, v := range ordered {
		total += v
	}
	return total
}

// MaxFloat returns the largest value, or 0 for an empty slice.
func MaxFloat(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	best := values[0]
	for _, v := range values[1:] {
		if v > best {
			best = v
		}
	}
	return best
}

// DurationText renders a whole number of seconds as a deterministic
// day/hour/minute/second string.
func DurationText(seconds int64) string {
	neg := seconds < 0
	if neg {
		seconds = -seconds
	}
	days := seconds / 86400
	seconds -= days * 86400
	hours := seconds / 3600
	seconds -= hours * 3600
	minutes := seconds / 60
	seconds -= minutes * 60
	var b strings.Builder
	if neg {
		b.WriteString("-")
	}
	if days > 0 {
		b.WriteString(strconv.FormatInt(days, 10))
		b.WriteString("d")
	}
	if days > 0 || hours > 0 {
		b.WriteString(strconv.FormatInt(hours, 10))
		b.WriteString("h")
	}
	if days > 0 || hours > 0 || minutes > 0 {
		b.WriteString(strconv.FormatInt(minutes, 10))
		b.WriteString("m")
	}
	b.WriteString(strconv.FormatInt(seconds, 10))
	b.WriteString("s")
	return b.String()
}

// SecondsToWholeSeconds rounds a floating mission time to whole seconds using
// half-away-from-zero so that schedules never carry sub-second noise.
func SecondsToWholeSeconds(t float64) int64 {
	if math.IsNaN(t) || math.IsInf(t, 0) {
		return 0
	}
	if t < 0 {
		return -int64(math.Floor(-t + 0.5))
	}
	return int64(math.Floor(t + 0.5))
}

// Pad right-pads s with spaces to width, and never truncates.
func Pad(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

// PadLeft left-pads s with spaces to width, and never truncates.
func PadLeft(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return strings.Repeat(" ", width-len(s)) + s
}
