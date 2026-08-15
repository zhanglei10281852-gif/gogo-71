package orbit

// RICFrame is the local orbital frame of a primary object:
//
//	radial      unit vector along the geocentric position
//	cross-track unit vector along the orbit normal (position x velocity)
//	in-track    completes the right-handed triad, cross x radial, and points
//	            along the velocity for a circular orbit
//
// For circular orbits the in-track axis coincides with the velocity direction,
// which is what makes the along-track manoeuvre arithmetic in package maneuver
// exact within this model.
type RICFrame struct {
	Radial     Vec3 `json:"radial"`
	InTrack    Vec3 `json:"in_track"`
	CrossTrack Vec3 `json:"cross_track"`
}

// NewRICFrame builds the frame of a state. Degenerate states (zero position or
// velocity) produce a zero frame rather than NaNs.
func NewRICFrame(s State) RICFrame {
	radial := s.Position.Unit()
	cross := s.Position.Cross(s.Velocity).Unit()
	inTrack := cross.Cross(radial).Unit()
	return RICFrame{Radial: radial, InTrack: inTrack, CrossTrack: cross}
}

// Decompose projects a vector expressed in the inertial frame onto the triad.
func (f RICFrame) Decompose(v Vec3) (radial, inTrack, crossTrack float64) {
	return v.Dot(f.Radial), v.Dot(f.InTrack), v.Dot(f.CrossTrack)
}

// Compose rebuilds an inertial vector from its RIC components.
func (f RICFrame) Compose(radial, inTrack, crossTrack float64) Vec3 {
	return f.Radial.Scale(radial).
		Add(f.InTrack.Scale(inTrack)).
		Add(f.CrossTrack.Scale(crossTrack))
}

// Orthogonality returns the largest absolute dot product between distinct
// axes. A healthy frame returns a value near zero; the screening stage uses it
// as a numerical self-check.
func (f RICFrame) Orthogonality() float64 {
	worst := 0.0
	for _, d := range []float64{
		f.Radial.Dot(f.InTrack),
		f.Radial.Dot(f.CrossTrack),
		f.InTrack.Dot(f.CrossTrack),
	} {
		if d < 0 {
			d = -d
		}
		if d > worst {
			worst = d
		}
	}
	return worst
}

// EncounterPlaneMiss returns the miss distance projected into the plane
// perpendicular to the relative velocity, which is the plane the simplified
// collision-probability model integrates over.
//
// The projection removes the component of the separation that lies along the
// relative velocity; at the true closest approach that component is already
// zero, so the value differs from the full separation only when evaluated away
// from the minimum.
func EncounterPlaneMiss(separation, relVelocity Vec3) float64 {
	dir := relVelocity.Unit()
	if dir == (Vec3{}) {
		return separation.Norm()
	}
	along := separation.Dot(dir)
	return separation.Sub(dir.Scale(along)).Norm()
}
