package orbit

import (
	"errors"
	"fmt"
	"math"
	"sort"

	"DebrisLedger/internal/model"
)

// MaxRefinedCandidates caps how many grid-local minima are refined. Candidates
// are considered in ascending separation order, so the cap only ever discards
// shallow minima that cannot become the global optimum after refinement.
const MaxRefinedCandidates = 8

// invPhi is 1/golden-ratio, the golden-section shrink factor.
var invPhi = (math.Sqrt(5) - 1) / 2

// SearchOptions parameterises the closest-approach search.
type SearchOptions struct {
	StartS        float64
	EndS          float64
	CoarseStepS   float64
	ToleranceS    float64
	MaxIterations int
}

// Validate rejects option sets that cannot produce a search grid.
func (o SearchOptions) Validate() error {
	if !(o.EndS > o.StartS) {
		return fmt.Errorf("search window end %.6f must be after start %.6f", o.EndS, o.StartS)
	}
	if !(o.CoarseStepS > 0) {
		return errors.New("coarse step must be positive")
	}
	if o.CoarseStepS > o.EndS-o.StartS {
		return fmt.Errorf("coarse step %.6f s exceeds the %.6f s window", o.CoarseStepS, o.EndS-o.StartS)
	}
	if !(o.ToleranceS > 0) {
		return errors.New("refinement tolerance must be positive")
	}
	if o.MaxIterations < 1 {
		return errors.New("iteration cap must be positive")
	}
	return nil
}

// Approach is the outcome of a closest-approach search.
type Approach struct {
	TimeS                float64 `json:"tca_s"`
	MissKm               float64 `json:"miss_km"`
	RelSpeedKmS          float64 `json:"rel_speed_km_s"`
	RangeRateKmS         float64 `json:"range_rate_km_s"`
	RadialKm             float64 `json:"radial_km"`
	InTrackKm            float64 `json:"in_track_km"`
	CrossTrackKm         float64 `json:"cross_track_km"`
	EncounterPlaneMissKm float64 `json:"encounter_plane_miss_km"`
	CoarseSamples        int     `json:"coarse_samples"`
	RefineIterations     int     `json:"refine_iterations"`
	Candidates           int     `json:"candidates"`
	Bracketed            bool    `json:"bracketed"`
	FrameOrthogonality   float64 `json:"frame_orthogonality"`
}

// ClosestApproach finds the minimum separation between two element sets inside
// the search window.
//
// Stage one samples a uniform time grid and records every local minimum,
// including the window edges. Stage two refines each bracketed minimum with a
// golden-section search, then tightens the result by bisecting the range rate
// (which changes sign exactly at a separation extremum). Both stages are
// deterministic: no randomness, no wall clock, fixed iteration ordering.
func ClosestApproach(p Propagator, primary, secondary model.Orbit, opts SearchOptions) (Approach, error) {
	if err := opts.Validate(); err != nil {
		return Approach{}, err
	}
	f := func(t float64) float64 { return p.SeparationAt(primary, secondary, t) }

	times, values := sampleGrid(f, opts)
	candidates := localMinima(times, values)
	if len(candidates) == 0 {
		return Approach{}, errors.New("no local minimum found on the coarse grid")
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].value != candidates[j].value {
			return candidates[i].value < candidates[j].value
		}
		return candidates[i].index < candidates[j].index
	})
	if len(candidates) > MaxRefinedCandidates {
		candidates = candidates[:MaxRefinedCandidates]
	}

	bestT := candidates[0].time
	bestV := candidates[0].value
	bestBracketed := false
	iterations := 0
	for _, c := range candidates {
		lo, hi := bracketOf(times, c.index)
		t, v, iters, bracketed := refineMinimum(f, lo, hi, opts)
		iterations += iters
		if v < bestV || (v == bestV && t < bestT) {
			bestT, bestV, bestBracketed = t, v, bracketed
		}
	}

	rel := p.Relative(primary, secondary, bestT)
	primaryState := p.StateAt(primary, bestT)
	secondaryState := p.StateAt(secondary, bestT)
	sep := secondaryState.Position.Sub(primaryState.Position)
	relVel := secondaryState.Velocity.Sub(primaryState.Velocity)
	frame := NewRICFrame(primaryState)

	return Approach{
		TimeS:                bestT,
		MissKm:               bestV,
		RelSpeedKmS:          rel.RelSpeedKmS,
		RangeRateKmS:         rel.RangeRateKmS,
		RadialKm:             rel.Radial,
		InTrackKm:            rel.InTrack,
		CrossTrackKm:         rel.CrossTrack,
		EncounterPlaneMissKm: EncounterPlaneMiss(sep, relVel),
		CoarseSamples:        len(times),
		RefineIterations:     iterations,
		Candidates:           len(candidates),
		Bracketed:            bestBracketed,
		FrameOrthogonality:   frame.Orthogonality(),
	}, nil
}

type gridCandidate struct {
	index int
	time  float64
	value float64
}

// sampleGrid evaluates f on a uniform grid that always includes both window
// edges. Steps are computed from the index rather than accumulated so that
// floating-point drift cannot make the grid input-order dependent.
func sampleGrid(f func(float64) float64, opts SearchOptions) ([]float64, []float64) {
	span := opts.EndS - opts.StartS
	steps := int(math.Floor(span/opts.CoarseStepS + 0.5))
	if steps < 1 {
		steps = 1
	}
	times := make([]float64, 0, steps+1)
	values := make([]float64, 0, steps+1)
	for i := 0; i <= steps; i++ {
		t := opts.StartS + span*float64(i)/float64(steps)
		times = append(times, t)
		values = append(values, f(t))
	}
	return times, values
}

// localMinima returns the grid points that are no larger than both neighbours,
// treating the window edges as one-sided minima.
func localMinima(times, values []float64) []gridCandidate {
	out := make([]gridCandidate, 0, 4)
	n := len(values)
	for i := 0; i < n; i++ {
		leftOK := i == 0 || values[i] <= values[i-1]
		rightOK := i == n-1 || values[i] <= values[i+1]
		if leftOK && rightOK {
			out = append(out, gridCandidate{index: i, time: times[i], value: values[i]})
		}
	}
	if len(out) == 0 {
		best := 0
		for i := 1; i < n; i++ {
			if values[i] < values[best] {
				best = i
			}
		}
		out = append(out, gridCandidate{index: best, time: times[best], value: values[best]})
	}
	return out
}

func bracketOf(times []float64, index int) (float64, float64) {
	lo := index - 1
	if lo < 0 {
		lo = 0
	}
	hi := index + 1
	if hi > len(times)-1 {
		hi = len(times) - 1
	}
	return times[lo], times[hi]
}

// refineMinimum runs golden-section then range-rate bisection on [lo, hi].
func refineMinimum(f func(float64) float64, lo, hi float64, opts SearchOptions) (float64, float64, int, bool) {
	if hi <= lo {
		return lo, f(lo), 0, false
	}
	t, v, iters := GoldenSectionMin(f, lo, hi, opts.ToleranceS, opts.MaxIterations)
	width := math.Max(opts.ToleranceS, (hi-lo)/64)
	bt, bv, biters, ok := bisectRangeRate(f, t-width, t+width, lo, hi, opts)
	iters += biters
	if ok && bv <= v {
		return bt, bv, iters, true
	}
	interior := t > lo+opts.ToleranceS && t < hi-opts.ToleranceS
	return t, v, iters, interior
}

// GoldenSectionMin minimises f on [a, b] assuming a single interior minimum.
// It returns the argument, the value and the number of evaluations performed.
//
// Golden-section is used instead of a derivative method because the separation
// function is cheap but its analytic derivative is awkward once nodal drift is
// enabled, and because the shrink factor is constant, which keeps the iteration
// count a pure function of the requested tolerance.
func GoldenSectionMin(f func(float64) float64, a, b, tol float64, maxIter int) (float64, float64, int) {
	if b < a {
		a, b = b, a
	}
	c := b - (b-a)*invPhi
	d := a + (b-a)*invPhi
	fc, fd := f(c), f(d)
	evals := 2
	for i := 0; i < maxIter && (b-a) > tol; i++ {
		if fc < fd || (fc == fd && c < d) {
			b, d, fd = d, c, fc
			c = b - (b-a)*invPhi
			fc = f(c)
		} else {
			a, c, fc = c, d, fd
			d = a + (b-a)*invPhi
			fd = f(d)
		}
		evals++
	}
	mid := (a + b) / 2
	fm := f(mid)
	evals++
	if fc < fm {
		mid, fm = c, fc
	}
	if fd < fm {
		mid, fm = d, fd
	}
	return mid, fm, evals
}

// bisectRangeRate finds the instant where the numerical range rate changes from
// negative to positive, which is the separation minimum. The bracket is clamped
// to the coarse cell so that refinement can never leave the searched window.
func bisectRangeRate(f func(float64) float64, lo, hi, clampLo, clampHi float64, opts SearchOptions) (float64, float64, int, bool) {
	if lo < clampLo {
		lo = clampLo
	}
	if hi > clampHi {
		hi = clampHi
	}
	if hi <= lo {
		return 0, 0, 0, false
	}
	h := math.Max(opts.ToleranceS/8, 1e-6)
	rate := func(t float64) float64 { return (f(t+h) - f(t-h)) / (2 * h) }
	rlo, rhi := rate(lo), rate(hi)
	evals := 4
	if rlo > 0 || rhi < 0 {
		return 0, 0, evals, false
	}
	for i := 0; i < opts.MaxIterations && (hi-lo) > opts.ToleranceS; i++ {
		mid := (lo + hi) / 2
		rm := rate(mid)
		evals += 2
		if rm < 0 {
			lo, rlo = mid, rm
		} else {
			hi, rhi = mid, rm
		}
	}
	mid := (lo + hi) / 2
	return mid, f(mid), evals + 1, true
}

// BruteForceMinimum is the reference implementation used by the numerical
// tests: it scans the window with a fixed fine step and returns the best
// sample. It is exported so that other packages can cross-check the refined
// search without duplicating the sampling logic.
func BruteForceMinimum(p Propagator, primary, secondary model.Orbit, startS, endS, stepS float64) (float64, float64) {
	bestT, bestV := startS, math.Inf(1)
	if stepS <= 0 || endS <= startS {
		return startS, p.SeparationAt(primary, secondary, startS)
	}
	steps := int(math.Floor((endS-startS)/stepS + 0.5))
	if steps < 1 {
		steps = 1
	}
	for i := 0; i <= steps; i++ {
		t := startS + (endS-startS)*float64(i)/float64(steps)
		v := p.SeparationAt(primary, secondary, t)
		if v < bestV {
			bestT, bestV = t, v
		}
	}
	return bestT, bestV
}

// InsideVolume reports whether the RIC components of an approach fall inside an
// ellipsoidal screening volume, using the normalised sum of squares.
func InsideVolume(a Approach, v model.ScreeningVolume) bool {
	if v.RadialKm <= 0 || v.InTrackKm <= 0 || v.CrossTrackKm <= 0 {
		return false
	}
	r := a.RadialKm / v.RadialKm
	i := a.InTrackKm / v.InTrackKm
	c := a.CrossTrackKm / v.CrossTrackKm
	return r*r+i*i+c*c <= 1
}
