// Package orbit implements the deliberately simplified orbital mechanics used
// by DebrisLedger. Only the Go standard library is used.
//
// Model statement
//
//  1. Every orbit is circular: the geocentric radius is constant and equal to
//     the Earth equatorial radius plus the declared altitude.
//  2. Motion is uniform along the orbit; the argument of latitude advances at
//     the Keplerian mean motion for that radius.
//  3. The only perturbation modelled is the secular J2 regression of the
//     ascending node, and only when enabled by configuration.
//  4. Atmospheric drag, solar radiation pressure, luni-solar attraction,
//     higher zonal and tesseral harmonics, and Earth rotation are ignored.
//  5. There is no eccentricity, so there is no argument of perigee and no
//     radial oscillation.
//
// The model is a study aid. It is not fit for operational spaceflight
// decisions: real conjunction assessment requires full force models,
// measurement-derived covariances and validated propagators.
package orbit

import "math"

// Vec3 is a Cartesian vector in the Earth-centred inertial frame, in km (or
// km/s when it holds a velocity).
type Vec3 struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

// Add returns a+b.
func (a Vec3) Add(b Vec3) Vec3 { return Vec3{a.X + b.X, a.Y + b.Y, a.Z + b.Z} }

// Sub returns a-b.
func (a Vec3) Sub(b Vec3) Vec3 { return Vec3{a.X - b.X, a.Y - b.Y, a.Z - b.Z} }

// Scale returns a*s.
func (a Vec3) Scale(s float64) Vec3 { return Vec3{a.X * s, a.Y * s, a.Z * s} }

// Dot returns the scalar product.
func (a Vec3) Dot(b Vec3) float64 { return a.X*b.X + a.Y*b.Y + a.Z*b.Z }

// Cross returns the vector product a x b.
func (a Vec3) Cross(b Vec3) Vec3 {
	return Vec3{
		X: a.Y*b.Z - a.Z*b.Y,
		Y: a.Z*b.X - a.X*b.Z,
		Z: a.X*b.Y - a.Y*b.X,
	}
}

// Norm returns the Euclidean length.
func (a Vec3) Norm() float64 { return math.Sqrt(a.Dot(a)) }

// Unit returns a vector of length one pointing along a. The zero vector is
// returned unchanged, which keeps frame construction total.
func (a Vec3) Unit() Vec3 {
	n := a.Norm()
	if n == 0 {
		return Vec3{}
	}
	return a.Scale(1 / n)
}

// Distance returns |a-b|.
func (a Vec3) Distance(b Vec3) float64 { return a.Sub(b).Norm() }

// AngleRad returns the unsigned angle between a and b in radians. The result
// is clamped to [0, pi] so that rounding cannot push the cosine out of domain.
func (a Vec3) AngleRad(b Vec3) float64 {
	na, nb := a.Norm(), b.Norm()
	if na == 0 || nb == 0 {
		return 0
	}
	c := a.Dot(b) / (na * nb)
	if c > 1 {
		c = 1
	}
	if c < -1 {
		c = -1
	}
	return math.Acos(c)
}

// RotateZ rotates a about the inertial Z axis by angle radians.
func (a Vec3) RotateZ(angle float64) Vec3 {
	s, c := math.Sincos(angle)
	return Vec3{
		X: a.X*c - a.Y*s,
		Y: a.X*s + a.Y*c,
		Z: a.Z,
	}
}

// RotateX rotates a about the inertial X axis by angle radians.
func (a Vec3) RotateX(angle float64) Vec3 {
	s, c := math.Sincos(angle)
	return Vec3{
		X: a.X,
		Y: a.Y*c - a.Z*s,
		Z: a.Y*s + a.Z*c,
	}
}

// Deg converts radians to degrees.
func Deg(rad float64) float64 { return rad * 180 / math.Pi }

// Rad converts degrees to radians.
func Rad(deg float64) float64 { return deg * math.Pi / 180 }

// WrapAngleRad maps an angle onto [0, 2pi).
func WrapAngleRad(a float64) float64 {
	twoPi := 2 * math.Pi
	a = math.Mod(a, twoPi)
	if a < 0 {
		a += twoPi
	}
	return a
}

// WrapAngleDeg maps an angle onto [0, 360).
func WrapAngleDeg(a float64) float64 {
	a = math.Mod(a, 360)
	if a < 0 {
		a += 360
	}
	return a
}

// SignedAngleDiffDeg returns the smallest signed difference a-b in (-180, 180].
func SignedAngleDiffDeg(a, b float64) float64 {
	d := math.Mod(a-b, 360)
	if d > 180 {
		d -= 360
	}
	if d <= -180 {
		d += 360
	}
	return d
}
