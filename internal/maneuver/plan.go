// Package maneuver turns actionable conjunctions into a collision-avoidance
// burn schedule.
//
// Only along-track (tangential) burns are planned. Within the simplified
// circular-orbit model a tangential impulse is the cheapest way to move a
// spacecraft relative to a fixed point in its own orbit, and its effect grows
// linearly with the lead time, which makes the required delta-v invertible in
// closed form.
//
// Sign convention. A prograde impulse raises the semi-major axis, which lowers
// the mean motion, so the asset progressively falls behind its unperturbed
// position. The catalogue object keeps its trajectory, therefore the in-track
// component of the relative position, measured as object minus asset in the
// asset frame, increases. A retrograde impulse does the opposite. The planner
// always pushes the relative in-track component further away from zero, which
// is the direction that needs the least delta-v.
package maneuver

import (
	"math"
	"sort"

	"DebrisLedger/internal/config"
	"DebrisLedger/internal/model"
	"DebrisLedger/internal/numeric"
	"DebrisLedger/internal/orbit"
	"DebrisLedger/internal/screen"
)

// Burn statuses.
const (
	StatusScheduled   = "scheduled"
	StatusConflict    = "conflict"
	StatusOverBudget  = "budget-exceeded"
	StatusOverPerBurn = "per-burn-cap-exceeded"
	StatusNoLeadTime  = "insufficient-lead-time"
	StatusNotRequired = "not-required"
	StatusAllowance   = "burn-allowance-exceeded"
)

// Burn is a single planned or rejected avoidance manoeuvre.
type Burn struct {
	AssetID              string             `json:"asset_id"`
	AssetName            string             `json:"asset_name"`
	ObjectID             string             `json:"object_id"`
	ObjectName           string             `json:"object_name"`
	ConjunctionKey       string             `json:"conjunction_key"`
	TCASeconds           int64              `json:"tca_s"`
	BurnSeconds          int64              `json:"burn_s"`
	LeadSeconds          int64              `json:"lead_s"`
	Direction            string             `json:"direction"`
	ThrusterType         model.ThrusterType `json:"thruster_type"`
	DeltaVMps            float64            `json:"delta_v_mps"`
	RequiredShiftKm      float64            `json:"required_shift_km"`
	AchievedShiftKm      float64            `json:"achieved_shift_km"`
	SemiMajorAxisDeltaKm float64            `json:"semi_major_axis_delta_km"`
	OriginalMissKm       float64            `json:"original_miss_km"`
	PredictedMissKm      float64            `json:"predicted_miss_km"`
	OriginalProbability  float64            `json:"original_probability"`
	PredictedProbability float64            `json:"predicted_probability"`
	CumulativeDeltaVMps  float64            `json:"cumulative_delta_v_mps"`
	Status               string             `json:"status"`
	Reason               string             `json:"reason"`
	ConflictsWith        []string           `json:"conflicts_with"`
}

// Window returns the exclusive time window the burn occupies, guarded on both
// sides by the configured conflict guard.
func (b Burn) Window(guard int64) (int64, int64) {
	return b.BurnSeconds - guard, b.TCASeconds + guard
}

// AssetSummary aggregates the planning outcome for one asset.
type AssetSummary struct {
	AssetID        string  `json:"asset_id"`
	AssetName      string  `json:"asset_name"`
	BudgetMps      float64 `json:"budget_mps"`
	PlannedMps     float64 `json:"planned_mps"`
	RemainingMps   float64 `json:"remaining_mps"`
	UtilizationPct float64 `json:"utilization_pct"`
	Scheduled      int     `json:"scheduled"`
	Rejected       int     `json:"rejected"`
	Conflicts      int     `json:"conflicts"`
	MaxBurnsPerDay int     `json:"max_burns_per_day"`
	EffectiveLeadS int64   `json:"effective_lead_s"`
}

// Stats summarises the whole schedule.
type Stats struct {
	Candidates           int     `json:"candidates"`
	Scheduled            int     `json:"scheduled"`
	Rejected             int     `json:"rejected"`
	Conflicts            int     `json:"conflicts"`
	TotalDeltaVMps       float64 `json:"total_delta_v_mps"`
	MaxDeltaVMps         float64 `json:"max_delta_v_mps"`
	ProbabilityBefore    float64 `json:"probability_before"`
	ProbabilityAfter     float64 `json:"probability_after"`
	ProbabilityReduction float64 `json:"probability_reduction"`
}

// Plan is the deterministic manoeuvre schedule.
type Plan struct {
	ScenarioLabel  string         `json:"scenario_label"`
	ConfigLabel    string         `json:"config_label"`
	EpochSeconds   int64          `json:"epoch_s"`
	RequiredMissKm float64        `json:"required_miss_km"`
	Burns          []Burn         `json:"burns"`
	Assets         []AssetSummary `json:"assets"`
	Stats          Stats          `json:"stats"`
	Assumptions    []string       `json:"assumptions"`
}

// Scheduled returns only the burns that made it into the schedule.
func (p Plan) Scheduled() []Burn {
	out := make([]Burn, 0, len(p.Burns))
	for _, b := range p.Burns {
		if b.Status == StatusScheduled {
			out = append(out, b)
		}
	}
	return out
}

// Assumptions returned with every plan.
func planAssumptions() []string {
	return []string{
		"only tangential (along-track) impulses are planned",
		"in-track displacement after an impulse is s = 3 * dv * lead_time",
		"radial and cross-track components of the miss vector are unchanged by the burn",
		"the catalogue object is assumed not to manoeuvre",
		"delta-v is rounded up to the commandable resolution and carries the configured margin",
		"a burn occupies its whole lead interval, guarded on both sides, for conflict detection",
	}
}

// Build plans avoidance manoeuvres for every actionable conjunction.
//
// Processing order is by asset identifier, then by closest-approach time. That
// order is what makes the delta-v accounting and the conflict resolution
// reproducible: the earlier conjunction always gets the slot.
func Build(sc *model.Scenario, cfg config.Config, res screen.Result) Plan {
	plan := Plan{
		ScenarioLabel:  sc.Label,
		ConfigLabel:    cfg.Label,
		EpochSeconds:   sc.EpochSeconds,
		RequiredMissKm: cfg.Screening.RequiredMissDistanceKm,
		Assumptions:    planAssumptions(),
	}
	grouped := res.ByAsset()
	summaries := make([]AssetSummary, 0, len(sc.Assets))

	for _, asset := range sc.Assets {
		candidates := actionableFor(grouped[asset.ID])
		summary := AssetSummary{
			AssetID:        asset.ID,
			AssetName:      asset.Name,
			BudgetMps:      numeric.Round(asset.Maneuver.DeltaVBudgetMps, cfg.Output.DeltaVDecimals),
			MaxBurnsPerDay: asset.Maneuver.MaxBurnsPerDay,
			EffectiveLeadS: asset.Maneuver.EffectiveLeadSeconds(),
		}
		var scheduled []Burn
		used := 0.0
		for _, c := range candidates {
			plan.Stats.Candidates++
			burn := planBurn(sc, cfg, asset, c)
			if burn.Status == StatusScheduled {
				if conflicts := findConflicts(scheduled, burn, cfg.Maneuver.ConflictGuardSeconds); len(conflicts) > 0 {
					burn.Status = StatusConflict
					burn.Reason = "overlaps an already scheduled manoeuvre for this asset"
					burn.ConflictsWith = conflicts
				} else if !allowanceOK(scheduled, burn, asset.Maneuver.MaxBurnsPerDay) {
					burn.Status = StatusAllowance
					burn.Reason = "exceeds the declared burn allowance within a 24 h window"
				} else if used+burn.DeltaVMps > asset.Maneuver.DeltaVBudgetMps+1e-9 {
					burn.Status = StatusOverBudget
					burn.Reason = "remaining delta-v budget is insufficient"
				}
			}
			if burn.Status == StatusScheduled {
				used += burn.DeltaVMps
				burn.CumulativeDeltaVMps = numeric.Round(used, cfg.Output.DeltaVDecimals)
				scheduled = append(scheduled, burn)
				summary.Scheduled++
			} else {
				burn.CumulativeDeltaVMps = numeric.Round(used, cfg.Output.DeltaVDecimals)
				summary.Rejected++
				if burn.Status == StatusConflict {
					summary.Conflicts++
				}
			}
			plan.Burns = append(plan.Burns, burn)
		}
		summary.PlannedMps = numeric.Round(used, cfg.Output.DeltaVDecimals)
		summary.RemainingMps = numeric.Round(asset.Maneuver.DeltaVBudgetMps-used, cfg.Output.DeltaVDecimals)
		summary.UtilizationPct = numeric.Round(numeric.Ratio(used, asset.Maneuver.DeltaVBudgetMps)*100, 3)
		summaries = append(summaries, summary)
	}

	sort.SliceStable(summaries, func(i, j int) bool { return summaries[i].AssetID < summaries[j].AssetID })
	plan.Assets = summaries
	sortBurns(plan.Burns)
	plan.Stats = summarise(plan.Burns, cfg)
	return plan
}

func actionableFor(list []screen.Conjunction) []screen.Conjunction {
	out := make([]screen.Conjunction, 0, len(list))
	for _, c := range list {
		if c.Actionable {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].TCASeconds != out[j].TCASeconds {
			return out[i].TCASeconds < out[j].TCASeconds
		}
		return out[i].ObjectID < out[j].ObjectID
	})
	return out
}

// planBurn computes the minimal along-track delta-v that lifts the miss
// distance above the required threshold.
//
// With the radial component r and the cross-track component c left untouched,
// the post-burn miss distance is sqrt(r^2 + (i+s)^2 + c^2). Requiring that to
// reach the threshold D gives |i+s| >= K with K = sqrt(D^2 - r^2 - c^2), so the
// smallest displacement is K - |i| applied in the sign of i.
func planBurn(sc *model.Scenario, cfg config.Config, asset model.Asset, c screen.Conjunction) Burn {
	burn := Burn{
		AssetID:             asset.ID,
		AssetName:           asset.Name,
		ObjectID:            c.ObjectID,
		ObjectName:          c.ObjectName,
		ConjunctionKey:      c.Key(),
		TCASeconds:          c.TCASeconds,
		ThrusterType:        asset.Maneuver.ThrusterType,
		OriginalMissKm:      c.MissKm,
		OriginalProbability: c.Probability,
		ConflictsWith:       []string{},
	}
	lead := cfg.Maneuver.PlanningLeadSeconds
	if eff := asset.Maneuver.EffectiveLeadSeconds(); eff > lead {
		lead = eff
	}
	burn.LeadSeconds = lead
	burn.BurnSeconds = c.TCASeconds - lead
	required := cfg.Screening.RequiredMissDistanceKm

	if c.MissKm >= required {
		burn.Status = StatusNotRequired
		burn.Reason = "miss distance already meets the required threshold"
		return burn
	}
	if burn.BurnSeconds < sc.EpochSeconds {
		burn.Status = StatusNoLeadTime
		burn.Reason = "the closest approach happens before the earliest commandable burn"
		return burn
	}

	outOfPlane := c.RadialKm*c.RadialKm + c.CrossTrackKm*c.CrossTrackKm
	target := required*required - outOfPlane
	if target < 0 {
		target = 0
	}
	k := math.Sqrt(target)
	shift := k - math.Abs(c.InTrackKm)
	if shift < 0 {
		shift = 0
	}
	direction := "prograde"
	if c.InTrackKm < 0 {
		direction = "retrograde"
	}
	burn.Direction = direction
	burn.RequiredShiftKm = numeric.Round(shift, cfg.Output.DistanceDecimals)

	raw := orbit.DeltaVForAlongTrackKm(shift, float64(lead))
	dv := quantise(raw*cfg.Maneuver.DeltaVMarginFactor, cfg.Maneuver.MinDeltaVResolutionMps)
	burn.DeltaVMps = numeric.Round(dv, cfg.Output.DeltaVDecimals)
	if dv > cfg.Maneuver.MaxDeltaVPerBurnMps {
		burn.Status = StatusOverPerBurn
		burn.Reason = "required delta-v exceeds the per-burn cap"
		return burn
	}

	achieved := orbit.AlongTrackDisplacementKm(dv, float64(lead))
	signed := achieved
	if direction == "retrograde" {
		signed = -achieved
	}
	newInTrack := c.InTrackKm + signed
	newMiss := math.Sqrt(outOfPlane + newInTrack*newInTrack)
	sigma := screen.PositionSigmaKm(cfg.Probability, float64(c.LeadSeconds))
	burn.AchievedShiftKm = numeric.Round(achieved, cfg.Output.DistanceDecimals)
	burn.SemiMajorAxisDeltaKm = numeric.Round(orbit.SemiMajorAxisChangeKm(asset.Orbit.AltitudeKm, dv), 6)
	burn.PredictedMissKm = numeric.Round(newMiss, cfg.Output.DistanceDecimals)
	burn.PredictedProbability = numeric.RoundSignificant(
		screen.CollisionProbability(newMiss, c.CombinedRadiusM, sigma), cfg.Output.ProbabilityExponentDigits)
	burn.Status = StatusScheduled
	burn.Reason = "minimal along-track correction raising the miss distance above the threshold"
	return burn
}

// quantise rounds a delta-v up to the next commandable increment so that the
// planned value is always achievable and never under-delivers.
func quantise(dv, resolution float64) float64 {
	if resolution <= 0 {
		return dv
	}
	steps := math.Ceil(dv/resolution - 1e-9)
	if steps < 1 {
		steps = 1
	}
	return steps * resolution
}

func findConflicts(scheduled []Burn, candidate Burn, guard int64) []string {
	cs, ce := candidate.Window(guard)
	var out []string
	for _, b := range scheduled {
		bs, be := b.Window(guard)
		if cs <= be && bs <= ce {
			out = append(out, b.ConjunctionKey)
		}
	}
	sort.Strings(out)
	return out
}

func allowanceOK(scheduled []Burn, candidate Burn, maxPerDay int) bool {
	if maxPerDay <= 0 {
		return false
	}
	count := 1
	for _, b := range scheduled {
		diff := candidate.BurnSeconds - b.BurnSeconds
		if diff < 0 {
			diff = -diff
		}
		if diff < 86400 {
			count++
		}
	}
	return count <= maxPerDay
}

func sortBurns(list []Burn) {
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if a.BurnSeconds != b.BurnSeconds {
			return a.BurnSeconds < b.BurnSeconds
		}
		if a.AssetID != b.AssetID {
			return a.AssetID < b.AssetID
		}
		if a.ObjectID != b.ObjectID {
			return a.ObjectID < b.ObjectID
		}
		return a.Status < b.Status
	})
}

func summarise(list []Burn, cfg config.Config) Stats {
	st := Stats{}
	before := make([]float64, 0, len(list))
	after := make([]float64, 0, len(list))
	deltas := make([]float64, 0, len(list))
	for _, b := range list {
		before = append(before, b.OriginalProbability)
		if b.Status == StatusScheduled {
			st.Scheduled++
			deltas = append(deltas, b.DeltaVMps)
			after = append(after, b.PredictedProbability)
			if b.DeltaVMps > st.MaxDeltaVMps {
				st.MaxDeltaVMps = b.DeltaVMps
			}
		} else {
			st.Rejected++
			after = append(after, b.OriginalProbability)
			if b.Status == StatusConflict {
				st.Conflicts++
			}
		}
	}
	st.Candidates = len(list)
	dd := cfg.Output.DeltaVDecimals
	st.TotalDeltaVMps = numeric.Round(numeric.SumFloats(deltas), dd)
	st.MaxDeltaVMps = numeric.Round(st.MaxDeltaVMps, dd)
	pd := cfg.Output.ProbabilityExponentDigits
	st.ProbabilityBefore = numeric.RoundSignificant(numeric.SumFloats(before), pd)
	st.ProbabilityAfter = numeric.RoundSignificant(numeric.SumFloats(after), pd)
	st.ProbabilityReduction = numeric.RoundSignificant(st.ProbabilityBefore-st.ProbabilityAfter, pd)
	return st
}
