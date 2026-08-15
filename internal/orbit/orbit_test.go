package orbit

import (
	"math"
	"testing"

	"DebrisLedger/internal/model"
)

func TestMeanMotionAndPeriod(t *testing.T) {
	// A 500 km circular orbit has a period of about 94.6 minutes.
	period := PeriodSeconds(500)
	if period < 5660 || period > 5680 {
		t.Fatalf("period at 500 km = %.3f s, want about 5670 s", period)
	}
	n := MeanMotionRadPerS(500)
	if diff := math.Abs(n*period - 2*math.Pi); diff > 1e-9 {
		t.Fatalf("mean motion and period inconsistent: n*T-2pi = %g", diff)
	}
	if speed := SpeedKmPerS(500); speed < 7.5 || speed > 7.7 {
		t.Fatalf("speed at 500 km = %.4f km/s, want about 7.61", speed)
	}
}

func TestPropagationKeepsRadiusAndSpeed(t *testing.T) {
	p := NewPropagator(true)
	o := model.Orbit{AltitudeKm: 620, InclinationDeg: 51.6, RAANDeg: 130, ArgLatDeg: 40, EpochSeconds: 0}
	wantR := SemiMajorAxisKm(o.AltitudeKm)
	wantV := SpeedKmPerS(o.AltitudeKm)
	for _, tt := range []float64{0, 137.5, 3600, 86400, 259200} {
		st := p.StateAt(o, tt)
		if diff := math.Abs(st.Position.Norm() - wantR); diff > 1e-9 {
			t.Fatalf("t=%v radius drift %g km", tt, diff)
		}
		if diff := math.Abs(st.Velocity.Norm() - wantV); diff > 1e-12 {
			t.Fatalf("t=%v speed drift %g km/s", tt, diff)
		}
		if dot := st.Position.Dot(st.Velocity); math.Abs(dot) > 1e-6 {
			t.Fatalf("t=%v position and velocity not perpendicular: %g", tt, dot)
		}
	}
}

func TestPropagationCompletesOneRevolution(t *testing.T) {
	p := NewPropagator(false)
	o := model.Orbit{AltitudeKm: 800, InclinationDeg: 98.6, RAANDeg: 12, ArgLatDeg: 200, EpochSeconds: 0}
	period := PeriodSeconds(o.AltitudeKm)
	start := p.StateAt(o, 0)
	after := p.StateAt(o, period)
	if d := start.Position.Distance(after.Position); d > 1e-6 {
		t.Fatalf("after one period the position moved %g km", d)
	}
}

func TestNodalDriftSigns(t *testing.T) {
	prograde := NodalDriftRadPerS(700, 45)
	retrograde := NodalDriftRadPerS(700, 135)
	polar := NodalDriftRadPerS(700, 90)
	if prograde >= 0 {
		t.Fatalf("prograde orbit should regress, got %g", prograde)
	}
	if retrograde <= 0 {
		t.Fatalf("retrograde orbit should advance, got %g", retrograde)
	}
	if math.Abs(polar) > 1e-18 {
		t.Fatalf("polar orbit should not drift, got %g", polar)
	}
	// A 700 km sun-synchronous-like orbit drifts a few degrees per day.
	degPerDay := math.Abs(Deg(prograde) * SecondsPerDay)
	if degPerDay < 1 || degPerDay > 10 {
		t.Fatalf("nodal drift %.4f deg/day is outside the plausible band", degPerDay)
	}
}

func TestRICFrameIsOrthonormal(t *testing.T) {
	p := NewPropagator(true)
	o := model.Orbit{AltitudeKm: 550, InclinationDeg: 53, RAANDeg: 84, ArgLatDeg: 12, EpochSeconds: 0}
	frame := NewRICFrame(p.StateAt(o, 1234))
	if worst := frame.Orthogonality(); worst > 1e-12 {
		t.Fatalf("frame not orthogonal: worst dot product %g", worst)
	}
	for _, axis := range []Vec3{frame.Radial, frame.InTrack, frame.CrossTrack} {
		if diff := math.Abs(axis.Norm() - 1); diff > 1e-12 {
			t.Fatalf("axis is not unit length: %g", diff)
		}
	}
	v := Vec3{X: 1.5, Y: -2.25, Z: 0.75}
	r, i, c := frame.Decompose(v)
	back := frame.Compose(r, i, c)
	if d := back.Distance(v); d > 1e-12 {
		t.Fatalf("decompose/compose round trip error %g", d)
	}
}

func TestRelativeGeometryOfCoplanarPair(t *testing.T) {
	p := NewPropagator(false)
	primary := model.Orbit{AltitudeKm: 550, InclinationDeg: 53, RAANDeg: 84, ArgLatDeg: 12, EpochSeconds: 0}
	secondary := primary
	secondary.AltitudeKm = 551
	rel := p.Relative(primary, secondary, 0)
	if diff := math.Abs(rel.Radial - 1); diff > 1e-9 {
		t.Fatalf("radial separation %.9f km, want 1", rel.Radial)
	}
	if math.Abs(rel.CrossTrack) > 1e-9 {
		t.Fatalf("coplanar pair has cross-track separation %g", rel.CrossTrack)
	}
	if math.Abs(rel.InTrack) > 1e-6 {
		t.Fatalf("aligned pair has in-track separation %g", rel.InTrack)
	}
}

func TestAlongTrackDisplacementRoundTrip(t *testing.T) {
	dv := 0.075
	lead := 7200.0
	shift := AlongTrackDisplacementKm(dv, lead)
	if diff := math.Abs(shift - 3*(dv/1000)*lead); diff > 1e-15 {
		t.Fatalf("displacement formula changed: %g", shift)
	}
	back := DeltaVForAlongTrackKm(shift, lead)
	if diff := math.Abs(back - dv); diff > 1e-12 {
		t.Fatalf("round trip delta-v %g, want %g", back, dv)
	}
	if math.IsInf(DeltaVForAlongTrackKm(1, 0), 1) != true {
		t.Fatal("a zero lead time must make the required delta-v infinite")
	}
}

func TestSemiMajorAxisChange(t *testing.T) {
	// A 1 m/s tangential burn at 550 km raises the semi-major axis by about
	// 1.8 km: da = 2 dv / n.
	da := SemiMajorAxisChangeKm(550, 1)
	if da < 1.7 || da > 2.0 {
		t.Fatalf("semi-major axis change %.4f km is outside the expected band", da)
	}
	if SemiMajorAxisChangeKm(550, 0) != 0 {
		t.Fatal("a zero burn must not change the semi-major axis")
	}
}

func TestHohmannAgainstKnownTransfer(t *testing.T) {
	// The classic 300 km to 35786 km transfer needs about 3900 m/s.
	dv := HohmannDeltaVMps(300, 35786)
	if dv < 3800 || dv > 4000 {
		t.Fatalf("LEO to GEO transfer %.1f m/s, want about 3900", dv)
	}
	if HohmannDeltaVMps(500, 500) != 0 {
		t.Fatal("a transfer between identical orbits must be free")
	}
	up := HohmannDeltaVMps(500, 700)
	down := HohmannDeltaVMps(700, 500)
	if diff := math.Abs(up - down); diff > 1e-9 {
		t.Fatalf("Hohmann cost is not symmetric: %g vs %g", up, down)
	}
	if HohmannTransferSeconds(500, 700) <= 0 {
		t.Fatal("transfer time must be positive")
	}
}

func TestPlaneChangeCost(t *testing.T) {
	if PlaneChangeDeltaVMps(700, 0) != 0 {
		t.Fatal("a zero plane change must be free")
	}
	small := PlaneChangeDeltaVMps(700, 1)
	large := PlaneChangeDeltaVMps(700, 30)
	if small >= large {
		t.Fatalf("plane change cost must grow with the angle: %g vs %g", small, large)
	}
	// dv = 2 v sin(theta/2); a 60 degree change costs exactly the orbital speed.
	sixty := PlaneChangeDeltaVMps(700, 60)
	if diff := math.Abs(sixty - SpeedKmPerS(700)*1000); diff > 1e-6 {
		t.Fatalf("60 degree plane change %.6f m/s, want the orbital speed", sixty)
	}
}

func TestPlaneAngleBetweenOrbits(t *testing.T) {
	p := NewPropagator(false)
	a := model.Orbit{AltitudeKm: 700, InclinationDeg: 98, RAANDeg: 10, EpochSeconds: 0}
	b := a
	if angle := Deg(p.PlaneAngleRad(a, b, 0)); angle > 1e-12 {
		t.Fatalf("identical planes have angle %g", angle)
	}
	b.InclinationDeg = 98.001
	if angle := Deg(p.PlaneAngleRad(a, b, 0)); math.Abs(angle-0.001) > 1e-9 {
		t.Fatalf("a 0.001 deg inclination difference gave %g deg", angle)
	}
	b = a
	b.RAANDeg = 190
	// Two orbits with the same inclination and opposite nodes are separated by
	// twice the inclination when measured through the equator crossing.
	angle := Deg(p.PlaneAngleRad(a, b, 0))
	if angle < 160 || angle > 180 {
		t.Fatalf("opposed nodes give angle %.4f deg, want close to 164", angle)
	}
}

func TestPlaneNormalAgreesWithStateCrossProduct(t *testing.T) {
	p := NewPropagator(true)
	o := model.Orbit{AltitudeKm: 705, InclinationDeg: 98.2, RAANDeg: 210.75, ArgLatDeg: 145.3, EpochSeconds: 0}
	for _, tt := range []float64{0, 2500, 86400} {
		fromState := p.PlaneNormal(o, tt)
		analytic := p.PlaneNormalAt(o, tt)
		if d := fromState.Distance(analytic); d > 1e-9 {
			t.Fatalf("t=%v normals disagree by %g", tt, d)
		}
	}
}

func TestRAANWaitUsesDifferentialDrift(t *testing.T) {
	chaser := model.Orbit{AltitudeKm: 700, InclinationDeg: 98.0, RAANDeg: 210, EpochSeconds: 0}
	target := model.Orbit{AltitudeKm: 760, InclinationDeg: 96.0, RAANDeg: 215, EpochSeconds: 0}
	wait := RAANWaitSeconds(chaser, target, 5, 400*86400)
	if wait <= 0 {
		t.Fatalf("expected a bounded wait, got %d", wait)
	}
	if wait > 400*86400 {
		t.Fatalf("wait %d exceeds the cap", wait)
	}
	if capped := RAANWaitSeconds(chaser, target, 5, 10); capped != -1 {
		t.Fatalf("a tight cap must reject the wait, got %d", capped)
	}
	same := RAANWaitSeconds(chaser, chaser, 5, 400*86400)
	if same != -1 {
		t.Fatalf("identical orbits cannot drift apart, got %d", same)
	}
}

func TestPlanTransferPrefersDriftWhenCheaper(t *testing.T) {
	p := NewPropagator(true)
	cfg := TransferConfig{PlaneChangeEfficiency: 0.95, PhasingMarginMps: 4, MaxRAANWaitSeconds: 200 * 86400}
	from := model.Orbit{AltitudeKm: 700, InclinationDeg: 98.0, RAANDeg: 210, EpochSeconds: 0}
	to := model.Orbit{AltitudeKm: 720, InclinationDeg: 96.05, RAANDeg: 225, EpochSeconds: 0}
	withDrift := PlanTransfer(p, from, to, 0, cfg)
	noDrift := PlanTransfer(p, from, to, 0, TransferConfig{
		PlaneChangeEfficiency: 0.95, PhasingMarginMps: 4, MaxRAANWaitSeconds: 0,
	})
	if withDrift.TotalDeltaV > noDrift.TotalDeltaV {
		t.Fatalf("allowing a coast made the transfer more expensive: %.3f vs %.3f",
			withDrift.TotalDeltaV, noDrift.TotalDeltaV)
	}
	if withDrift.RAANWaitSeconds <= 0 {
		t.Fatalf("expected the drift option to be selected, wait was %d", withDrift.RAANWaitSeconds)
	}
	if withDrift.AltitudeDeltaV <= 0 {
		t.Fatal("an altitude change must cost something")
	}
}

func TestDisposalCosts(t *testing.T) {
	deorbit := DeorbitDeltaVMps(600, 70)
	if deorbit < 100 || deorbit > 250 {
		t.Fatalf("deorbit from 600 km costs %.2f m/s, outside the expected band", deorbit)
	}
	if DeorbitDeltaVMps(100, 200) != 0 {
		t.Fatal("lowering a perigee above the current orbit must be free")
	}
	grave := GraveyardDeltaVMps(1860, 300)
	if grave <= 0 {
		t.Fatal("raising an orbit must cost something")
	}
	if GraveyardDeltaVMps(1860, 300) != HohmannDeltaVMps(1860, 2160) {
		t.Fatal("graveyard cost must equal the equivalent Hohmann transfer")
	}
}

func TestAngleHelpers(t *testing.T) {
	if got := WrapAngleDeg(-10); math.Abs(got-350) > 1e-12 {
		t.Fatalf("WrapAngleDeg(-10) = %g", got)
	}
	if got := WrapAngleDeg(730); math.Abs(got-10) > 1e-12 {
		t.Fatalf("WrapAngleDeg(730) = %g", got)
	}
	if got := WrapAngleRad(-math.Pi / 2); math.Abs(got-1.5*math.Pi) > 1e-12 {
		t.Fatalf("WrapAngleRad(-pi/2) = %g", got)
	}
	if got := SignedAngleDiffDeg(10, 350); math.Abs(got-20) > 1e-12 {
		t.Fatalf("SignedAngleDiffDeg(10,350) = %g", got)
	}
	if got := SignedAngleDiffDeg(350, 10); math.Abs(got+20) > 1e-12 {
		t.Fatalf("SignedAngleDiffDeg(350,10) = %g", got)
	}
	if got := Deg(Rad(37.5)); math.Abs(got-37.5) > 1e-12 {
		t.Fatalf("degree round trip = %g", got)
	}
}

func TestShiftArgLatMovesAlongTrack(t *testing.T) {
	p := NewPropagator(false)
	o := model.Orbit{AltitudeKm: 550, InclinationDeg: 53, RAANDeg: 84, ArgLatDeg: 12, EpochSeconds: 0}
	shifted := ShiftArgLatDeg(o, 10)
	d := p.StateAt(o, 0).Position.Distance(p.StateAt(shifted, 0).Position)
	if math.Abs(d-10) > 1e-3 {
		t.Fatalf("a 10 km shift moved the object %.6f km", d)
	}
	if WithAltitude(o, 600).AltitudeKm != 600 {
		t.Fatal("WithAltitude must change the altitude")
	}
	if diff := math.Abs(AltitudeKm(SemiMajorAxisKm(432.1)) - 432.1); diff > 1e-9 {
		t.Fatalf("altitude and radius conversions must invert, error %g", diff)
	}
}

func TestVectorAlgebra(t *testing.T) {
	a := Vec3{1, 0, 0}
	b := Vec3{0, 1, 0}
	if c := a.Cross(b); c != (Vec3{0, 0, 1}) {
		t.Fatalf("cross product = %+v", c)
	}
	if got := a.Dot(b); got != 0 {
		t.Fatalf("dot product = %g", got)
	}
	if got := a.Add(b).Sub(b); got != a {
		t.Fatalf("add/sub round trip = %+v", got)
	}
	if got := a.Scale(3).Norm(); math.Abs(got-3) > 1e-12 {
		t.Fatalf("scaled norm = %g", got)
	}
	if got := (Vec3{}).Unit(); got != (Vec3{}) {
		t.Fatalf("unit of the zero vector = %+v", got)
	}
	if got := Deg(a.AngleRad(b)); math.Abs(got-90) > 1e-9 {
		t.Fatalf("angle = %g deg", got)
	}
	if got := (Vec3{}).AngleRad(b); got != 0 {
		t.Fatalf("angle with the zero vector = %g", got)
	}
	rotated := a.RotateZ(math.Pi / 2)
	if math.Abs(rotated.Y-1) > 1e-12 || math.Abs(rotated.X) > 1e-12 {
		t.Fatalf("RotateZ = %+v", rotated)
	}
	rotatedX := b.RotateX(math.Pi / 2)
	if math.Abs(rotatedX.Z-1) > 1e-12 {
		t.Fatalf("RotateX = %+v", rotatedX)
	}
}

func TestEncounterPlaneMiss(t *testing.T) {
	sep := Vec3{X: 1, Y: 2, Z: 0}
	vel := Vec3{X: 0, Y: 5, Z: 0}
	if got := EncounterPlaneMiss(sep, vel); math.Abs(got-1) > 1e-12 {
		t.Fatalf("encounter-plane miss = %g, want 1", got)
	}
	if got := EncounterPlaneMiss(sep, Vec3{}); math.Abs(got-sep.Norm()) > 1e-12 {
		t.Fatalf("degenerate velocity should fall back to the full separation, got %g", got)
	}
}

func TestInsideVolume(t *testing.T) {
	volume := model.ScreeningVolume{Name: "v", RadialKm: 1, InTrackKm: 10, CrossTrackKm: 1}
	inside := Approach{RadialKm: 0.4, InTrackKm: 2, CrossTrackKm: 0.2}
	outside := Approach{RadialKm: 0.9, InTrackKm: 9, CrossTrackKm: 0.9}
	if !InsideVolume(inside, volume) {
		t.Fatal("expected the approach to be inside the volume")
	}
	if InsideVolume(outside, volume) {
		t.Fatal("expected the approach to be outside the volume")
	}
	if InsideVolume(inside, model.ScreeningVolume{}) {
		t.Fatal("a degenerate volume can never contain an approach")
	}
}
