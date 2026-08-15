// Package mission plans active debris-removal missions: it assigns ranked
// targets to chaser vehicles, costs the transfers, chooses a disposal action per
// target and emits an ordered timeline.
//
// Modelling decisions that the reader must know about:
//
//   - The chaser is massless from the propulsion point of view: delta-v is
//     accounted against a fixed budget, no rocket equation, no propellant mass.
//   - Disposal is performed by a detachable kit. The kit's impulse is charged to
//     the mission budget, but the chaser's own orbit is unchanged by it, so a
//     chaser can service several targets in one mission.
//   - After a capture the chaser is left in the target's orbit, which becomes
//     the departure orbit of the next leg.
//   - Rendezvous ordering is a deterministic nearest-neighbour walk in transfer
//     cost, not a global optimum.
package mission

import (
	"fmt"
	"sort"

	"DebrisLedger/internal/compliance"
	"DebrisLedger/internal/config"
	"DebrisLedger/internal/model"
	"DebrisLedger/internal/numeric"
	"DebrisLedger/internal/orbit"
	"DebrisLedger/internal/rank"
)

// Leg statuses.
const (
	LegPlanned        = "planned"
	LegOverBudget     = "budget-exceeded"
	LegSlotsExhausted = "slots-exhausted"
)

// Event kinds, in the order they can appear inside one leg.
const (
	EventMissionStart = "mission-start"
	EventTransferBurn = "transfer-burn"
	EventDriftCoast   = "raan-drift-coast"
	EventRendezvous   = "rendezvous"
	EventCapture      = "capture"
	EventDisposalBurn = "disposal-burn"
	EventRelease      = "release"
	EventMissionEnd   = "mission-end"
)

// Leg is one target visit.
type Leg struct {
	Sequence            int                 `json:"sequence"`
	TargetID            string              `json:"target_id"`
	TargetName          string              `json:"target_name"`
	TargetRank          int                 `json:"target_rank"`
	RiskScore           float64             `json:"risk_score"`
	FromAltitudeKm      float64             `json:"from_altitude_km"`
	ToAltitudeKm        float64             `json:"to_altitude_km"`
	PlaneAngleDeg       float64             `json:"plane_angle_deg"`
	AltitudeDeltaVMps   float64             `json:"altitude_delta_v_mps"`
	PlaneDeltaVMps      float64             `json:"plane_delta_v_mps"`
	PhasingDeltaVMps    float64             `json:"phasing_delta_v_mps"`
	CaptureDeltaVMps    float64             `json:"capture_delta_v_mps"`
	DisposalDeltaVMps   float64             `json:"disposal_delta_v_mps"`
	LegDeltaVMps        float64             `json:"leg_delta_v_mps"`
	CumulativeDeltaVMps float64             `json:"cumulative_delta_v_mps"`
	RAANWaitSeconds     int64               `json:"raan_wait_s"`
	CoastSeconds        int64               `json:"coast_s"`
	DockingSeconds      int64               `json:"docking_s"`
	StartSeconds        int64               `json:"start_s"`
	EndSeconds          int64               `json:"end_s"`
	Disposal            compliance.Disposal `json:"disposal"`
	Status              string              `json:"status"`
	Reason              string              `json:"reason"`
}

// Event is one timeline entry.
type Event struct {
	Sequence    int     `json:"sequence"`
	TimeSeconds int64   `json:"time_s"`
	Kind        string  `json:"kind"`
	TargetID    string  `json:"target_id"`
	DeltaVMps   float64 `json:"delta_v_mps"`
	Detail      string  `json:"detail"`
}

// SkippedTarget explains why a ranked target was not serviced.
type SkippedTarget struct {
	TargetID  string  `json:"target_id"`
	RiskScore float64 `json:"risk_score"`
	Reason    string  `json:"reason"`
}

// Stats summarises one mission.
type Stats struct {
	LegsPlanned     int     `json:"legs_planned"`
	LegsRejected    int     `json:"legs_rejected"`
	MassRemovedKg   float64 `json:"mass_removed_kg"`
	RiskRemoved     float64 `json:"risk_removed"`
	DurationSeconds int64   `json:"duration_s"`
	ReentryCount    int     `json:"reentry_count"`
	GraveyardCount  int     `json:"graveyard_count"`
	NoDisposalCount int     `json:"no_disposal_count"`
}

// Mission is the plan for a single chaser.
type Mission struct {
	ChaserID         string              `json:"chaser_id"`
	ChaserName       string              `json:"chaser_name"`
	EpochSeconds     int64               `json:"epoch_s"`
	CaptureSlots     int                 `json:"capture_slots"`
	DeltaVBudgetMps  float64             `json:"delta_v_budget_mps"`
	ReserveMps       float64             `json:"reserve_mps"`
	UsableMps        float64             `json:"usable_mps"`
	PlannedDeltaVMps float64             `json:"planned_delta_v_mps"`
	RemainingMps     float64             `json:"remaining_mps"`
	BudgetOK         bool                `json:"budget_ok"`
	Legs             []Leg               `json:"legs"`
	Timeline         []Event             `json:"timeline"`
	Findings         compliance.Findings `json:"findings"`
	Stats            Stats               `json:"stats"`
}

// Plan is the whole removal campaign: one mission per declared chaser plus the
// campaign-level accounting.
type Plan struct {
	ScenarioLabel string                  `json:"scenario_label"`
	ConfigLabel   string                  `json:"config_label"`
	EpochSeconds  int64                   `json:"epoch_s"`
	Missions      []Mission               `json:"missions"`
	Skipped       []SkippedTarget         `json:"skipped_targets"`
	ResidualRisk  compliance.ResidualRisk `json:"residual_risk"`
	Findings      compliance.Findings     `json:"findings"`
	Assumptions   []string                `json:"assumptions"`
}

// RemovedIDs returns every target that a mission captures, sorted.
func (p Plan) RemovedIDs() []string {
	out := []string{}
	for _, m := range p.Missions {
		for _, leg := range m.Legs {
			if leg.Status == LegPlanned {
				out = append(out, leg.TargetID)
			}
		}
	}
	sort.Strings(out)
	return out
}

// Build plans one mission per chaser from the ranked target list.
func Build(sc *model.Scenario, cfg config.Config, ranking rank.Ranking) Plan {
	plan := Plan{
		ScenarioLabel: sc.Label,
		ConfigLabel:   cfg.Label,
		EpochSeconds:  sc.EpochSeconds,
		Assumptions:   missionAssumptions(),
		Skipped:       []SkippedTarget{},
		Findings:      compliance.Findings{},
	}
	chasers := make([]model.Chaser, len(sc.Chasers))
	copy(chasers, sc.Chasers)
	sort.SliceStable(chasers, func(i, j int) bool { return chasers[i].ID < chasers[j].ID })

	pending := ranking.FeasibleTargets()
	assigned := map[string]bool{}
	for _, ch := range chasers {
		m := planMission(sc, cfg, ch, pending, assigned)
		plan.Missions = append(plan.Missions, m)
		plan.Findings = append(plan.Findings, m.Findings...)
	}
	for _, t := range pending {
		if assigned[t.ObjectID] {
			continue
		}
		plan.Skipped = append(plan.Skipped, SkippedTarget{
			TargetID:  t.ObjectID,
			RiskScore: t.RiskScore,
			Reason:    "no chaser had budget, slots or compatible limits left for this target",
		})
	}
	for _, t := range ranking.Targets {
		if t.Feasibility.Capturable {
			continue
		}
		reason := "capture infeasible"
		if len(t.Feasibility.Blockers) > 0 {
			reason = "capture infeasible: " + t.Feasibility.Blockers[0]
		}
		plan.Skipped = append(plan.Skipped, SkippedTarget{
			TargetID:  t.ObjectID,
			RiskScore: t.RiskScore,
			Reason:    reason,
		})
	}
	sort.SliceStable(plan.Skipped, func(i, j int) bool {
		if plan.Skipped[i].RiskScore != plan.Skipped[j].RiskScore {
			return plan.Skipped[i].RiskScore > plan.Skipped[j].RiskScore
		}
		return plan.Skipped[i].TargetID < plan.Skipped[j].TargetID
	})
	plan.ResidualRisk = compliance.ComputeResidualRisk(ranking.RiskInputs(), plan.RemovedIDs())
	plan.Findings.Sort()
	return plan
}

// planMission runs the nearest-neighbour walk for one chaser.
func planMission(sc *model.Scenario, cfg config.Config, ch model.Chaser,
	pending []rank.Target, assigned map[string]bool) Mission {
	prop := orbit.NewPropagator(cfg.Propagation.IncludeJ2NodalDrift)
	reserve := ch.DeltaVBudgetMps * cfg.Mission.ReserveFraction
	m := Mission{
		ChaserID:        ch.ID,
		ChaserName:      ch.Name,
		EpochSeconds:    sc.EpochSeconds,
		CaptureSlots:    ch.CaptureSlots,
		DeltaVBudgetMps: numeric.Round(ch.DeltaVBudgetMps, cfg.Output.DeltaVDecimals),
		ReserveMps:      numeric.Round(reserve, cfg.Output.DeltaVDecimals),
		UsableMps:       numeric.Round(ch.DeltaVBudgetMps-reserve, cfg.Output.DeltaVDecimals),
		Findings:        compliance.Findings{},
		Legs:            []Leg{},
		Timeline:        []Event{},
	}
	usable := ch.DeltaVBudgetMps - reserve
	current := ch.Orbit
	current.EpochSeconds = sc.EpochSeconds
	clock := sc.EpochSeconds
	used := 0.0
	seq := 0

	m.Timeline = append(m.Timeline, Event{
		Sequence:    len(m.Timeline) + 1,
		TimeSeconds: clock,
		Kind:        EventMissionStart,
		Detail: fmt.Sprintf("chaser %s departs a %s km / %s deg orbit with %s m/s usable",
			ch.ID, numeric.Fixed(ch.Orbit.AltitudeKm, 3), numeric.Fixed(ch.Orbit.InclinationDeg, 3),
			numeric.Fixed(usable, cfg.Output.DeltaVDecimals)),
	})

	for seq < ch.CaptureSlots {
		candidate, transfer, obj, ok := selectNext(prop, sc, cfg, ch, current, clock, pending, assigned)
		if !ok {
			break
		}
		disposal := compliance.PlanDisposal(cfg.Compliance, obj)
		legDV := transfer.TotalDeltaV + cfg.Mission.CaptureMarginMps + disposal.DeltaVMps
		if used+legDV > usable+1e-9 {
			m.Legs = append(m.Legs, Leg{
				Sequence:            seq + 1,
				TargetID:            candidate.ObjectID,
				TargetName:          candidate.ObjectName,
				TargetRank:          candidate.Rank,
				RiskScore:           candidate.RiskScore,
				FromAltitudeKm:      numeric.Round(current.AltitudeKm, 4),
				ToAltitudeKm:        numeric.Round(obj.Orbit.AltitudeKm, 4),
				LegDeltaVMps:        numeric.Round(legDV, cfg.Output.DeltaVDecimals),
				CumulativeDeltaVMps: numeric.Round(used, cfg.Output.DeltaVDecimals),
				Status:              LegOverBudget,
				Reason: fmt.Sprintf("leg needs %s m/s but only %s m/s remains",
					numeric.Fixed(legDV, cfg.Output.DeltaVDecimals),
					numeric.Fixed(usable-used, cfg.Output.DeltaVDecimals)),
			})
			m.Stats.LegsRejected++
			break
		}

		seq++
		assigned[candidate.ObjectID] = true
		leg := Leg{
			Sequence:          seq,
			TargetID:          candidate.ObjectID,
			TargetName:        candidate.ObjectName,
			TargetRank:        candidate.Rank,
			RiskScore:         candidate.RiskScore,
			FromAltitudeKm:    numeric.Round(current.AltitudeKm, 4),
			ToAltitudeKm:      numeric.Round(obj.Orbit.AltitudeKm, 4),
			PlaneAngleDeg:     numeric.Round(transfer.PlaneAngleDeg, 6),
			AltitudeDeltaVMps: numeric.Round(transfer.AltitudeDeltaV, cfg.Output.DeltaVDecimals),
			PlaneDeltaVMps:    numeric.Round(transfer.PlaneDeltaV, cfg.Output.DeltaVDecimals),
			PhasingDeltaVMps:  numeric.Round(transfer.PhasingDeltaV, cfg.Output.DeltaVDecimals),
			CaptureDeltaVMps:  numeric.Round(cfg.Mission.CaptureMarginMps, cfg.Output.DeltaVDecimals),
			DisposalDeltaVMps: numeric.Round(disposal.DeltaVMps, cfg.Output.DeltaVDecimals),
			LegDeltaVMps:      numeric.Round(legDV, cfg.Output.DeltaVDecimals),
			RAANWaitSeconds:   transfer.RAANWaitSeconds,
			CoastSeconds:      transfer.CoastSeconds,
			DockingSeconds:    ch.DockingTimeS,
			StartSeconds:      clock,
			Disposal:          disposal,
			Status:            LegPlanned,
			Reason:            "nearest-neighbour transfer inside the remaining budget",
		}

		burnTime := clock
		m.Timeline = append(m.Timeline, Event{
			Sequence:    len(m.Timeline) + 1,
			TimeSeconds: burnTime,
			Kind:        EventTransferBurn,
			TargetID:    candidate.ObjectID,
			DeltaVMps:   numeric.Round(transfer.AltitudeDeltaV+transfer.PlaneDeltaV+transfer.PhasingDeltaV, cfg.Output.DeltaVDecimals),
			Detail: fmt.Sprintf("raise/lower from %s km to %s km and rotate %s deg of plane",
				numeric.Fixed(current.AltitudeKm, 3), numeric.Fixed(obj.Orbit.AltitudeKm, 3),
				numeric.Fixed(transfer.PlaneAngleDeg, 4)),
		})
		if transfer.RAANWaitSeconds > 0 {
			clock += transfer.RAANWaitSeconds
			m.Timeline = append(m.Timeline, Event{
				Sequence:    len(m.Timeline) + 1,
				TimeSeconds: clock,
				Kind:        EventDriftCoast,
				TargetID:    candidate.ObjectID,
				Detail: fmt.Sprintf("coast %s so differential nodal drift closes %s deg of RAAN",
					numeric.DurationText(transfer.RAANWaitSeconds), numeric.Fixed(transfer.RAANDriftUsedDeg, 4)),
			})
		}
		clock += transfer.CoastSeconds + cfg.Mission.TransferSettleSeconds
		m.Timeline = append(m.Timeline, Event{
			Sequence:    len(m.Timeline) + 1,
			TimeSeconds: clock,
			Kind:        EventRendezvous,
			TargetID:    candidate.ObjectID,
			Detail: fmt.Sprintf("station-keeping alongside %s at %s km",
				candidate.ObjectID, numeric.Fixed(obj.Orbit.AltitudeKm, 3)),
		})
		clock += ch.DockingTimeS
		m.Timeline = append(m.Timeline, Event{
			Sequence:    len(m.Timeline) + 1,
			TimeSeconds: clock,
			Kind:        EventCapture,
			TargetID:    candidate.ObjectID,
			DeltaVMps:   numeric.Round(cfg.Mission.CaptureMarginMps, cfg.Output.DeltaVDecimals),
			Detail: fmt.Sprintf("capture on attach point %q after %s of proximity operations",
				obj.AttachPoint, numeric.DurationText(ch.DockingTimeS)),
		})
		if disposal.Action != compliance.ActionNone {
			m.Timeline = append(m.Timeline, Event{
				Sequence:    len(m.Timeline) + 1,
				TimeSeconds: clock,
				Kind:        EventDisposalBurn,
				TargetID:    candidate.ObjectID,
				DeltaVMps:   numeric.Round(disposal.DeltaVMps, cfg.Output.DeltaVDecimals),
				Detail: fmt.Sprintf("%s to %s km", disposal.Action,
					numeric.Fixed(disposal.TargetAltitudeKm, 3)),
			})
		}
		m.Timeline = append(m.Timeline, Event{
			Sequence:    len(m.Timeline) + 1,
			TimeSeconds: clock,
			Kind:        EventRelease,
			TargetID:    candidate.ObjectID,
			Detail:      fmt.Sprintf("disposal kit released, chaser stays at %s km", numeric.Fixed(obj.Orbit.AltitudeKm, 3)),
		})

		used += legDV
		leg.EndSeconds = clock
		leg.CumulativeDeltaVMps = numeric.Round(used, cfg.Output.DeltaVDecimals)
		m.Legs = append(m.Legs, leg)
		m.Findings = append(m.Findings, disposal.Findings...)
		m.Stats.LegsPlanned++
		m.Stats.MassRemovedKg += obj.MassKg
		m.Stats.RiskRemoved += candidate.RiskScore
		switch disposal.Action {
		case compliance.ActionReentry:
			m.Stats.ReentryCount++
		case compliance.ActionGraveyard:
			m.Stats.GraveyardCount++
		default:
			m.Stats.NoDisposalCount++
		}

		current = obj.Orbit
		current.EpochSeconds = clock
		current.ArgLatDeg = orbit.WrapAngleDeg(orbit.Deg(prop.ArgLatRadAt(obj.Orbit, float64(clock))))
		current.RAANDeg = orbit.WrapAngleDeg(orbit.Deg(prop.RAANRadAt(obj.Orbit, float64(clock))))
	}

	if seq >= ch.CaptureSlots && ch.CaptureSlots > 0 {
		m.Findings = append(m.Findings, compliance.Finding{
			RuleID:  "MSN-01-capture-slots",
			Subject: ch.ID,
			Status:  compliance.StatusPass,
			Detail:  fmt.Sprintf("all %d capture slots used", ch.CaptureSlots),
		})
	}
	m.Timeline = append(m.Timeline, Event{
		Sequence:    len(m.Timeline) + 1,
		TimeSeconds: clock,
		Kind:        EventMissionEnd,
		Detail: fmt.Sprintf("%d target(s) removed using %s m/s of %s m/s usable",
			m.Stats.LegsPlanned, numeric.Fixed(used, cfg.Output.DeltaVDecimals),
			numeric.Fixed(usable, cfg.Output.DeltaVDecimals)),
	})

	m.PlannedDeltaVMps = numeric.Round(used, cfg.Output.DeltaVDecimals)
	m.RemainingMps = numeric.Round(usable-used, cfg.Output.DeltaVDecimals)
	m.BudgetOK = used <= usable+1e-9
	m.Stats.MassRemovedKg = numeric.Round(m.Stats.MassRemovedKg, 3)
	m.Stats.RiskRemoved = numeric.Round(m.Stats.RiskRemoved, 6)
	m.Stats.DurationSeconds = clock - sc.EpochSeconds
	m.Findings = append(m.Findings, budgetFinding(ch, m, cfg))
	m.Findings.Sort()
	return m
}

func budgetFinding(ch model.Chaser, m Mission, cfg config.Config) compliance.Finding {
	status := compliance.StatusPass
	detail := fmt.Sprintf("planned %s m/s stays inside the %s m/s usable budget (%s m/s reserve held)",
		numeric.Fixed(m.PlannedDeltaVMps, cfg.Output.DeltaVDecimals),
		numeric.Fixed(m.UsableMps, cfg.Output.DeltaVDecimals),
		numeric.Fixed(m.ReserveMps, cfg.Output.DeltaVDecimals))
	if !m.BudgetOK {
		status = compliance.StatusFail
		detail = fmt.Sprintf("planned %s m/s exceeds the %s m/s usable budget",
			numeric.Fixed(m.PlannedDeltaVMps, cfg.Output.DeltaVDecimals),
			numeric.Fixed(m.UsableMps, cfg.Output.DeltaVDecimals))
	}
	return compliance.Finding{
		RuleID:  "MSN-02-delta-v-budget",
		Subject: ch.ID,
		Status:  status,
		Detail:  detail,
	}
}

// selectNext picks the cheapest reachable unassigned target for the chaser.
//
// Ties are broken by risk score (higher first) and then by identifier, so the
// walk never depends on iteration order.
func selectNext(prop orbit.Propagator, sc *model.Scenario, cfg config.Config, ch model.Chaser,
	current model.Orbit, clock int64, pending []rank.Target,
	assigned map[string]bool) (rank.Target, orbit.Transfer, model.CatalogObject, bool) {
	tcfg := orbit.TransferConfig{
		PlaneChangeEfficiency: cfg.Mission.PlaneChangeEfficiency,
		PhasingMarginMps:      cfg.Mission.PhasingMarginMps,
		MaxRAANWaitSeconds:    cfg.Mission.MaxRAANWaitSeconds,
	}
	var (
		bestTarget   rank.Target
		bestTransfer orbit.Transfer
		bestObject   model.CatalogObject
		found        bool
	)
	for _, t := range pending {
		if assigned[t.ObjectID] {
			continue
		}
		obj, ok := sc.ObjectByID(t.ObjectID)
		if !ok {
			continue
		}
		if obj.MassKg > ch.MaxCaptureMassKg || obj.TumbleRateDegPerS > ch.MaxTumbleRateDegPerS {
			continue
		}
		transfer := orbit.PlanTransfer(prop, current, obj.Orbit, float64(clock), tcfg)
		if !found || better(transfer, bestTransfer, t, bestTarget) {
			bestTarget, bestTransfer, bestObject, found = t, transfer, obj, true
		}
	}
	return bestTarget, bestTransfer, bestObject, found
}

func better(candidate, incumbent orbit.Transfer, candidateTarget, incumbentTarget rank.Target) bool {
	if candidate.TotalDeltaV != incumbent.TotalDeltaV {
		return candidate.TotalDeltaV < incumbent.TotalDeltaV
	}
	if candidateTarget.RiskScore != incumbentTarget.RiskScore {
		return candidateTarget.RiskScore > incumbentTarget.RiskScore
	}
	return candidateTarget.ObjectID < incumbentTarget.ObjectID
}

func missionAssumptions() []string {
	return []string{
		"altitude changes are two-impulse Hohmann transfers between circular orbits",
		"plane changes are single impulses at the higher radius, added scalar-wise to the altitude cost",
		"a coast is preferred over a RAAN change whenever differential nodal drift can close the offset in time",
		"phasing and capture are charged as flat configured margins",
		"disposal is performed by a detachable kit, so the chaser orbit is unchanged by the disposal impulse",
		"target ordering is a deterministic nearest-neighbour walk, not a global optimum",
		"no propellant mass, no rocket equation, no thruster duty cycle",
	}
}
