// Package model holds the domain types of DebrisLedger: catalogued orbital
// objects, operator-owned assets, chaser vehicles, screening volumes and the
// scenario container that binds them together.
//
// All epochs are integral seconds on a single mission clock supplied by the
// input data. The tool never reads the wall clock, so every artefact produced
// from a given input is byte-for-byte reproducible.
package model

import "sort"

// ObjectClass is the coarse taxonomy used by the screening and ranking stages.
type ObjectClass string

// Supported object classes.
const (
	ClassPayload    ObjectClass = "payload"
	ClassRocketBody ObjectClass = "rocket-body"
	ClassFragment   ObjectClass = "fragment"
	ClassUnknown    ObjectClass = "unknown"
)

// ObjectClasses returns every accepted class in deterministic order.
func ObjectClasses() []ObjectClass {
	return []ObjectClass{ClassPayload, ClassRocketBody, ClassFragment, ClassUnknown}
}

// Valid reports whether c is one of the accepted classes.
func (c ObjectClass) Valid() bool {
	for _, known := range ObjectClasses() {
		if c == known {
			return true
		}
	}
	return false
}

// DebrisCandidate reports whether objects of this class may be considered as
// active-removal targets. Payloads are excluded because the tool has no way of
// knowing whether a payload is still under control.
func (c ObjectClass) DebrisCandidate() bool {
	return c == ClassRocketBody || c == ClassFragment
}

// ThrusterType enumerates the propulsion families the planner understands.
type ThrusterType string

// Supported thruster types.
const (
	ThrusterChemical ThrusterType = "chemical"
	ThrusterElectric ThrusterType = "electric"
	ThrusterColdGas  ThrusterType = "cold-gas"
)

// ThrusterTypes returns every accepted thruster type in deterministic order.
func ThrusterTypes() []ThrusterType {
	return []ThrusterType{ThrusterChemical, ThrusterElectric, ThrusterColdGas}
}

// Valid reports whether t is one of the accepted thruster types.
func (t ThrusterType) Valid() bool {
	for _, known := range ThrusterTypes() {
		if t == known {
			return true
		}
	}
	return false
}

// MinBurnLeadSeconds is the shortest lead time the propulsion family can be
// commanded with, independent of the operator-declared minimum. Electric
// propulsion needs a long low-thrust arc, cold gas is nearly instantaneous.
func (t ThrusterType) MinBurnLeadSeconds() int64 {
	switch t {
	case ThrusterElectric:
		return 21600
	case ThrusterChemical:
		return 3600
	case ThrusterColdGas:
		return 900
	default:
		return 3600
	}
}

// Orbit is a deliberately simplified circular / near-circular element set.
//
// The model carries no eccentricity: the radius is constant and equal to the
// Earth equatorial radius plus AltitudeKm. ArgLatDeg is the argument of
// latitude (angle from the ascending node along the orbit) at EpochSeconds.
type Orbit struct {
	AltitudeKm     float64 `json:"altitude_km"`
	InclinationDeg float64 `json:"inclination_deg"`
	RAANDeg        float64 `json:"raan_deg"`
	ArgLatDeg      float64 `json:"arg_lat_deg"`
	EpochSeconds   int64   `json:"epoch_s"`
}

// CatalogObject is a tracked object that is not owned by the operator.
type CatalogObject struct {
	ID                string      `json:"id"`
	Name              string      `json:"name"`
	Class             ObjectClass `json:"class"`
	MassKg            float64     `json:"mass_kg"`
	AreaM2            float64     `json:"area_m2"`
	Orbit             Orbit       `json:"orbit"`
	TumbleRateDegPerS float64     `json:"tumble_rate_deg_s"`
	AttachPoint       string      `json:"attach_point"`
	Notes             string      `json:"notes,omitempty"`
}

// AreaToMass returns the ballistic ratio in m^2/kg, used by the simplified
// orbital-lifetime estimate.
func (o CatalogObject) AreaToMass() float64 {
	if o.MassKg <= 0 {
		return 0
	}
	return o.AreaM2 / o.MassKg
}

// HasAttachPoint reports whether a capture interface was declared.
func (o CatalogObject) HasAttachPoint() bool {
	return o.AttachPoint != "" && o.AttachPoint != "none"
}

// ManeuverCapability is the ground-truth propulsive capability of an asset.
type ManeuverCapability struct {
	DeltaVBudgetMps float64      `json:"delta_v_budget_mps"`
	ThrusterType    ThrusterType `json:"thruster_type"`
	MinLeadTimeS    int64        `json:"min_lead_time_s"`
	MaxBurnsPerDay  int          `json:"max_burns_per_day"`
}

// EffectiveLeadSeconds combines the operator-declared minimum lead time with
// the physical floor implied by the thruster family.
func (m ManeuverCapability) EffectiveLeadSeconds() int64 {
	floor := m.ThrusterType.MinBurnLeadSeconds()
	if m.MinLeadTimeS > floor {
		return m.MinLeadTimeS
	}
	return floor
}

// Asset is an operator-owned spacecraft that can be commanded.
type Asset struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	MassKg      float64            `json:"mass_kg"`
	AreaM2      float64            `json:"area_m2"`
	Orbit       Orbit              `json:"orbit"`
	Maneuver    ManeuverCapability `json:"maneuver"`
	Criticality int                `json:"criticality"`
}

// Chaser is a removal vehicle: it rendezvouses with debris, captures it and
// performs the disposal burn.
type Chaser struct {
	ID                   string  `json:"id"`
	Name                 string  `json:"name"`
	DryMassKg            float64 `json:"dry_mass_kg"`
	DeltaVBudgetMps      float64 `json:"delta_v_budget_mps"`
	CaptureSlots         int     `json:"capture_slots"`
	DockingTimeS         int64   `json:"docking_time_s"`
	MaxCaptureMassKg     float64 `json:"max_capture_mass_kg"`
	MaxTumbleRateDegPerS float64 `json:"max_tumble_rate_deg_s"`
	Orbit                Orbit   `json:"orbit"`
}

// ScreeningVolume is the ellipsoidal box, expressed in the radial /
// in-track / cross-track frame of the asset, that triggers a conjunction
// record when the closest approach falls inside it.
type ScreeningVolume struct {
	Name           string  `json:"name"`
	RadialKm       float64 `json:"radial_km"`
	InTrackKm      float64 `json:"in_track_km"`
	CrossTrackKm   float64 `json:"cross_track_km"`
	AppliesToClass string  `json:"applies_to_class,omitempty"`
}

// Applies reports whether the volume is used for the given object class. An
// empty AppliesToClass matches every class.
func (v ScreeningVolume) Applies(c ObjectClass) bool {
	return v.AppliesToClass == "" || v.AppliesToClass == string(c)
}

// Scenario is the complete offline input: a catalogue, the operator fleet, the
// removal vehicles and the screening volumes.
type Scenario struct {
	Label            string            `json:"label"`
	EpochSeconds     int64             `json:"epoch_s"`
	HorizonSeconds   int64             `json:"horizon_s"`
	Objects          []CatalogObject   `json:"objects"`
	Assets           []Asset           `json:"assets"`
	Chasers          []Chaser          `json:"chasers"`
	ScreeningVolumes []ScreeningVolume `json:"screening_volumes"`
}

// Sort orders every collection by identifier so that downstream iteration is
// deterministic regardless of input ordering.
func (s *Scenario) Sort() {
	sort.SliceStable(s.Objects, func(i, j int) bool { return s.Objects[i].ID < s.Objects[j].ID })
	sort.SliceStable(s.Assets, func(i, j int) bool { return s.Assets[i].ID < s.Assets[j].ID })
	sort.SliceStable(s.Chasers, func(i, j int) bool { return s.Chasers[i].ID < s.Chasers[j].ID })
	sort.SliceStable(s.ScreeningVolumes, func(i, j int) bool {
		return s.ScreeningVolumes[i].Name < s.ScreeningVolumes[j].Name
	})
}

// ObjectByID returns the catalogue entry with the given identifier.
func (s *Scenario) ObjectByID(id string) (CatalogObject, bool) {
	for _, obj := range s.Objects {
		if obj.ID == id {
			return obj, true
		}
	}
	return CatalogObject{}, false
}

// AssetByID returns the asset with the given identifier.
func (s *Scenario) AssetByID(id string) (Asset, bool) {
	for _, a := range s.Assets {
		if a.ID == id {
			return a, true
		}
	}
	return Asset{}, false
}

// ChaserByID returns the chaser with the given identifier.
func (s *Scenario) ChaserByID(id string) (Chaser, bool) {
	for _, c := range s.Chasers {
		if c.ID == id {
			return c, true
		}
	}
	return Chaser{}, false
}

// VolumeFor returns the tightest screening volume that applies to the class,
// falling back to a generous default when the scenario declares none.
func (s *Scenario) VolumeFor(c ObjectClass) ScreeningVolume {
	best := ScreeningVolume{}
	found := false
	for _, v := range s.ScreeningVolumes {
		if !v.Applies(c) {
			continue
		}
		if !found || volumeMagnitude(v) < volumeMagnitude(best) {
			best = v
			found = true
		}
	}
	if !found {
		return ScreeningVolume{Name: "default", RadialKm: 2, InTrackKm: 25, CrossTrackKm: 2}
	}
	return best
}

func volumeMagnitude(v ScreeningVolume) float64 {
	return v.RadialKm * v.InTrackKm * v.CrossTrackKm
}

// EndSeconds is the exclusive end of the screening horizon.
func (s *Scenario) EndSeconds() int64 {
	return s.EpochSeconds + s.HorizonSeconds
}
