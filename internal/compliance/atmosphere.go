// Package compliance implements the post-mission-disposal rules, the simplified
// orbital-lifetime estimate they depend on, and the residual-risk accounting
// used by the reports.
package compliance

import (
	"math"

	"DebrisLedger/internal/orbit"
)

// dragCoefficient is the flat drag coefficient applied to every object. Real
// analyses derive it per object from attitude and shape.
const dragCoefficient = 2.2

// MaxLifetimeYears caps the lifetime estimate. Above the top of the density
// table the estimate is meaningless, so it saturates instead of overflowing.
const MaxLifetimeYears = 1.0e6

// densityLayer is one segment of a piecewise exponential atmosphere.
type densityLayer struct {
	BaseAltitudeKm float64
	BaseDensity    float64 // kg/m^3
	ScaleHeightKm  float64
}

// densityTable is the classic piecewise exponential atmosphere used in
// textbook decay estimates. It is a static, non-varying model: no solar
// activity, no diurnal bulge, no geomagnetic response.
var densityTable = []densityLayer{
	{0, 1.225, 7.249},
	{25, 3.899e-2, 7.249},
	{30, 1.774e-2, 6.349},
	{40, 3.972e-3, 8.382},
	{50, 1.057e-3, 7.714},
	{60, 3.206e-4, 6.549},
	{70, 8.770e-5, 5.799},
	{80, 1.905e-5, 5.382},
	{90, 3.396e-6, 5.877},
	{100, 5.297e-7, 7.263},
	{110, 9.661e-8, 9.473},
	{120, 2.438e-8, 12.636},
	{130, 8.484e-9, 16.149},
	{140, 3.845e-9, 22.523},
	{150, 2.070e-9, 29.740},
	{180, 5.464e-10, 37.105},
	{200, 2.789e-10, 45.546},
	{250, 7.248e-11, 53.628},
	{300, 2.418e-11, 53.298},
	{350, 9.518e-12, 58.515},
	{400, 3.725e-12, 60.828},
	{450, 1.585e-12, 63.822},
	{500, 6.967e-13, 71.835},
	{600, 1.454e-13, 88.667},
	{700, 3.614e-14, 124.64},
	{800, 1.170e-14, 181.05},
	{900, 5.245e-15, 268.00},
	{1000, 3.019e-15, 268.00},
}

// AtmosphericDensity returns the density in kg/m^3 at the given altitude using
// the piecewise exponential table. Below the table the surface layer is used;
// above 1100 km the density is treated as zero.
func AtmosphericDensity(altitudeKm float64) float64 {
	if altitudeKm > 1100 {
		return 0
	}
	layer := densityTable[0]
	for _, candidate := range densityTable {
		if altitudeKm >= candidate.BaseAltitudeKm {
			layer = candidate
			continue
		}
		break
	}
	exponent := -(altitudeKm - layer.BaseAltitudeKm) / layer.ScaleHeightKm
	return layer.BaseDensity * math.Exp(exponent)
}

// scaleHeightKm returns the scale height of the layer containing altitudeKm.
func scaleHeightKm(altitudeKm float64) float64 {
	layer := densityTable[0]
	for _, candidate := range densityTable {
		if altitudeKm >= candidate.BaseAltitudeKm {
			layer = candidate
			continue
		}
		break
	}
	return layer.ScaleHeightKm
}

// LifetimeYears estimates the remaining orbital lifetime of a circular orbit.
//
// Model. For a circular orbit under drag the semi-major axis decays as
//
//	da/dt = -Cd * (A/m) * rho(h) * sqrt(mu * a)
//
// Because the density falls off exponentially with a scale height H, almost all
// of the remaining lifetime is spent near the current altitude, so the time to
// descend by one scale height is a usable estimate:
//
//	T = H / (Cd * (A/m) * rho(h) * sqrt(mu * a))
//
// Everything is evaluated in SI units. The estimate ignores solar-cycle
// variation, attitude changes, the shrinking of the orbit during the descent
// and any active control. It is only used for coarse compliance triage.
func LifetimeYears(altitudeKm, areaToMassM2PerKg float64) float64 {
	if areaToMassM2PerKg <= 0 {
		return MaxLifetimeYears
	}
	rho := AtmosphericDensity(altitudeKm)
	if rho <= 0 {
		return MaxLifetimeYears
	}
	aMeters := orbit.SemiMajorAxisKm(altitudeKm) * 1000
	muSI := orbit.MuKm3PerS2 * 1e9
	rate := dragCoefficient * areaToMassM2PerKg * rho * math.Sqrt(muSI*aMeters)
	if rate <= 0 {
		return MaxLifetimeYears
	}
	scaleMeters := scaleHeightKm(altitudeKm) * 1000
	seconds := scaleMeters / rate
	years := seconds / orbit.SecondsPerJulianYear
	if years > MaxLifetimeYears || math.IsInf(years, 1) {
		return MaxLifetimeYears
	}
	return years
}

// DecaysWithin reports whether the estimated lifetime is inside the limit.
func DecaysWithin(altitudeKm, areaToMassM2PerKg, limitYears float64) bool {
	return LifetimeYears(altitudeKm, areaToMassM2PerKg) <= limitYears
}

// NaturalDecayAltitudeKm returns the highest circular altitude whose estimated
// lifetime is still inside limitYears for the given ballistic ratio.
//
// The search is a deterministic bisection over the altitude band supported by
// the density table. It is used to explain compliance findings: an object above
// this altitude cannot rely on natural decay.
func NaturalDecayAltitudeKm(areaToMassM2PerKg, limitYears float64) float64 {
	lo, hi := 100.0, 1100.0
	if !DecaysWithin(lo, areaToMassM2PerKg, limitYears) {
		return lo
	}
	if DecaysWithin(hi, areaToMassM2PerKg, limitYears) {
		return hi
	}
	for i := 0; i < 80 && hi-lo > 1e-4; i++ {
		mid := (lo + hi) / 2
		if DecaysWithin(mid, areaToMassM2PerKg, limitYears) {
			lo = mid
		} else {
			hi = mid
		}
	}
	return lo
}
