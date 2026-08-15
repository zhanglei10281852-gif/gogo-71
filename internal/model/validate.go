package model

import (
	"fmt"
	"sort"
	"strings"
)

// Issue is a single validation finding. Path locates the offending field using
// a dotted JSON-like path so that reports can be diffed easily.
type Issue struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// Issues is a sortable collection of findings.
type Issues []Issue

// Add appends a formatted finding.
func (is *Issues) Add(path, format string, args ...any) {
	*is = append(*is, Issue{Path: path, Message: fmt.Sprintf(format, args...)})
}

// Sort orders findings by path and then message.
func (is Issues) Sort() {
	sort.SliceStable(is, func(i, j int) bool {
		if is[i].Path != is[j].Path {
			return is[i].Path < is[j].Path
		}
		return is[i].Message < is[j].Message
	})
}

// Err converts the collection into an error, or nil when empty.
func (is Issues) Err() error {
	if len(is) == 0 {
		return nil
	}
	is.Sort()
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%d validation issue(s):", len(is)))
	for _, issue := range is {
		b.WriteString("\n  - ")
		b.WriteString(issue.Path)
		b.WriteString(": ")
		b.WriteString(issue.Message)
	}
	return fmt.Errorf("%s", b.String())
}

// Physical bounds accepted by the simplified model. Anything outside these
// bounds is rejected rather than silently producing meaningless numbers.
const (
	MinAltitudeKm = 160.0
	MaxAltitudeKm = 45000.0
	MaxMassKg     = 2.0e6
	MaxAreaM2     = 5.0e4
)

// Validate checks the orbit element set.
func (o Orbit) Validate(path string) Issues {
	var is Issues
	if o.AltitudeKm < MinAltitudeKm || o.AltitudeKm > MaxAltitudeKm {
		is.Add(path+".altitude_km", "altitude %.3f km outside supported band [%.0f, %.0f]",
			o.AltitudeKm, MinAltitudeKm, MaxAltitudeKm)
	}
	if o.InclinationDeg < 0 || o.InclinationDeg > 180 {
		is.Add(path+".inclination_deg", "inclination %.3f deg outside [0, 180]", o.InclinationDeg)
	}
	if o.RAANDeg < 0 || o.RAANDeg >= 360 {
		is.Add(path+".raan_deg", "RAAN %.3f deg outside [0, 360)", o.RAANDeg)
	}
	if o.ArgLatDeg < 0 || o.ArgLatDeg >= 360 {
		is.Add(path+".arg_lat_deg", "argument of latitude %.3f deg outside [0, 360)", o.ArgLatDeg)
	}
	if o.EpochSeconds < 0 {
		is.Add(path+".epoch_s", "epoch must be a non-negative mission-clock second, got %d", o.EpochSeconds)
	}
	return is
}

// Validate checks a catalogue entry.
func (o CatalogObject) Validate(path string) Issues {
	var is Issues
	if strings.TrimSpace(o.ID) == "" {
		is.Add(path+".id", "identifier must not be empty")
	} else if err := checkIdentifier(o.ID); err != nil {
		is.Add(path+".id", "%v", err)
	}
	if strings.TrimSpace(o.Name) == "" {
		is.Add(path+".name", "name must not be empty")
	}
	if !o.Class.Valid() {
		is.Add(path+".class", "unknown class %q (accepted: %s)", string(o.Class), joinClasses())
	}
	if o.MassKg <= 0 || o.MassKg > MaxMassKg {
		is.Add(path+".mass_kg", "mass %.3f kg outside (0, %.0f]", o.MassKg, MaxMassKg)
	}
	if o.AreaM2 <= 0 || o.AreaM2 > MaxAreaM2 {
		is.Add(path+".area_m2", "cross-sectional area %.4f m^2 outside (0, %.0f]", o.AreaM2, MaxAreaM2)
	}
	if o.TumbleRateDegPerS < 0 || o.TumbleRateDegPerS > 3600 {
		is.Add(path+".tumble_rate_deg_s", "tumble rate %.4f deg/s outside [0, 3600]", o.TumbleRateDegPerS)
	}
	is = append(is, o.Orbit.Validate(path+".orbit")...)
	return is
}

// Validate checks an operator asset.
func (a Asset) Validate(path string) Issues {
	var is Issues
	if strings.TrimSpace(a.ID) == "" {
		is.Add(path+".id", "identifier must not be empty")
	} else if err := checkIdentifier(a.ID); err != nil {
		is.Add(path+".id", "%v", err)
	}
	if strings.TrimSpace(a.Name) == "" {
		is.Add(path+".name", "name must not be empty")
	}
	if a.MassKg <= 0 || a.MassKg > MaxMassKg {
		is.Add(path+".mass_kg", "mass %.3f kg outside (0, %.0f]", a.MassKg, MaxMassKg)
	}
	if a.AreaM2 <= 0 || a.AreaM2 > MaxAreaM2 {
		is.Add(path+".area_m2", "cross-sectional area %.4f m^2 outside (0, %.0f]", a.AreaM2, MaxAreaM2)
	}
	if a.Criticality < 1 || a.Criticality > 5 {
		is.Add(path+".criticality", "criticality %d outside [1, 5]", a.Criticality)
	}
	is = append(is, a.Orbit.Validate(path+".orbit")...)
	is = append(is, a.Maneuver.Validate(path+".maneuver")...)
	return is
}

// Validate checks a manoeuvre capability declaration.
func (m ManeuverCapability) Validate(path string) Issues {
	var is Issues
	if m.DeltaVBudgetMps < 0 || m.DeltaVBudgetMps > 5000 {
		is.Add(path+".delta_v_budget_mps", "delta-v budget %.4f m/s outside [0, 5000]", m.DeltaVBudgetMps)
	}
	if !m.ThrusterType.Valid() {
		is.Add(path+".thruster_type", "unknown thruster type %q (accepted: %s)",
			string(m.ThrusterType), joinThrusters())
	}
	if m.MinLeadTimeS < 0 || m.MinLeadTimeS > 30*86400 {
		is.Add(path+".min_lead_time_s", "minimum lead time %d s outside [0, 2592000]", m.MinLeadTimeS)
	}
	if m.MaxBurnsPerDay < 1 || m.MaxBurnsPerDay > 96 {
		is.Add(path+".max_burns_per_day", "burn allowance %d outside [1, 96]", m.MaxBurnsPerDay)
	}
	return is
}

// Validate checks a chaser vehicle.
func (c Chaser) Validate(path string) Issues {
	var is Issues
	if strings.TrimSpace(c.ID) == "" {
		is.Add(path+".id", "identifier must not be empty")
	} else if err := checkIdentifier(c.ID); err != nil {
		is.Add(path+".id", "%v", err)
	}
	if strings.TrimSpace(c.Name) == "" {
		is.Add(path+".name", "name must not be empty")
	}
	if c.DryMassKg <= 0 || c.DryMassKg > MaxMassKg {
		is.Add(path+".dry_mass_kg", "dry mass %.3f kg outside (0, %.0f]", c.DryMassKg, MaxMassKg)
	}
	if c.DeltaVBudgetMps <= 0 || c.DeltaVBudgetMps > 20000 {
		is.Add(path+".delta_v_budget_mps", "delta-v budget %.3f m/s outside (0, 20000]", c.DeltaVBudgetMps)
	}
	if c.CaptureSlots < 1 || c.CaptureSlots > 32 {
		is.Add(path+".capture_slots", "capture slots %d outside [1, 32]", c.CaptureSlots)
	}
	if c.DockingTimeS < 0 || c.DockingTimeS > 30*86400 {
		is.Add(path+".docking_time_s", "docking time %d s outside [0, 2592000]", c.DockingTimeS)
	}
	if c.MaxCaptureMassKg <= 0 || c.MaxCaptureMassKg > MaxMassKg {
		is.Add(path+".max_capture_mass_kg", "capture mass limit %.3f kg outside (0, %.0f]",
			c.MaxCaptureMassKg, MaxMassKg)
	}
	if c.MaxTumbleRateDegPerS < 0 || c.MaxTumbleRateDegPerS > 360 {
		is.Add(path+".max_tumble_rate_deg_s", "tumble limit %.4f deg/s outside [0, 360]", c.MaxTumbleRateDegPerS)
	}
	is = append(is, c.Orbit.Validate(path+".orbit")...)
	return is
}

// Validate checks a screening volume.
func (v ScreeningVolume) Validate(path string) Issues {
	var is Issues
	if strings.TrimSpace(v.Name) == "" {
		is.Add(path+".name", "name must not be empty")
	}
	if v.RadialKm <= 0 || v.RadialKm > 500 {
		is.Add(path+".radial_km", "radial half-width %.4f km outside (0, 500]", v.RadialKm)
	}
	if v.InTrackKm <= 0 || v.InTrackKm > 5000 {
		is.Add(path+".in_track_km", "in-track half-width %.4f km outside (0, 5000]", v.InTrackKm)
	}
	if v.CrossTrackKm <= 0 || v.CrossTrackKm > 500 {
		is.Add(path+".cross_track_km", "cross-track half-width %.4f km outside (0, 500]", v.CrossTrackKm)
	}
	if v.AppliesToClass != "" && !ObjectClass(v.AppliesToClass).Valid() {
		is.Add(path+".applies_to_class", "unknown class %q", v.AppliesToClass)
	}
	return is
}

// Validate performs whole-scenario validation, including cross-references and
// duplicate detection.
func (s *Scenario) Validate() error {
	var is Issues
	if strings.TrimSpace(s.Label) == "" {
		is.Add("label", "scenario label must not be empty")
	}
	if s.EpochSeconds < 0 {
		is.Add("epoch_s", "scenario epoch must be non-negative, got %d", s.EpochSeconds)
	}
	if s.HorizonSeconds < 600 || s.HorizonSeconds > 30*86400 {
		is.Add("horizon_s", "screening horizon %d s outside [600, 2592000]", s.HorizonSeconds)
	}
	if len(s.Objects) == 0 {
		is.Add("objects", "at least one catalogue object is required")
	}
	if len(s.Assets) == 0 {
		is.Add("assets", "at least one operator asset is required")
	}
	seen := map[string]string{}
	for i, obj := range s.Objects {
		path := fmt.Sprintf("objects[%d]", i)
		is = append(is, obj.Validate(path)...)
		markDuplicate(&is, seen, obj.ID, path)
		if obj.Orbit.EpochSeconds < s.EpochSeconds {
			is.Add(path+".orbit.epoch_s", "epoch %d precedes scenario epoch %d", obj.Orbit.EpochSeconds, s.EpochSeconds)
		}
	}
	for i, a := range s.Assets {
		path := fmt.Sprintf("assets[%d]", i)
		is = append(is, a.Validate(path)...)
		markDuplicate(&is, seen, a.ID, path)
		if a.Orbit.EpochSeconds < s.EpochSeconds {
			is.Add(path+".orbit.epoch_s", "epoch %d precedes scenario epoch %d", a.Orbit.EpochSeconds, s.EpochSeconds)
		}
		if a.Maneuver.EffectiveLeadSeconds() >= s.HorizonSeconds {
			is.Add(path+".maneuver.min_lead_time_s",
				"effective lead time %d s leaves no room inside the %d s horizon",
				a.Maneuver.EffectiveLeadSeconds(), s.HorizonSeconds)
		}
	}
	for i, c := range s.Chasers {
		path := fmt.Sprintf("chasers[%d]", i)
		is = append(is, c.Validate(path)...)
		markDuplicate(&is, seen, c.ID, path)
	}
	volNames := map[string]string{}
	for i, v := range s.ScreeningVolumes {
		path := fmt.Sprintf("screening_volumes[%d]", i)
		is = append(is, v.Validate(path)...)
		if prev, ok := volNames[v.Name]; ok {
			is.Add(path+".name", "duplicate screening volume name %q (first seen at %s)", v.Name, prev)
		} else {
			volNames[v.Name] = path
		}
	}
	return is.Err()
}

func markDuplicate(is *Issues, seen map[string]string, id, path string) {
	if id == "" {
		return
	}
	if prev, ok := seen[id]; ok {
		is.Add(path+".id", "duplicate identifier %q (first seen at %s)", id, prev)
		return
	}
	seen[id] = path
}

func checkIdentifier(id string) error {
	if len(id) > 64 {
		return fmt.Errorf("identifier %q longer than 64 characters", id)
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return fmt.Errorf("identifier %q contains unsupported rune %q", id, r)
		}
	}
	return nil
}

func joinClasses() string {
	parts := make([]string, 0, len(ObjectClasses()))
	for _, c := range ObjectClasses() {
		parts = append(parts, string(c))
	}
	return strings.Join(parts, ", ")
}

func joinThrusters() string {
	parts := make([]string, 0, len(ThrusterTypes()))
	for _, t := range ThrusterTypes() {
		parts = append(parts, string(t))
	}
	return strings.Join(parts, ", ")
}
