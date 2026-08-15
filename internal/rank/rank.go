// Package rank scores catalogue objects as active-removal targets and produces
// a deterministic ranked list.
//
// The risk score is a weighted sum of three normalised terms:
//
//	mass         how much debris-generating material the object represents
//	probability  the summed collision probability it contributes to the fleet
//	congestion   how crowded its altitude and inclination neighbourhood is
//
// Each term is normalised against a configured reference value and clamped to
// [0, 1], so the score itself is always in [0, 1] and is comparable between
// runs with the same configuration.
package rank

import (
	"fmt"
	"sort"

	"DebrisLedger/internal/compliance"
	"DebrisLedger/internal/config"
	"DebrisLedger/internal/model"
	"DebrisLedger/internal/numeric"
	"DebrisLedger/internal/orbit"
	"DebrisLedger/internal/screen"
)

// Feasibility records whether any declared chaser can capture the object.
type Feasibility struct {
	Capturable    bool     `json:"capturable"`
	TumbleOK      bool     `json:"tumble_ok"`
	MassOK        bool     `json:"mass_ok"`
	AttachPointOK bool     `json:"attach_point_ok"`
	ChaserID      string   `json:"chaser_id"`
	Blockers      []string `json:"blockers"`
}

// Target is one scored catalogue object.
type Target struct {
	Rank              int               `json:"rank"`
	ObjectID          string            `json:"object_id"`
	ObjectName        string            `json:"object_name"`
	Class             model.ObjectClass `json:"class"`
	MassKg            float64           `json:"mass_kg"`
	AreaM2            float64           `json:"area_m2"`
	AltitudeKm        float64           `json:"altitude_km"`
	InclinationDeg    float64           `json:"inclination_deg"`
	RAANDeg           float64           `json:"raan_deg"`
	TumbleRateDegPerS float64           `json:"tumble_rate_deg_s"`
	AttachPoint       string            `json:"attach_point"`
	Congestion        int               `json:"congestion"`
	Probability       float64           `json:"probability_contribution"`
	MassScore         float64           `json:"mass_score"`
	ProbabilityScore  float64           `json:"probability_score"`
	CongestionScore   float64           `json:"congestion_score"`
	RiskScore         float64           `json:"risk_score"`
	LifetimeYears     float64           `json:"lifetime_years"`
	Feasibility       Feasibility       `json:"feasibility"`
}

// Stats summarises a ranking run.
type Stats struct {
	Evaluated        int     `json:"evaluated"`
	Excluded         int     `json:"excluded"`
	Listed           int     `json:"listed"`
	Feasible         int     `json:"feasible"`
	Infeasible       int     `json:"infeasible"`
	TopScore         float64 `json:"top_score"`
	TotalMassKg      float64 `json:"total_mass_kg"`
	FeasibleMassKg   float64 `json:"feasible_mass_kg"`
	TotalRisk        float64 `json:"total_risk"`
	FeasibleRisk     float64 `json:"feasible_risk"`
	CongestionMedian int     `json:"congestion_median"`
}

// Ranking is the deterministic ranked target list.
type Ranking struct {
	ScenarioLabel string   `json:"scenario_label"`
	ConfigLabel   string   `json:"config_label"`
	EpochSeconds  int64    `json:"epoch_s"`
	Targets       []Target `json:"targets"`
	Stats         Stats    `json:"stats"`
	Assumptions   []string `json:"assumptions"`
}

// FeasibleTargets returns the capturable targets in rank order.
func (r Ranking) FeasibleTargets() []Target {
	out := make([]Target, 0, len(r.Targets))
	for _, t := range r.Targets {
		if t.Feasibility.Capturable {
			out = append(out, t)
		}
	}
	return out
}

// TargetByID looks up a scored target.
func (r Ranking) TargetByID(id string) (Target, bool) {
	for _, t := range r.Targets {
		if t.ObjectID == id {
			return t, true
		}
	}
	return Target{}, false
}

// RiskInputs converts the ranking into residual-risk inputs.
func (r Ranking) RiskInputs() []compliance.RiskInput {
	out := make([]compliance.RiskInput, 0, len(r.Targets))
	for _, t := range r.Targets {
		out = append(out, compliance.RiskInput{
			ObjectID:    t.ObjectID,
			MassKg:      t.MassKg,
			RiskScore:   t.RiskScore,
			Probability: t.Probability,
		})
	}
	return out
}

// Build scores every debris-class catalogue object and ranks it.
//
// Payload-class objects are excluded: the tool has no way to tell an active
// payload from a derelict one, and capturing an active spacecraft is not a
// debris-removal decision.
func Build(sc *model.Scenario, cfg config.Config, res screen.Result) Ranking {
	probabilities := res.ProbabilityByObject()
	congestion := screen.Congestion(sc, cfg.Ranking)
	out := Ranking{
		ScenarioLabel: sc.Label,
		ConfigLabel:   cfg.Label,
		EpochSeconds:  sc.EpochSeconds,
		Assumptions:   rankAssumptions(),
	}
	chasers := make([]model.Chaser, len(sc.Chasers))
	copy(chasers, sc.Chasers)
	sort.SliceStable(chasers, func(i, j int) bool { return chasers[i].ID < chasers[j].ID })

	congestionValues := make([]int, 0, len(sc.Objects))
	for _, obj := range sc.Objects {
		if !obj.Class.DebrisCandidate() {
			out.Stats.Excluded++
			continue
		}
		out.Stats.Evaluated++
		t := score(cfg, obj, probabilities[obj.ID], congestion[obj.ID])
		t.Feasibility = assess(chasers, obj)
		out.Targets = append(out.Targets, t)
		congestionValues = append(congestionValues, t.Congestion)
	}

	sortTargets(out.Targets)
	if cfg.Ranking.MaxTargets > 0 && len(out.Targets) > cfg.Ranking.MaxTargets {
		out.Targets = out.Targets[:cfg.Ranking.MaxTargets]
	}
	for i := range out.Targets {
		out.Targets[i].Rank = i + 1
	}
	out.Stats = finalise(out.Stats, out.Targets, congestionValues)
	return out
}

func score(cfg config.Config, obj model.CatalogObject, probability float64, congestion int) Target {
	rc := cfg.Ranking
	massScore := numeric.Clamp(numeric.Ratio(obj.MassKg, rc.ReferenceMassKg), 0, 1)
	probScore := numeric.Clamp(numeric.Ratio(probability, rc.ReferenceProbability), 0, 1)
	congScore := numeric.Clamp(numeric.Ratio(float64(congestion), float64(rc.ReferenceCongestion)), 0, 1)
	risk := rc.MassWeight*massScore + rc.ProbabilityWeight*probScore + rc.CongestionWeight*congScore
	return Target{
		ObjectID:          obj.ID,
		ObjectName:        obj.Name,
		Class:             obj.Class,
		MassKg:            numeric.Round(obj.MassKg, 3),
		AreaM2:            numeric.Round(obj.AreaM2, 4),
		AltitudeKm:        numeric.Round(obj.Orbit.AltitudeKm, 4),
		InclinationDeg:    numeric.Round(obj.Orbit.InclinationDeg, 4),
		RAANDeg:           numeric.Round(obj.Orbit.RAANDeg, 4),
		TumbleRateDegPerS: numeric.Round(obj.TumbleRateDegPerS, 4),
		AttachPoint:       obj.AttachPoint,
		Congestion:        congestion,
		Probability:       numeric.RoundSignificant(probability, cfg.Output.ProbabilityExponentDigits),
		MassScore:         numeric.Round(massScore, 6),
		ProbabilityScore:  numeric.Round(probScore, 6),
		CongestionScore:   numeric.Round(congScore, 6),
		RiskScore:         numeric.Round(risk, 6),
		LifetimeYears:     numeric.Round(compliance.LifetimeYears(obj.Orbit.AltitudeKm, obj.AreaToMass()), 4),
	}
}

// assess walks the chasers in identifier order and reports the first one that
// can capture the object, together with the blocking reasons when none can.
func assess(chasers []model.Chaser, obj model.CatalogObject) Feasibility {
	f := Feasibility{Blockers: []string{}}
	if !obj.HasAttachPoint() {
		f.Blockers = append(f.Blockers, "no usable attach point declared")
	} else {
		f.AttachPointOK = true
	}
	if len(chasers) == 0 {
		f.Blockers = append(f.Blockers, "scenario declares no chaser vehicles")
		sort.Strings(f.Blockers)
		return f
	}
	bestMass := 0.0
	bestTumble := 0.0
	for _, ch := range chasers {
		if ch.MaxCaptureMassKg > bestMass {
			bestMass = ch.MaxCaptureMassKg
		}
		if ch.MaxTumbleRateDegPerS > bestTumble {
			bestTumble = ch.MaxTumbleRateDegPerS
		}
		massOK := obj.MassKg <= ch.MaxCaptureMassKg
		tumbleOK := obj.TumbleRateDegPerS <= ch.MaxTumbleRateDegPerS
		if massOK && tumbleOK && f.AttachPointOK {
			f.Capturable = true
			f.MassOK = true
			f.TumbleOK = true
			f.ChaserID = ch.ID
			return f
		}
	}
	f.MassOK = obj.MassKg <= bestMass
	f.TumbleOK = obj.TumbleRateDegPerS <= bestTumble
	if !f.MassOK {
		f.Blockers = append(f.Blockers,
			fmt.Sprintf("mass %s kg exceeds every chaser capture limit (best %s kg)",
				numeric.Fixed(obj.MassKg, 3), numeric.Fixed(bestMass, 3)))
	}
	if !f.TumbleOK {
		f.Blockers = append(f.Blockers,
			fmt.Sprintf("tumble rate %s deg/s exceeds every chaser limit (best %s deg/s)",
				numeric.Fixed(obj.TumbleRateDegPerS, 4), numeric.Fixed(bestTumble, 4)))
	}
	sort.Strings(f.Blockers)
	return f
}

func sortTargets(list []Target) {
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if a.RiskScore != b.RiskScore {
			return a.RiskScore > b.RiskScore
		}
		if a.MassKg != b.MassKg {
			return a.MassKg > b.MassKg
		}
		if a.Congestion != b.Congestion {
			return a.Congestion > b.Congestion
		}
		return a.ObjectID < b.ObjectID
	})
}

func finalise(st Stats, targets []Target, congestionValues []int) Stats {
	st.Listed = len(targets)
	masses := make([]float64, 0, len(targets))
	feasibleMasses := make([]float64, 0, len(targets))
	risks := make([]float64, 0, len(targets))
	feasibleRisks := make([]float64, 0, len(targets))
	for _, t := range targets {
		masses = append(masses, t.MassKg)
		risks = append(risks, t.RiskScore)
		if t.Feasibility.Capturable {
			st.Feasible++
			feasibleMasses = append(feasibleMasses, t.MassKg)
			feasibleRisks = append(feasibleRisks, t.RiskScore)
		} else {
			st.Infeasible++
		}
		if t.RiskScore > st.TopScore {
			st.TopScore = t.RiskScore
		}
	}
	st.TotalMassKg = numeric.Round(numeric.SumFloats(masses), 3)
	st.FeasibleMassKg = numeric.Round(numeric.SumFloats(feasibleMasses), 3)
	st.TotalRisk = numeric.Round(numeric.SumFloats(risks), 6)
	st.FeasibleRisk = numeric.Round(numeric.SumFloats(feasibleRisks), 6)
	st.TopScore = numeric.Round(st.TopScore, 6)
	st.CongestionMedian = median(congestionValues)
	return st
}

func median(values []int) int {
	if len(values) == 0 {
		return 0
	}
	ordered := make([]int, len(values))
	copy(ordered, values)
	sort.Ints(ordered)
	mid := len(ordered) / 2
	if len(ordered)%2 == 1 {
		return ordered[mid]
	}
	return (ordered[mid-1] + ordered[mid]) / 2
}

// PlaneOffsetDeg reports the plane angle between a target and a chaser at the
// scenario epoch, which the mission planner uses for its first ordering guess.
func PlaneOffsetDeg(prop orbit.Propagator, chaser model.Chaser, t Target, atTimeS float64) float64 {
	targetOrbit := model.Orbit{
		AltitudeKm:     t.AltitudeKm,
		InclinationDeg: t.InclinationDeg,
		RAANDeg:        t.RAANDeg,
		EpochSeconds:   int64(atTimeS),
	}
	return orbit.Deg(prop.PlaneAngleRad(chaser.Orbit, targetOrbit, atTimeS))
}

func rankAssumptions() []string {
	return []string{
		"only rocket-body and fragment classes are considered for removal",
		"mass, probability and congestion terms are normalised against configured references and clamped to [0, 1]",
		"congestion counts catalogue neighbours inside the configured altitude and inclination bands",
		"capture feasibility needs an attach point, a mass inside the chaser limit and a tumble rate inside the chaser limit",
		"the first chaser in identifier order that satisfies every limit is recorded",
	}
}
