package compliance

import (
	"fmt"
	"sort"

	"DebrisLedger/internal/config"
	"DebrisLedger/internal/model"
	"DebrisLedger/internal/numeric"
	"DebrisLedger/internal/orbit"
)

// Disposal actions.
const (
	ActionReentry   = "controlled-reentry"
	ActionGraveyard = "graveyard"
	ActionNone      = "none"
)

// Finding statuses.
const (
	StatusPass          = "pass"
	StatusFail          = "fail"
	StatusNotApplicable = "not-applicable"
)

// Rule identifiers, stable across releases so that reports can be diffed.
const (
	RulePMDLifetime      = "PMD-01-residual-lifetime"
	RulePMDProtected     = "PMD-02-protected-region"
	RuleDisposalAction   = "PMD-03-disposal-action"
	RuleReentryPerigee   = "PMD-04-reentry-perigee"
	RuleGraveyardClear   = "PMD-05-graveyard-clearance"
	RuleCaptureIntegrity = "PMD-06-capture-integrity"
)

// Finding is the outcome of evaluating one rule against one subject.
type Finding struct {
	RuleID  string `json:"rule_id"`
	Subject string `json:"subject"`
	Status  string `json:"status"`
	Detail  string `json:"detail"`
}

// Findings is a sortable collection of findings.
type Findings []Finding

// Sort orders findings by subject then rule so that the report is stable.
func (f Findings) Sort() {
	sort.SliceStable(f, func(i, j int) bool {
		if f[i].Subject != f[j].Subject {
			return f[i].Subject < f[j].Subject
		}
		if f[i].RuleID != f[j].RuleID {
			return f[i].RuleID < f[j].RuleID
		}
		return f[i].Detail < f[j].Detail
	})
}

// Failures returns only the failing findings.
func (f Findings) Failures() Findings {
	out := Findings{}
	for _, item := range f {
		if item.Status == StatusFail {
			out = append(out, item)
		}
	}
	return out
}

// Disposal is the planned end state of a removed object.
type Disposal struct {
	ObjectID              string   `json:"object_id"`
	Action                string   `json:"action"`
	DeltaVMps             float64  `json:"delta_v_mps"`
	FromAltitudeKm        float64  `json:"from_altitude_km"`
	TargetAltitudeKm      float64  `json:"target_altitude_km"`
	TargetPerigeeKm       float64  `json:"target_perigee_km"`
	LifetimeBeforeYears   float64  `json:"lifetime_before_years"`
	LifetimeAfterYears    float64  `json:"lifetime_after_years"`
	NaturalDecayCeilingKm float64  `json:"natural_decay_ceiling_km"`
	Compliant             bool     `json:"compliant"`
	Findings              Findings `json:"findings"`
}

// PlanDisposal chooses and costs the disposal action for one object.
//
// Policy, in order:
//
//  1. If the object already decays inside the configured lifetime limit, no
//     disposal burn is charged.
//  2. Otherwise, below the reentry ceiling the object is de-orbited by lowering
//     its perigee to the configured target.
//  3. Above the ceiling it is raised into a graveyard orbit clear of the
//     protected region.
func PlanDisposal(cfg config.ComplianceConfig, obj model.CatalogObject) Disposal {
	ratio := obj.AreaToMass()
	before := LifetimeYears(obj.Orbit.AltitudeKm, ratio)
	d := Disposal{
		ObjectID:              obj.ID,
		FromAltitudeKm:        numeric.Round(obj.Orbit.AltitudeKm, 4),
		LifetimeBeforeYears:   numeric.Round(before, 4),
		NaturalDecayCeilingKm: numeric.Round(NaturalDecayAltitudeKm(ratio, cfg.MaxPostMissionLifetimeYears), 4),
	}

	switch {
	case before <= cfg.MaxPostMissionLifetimeYears:
		d.Action = ActionNone
		d.TargetAltitudeKm = d.FromAltitudeKm
		d.LifetimeAfterYears = d.LifetimeBeforeYears
	case obj.Orbit.AltitudeKm <= cfg.ReentryMaxAltitudeKm:
		d.Action = ActionReentry
		d.DeltaVMps = numeric.Round(orbit.DeorbitDeltaVMps(obj.Orbit.AltitudeKm, cfg.ReentryTargetPerigeeKm), 5)
		d.TargetPerigeeKm = numeric.Round(cfg.ReentryTargetPerigeeKm, 4)
		d.TargetAltitudeKm = d.TargetPerigeeKm
		d.LifetimeAfterYears = 0
	default:
		d.Action = ActionGraveyard
		d.DeltaVMps = numeric.Round(orbit.GraveyardDeltaVMps(obj.Orbit.AltitudeKm, cfg.GraveyardRaiseKm), 5)
		d.TargetAltitudeKm = numeric.Round(obj.Orbit.AltitudeKm+cfg.GraveyardRaiseKm, 4)
		d.LifetimeAfterYears = numeric.Round(LifetimeYears(d.TargetAltitudeKm, ratio), 4)
	}

	d.Findings = evaluateDisposal(cfg, obj, d)
	d.Findings.Sort()
	d.Compliant = len(d.Findings.Failures()) == 0
	return d
}

func evaluateDisposal(cfg config.ComplianceConfig, obj model.CatalogObject, d Disposal) Findings {
	out := Findings{}
	subject := obj.ID

	switch d.Action {
	case ActionReentry:
		out = append(out, Finding{
			RuleID:  RulePMDLifetime,
			Subject: subject,
			Status:  StatusPass,
			Detail: fmt.Sprintf("controlled reentry removes the object; estimated lifetime falls from %s to 0 years",
				numeric.Fixed(d.LifetimeBeforeYears, 3)),
		})
		if cfg.ReentryTargetPerigeeKm >= model.MinAltitudeKm {
			out = append(out, Finding{
				RuleID:  RuleReentryPerigee,
				Subject: subject,
				Status:  StatusFail,
				Detail: fmt.Sprintf("target perigee %s km is not low enough to guarantee reentry",
					numeric.Fixed(cfg.ReentryTargetPerigeeKm, 3)),
			})
		} else {
			out = append(out, Finding{
				RuleID:  RuleReentryPerigee,
				Subject: subject,
				Status:  StatusPass,
				Detail: fmt.Sprintf("target perigee %s km lies below the %s km reentry threshold",
					numeric.Fixed(cfg.ReentryTargetPerigeeKm, 3), numeric.Fixed(model.MinAltitudeKm, 3)),
			})
		}
		out = append(out, Finding{
			RuleID:  RulePMDProtected,
			Subject: subject,
			Status:  StatusPass,
			Detail:  "object leaves the protected region entirely",
		})
	case ActionGraveyard:
		lifetimeOK := d.TargetAltitudeKm >= cfg.ProtectedRegionTopKm
		status := StatusPass
		detail := fmt.Sprintf("graveyard altitude %s km clears the %s km protected region ceiling",
			numeric.Fixed(d.TargetAltitudeKm, 3), numeric.Fixed(cfg.ProtectedRegionTopKm, 3))
		if !lifetimeOK {
			status = StatusFail
			detail = fmt.Sprintf("graveyard altitude %s km still lies inside the %s km protected region",
				numeric.Fixed(d.TargetAltitudeKm, 3), numeric.Fixed(cfg.ProtectedRegionTopKm, 3))
		}
		out = append(out, Finding{RuleID: RuleGraveyardClear, Subject: subject, Status: status, Detail: detail})
		out = append(out, Finding{
			RuleID:  RulePMDProtected,
			Subject: subject,
			Status:  status,
			Detail:  detail,
		})
		lifeStatus := StatusFail
		lifeDetail := fmt.Sprintf("residual lifetime %s years exceeds the %s year limit",
			numeric.Fixed(d.LifetimeAfterYears, 3), numeric.Fixed(cfg.MaxPostMissionLifetimeYears, 3))
		if d.TargetAltitudeKm >= cfg.ProtectedRegionTopKm {
			lifeStatus = StatusNotApplicable
			lifeDetail = "object is disposed above the protected region, where the lifetime limit does not apply"
		} else if d.LifetimeAfterYears <= cfg.MaxPostMissionLifetimeYears {
			lifeStatus = StatusPass
			lifeDetail = fmt.Sprintf("residual lifetime %s years is inside the %s year limit",
				numeric.Fixed(d.LifetimeAfterYears, 3), numeric.Fixed(cfg.MaxPostMissionLifetimeYears, 3))
		}
		out = append(out, Finding{RuleID: RulePMDLifetime, Subject: subject, Status: lifeStatus, Detail: lifeDetail})
	default:
		out = append(out, Finding{
			RuleID:  RulePMDLifetime,
			Subject: subject,
			Status:  StatusPass,
			Detail: fmt.Sprintf("natural decay in %s years is inside the %s year limit",
				numeric.Fixed(d.LifetimeBeforeYears, 3), numeric.Fixed(cfg.MaxPostMissionLifetimeYears, 3)),
		})
		out = append(out, Finding{
			RuleID:  RulePMDProtected,
			Subject: subject,
			Status:  StatusNotApplicable,
			Detail:  "no disposal manoeuvre is required, so no new protected-region occupancy is created",
		})
	}

	actionStatus := StatusPass
	actionDetail := fmt.Sprintf("action %q matches the altitude policy (reentry ceiling %s km)",
		d.Action, numeric.Fixed(cfg.ReentryMaxAltitudeKm, 3))
	if d.Action == ActionGraveyard && obj.Orbit.AltitudeKm <= cfg.ReentryMaxAltitudeKm {
		actionStatus = StatusFail
		actionDetail = "graveyard disposal selected below the reentry ceiling"
	}
	out = append(out, Finding{RuleID: RuleDisposalAction, Subject: subject, Status: actionStatus, Detail: actionDetail})

	captureStatus := StatusPass
	captureDetail := fmt.Sprintf("declared attach point %q is usable", obj.AttachPoint)
	if !obj.HasAttachPoint() {
		captureStatus = StatusFail
		captureDetail = "no usable attach point declared, so a controlled disposal cannot be guaranteed"
	}
	out = append(out, Finding{RuleID: RuleCaptureIntegrity, Subject: subject, Status: captureStatus, Detail: captureDetail})

	return out
}

// ResidualRisk compares the catalogue risk before and after a set of removals.
type ResidualRisk struct {
	ObjectsBefore        int      `json:"objects_before"`
	ObjectsAfter         int      `json:"objects_after"`
	MassBeforeKg         float64  `json:"mass_before_kg"`
	MassAfterKg          float64  `json:"mass_after_kg"`
	RiskBefore           float64  `json:"risk_before"`
	RiskAfter            float64  `json:"risk_after"`
	RiskReduction        float64  `json:"risk_reduction"`
	RiskReductionPct     float64  `json:"risk_reduction_pct"`
	ProbabilityBefore    float64  `json:"probability_before"`
	ProbabilityAfter     float64  `json:"probability_after"`
	ProbabilityReduction float64  `json:"probability_reduction"`
	RemovedIDs           []string `json:"removed_ids"`
}

// RiskInput is one catalogue object's contribution to the residual-risk sum.
type RiskInput struct {
	ObjectID    string
	MassKg      float64
	RiskScore   float64
	Probability float64
}

// ComputeResidualRisk sums the contributions of every object and of the subset
// that remains after the removals.
func ComputeResidualRisk(inputs []RiskInput, removed []string) ResidualRisk {
	removedSet := make(map[string]bool, len(removed))
	for _, id := range removed {
		removedSet[id] = true
	}
	ordered := make([]RiskInput, len(inputs))
	copy(ordered, inputs)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].ObjectID < ordered[j].ObjectID })

	var massBefore, massAfter, riskBefore, riskAfter, probBefore, probAfter []float64
	out := ResidualRisk{RemovedIDs: []string{}}
	for _, in := range ordered {
		out.ObjectsBefore++
		massBefore = append(massBefore, in.MassKg)
		riskBefore = append(riskBefore, in.RiskScore)
		probBefore = append(probBefore, in.Probability)
		if removedSet[in.ObjectID] {
			out.RemovedIDs = append(out.RemovedIDs, in.ObjectID)
			continue
		}
		out.ObjectsAfter++
		massAfter = append(massAfter, in.MassKg)
		riskAfter = append(riskAfter, in.RiskScore)
		probAfter = append(probAfter, in.Probability)
	}
	sort.Strings(out.RemovedIDs)
	out.MassBeforeKg = numeric.Round(numeric.SumFloats(massBefore), 3)
	out.MassAfterKg = numeric.Round(numeric.SumFloats(massAfter), 3)
	out.RiskBefore = numeric.Round(numeric.SumFloats(riskBefore), 6)
	out.RiskAfter = numeric.Round(numeric.SumFloats(riskAfter), 6)
	out.RiskReduction = numeric.Round(out.RiskBefore-out.RiskAfter, 6)
	out.RiskReductionPct = numeric.Round(numeric.Ratio(out.RiskReduction, out.RiskBefore)*100, 3)
	out.ProbabilityBefore = numeric.RoundSignificant(numeric.SumFloats(probBefore), 6)
	out.ProbabilityAfter = numeric.RoundSignificant(numeric.SumFloats(probAfter), 6)
	out.ProbabilityReduction = numeric.RoundSignificant(out.ProbabilityBefore-out.ProbabilityAfter, 6)
	return out
}

// Assumptions returns the compliance-model caveats included in reports.
func Assumptions() []string {
	return []string{
		"orbital lifetime uses a static piecewise exponential atmosphere and a flat drag coefficient",
		"lifetime is approximated by the time to descend one density scale height",
		"solar-cycle variation, attitude changes and manoeuvres during decay are ignored",
		"disposal cost is a single-impulse perigee lowering or a two-impulse altitude raise",
		"compliance rules are the tool's own simplified restatement of common disposal practice",
	}
}
