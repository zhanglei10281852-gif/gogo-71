package orbit

import (
	"math"

	"DebrisLedger/internal/model"
)

// Transfer summarises the cost of moving a chaser from one circular orbit to
// another under the simplified model.
//
// Assumptions:
//   - Altitude changes use a two-impulse Hohmann transfer between coplanar
//     circular orbits.
//   - The plane change is applied as a single impulse at the higher of the two
//     radii, where the orbital speed is lowest, and is therefore cheapest.
//   - Altitude and plane costs are added scalar-wise instead of being combined
//     vectorially. This overestimates a real combined burn, which is the safe
//     direction for a budget check.
//   - Phasing along the orbit is charged as a flat configured margin because the
//     model has no notion of a phasing spiral duration.
type Transfer struct {
	FromAltitudeKm   float64 `json:"from_altitude_km"`
	ToAltitudeKm     float64 `json:"to_altitude_km"`
	AltitudeDeltaV   float64 `json:"altitude_delta_v_mps"`
	PlaneAngleDeg    float64 `json:"plane_angle_deg"`
	PlaneDeltaV      float64 `json:"plane_delta_v_mps"`
	PhasingDeltaV    float64 `json:"phasing_delta_v_mps"`
	TotalDeltaV      float64 `json:"total_delta_v_mps"`
	CoastSeconds     int64   `json:"coast_s"`
	RAANWaitSeconds  int64   `json:"raan_wait_s"`
	RAANDriftUsedDeg float64 `json:"raan_drift_used_deg"`
}

// HohmannDeltaVMps returns the total two-impulse cost, in m/s, of a coplanar
// transfer between circular orbits at the given altitudes.
func HohmannDeltaVMps(fromAltKm, toAltKm float64) float64 {
	r1 := SemiMajorAxisKm(fromAltKm)
	r2 := SemiMajorAxisKm(toAltKm)
	if r1 <= 0 || r2 <= 0 {
		return 0
	}
	if math.Abs(r2-r1) < 1e-12 {
		return 0
	}
	sum := r1 + r2
	v1 := math.Sqrt(MuKm3PerS2 / r1)
	v2 := math.Sqrt(MuKm3PerS2 / r2)
	dv1 := v1 * (math.Sqrt(2*r2/sum) - 1)
	dv2 := v2 * (1 - math.Sqrt(2*r1/sum))
	return (math.Abs(dv1) + math.Abs(dv2)) * 1000
}

// HohmannTransferSeconds returns the half-period of the transfer ellipse.
func HohmannTransferSeconds(fromAltKm, toAltKm float64) float64 {
	r1 := SemiMajorAxisKm(fromAltKm)
	r2 := SemiMajorAxisKm(toAltKm)
	at := (r1 + r2) / 2
	if at <= 0 {
		return 0
	}
	return math.Pi * math.Sqrt(at*at*at/MuKm3PerS2)
}

// PlaneChangeDeltaVMps returns the single-impulse cost, in m/s, of rotating the
// orbit plane by angleDeg at the given altitude: dv = 2 v sin(theta/2).
func PlaneChangeDeltaVMps(altitudeKm, angleDeg float64) float64 {
	if angleDeg <= 0 {
		return 0
	}
	v := SpeedKmPerS(altitudeKm)
	return 2 * v * math.Sin(Rad(angleDeg)/2) * 1000
}

// RAANWaitSeconds returns how long the chaser must coast for differential nodal
// drift to close a RAAN difference of deltaRAANDeg, or -1 when the drift rates
// are too close for the wait to be bounded by maxWait.
//
// The chaser and the target regress at different rates because their altitudes
// and inclinations differ; the difference of the two secular rates is used.
func RAANWaitSeconds(chaser, target model.Orbit, deltaRAANDeg float64, maxWait int64) int64 {
	rateChaser := NodalDriftRadPerS(chaser.AltitudeKm, chaser.InclinationDeg)
	rateTarget := NodalDriftRadPerS(target.AltitudeKm, target.InclinationDeg)
	diff := rateChaser - rateTarget
	if math.Abs(diff) < 1e-14 {
		return -1
	}
	// The chaser node must gain need radians relative to the target node. The
	// differential rate closes that gap; when the rate has the wrong sign the
	// gap closes on a later revolution of the relative node angle instead.
	need := Rad(deltaRAANDeg)
	seconds := need / diff
	twoPiSeconds := 2 * math.Pi / math.Abs(diff)
	for seconds < 0 {
		seconds += twoPiSeconds
	}
	if seconds > float64(maxWait) {
		return -1
	}
	return int64(math.Floor(seconds + 0.5))
}

// PlanTransfer builds the cheapest transfer the model can express between two
// circular orbits, optionally trading a coast against a plane change.
//
// Two options are always evaluated and the cheaper one wins, ties going to the
// immediate option so that the result never depends on map or slice ordering:
//
//	immediate: pay the full plane angle now
//	drift:     coast until differential nodal drift removes the RAAN offset,
//	           then pay only the inclination difference
func PlanTransfer(p Propagator, from, to model.Orbit, atTimeS float64, cfg TransferConfig) Transfer {
	higher := math.Max(from.AltitudeKm, to.AltitudeKm)
	altDV := HohmannDeltaVMps(from.AltitudeKm, to.AltitudeKm)
	coast := int64(math.Floor(HohmannTransferSeconds(from.AltitudeKm, to.AltitudeKm) + 0.5))

	immediateAngle := Deg(p.PlaneAngleRad(from, to, atTimeS))
	immediatePlaneDV := PlaneChangeDeltaVMps(higher, immediateAngle) / cfg.PlaneChangeEfficiency

	best := Transfer{
		FromAltitudeKm: from.AltitudeKm,
		ToAltitudeKm:   to.AltitudeKm,
		AltitudeDeltaV: altDV,
		PlaneAngleDeg:  immediateAngle,
		PlaneDeltaV:    immediatePlaneDV,
		PhasingDeltaV:  cfg.PhasingMarginMps,
		CoastSeconds:   coast,
	}
	best.TotalDeltaV = best.AltitudeDeltaV + best.PlaneDeltaV + best.PhasingDeltaV

	deltaRAAN := WrapAngleDeg(Deg(p.RAANRadAt(to, atTimeS)) - Deg(p.RAANRadAt(from, atTimeS)))
	if wait := RAANWaitSeconds(from, to, deltaRAAN, cfg.MaxRAANWaitSeconds); wait >= 0 {
		driftAngle := math.Abs(SignedAngleDiffDeg(to.InclinationDeg, from.InclinationDeg))
		driftPlaneDV := PlaneChangeDeltaVMps(higher, driftAngle) / cfg.PlaneChangeEfficiency
		driftTotal := altDV + driftPlaneDV + cfg.PhasingMarginMps
		if driftTotal < best.TotalDeltaV {
			best.PlaneAngleDeg = driftAngle
			best.PlaneDeltaV = driftPlaneDV
			best.TotalDeltaV = driftTotal
			best.RAANWaitSeconds = wait
			best.RAANDriftUsedDeg = deltaRAAN
		}
	}
	return best
}

// TransferConfig is the subset of mission configuration the transfer maths
// needs. It is defined here so that package orbit stays free of dependencies on
// the configuration package.
type TransferConfig struct {
	PlaneChangeEfficiency float64
	PhasingMarginMps      float64
	MaxRAANWaitSeconds    int64
}

// DeorbitDeltaVMps returns the single-impulse cost, in m/s, of lowering the
// perigee of a circular orbit at altitudeKm down to targetPerigeeKm. This is
// the first burn of a Hohmann transfer to the lower radius.
func DeorbitDeltaVMps(altitudeKm, targetPerigeeKm float64) float64 {
	r1 := SemiMajorAxisKm(altitudeKm)
	rp := SemiMajorAxisKm(targetPerigeeKm)
	if rp >= r1 || r1 <= 0 {
		return 0
	}
	v1 := math.Sqrt(MuKm3PerS2 / r1)
	dv := v1 * (1 - math.Sqrt(2*rp/(r1+rp)))
	return math.Abs(dv) * 1000
}

// GraveyardDeltaVMps returns the cost, in m/s, of raising a circular orbit by
// raiseKm using a full two-impulse transfer, which leaves the object circular
// at the higher altitude.
func GraveyardDeltaVMps(altitudeKm, raiseKm float64) float64 {
	return HohmannDeltaVMps(altitudeKm, altitudeKm+raiseKm)
}

// AlongTrackDisplacementKm returns the in-track displacement, in km, produced
// after leadSeconds by a tangential impulse of deltaVMps applied to a circular
// orbit.
//
// Derivation inside the model: a tangential impulse dv changes the semi-major
// axis by da = 2 dv / n. The mean motion changes by dn = -3/2 n da / a, so the
// along-track angular error grows linearly, and the arc length after time dt is
//
//	s = a * dn * dt = -3 * dv * dt
//
// The sign convention returned here is positive for a prograde burn producing a
// forward displacement of the reference point, so the caller works with
// magnitudes and applies its own sign.
func AlongTrackDisplacementKm(deltaVMps, leadSeconds float64) float64 {
	return 3 * (deltaVMps / 1000) * leadSeconds
}

// DeltaVForAlongTrackKm inverts AlongTrackDisplacementKm.
func DeltaVForAlongTrackKm(displacementKm, leadSeconds float64) float64 {
	if leadSeconds <= 0 {
		return math.Inf(1)
	}
	return math.Abs(displacementKm) / (3 * leadSeconds) * 1000
}

// SemiMajorAxisChangeKm returns the semi-major axis change, in km, caused by a
// tangential impulse of deltaVMps on a circular orbit at altitudeKm.
func SemiMajorAxisChangeKm(altitudeKm, deltaVMps float64) float64 {
	n := MeanMotionRadPerS(altitudeKm)
	if n == 0 {
		return 0
	}
	return 2 * (deltaVMps / 1000) / n
}
