package screen

import (
	"math"

	"DebrisLedger/internal/config"
	"DebrisLedger/internal/numeric"
)

// CombinedHardBodyRadiusM returns the sum of the equivalent circular radii of
// two objects plus a fixed safety margin, in metres.
//
// The equivalent radius of an object of cross-sectional area A is sqrt(A/pi):
// the model treats both objects as spheres whose projected area matches the
// declared cross-section.
func CombinedHardBodyRadiusM(areaAM2, areaBM2, marginM float64) float64 {
	ra := math.Sqrt(math.Max(areaAM2, 0) / math.Pi)
	rb := math.Sqrt(math.Max(areaBM2, 0) / math.Pi)
	return ra + rb + math.Max(marginM, 0)
}

// PositionSigmaKm returns the 1-sigma isotropic positional uncertainty, in km,
// used at a closest approach that is leadSeconds away from the state epoch.
//
// The growth is linear in elapsed time, which mimics the way a real covariance
// inflates as a state ages without pretending to propagate one.
func PositionSigmaKm(cfg config.ProbabilityConfig, leadSeconds float64) float64 {
	if leadSeconds < 0 {
		leadSeconds = 0
	}
	days := leadSeconds / 86400
	sigma := cfg.SigmaBaseKm + cfg.SigmaGrowthKmPerDay*days
	if sigma < cfg.SigmaFloorKm {
		sigma = cfg.SigmaFloorKm
	}
	return sigma
}

// CollisionProbability returns the simplified collision-probability estimate.
//
// Assumptions, stated in full because the number is meaningless without them:
//
//  1. The encounter is treated as rectilinear and instantaneous: both objects
//     move on straight lines through the encounter plane, which is the plane
//     perpendicular to the relative velocity at the closest approach.
//  2. The combined positional uncertainty in that plane is a circular Gaussian
//     with a single standard deviation sigma. A real assessment uses a full
//     3x3 covariance per object, mapped into the encounter plane, and the
//     resulting ellipse is usually highly eccentric.
//  3. The two objects are spheres whose combined radius R is the sum of their
//     equivalent circular radii plus a configured margin.
//  4. The uncertainty is centred on the computed miss vector, i.e. the
//     propagated relative position is taken as the mean of the distribution.
//
// Under those assumptions the probability of the relative position falling
// inside the combined hard-body disc is
//
//	Pc = exp(-d^2 / (2 sigma^2)) * (1 - exp(-R^2 / (2 sigma^2)))
//
// The first factor is the Gaussian density ratio at the miss distance d, the
// second is the mass of the disc of radius R at the distribution centre. The
// expression is exact for d = 0 and is the standard small-R approximation
// otherwise; it is monotone decreasing in d and monotone increasing in R, which
// is what the severity classification relies on.
func CollisionProbability(missKm, combinedRadiusM, sigmaKm float64) float64 {
	if sigmaKm <= 0 {
		return 0
	}
	if missKm < 0 {
		missKm = -missKm
	}
	radiusKm := combinedRadiusM / 1000
	if radiusKm <= 0 {
		return 0
	}
	twoSigmaSq := 2 * sigmaKm * sigmaKm
	density := math.Exp(-(missKm * missKm) / twoSigmaSq)
	discMass := -math.Expm1(-(radiusKm * radiusKm) / twoSigmaSq)
	return numeric.Clamp(density*discMass, 0, 1)
}

// Classify maps a probability and a miss distance onto a severity label.
//
// Both axes are considered and the more severe verdict wins: a very small miss
// distance is reported as severe even when the probability model, with a wide
// sigma, dilutes the probability.
func Classify(cfg config.SeverityConfig, probability, missKm float64) string {
	byProbability := "none"
	switch {
	case probability >= cfg.CriticalProbability:
		byProbability = "critical"
	case probability >= cfg.HighProbability:
		byProbability = "high"
	case probability >= cfg.ModerateProbability:
		byProbability = "moderate"
	case probability >= cfg.LowProbability:
		byProbability = "low"
	}
	byDistance := "none"
	switch {
	case missKm <= cfg.CriticalMissKm:
		byDistance = "critical"
	case missKm <= cfg.HighMissKm:
		byDistance = "high"
	}
	if config.SeverityRank(byDistance) > config.SeverityRank(byProbability) {
		return byDistance
	}
	return byProbability
}

// ProbabilityAssumptions returns the assumption list embedded in reports.
func ProbabilityAssumptions() []string {
	return []string{
		"circular-orbit propagation only; no eccentricity, drag or third-body terms",
		"secular J2 nodal regression is the only perturbation, and only when enabled",
		"collision probability uses a rectilinear encounter with one isotropic sigma",
		"objects are spheres whose projected area matches the declared cross-section",
		"positional sigma grows linearly with the time between epoch and closest approach",
		"severity takes the worse of the probability verdict and the miss-distance verdict",
		"not suitable for operational spaceflight decisions",
	}
}
