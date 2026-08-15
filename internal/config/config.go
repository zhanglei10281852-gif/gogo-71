// Package config carries every tunable threshold used by DebrisLedger.
//
// A configuration file is decoded strictly: an unknown key is an error, so a
// typo can never silently fall back to a default. Fields left out of the file
// keep the built-in default, which is why every field is a pointer-free value
// applied on top of Default().
package config

import (
	"fmt"
	"sort"
	"strings"

	"DebrisLedger/internal/model"
	"DebrisLedger/internal/strictjson"
)

// PropagationConfig controls the closest-approach search.
type PropagationConfig struct {
	CoarseStepSeconds   float64 `json:"coarse_step_s"`
	RefineToleranceS    float64 `json:"refine_tolerance_s"`
	MaxRefineIterations int     `json:"max_refine_iterations"`
	IncludeJ2NodalDrift bool    `json:"include_j2_nodal_drift"`
}

// ScreeningConfig controls which close approaches become conjunctions.
type ScreeningConfig struct {
	MissDistanceThresholdKm float64  `json:"miss_distance_threshold_km"`
	RequiredMissDistanceKm  float64  `json:"required_miss_distance_km"`
	MaxConjunctionsPerAsset int      `json:"max_conjunctions_per_asset"`
	SkipSameObjectClasses   []string `json:"skip_same_object_classes"`
}

// ProbabilityConfig parameterises the simplified collision-probability model.
//
// The model assumes a single isotropic positional uncertainty in the encounter
// plane whose 1-sigma value grows linearly with the time remaining until the
// closest approach. See package screen for the full statement of assumptions.
type ProbabilityConfig struct {
	SigmaBaseKm         float64 `json:"sigma_base_km"`
	SigmaGrowthKmPerDay float64 `json:"sigma_growth_km_per_day"`
	SigmaFloorKm        float64 `json:"sigma_floor_km"`
	HardBodyMarginM     float64 `json:"hard_body_margin_m"`
}

// SeverityConfig maps a probability and miss distance onto a severity label.
type SeverityConfig struct {
	CriticalProbability float64 `json:"critical_probability"`
	HighProbability     float64 `json:"high_probability"`
	ModerateProbability float64 `json:"moderate_probability"`
	LowProbability      float64 `json:"low_probability"`
	CriticalMissKm      float64 `json:"critical_miss_km"`
	HighMissKm          float64 `json:"high_miss_km"`
	ActionableSeverity  string  `json:"actionable_severity"`
}

// ManeuverConfig controls collision-avoidance planning.
type ManeuverConfig struct {
	PlanningLeadSeconds    int64   `json:"planning_lead_s"`
	ConflictGuardSeconds   int64   `json:"conflict_guard_s"`
	DeltaVMarginFactor     float64 `json:"delta_v_margin_factor"`
	MaxDeltaVPerBurnMps    float64 `json:"max_delta_v_per_burn_mps"`
	MinDeltaVResolutionMps float64 `json:"min_delta_v_resolution_mps"`
}

// RankingConfig controls debris-removal target scoring.
type RankingConfig struct {
	MassWeight                   float64 `json:"mass_weight"`
	ProbabilityWeight            float64 `json:"probability_weight"`
	CongestionWeight             float64 `json:"congestion_weight"`
	ReferenceMassKg              float64 `json:"reference_mass_kg"`
	ReferenceProbability         float64 `json:"reference_probability"`
	CongestionAltitudeBandKm     float64 `json:"congestion_altitude_band_km"`
	CongestionInclinationBandDeg float64 `json:"congestion_inclination_band_deg"`
	ReferenceCongestion          int     `json:"reference_congestion"`
	MaxTargets                   int     `json:"max_targets"`
}

// MissionConfig controls removal-mission planning.
type MissionConfig struct {
	MaxRAANWaitSeconds    int64   `json:"max_raan_wait_s"`
	PhasingMarginMps      float64 `json:"phasing_margin_mps"`
	ReserveFraction       float64 `json:"reserve_fraction"`
	PlaneChangeEfficiency float64 `json:"plane_change_efficiency"`
	CaptureMarginMps      float64 `json:"capture_margin_mps"`
	TransferSettleSeconds int64   `json:"transfer_settle_s"`
}

// ComplianceConfig controls post-mission disposal rules.
type ComplianceConfig struct {
	MaxPostMissionLifetimeYears float64 `json:"max_post_mission_lifetime_years"`
	ReentryMaxAltitudeKm        float64 `json:"reentry_max_altitude_km"`
	ReentryTargetPerigeeKm      float64 `json:"reentry_target_perigee_km"`
	GraveyardRaiseKm            float64 `json:"graveyard_raise_km"`
	ProtectedRegionTopKm        float64 `json:"protected_region_top_km"`
}

// OutputConfig controls report rendering.
type OutputConfig struct {
	DistanceDecimals          int  `json:"distance_decimals"`
	DeltaVDecimals            int  `json:"delta_v_decimals"`
	ProbabilityExponentDigits int  `json:"probability_exponent_digits"`
	IncludeAssumptions        bool `json:"include_assumptions"`
}

// Config is the whole tunable surface of the tool.
type Config struct {
	Label       string            `json:"label"`
	Propagation PropagationConfig `json:"propagation"`
	Screening   ScreeningConfig   `json:"screening"`
	Probability ProbabilityConfig `json:"probability"`
	Severity    SeverityConfig    `json:"severity"`
	Maneuver    ManeuverConfig    `json:"maneuver"`
	Ranking     RankingConfig     `json:"ranking"`
	Mission     MissionConfig     `json:"mission"`
	Compliance  ComplianceConfig  `json:"compliance"`
	Output      OutputConfig      `json:"output"`
}

// Default returns the built-in configuration. Values are chosen to be
// plausible for a low-Earth-orbit study while keeping the arithmetic legible.
func Default() Config {
	return Config{
		Label: "debrisledger-default",
		Propagation: PropagationConfig{
			CoarseStepSeconds:   20,
			RefineToleranceS:    0.01,
			MaxRefineIterations: 200,
			IncludeJ2NodalDrift: true,
		},
		Screening: ScreeningConfig{
			MissDistanceThresholdKm: 5,
			RequiredMissDistanceKm:  1.5,
			MaxConjunctionsPerAsset: 64,
			SkipSameObjectClasses:   []string{},
		},
		Probability: ProbabilityConfig{
			SigmaBaseKm:         0.25,
			SigmaGrowthKmPerDay: 0.35,
			SigmaFloorKm:        0.05,
			HardBodyMarginM:     5,
		},
		Severity: SeverityConfig{
			CriticalProbability: 1e-4,
			HighProbability:     1e-5,
			ModerateProbability: 1e-6,
			LowProbability:      1e-8,
			CriticalMissKm:      0.2,
			HighMissKm:          0.75,
			ActionableSeverity:  "high",
		},
		Maneuver: ManeuverConfig{
			PlanningLeadSeconds:    7200,
			ConflictGuardSeconds:   1800,
			DeltaVMarginFactor:     1.15,
			MaxDeltaVPerBurnMps:    2.5,
			MinDeltaVResolutionMps: 0.0005,
		},
		Ranking: RankingConfig{
			MassWeight:                   0.4,
			ProbabilityWeight:            0.35,
			CongestionWeight:             0.25,
			ReferenceMassKg:              1500,
			ReferenceProbability:         1e-4,
			CongestionAltitudeBandKm:     40,
			CongestionInclinationBandDeg: 3,
			ReferenceCongestion:          6,
			MaxTargets:                   16,
		},
		Mission: MissionConfig{
			MaxRAANWaitSeconds:    45 * 86400,
			PhasingMarginMps:      4,
			ReserveFraction:       0.1,
			PlaneChangeEfficiency: 0.95,
			CaptureMarginMps:      2,
			TransferSettleSeconds: 5400,
		},
		Compliance: ComplianceConfig{
			MaxPostMissionLifetimeYears: 25,
			ReentryMaxAltitudeKm:        620,
			ReentryTargetPerigeeKm:      70,
			GraveyardRaiseKm:            300,
			ProtectedRegionTopKm:        2000,
		},
		Output: OutputConfig{
			DistanceDecimals:          4,
			DeltaVDecimals:            5,
			ProbabilityExponentDigits: 6,
			IncludeAssumptions:        true,
		},
	}
}

// Load reads a configuration file on top of the defaults. An empty path
// returns the defaults unchanged.
func Load(path string) (Config, error) {
	cfg := Default()
	if strings.TrimSpace(path) == "" {
		return cfg, nil
	}
	if err := strictjson.DecodeFile(path, &cfg); err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// ActionableRank maps the configured actionable severity onto its rank.
func (c Config) ActionableRank() int {
	return SeverityRank(c.Severity.ActionableSeverity)
}

// SeverityRank converts a severity label into an ordered rank. Unknown labels
// rank above critical so that they never accidentally trigger action.
func SeverityRank(label string) int {
	switch label {
	case "none":
		return 0
	case "low":
		return 1
	case "moderate":
		return 2
	case "high":
		return 3
	case "critical":
		return 4
	default:
		return 5
	}
}

// SeverityLabels lists the severity labels from least to most severe.
func SeverityLabels() []string {
	return []string{"none", "low", "moderate", "high", "critical"}
}

// SkipClassSet returns the configured skip list as a lookup set.
func (c Config) SkipClassSet() map[string]bool {
	set := make(map[string]bool, len(c.Screening.SkipSameObjectClasses))
	for _, cls := range c.Screening.SkipSameObjectClasses {
		set[cls] = true
	}
	return set
}

// Validate rejects configurations that would make the arithmetic meaningless.
func (c Config) Validate() error {
	var is model.Issues
	if strings.TrimSpace(c.Label) == "" {
		is.Add("label", "configuration label must not be empty")
	}
	c.Propagation.validate(&is)
	c.Screening.validate(&is)
	c.Probability.validate(&is)
	c.Severity.validate(&is)
	c.Maneuver.validate(&is)
	c.Ranking.validate(&is)
	c.Mission.validate(&is)
	c.Compliance.validate(&is)
	c.Output.validate(&is)
	return is.Err()
}

func (p PropagationConfig) validate(is *model.Issues) {
	if p.CoarseStepSeconds <= 0 || p.CoarseStepSeconds > 600 {
		is.Add("propagation.coarse_step_s", "coarse step %.4f s outside (0, 600]", p.CoarseStepSeconds)
	}
	if p.RefineToleranceS <= 0 || p.RefineToleranceS >= p.CoarseStepSeconds {
		is.Add("propagation.refine_tolerance_s",
			"refinement tolerance %.6f s must be positive and smaller than the coarse step", p.RefineToleranceS)
	}
	if p.MaxRefineIterations < 8 || p.MaxRefineIterations > 4096 {
		is.Add("propagation.max_refine_iterations", "iteration cap %d outside [8, 4096]", p.MaxRefineIterations)
	}
}

func (s ScreeningConfig) validate(is *model.Issues) {
	if s.MissDistanceThresholdKm <= 0 || s.MissDistanceThresholdKm > 500 {
		is.Add("screening.miss_distance_threshold_km",
			"screening threshold %.4f km outside (0, 500]", s.MissDistanceThresholdKm)
	}
	if s.RequiredMissDistanceKm <= 0 || s.RequiredMissDistanceKm > s.MissDistanceThresholdKm {
		is.Add("screening.required_miss_distance_km",
			"required miss distance %.4f km must be positive and not exceed the screening threshold %.4f km",
			s.RequiredMissDistanceKm, s.MissDistanceThresholdKm)
	}
	if s.MaxConjunctionsPerAsset < 1 || s.MaxConjunctionsPerAsset > 4096 {
		is.Add("screening.max_conjunctions_per_asset", "cap %d outside [1, 4096]", s.MaxConjunctionsPerAsset)
	}
	for i, cls := range s.SkipSameObjectClasses {
		if !model.ObjectClass(cls).Valid() {
			is.Add(fmt.Sprintf("screening.skip_same_object_classes[%d]", i), "unknown class %q", cls)
		}
	}
}

func (p ProbabilityConfig) validate(is *model.Issues) {
	if p.SigmaBaseKm <= 0 || p.SigmaBaseKm > 100 {
		is.Add("probability.sigma_base_km", "base sigma %.5f km outside (0, 100]", p.SigmaBaseKm)
	}
	if p.SigmaGrowthKmPerDay < 0 || p.SigmaGrowthKmPerDay > 100 {
		is.Add("probability.sigma_growth_km_per_day", "sigma growth %.5f km/day outside [0, 100]", p.SigmaGrowthKmPerDay)
	}
	if p.SigmaFloorKm <= 0 || p.SigmaFloorKm > p.SigmaBaseKm {
		is.Add("probability.sigma_floor_km",
			"sigma floor %.5f km must be positive and not exceed the base sigma", p.SigmaFloorKm)
	}
	if p.HardBodyMarginM < 0 || p.HardBodyMarginM > 1000 {
		is.Add("probability.hard_body_margin_m", "hard-body margin %.3f m outside [0, 1000]", p.HardBodyMarginM)
	}
}

func (s SeverityConfig) validate(is *model.Issues) {
	ordered := []struct {
		name string
		val  float64
	}{
		{"severity.critical_probability", s.CriticalProbability},
		{"severity.high_probability", s.HighProbability},
		{"severity.moderate_probability", s.ModerateProbability},
		{"severity.low_probability", s.LowProbability},
	}
	for _, o := range ordered {
		if o.val <= 0 || o.val >= 1 {
			is.Add(o.name, "probability threshold %.3e outside (0, 1)", o.val)
		}
	}
	for i := 0; i+1 < len(ordered); i++ {
		if ordered[i].val <= ordered[i+1].val {
			is.Add(ordered[i].name, "threshold %.3e must be greater than %s (%.3e)",
				ordered[i].val, ordered[i+1].name, ordered[i+1].val)
		}
	}
	if s.CriticalMissKm <= 0 || s.CriticalMissKm >= s.HighMissKm {
		is.Add("severity.critical_miss_km",
			"critical miss distance %.4f km must be positive and smaller than the high threshold %.4f km",
			s.CriticalMissKm, s.HighMissKm)
	}
	if !containsString(SeverityLabels(), s.ActionableSeverity) {
		is.Add("severity.actionable_severity", "unknown severity %q (accepted: %s)",
			s.ActionableSeverity, strings.Join(SeverityLabels(), ", "))
	}
}

func (m ManeuverConfig) validate(is *model.Issues) {
	if m.PlanningLeadSeconds < 60 || m.PlanningLeadSeconds > 30*86400 {
		is.Add("maneuver.planning_lead_s", "planning lead %d s outside [60, 2592000]", m.PlanningLeadSeconds)
	}
	if m.ConflictGuardSeconds < 0 || m.ConflictGuardSeconds > 7*86400 {
		is.Add("maneuver.conflict_guard_s", "conflict guard %d s outside [0, 604800]", m.ConflictGuardSeconds)
	}
	if m.DeltaVMarginFactor < 1 || m.DeltaVMarginFactor > 5 {
		is.Add("maneuver.delta_v_margin_factor", "margin factor %.4f outside [1, 5]", m.DeltaVMarginFactor)
	}
	if m.MaxDeltaVPerBurnMps <= 0 || m.MaxDeltaVPerBurnMps > 500 {
		is.Add("maneuver.max_delta_v_per_burn_mps", "per-burn cap %.4f m/s outside (0, 500]", m.MaxDeltaVPerBurnMps)
	}
	if m.MinDeltaVResolutionMps <= 0 || m.MinDeltaVResolutionMps > 1 {
		is.Add("maneuver.min_delta_v_resolution_mps",
			"commandable resolution %.6f m/s outside (0, 1]", m.MinDeltaVResolutionMps)
	}
}

func (r RankingConfig) validate(is *model.Issues) {
	sum := r.MassWeight + r.ProbabilityWeight + r.CongestionWeight
	if r.MassWeight < 0 || r.ProbabilityWeight < 0 || r.CongestionWeight < 0 {
		is.Add("ranking", "weights must be non-negative")
	}
	if diff := sum - 1; diff > 1e-9 || diff < -1e-9 {
		is.Add("ranking", "weights must sum to 1, got %.6f", sum)
	}
	if r.ReferenceMassKg <= 0 || r.ReferenceMassKg > model.MaxMassKg {
		is.Add("ranking.reference_mass_kg", "reference mass %.3f kg outside (0, %.0f]", r.ReferenceMassKg, model.MaxMassKg)
	}
	if r.ReferenceProbability <= 0 || r.ReferenceProbability >= 1 {
		is.Add("ranking.reference_probability", "reference probability %.3e outside (0, 1)", r.ReferenceProbability)
	}
	if r.CongestionAltitudeBandKm <= 0 || r.CongestionAltitudeBandKm > 2000 {
		is.Add("ranking.congestion_altitude_band_km", "band %.3f km outside (0, 2000]", r.CongestionAltitudeBandKm)
	}
	if r.CongestionInclinationBandDeg <= 0 || r.CongestionInclinationBandDeg > 180 {
		is.Add("ranking.congestion_inclination_band_deg", "band %.3f deg outside (0, 180]", r.CongestionInclinationBandDeg)
	}
	if r.ReferenceCongestion < 1 || r.ReferenceCongestion > 100000 {
		is.Add("ranking.reference_congestion", "reference congestion %d outside [1, 100000]", r.ReferenceCongestion)
	}
	if r.MaxTargets < 1 || r.MaxTargets > 1000 {
		is.Add("ranking.max_targets", "target cap %d outside [1, 1000]", r.MaxTargets)
	}
}

func (m MissionConfig) validate(is *model.Issues) {
	if m.MaxRAANWaitSeconds < 0 || m.MaxRAANWaitSeconds > 3650*86400 {
		is.Add("mission.max_raan_wait_s", "RAAN wait cap %d s outside [0, 315360000]", m.MaxRAANWaitSeconds)
	}
	if m.PhasingMarginMps < 0 || m.PhasingMarginMps > 500 {
		is.Add("mission.phasing_margin_mps", "phasing margin %.4f m/s outside [0, 500]", m.PhasingMarginMps)
	}
	if m.ReserveFraction < 0 || m.ReserveFraction >= 1 {
		is.Add("mission.reserve_fraction", "reserve fraction %.4f outside [0, 1)", m.ReserveFraction)
	}
	if m.PlaneChangeEfficiency <= 0 || m.PlaneChangeEfficiency > 1 {
		is.Add("mission.plane_change_efficiency", "efficiency %.4f outside (0, 1]", m.PlaneChangeEfficiency)
	}
	if m.CaptureMarginMps < 0 || m.CaptureMarginMps > 500 {
		is.Add("mission.capture_margin_mps", "capture margin %.4f m/s outside [0, 500]", m.CaptureMarginMps)
	}
	if m.TransferSettleSeconds < 0 || m.TransferSettleSeconds > 30*86400 {
		is.Add("mission.transfer_settle_s", "settle time %d s outside [0, 2592000]", m.TransferSettleSeconds)
	}
}

func (c ComplianceConfig) validate(is *model.Issues) {
	if c.MaxPostMissionLifetimeYears <= 0 || c.MaxPostMissionLifetimeYears > 500 {
		is.Add("compliance.max_post_mission_lifetime_years",
			"lifetime limit %.3f years outside (0, 500]", c.MaxPostMissionLifetimeYears)
	}
	if c.ReentryMaxAltitudeKm < model.MinAltitudeKm || c.ReentryMaxAltitudeKm > 2000 {
		is.Add("compliance.reentry_max_altitude_km",
			"reentry ceiling %.3f km outside [%.0f, 2000]", c.ReentryMaxAltitudeKm, model.MinAltitudeKm)
	}
	if c.ReentryTargetPerigeeKm < 40 || c.ReentryTargetPerigeeKm > 150 {
		is.Add("compliance.reentry_target_perigee_km",
			"target perigee %.3f km outside [40, 150]", c.ReentryTargetPerigeeKm)
	}
	if c.GraveyardRaiseKm < 10 || c.GraveyardRaiseKm > 5000 {
		is.Add("compliance.graveyard_raise_km", "graveyard raise %.3f km outside [10, 5000]", c.GraveyardRaiseKm)
	}
	if c.ProtectedRegionTopKm < 500 || c.ProtectedRegionTopKm > 5000 {
		is.Add("compliance.protected_region_top_km",
			"protected region ceiling %.3f km outside [500, 5000]", c.ProtectedRegionTopKm)
	}
}

func (o OutputConfig) validate(is *model.Issues) {
	if o.DistanceDecimals < 0 || o.DistanceDecimals > 12 {
		is.Add("output.distance_decimals", "decimals %d outside [0, 12]", o.DistanceDecimals)
	}
	if o.DeltaVDecimals < 0 || o.DeltaVDecimals > 12 {
		is.Add("output.delta_v_decimals", "decimals %d outside [0, 12]", o.DeltaVDecimals)
	}
	if o.ProbabilityExponentDigits < 1 || o.ProbabilityExponentDigits > 17 {
		is.Add("output.probability_exponent_digits", "digits %d outside [1, 17]", o.ProbabilityExponentDigits)
	}
}

func containsString(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

// Summary renders a deterministic single-line digest of the knobs that most
// affect the numbers, for inclusion in reports.
func (c Config) Summary() string {
	parts := []string{
		fmt.Sprintf("label=%s", c.Label),
		fmt.Sprintf("coarse_step_s=%g", c.Propagation.CoarseStepSeconds),
		fmt.Sprintf("refine_tol_s=%g", c.Propagation.RefineToleranceS),
		fmt.Sprintf("j2_nodal_drift=%t", c.Propagation.IncludeJ2NodalDrift),
		fmt.Sprintf("screen_km=%g", c.Screening.MissDistanceThresholdKm),
		fmt.Sprintf("required_km=%g", c.Screening.RequiredMissDistanceKm),
		fmt.Sprintf("sigma_base_km=%g", c.Probability.SigmaBaseKm),
		fmt.Sprintf("actionable=%s", c.Severity.ActionableSeverity),
	}
	sort.Strings(parts)
	return strings.Join(parts, " ")
}
