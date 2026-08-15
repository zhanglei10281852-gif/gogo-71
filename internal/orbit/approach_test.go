package orbit

import (
	"math"
	"testing"

	"DebrisLedger/internal/model"
)

// coplanarPair builds a leader/follower pair whose closest approach happens at a
// known instant.
//
// Both orbits share a plane and differ only in altitude. The higher orbit is
// slower, so an initial angular lead of du closes at the rate dn = n1 - n2 and
// the argument of latitude matches exactly at t = du/dn. At that instant the
// separation is exactly the radial difference of the two orbits.
func coplanarPair(altKm, deltaAltKm, wantTCA float64) (model.Orbit, model.Orbit, float64) {
	primary := model.Orbit{AltitudeKm: altKm, InclinationDeg: 53.1, RAANDeg: 84.5, ArgLatDeg: 12, EpochSeconds: 0}
	dn := MeanMotionRadPerS(altKm) - MeanMotionRadPerS(altKm+deltaAltKm)
	secondary := primary
	secondary.AltitudeKm = altKm + deltaAltKm
	secondary.ArgLatDeg = WrapAngleDeg(primary.ArgLatDeg + Deg(dn*wantTCA))
	return primary, secondary, wantTCA
}

func defaultOptions(end float64) SearchOptions {
	return SearchOptions{StartS: 0, EndS: end, CoarseStepS: 20, ToleranceS: 0.01, MaxIterations: 200}
}

func TestClosestApproachMatchesAnalyticTCA(t *testing.T) {
	p := NewPropagator(false)
	primary, secondary, wantTCA := coplanarPair(552, 0.5, 43200)
	app, err := ClosestApproach(p, primary, secondary, defaultOptions(259200))
	if err != nil {
		t.Fatalf("ClosestApproach: %v", err)
	}
	if diff := math.Abs(app.TimeS - wantTCA); diff > 0.05 {
		t.Fatalf("closest approach at %.4f s, want %.4f s (error %.4f s)", app.TimeS, wantTCA, diff)
	}
	if diff := math.Abs(app.MissKm - 0.5); diff > 1e-6 {
		t.Fatalf("miss distance %.9f km, want 0.5 km", app.MissKm)
	}
	if diff := math.Abs(app.RadialKm - 0.5); diff > 1e-6 {
		t.Fatalf("radial component %.9f km, want 0.5 km", app.RadialKm)
	}
	if math.Abs(app.InTrackKm) > 1e-3 {
		t.Fatalf("in-track component %.9f km should vanish at the closest approach", app.InTrackKm)
	}
	if math.Abs(app.CrossTrackKm) > 1e-9 {
		t.Fatalf("coplanar pair has cross-track component %.9f km", app.CrossTrackKm)
	}
	if math.Abs(app.RangeRateKmS) > 1e-6 {
		t.Fatalf("range rate %.9f km/s should vanish at the closest approach", app.RangeRateKmS)
	}
	if !app.Bracketed {
		t.Fatal("the minimum should have been bracketed")
	}
	if app.CoarseSamples < 2 || app.RefineIterations < 1 {
		t.Fatalf("unexpected search diagnostics: %+v", app)
	}
	if app.FrameOrthogonality > 1e-12 {
		t.Fatalf("frame self-check failed: %g", app.FrameOrthogonality)
	}
}

func TestClosestApproachAgreesWithBruteForce(t *testing.T) {
	p := NewPropagator(true)
	primary, secondary, wantTCA := coplanarPair(706.5, 0.5, 86400)
	app, err := ClosestApproach(p, primary, secondary, defaultOptions(259200))
	if err != nil {
		t.Fatalf("ClosestApproach: %v", err)
	}
	bruteT, bruteV := BruteForceMinimum(p, primary, secondary, wantTCA-600, wantTCA+600, 0.01)
	if app.MissKm > bruteV+1e-9 {
		t.Fatalf("refined miss %.12f km is worse than the brute-force miss %.12f km", app.MissKm, bruteV)
	}
	if diff := math.Abs(app.TimeS - bruteT); diff > 0.05 {
		t.Fatalf("refined TCA %.4f s differs from the brute-force TCA %.4f s by %.4f s", app.TimeS, bruteT, diff)
	}
}

func TestClosestApproachIsInsensitiveToCoarseStep(t *testing.T) {
	p := NewPropagator(true)
	primary, secondary, _ := coplanarPair(618, 0.35, 129600)
	var results []Approach
	for _, step := range []float64{10, 20, 25, 60} {
		opts := defaultOptions(259200)
		opts.CoarseStepS = step
		app, err := ClosestApproach(p, primary, secondary, opts)
		if err != nil {
			t.Fatalf("step %v: %v", step, err)
		}
		results = append(results, app)
	}
	for i := 1; i < len(results); i++ {
		if diff := math.Abs(results[i].TimeS - results[0].TimeS); diff > 0.2 {
			t.Fatalf("coarse step changed the TCA by %.4f s", diff)
		}
		if diff := math.Abs(results[i].MissKm - results[0].MissKm); diff > 1e-6 {
			t.Fatalf("coarse step changed the miss distance by %.9f km", diff)
		}
	}
}

func TestClosestApproachIsDeterministic(t *testing.T) {
	p := NewPropagator(true)
	primary, secondary, _ := coplanarPair(552, 0.4, 46800)
	first, err := ClosestApproach(p, primary, secondary, defaultOptions(259200))
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	for i := 0; i < 4; i++ {
		again, err := ClosestApproach(p, primary, secondary, defaultOptions(259200))
		if err != nil {
			t.Fatalf("repeat run: %v", err)
		}
		if again != first {
			t.Fatalf("repeat %d produced a different result:\n%+v\n%+v", i, again, first)
		}
	}
}

func TestClosestApproachRejectsBadOptions(t *testing.T) {
	p := NewPropagator(false)
	primary, secondary, _ := coplanarPair(552, 0.5, 43200)
	cases := []SearchOptions{
		{StartS: 0, EndS: 0, CoarseStepS: 10, ToleranceS: 0.1, MaxIterations: 10},
		{StartS: 0, EndS: 100, CoarseStepS: 0, ToleranceS: 0.1, MaxIterations: 10},
		{StartS: 0, EndS: 100, CoarseStepS: 200, ToleranceS: 0.1, MaxIterations: 10},
		{StartS: 0, EndS: 100, CoarseStepS: 10, ToleranceS: 0, MaxIterations: 10},
		{StartS: 0, EndS: 100, CoarseStepS: 10, ToleranceS: 0.1, MaxIterations: 0},
	}
	for i, opts := range cases {
		if _, err := ClosestApproach(p, primary, secondary, opts); err == nil {
			t.Fatalf("case %d: expected an error for %+v", i, opts)
		}
	}
}

func TestClosestApproachFindsEdgeMinimum(t *testing.T) {
	// The pair is configured so that the minimum lies beyond the end of the
	// window; the search must then return the window edge without failing.
	p := NewPropagator(false)
	primary, secondary, _ := coplanarPair(552, 0.5, 200000)
	opts := defaultOptions(20000)
	app, err := ClosestApproach(p, primary, secondary, opts)
	if err != nil {
		t.Fatalf("ClosestApproach: %v", err)
	}
	if app.TimeS < 19000 {
		t.Fatalf("expected the minimum at the end of the window, got %.3f s", app.TimeS)
	}
	if app.MissKm <= 0.5 {
		t.Fatalf("edge miss %.6f km should exceed the radial separation", app.MissKm)
	}
}

func TestGoldenSectionMinOnAnalyticFunction(t *testing.T) {
	f := func(x float64) float64 { return (x-3.7)*(x-3.7) + 2 }
	x, v, evals := GoldenSectionMin(f, 0, 10, 1e-9, 500)
	if math.Abs(x-3.7) > 1e-6 {
		t.Fatalf("minimum at %.12f, want 3.7", x)
	}
	if math.Abs(v-2) > 1e-9 {
		t.Fatalf("minimum value %.12f, want 2", v)
	}
	if evals < 3 {
		t.Fatalf("suspiciously few evaluations: %d", evals)
	}
	// A reversed bracket must be handled.
	xr, _, _ := GoldenSectionMin(f, 10, 0, 1e-9, 500)
	if math.Abs(xr-3.7) > 1e-6 {
		t.Fatalf("reversed bracket gave %.12f", xr)
	}
}

func TestGoldenSectionMinRespectsIterationCap(t *testing.T) {
	f := func(x float64) float64 { return math.Abs(x - 1) }
	x, _, _ := GoldenSectionMin(f, -100, 100, 1e-12, 5)
	if math.Abs(x-1) < 1e-6 {
		t.Fatal("a five-iteration search should not have converged fully")
	}
}

func TestLocalMinimaDetectsEdgesAndInterior(t *testing.T) {
	times := []float64{0, 1, 2, 3, 4}
	values := []float64{1, 2, 0.5, 2, 0.25}
	got := localMinima(times, values)
	if len(got) != 3 {
		t.Fatalf("expected three minima, got %d (%+v)", len(got), got)
	}
	if got[0].index != 0 || got[1].index != 2 || got[2].index != 4 {
		t.Fatalf("unexpected minima indices: %+v", got)
	}
	flat := localMinima([]float64{0, 1}, []float64{5, 5})
	if len(flat) != 2 {
		t.Fatalf("a flat function should report both samples, got %d", len(flat))
	}
}

func TestSampleGridIncludesBothEdges(t *testing.T) {
	times, values := sampleGrid(func(x float64) float64 { return x }, SearchOptions{
		StartS: 10, EndS: 70, CoarseStepS: 20, ToleranceS: 0.1, MaxIterations: 10,
	})
	if len(times) != 4 {
		t.Fatalf("expected 4 samples, got %d", len(times))
	}
	if times[0] != 10 || times[len(times)-1] != 70 {
		t.Fatalf("grid edges are %v and %v", times[0], times[len(times)-1])
	}
	if len(values) != len(times) {
		t.Fatal("times and values must have the same length")
	}
}

func TestBruteForceMinimumHandlesDegenerateWindow(t *testing.T) {
	p := NewPropagator(false)
	primary, secondary, _ := coplanarPair(552, 0.5, 43200)
	tt, v := BruteForceMinimum(p, primary, secondary, 100, 100, 1)
	if tt != 100 {
		t.Fatalf("degenerate window returned t=%v", tt)
	}
	if v <= 0 {
		t.Fatalf("degenerate window returned separation %v", v)
	}
}
