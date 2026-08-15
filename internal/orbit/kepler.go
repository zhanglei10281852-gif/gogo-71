package orbit

import (
	"math"

	"DebrisLedger/internal/model"
)

// Physical constants. Values are the standard WGS-84 / EGM-96 figures, kept in
// km-based units so that all distances in the tool are kilometres.
const (
	// EarthRadiusKm is the equatorial radius.
	EarthRadiusKm = 6378.137
	// MuKm3PerS2 is the geocentric gravitational constant.
	MuKm3PerS2 = 398600.4418
	// J2 is the second zonal harmonic coefficient.
	J2 = 1.08262668e-3
	// SecondsPerDay is the number of seconds in a mission-clock day.
	SecondsPerDay = 86400.0
	// SecondsPerJulianYear is used by the lifetime estimate.
	SecondsPerJulianYear = 365.25 * SecondsPerDay
)

// SemiMajorAxisKm returns the circular orbit radius for an altitude.
func SemiMajorAxisKm(altitudeKm float64) float64 {
	return EarthRadiusKm + altitudeKm
}

// AltitudeKm inverts SemiMajorAxisKm.
func AltitudeKm(radiusKm float64) float64 {
	return radiusKm - EarthRadiusKm
}

// MeanMotionRadPerS returns sqrt(mu/a^3) for a circular orbit.
func MeanMotionRadPerS(altitudeKm float64) float64 {
	a := SemiMajorAxisKm(altitudeKm)
	if a <= 0 {
		return 0
	}
	return math.Sqrt(MuKm3PerS2 / (a * a * a))
}

// PeriodSeconds returns the orbital period.
func PeriodSeconds(altitudeKm float64) float64 {
	n := MeanMotionRadPerS(altitudeKm)
	if n == 0 {
		return 0
	}
	return 2 * math.Pi / n
}

// SpeedKmPerS returns the circular orbital speed sqrt(mu/a).
func SpeedKmPerS(altitudeKm float64) float64 {
	a := SemiMajorAxisKm(altitudeKm)
	if a <= 0 {
		return 0
	}
	return math.Sqrt(MuKm3PerS2 / a)
}

// NodalDriftRadPerS returns the secular J2 regression of the ascending node
// for a circular orbit:
//
//	Omega_dot = -3/2 * J2 * n * (Re/a)^2 * cos(i)
//
// Prograde orbits regress (negative rate); retrograde orbits advance.
func NodalDriftRadPerS(altitudeKm, inclinationDeg float64) float64 {
	a := SemiMajorAxisKm(altitudeKm)
	if a <= 0 {
		return 0
	}
	n := MeanMotionRadPerS(altitudeKm)
	ratio := EarthRadiusKm / a
	return -1.5 * J2 * n * ratio * ratio * math.Cos(Rad(inclinationDeg))
}

// State is an inertial position/velocity pair at a mission-clock instant.
type State struct {
	TimeS    float64 `json:"time_s"`
	Position Vec3    `json:"position_km"`
	Velocity Vec3    `json:"velocity_km_s"`
}

// Propagator turns an element set into states. Options are captured once so
// that a screening run cannot mix drift settings between objects.
type Propagator struct {
	IncludeNodalDrift bool
}

// NewPropagator builds a propagator with the given drift setting.
func NewPropagator(includeNodalDrift bool) Propagator {
	return Propagator{IncludeNodalDrift: includeNodalDrift}
}

// ArgLatRadAt returns the argument of latitude at mission time t seconds.
func (p Propagator) ArgLatRadAt(o model.Orbit, t float64) float64 {
	dt := t - float64(o.EpochSeconds)
	return WrapAngleRad(Rad(o.ArgLatDeg) + MeanMotionRadPerS(o.AltitudeKm)*dt)
}

// RAANRadAt returns the right ascension of the ascending node at t seconds,
// including the J2 nodal drift when enabled.
func (p Propagator) RAANRadAt(o model.Orbit, t float64) float64 {
	raan := Rad(o.RAANDeg)
	if p.IncludeNodalDrift {
		dt := t - float64(o.EpochSeconds)
		raan += NodalDriftRadPerS(o.AltitudeKm, o.InclinationDeg) * dt
	}
	return WrapAngleRad(raan)
}

// StateAt propagates the element set to mission time t seconds.
//
// The perifocal-to-inertial chain is: place the object in the orbit plane at
// the argument of latitude, rotate by the inclination about X, then by the
// node about Z.
func (p Propagator) StateAt(o model.Orbit, t float64) State {
	r := SemiMajorAxisKm(o.AltitudeKm)
	v := SpeedKmPerS(o.AltitudeKm)
	u := p.ArgLatRadAt(o, t)
	su, cu := math.Sincos(u)
	inPlanePos := Vec3{X: r * cu, Y: r * su}
	inPlaneVel := Vec3{X: -v * su, Y: v * cu}
	inc := Rad(o.InclinationDeg)
	node := p.RAANRadAt(o, t)
	pos := inPlanePos.RotateX(inc).RotateZ(node)
	vel := inPlaneVel.RotateX(inc).RotateZ(node)
	return State{TimeS: t, Position: pos, Velocity: vel}
}

// PlaneNormal returns the unit orbit-normal vector at time t.
func (p Propagator) PlaneNormal(o model.Orbit, t float64) Vec3 {
	s := p.StateAt(o, t)
	return s.Position.Cross(s.Velocity).Unit()
}

// RelativeState is the difference between a secondary and a primary state.
type RelativeState struct {
	TimeS        float64 `json:"time_s"`
	SeparationKm float64 `json:"separation_km"`
	RangeRateKmS float64 `json:"range_rate_km_s"`
	RelSpeedKmS  float64 `json:"rel_speed_km_s"`
	Radial       float64 `json:"radial_km"`
	InTrack      float64 `json:"in_track_km"`
	CrossTrack   float64 `json:"cross_track_km"`
}

// Relative computes the separation of two element sets at time t, decomposed in
// the primary's radial / in-track / cross-track frame.
func (p Propagator) Relative(primary, secondary model.Orbit, t float64) RelativeState {
	ps := p.StateAt(primary, t)
	ss := p.StateAt(secondary, t)
	dPos := ss.Position.Sub(ps.Position)
	dVel := ss.Velocity.Sub(ps.Velocity)
	frame := NewRICFrame(ps)
	r, i, c := frame.Decompose(dPos)
	sep := dPos.Norm()
	rangeRate := 0.0
	if sep > 0 {
		rangeRate = dPos.Dot(dVel) / sep
	}
	return RelativeState{
		TimeS:        t,
		SeparationKm: sep,
		RangeRateKmS: rangeRate,
		RelSpeedKmS:  dVel.Norm(),
		Radial:       r,
		InTrack:      i,
		CrossTrack:   c,
	}
}

// SeparationAt is the scalar separation of two element sets at time t.
func (p Propagator) SeparationAt(primary, secondary model.Orbit, t float64) float64 {
	return p.StateAt(secondary, t).Position.Distance(p.StateAt(primary, t).Position)
}

// PlaneNormalAt returns the unit orbit-normal vector of an element set at time
// t, built directly from the inclination and the drifting node:
//
//	n = (sin(i) sin(RAAN), -sin(i) cos(RAAN), cos(i))
func (p Propagator) PlaneNormalAt(o model.Orbit, t float64) Vec3 {
	si, ci := math.Sincos(Rad(o.InclinationDeg))
	node := p.RAANRadAt(o, t)
	sn, cn := math.Sincos(node)
	return Vec3{X: si * sn, Y: -si * cn, Z: ci}
}

// PlaneAngleRad returns the angle between two orbit planes at time t.
//
// The textbook form of this angle is the spherical-triangle relation
//
//	cos(theta) = cos(i1)cos(i2) + sin(i1)sin(i2)cos(dRAAN)
//
// but taking an arc cosine of a value close to one loses most of the available
// precision, which matters here because nearly coplanar transfers are exactly
// the interesting case. The equivalent atan2 form on the plane normals is used
// instead: it stays accurate all the way down to a zero angle.
func (p Propagator) PlaneAngleRad(a, b model.Orbit, t float64) float64 {
	na := p.PlaneNormalAt(a, t)
	nb := p.PlaneNormalAt(b, t)
	return math.Atan2(na.Cross(nb).Norm(), na.Dot(nb))
}

// ShiftArgLatDeg returns a copy of o whose argument of latitude is displaced by
// the given along-track distance in km. Positive values move the object
// forward along its velocity vector.
func ShiftArgLatDeg(o model.Orbit, alongTrackKm float64) model.Orbit {
	r := SemiMajorAxisKm(o.AltitudeKm)
	if r <= 0 {
		return o
	}
	o.ArgLatDeg = WrapAngleDeg(o.ArgLatDeg + Deg(alongTrackKm/r))
	return o
}

// WithAltitude returns a copy of o at a different altitude, keeping the plane
// and the argument of latitude.
func WithAltitude(o model.Orbit, altitudeKm float64) model.Orbit {
	o.AltitudeKm = altitudeKm
	return o
}
