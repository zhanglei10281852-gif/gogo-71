package report

import (
	"fmt"
	"strconv"

	"DebrisLedger/internal/compliance"
	"DebrisLedger/internal/config"
	"DebrisLedger/internal/maneuver"
	"DebrisLedger/internal/mission"
	"DebrisLedger/internal/model"
	"DebrisLedger/internal/numeric"
	"DebrisLedger/internal/rank"
	"DebrisLedger/internal/screen"
	"DebrisLedger/internal/store"
)

// Banner is printed at the top of every text artefact so that a stored report
// always carries its own disclaimer.
const Banner = "DebrisLedger - simplified circular-orbit study tool (not for operational use)"

// ValidationText renders the outcome of a successful validation.
func ValidationText(sc *model.Scenario, cfg config.Config) string {
	var s Section
	s.Title(Banner)
	s.Heading("validation")
	s.Field("scenario", sc.Label)
	s.Field("configuration", cfg.Label)
	s.Field("epoch (s)", strconv.FormatInt(sc.EpochSeconds, 10))
	s.Field("horizon", numeric.DurationText(sc.HorizonSeconds))
	s.Field("catalogue objects", strconv.Itoa(len(sc.Objects)))
	s.Field("operator assets", strconv.Itoa(len(sc.Assets)))
	s.Field("chaser vehicles", strconv.Itoa(len(sc.Chasers)))
	s.Field("screening volumes", strconv.Itoa(len(sc.ScreeningVolumes)))
	s.Field("configuration digest", cfg.Summary())

	s.Heading("catalogue")
	t := NewTable([]string{"id", "class", "alt km", "inc deg", "raan deg", "mass kg", "area m2", "tumble deg/s", "attach"}, 2, 3, 4, 5, 6, 7)
	for _, obj := range sc.Objects {
		t.Add(obj.ID, string(obj.Class),
			numeric.Fixed(obj.Orbit.AltitudeKm, 3),
			numeric.Fixed(obj.Orbit.InclinationDeg, 3),
			numeric.Fixed(obj.Orbit.RAANDeg, 3),
			numeric.Fixed(obj.MassKg, 1),
			numeric.Fixed(obj.AreaM2, 3),
			numeric.Fixed(obj.TumbleRateDegPerS, 3),
			obj.AttachPoint)
	}
	s.Table(t)

	s.Heading("assets")
	at := NewTable([]string{"id", "alt km", "inc deg", "dv budget m/s", "thruster", "lead s", "criticality"}, 1, 2, 3, 5, 6)
	for _, a := range sc.Assets {
		at.Add(a.ID,
			numeric.Fixed(a.Orbit.AltitudeKm, 3),
			numeric.Fixed(a.Orbit.InclinationDeg, 3),
			numeric.Fixed(a.Maneuver.DeltaVBudgetMps, 3),
			string(a.Maneuver.ThrusterType),
			strconv.FormatInt(a.Maneuver.EffectiveLeadSeconds(), 10),
			strconv.Itoa(a.Criticality))
	}
	s.Table(at)

	s.Heading("chasers")
	ct := NewTable([]string{"id", "alt km", "inc deg", "dv budget m/s", "slots", "dock s", "max mass kg", "max tumble deg/s"}, 1, 2, 3, 4, 5, 6, 7)
	for _, c := range sc.Chasers {
		ct.Add(c.ID,
			numeric.Fixed(c.Orbit.AltitudeKm, 3),
			numeric.Fixed(c.Orbit.InclinationDeg, 3),
			numeric.Fixed(c.DeltaVBudgetMps, 3),
			strconv.Itoa(c.CaptureSlots),
			strconv.FormatInt(c.DockingTimeS, 10),
			numeric.Fixed(c.MaxCaptureMassKg, 1),
			numeric.Fixed(c.MaxTumbleRateDegPerS, 3))
	}
	s.Table(ct)

	s.Heading("screening volumes")
	vt := NewTable([]string{"name", "radial km", "in-track km", "cross-track km", "applies to"}, 1, 2, 3)
	for _, v := range sc.ScreeningVolumes {
		applies := v.AppliesToClass
		if applies == "" {
			applies = "(all classes)"
		}
		vt.Add(v.Name,
			numeric.Fixed(v.RadialKm, 3),
			numeric.Fixed(v.InTrackKm, 3),
			numeric.Fixed(v.CrossTrackKm, 3),
			applies)
	}
	s.Table(vt)
	return s.String()
}

// ScreeningText renders a screening result.
func ScreeningText(res screen.Result, cfg config.Config) string {
	var s Section
	s.Title(Banner)
	s.Heading("conjunction screening")
	s.Field("scenario", res.ScenarioLabel)
	s.Field("configuration", res.ConfigLabel)
	s.Field("epoch (s)", strconv.FormatInt(res.EpochSeconds, 10))
	s.Field("horizon", numeric.DurationText(res.HorizonSeconds))
	s.Field("pairs screened", strconv.Itoa(res.Stats.PairsScreened))
	s.Field("pairs skipped", strconv.Itoa(res.Stats.PairsSkipped))
	s.Field("search failures", strconv.Itoa(res.Stats.SearchFailures))
	s.Field("conjunctions", strconv.Itoa(res.Stats.ConjunctionCount))
	s.Field("actionable", strconv.Itoa(res.Stats.ActionableCount))
	s.Field("max probability", numeric.Sci(res.Stats.MaxProbability, cfg.Output.ProbabilityExponentDigits))
	s.Field("min miss distance km", numeric.Fixed(res.Stats.MinMissKm, cfg.Output.DistanceDecimals))

	s.Heading("severity histogram")
	ht := NewTable([]string{"severity", "count"}, 1)
	for _, row := range res.Stats.BySeverity {
		ht.Add(row.Severity, strconv.Itoa(row.Count))
	}
	s.Table(ht)

	s.Heading("conjunctions")
	dd := cfg.Output.DistanceDecimals
	t := NewTable([]string{"asset", "object", "class", "tca s", "lead", "miss km", "radial km", "in-track km", "cross km", "rel km/s", "sigma km", "pc", "severity", "act"},
		3, 5, 6, 7, 8, 9, 10, 11)
	for _, c := range res.Conjunctions {
		t.Add(c.AssetID, c.ObjectID, string(c.ObjectClass),
			strconv.FormatInt(c.TCASeconds, 10),
			numeric.DurationText(c.LeadSeconds),
			numeric.Fixed(c.MissKm, dd),
			numeric.Fixed(c.RadialKm, dd),
			numeric.Fixed(c.InTrackKm, dd),
			numeric.Fixed(c.CrossTrackKm, dd),
			numeric.Fixed(c.RelSpeedKmS, 4),
			numeric.Fixed(c.SigmaKm, 4),
			numeric.Sci(c.Probability, cfg.Output.ProbabilityExponentDigits),
			c.Severity,
			yesNo(c.Actionable))
	}
	s.Table(t)

	if len(res.Stats.TruncatedAssets) > 0 {
		s.Heading("truncated assets")
		s.Bullets(res.Stats.TruncatedAssets, true)
	}
	if cfg.Output.IncludeAssumptions {
		s.Heading("model assumptions")
		s.Bullets(res.Assumptions, false)
	}
	return s.String()
}

// ManeuverText renders an avoidance schedule.
func ManeuverText(plan maneuver.Plan, cfg config.Config) string {
	var s Section
	s.Title(Banner)
	s.Heading("collision-avoidance manoeuvre schedule")
	s.Field("scenario", plan.ScenarioLabel)
	s.Field("configuration", plan.ConfigLabel)
	s.Field("required miss km", numeric.Fixed(plan.RequiredMissKm, cfg.Output.DistanceDecimals))
	s.Field("candidates", strconv.Itoa(plan.Stats.Candidates))
	s.Field("scheduled", strconv.Itoa(plan.Stats.Scheduled))
	s.Field("rejected", strconv.Itoa(plan.Stats.Rejected))
	s.Field("conflicts", strconv.Itoa(plan.Stats.Conflicts))
	s.Field("total delta-v m/s", numeric.Fixed(plan.Stats.TotalDeltaVMps, cfg.Output.DeltaVDecimals))
	s.Field("probability before", numeric.Sci(plan.Stats.ProbabilityBefore, cfg.Output.ProbabilityExponentDigits))
	s.Field("probability after", numeric.Sci(plan.Stats.ProbabilityAfter, cfg.Output.ProbabilityExponentDigits))
	s.Field("probability reduction", numeric.Sci(plan.Stats.ProbabilityReduction, cfg.Output.ProbabilityExponentDigits))

	s.Heading("per-asset budget")
	at := NewTable([]string{"asset", "budget m/s", "planned m/s", "remaining m/s", "used %", "burns", "rejected", "conflicts", "lead s"},
		1, 2, 3, 4, 5, 6, 7, 8)
	for _, a := range plan.Assets {
		at.Add(a.AssetID,
			numeric.Fixed(a.BudgetMps, cfg.Output.DeltaVDecimals),
			numeric.Fixed(a.PlannedMps, cfg.Output.DeltaVDecimals),
			numeric.Fixed(a.RemainingMps, cfg.Output.DeltaVDecimals),
			numeric.Fixed(a.UtilizationPct, 2),
			strconv.Itoa(a.Scheduled),
			strconv.Itoa(a.Rejected),
			strconv.Itoa(a.Conflicts),
			strconv.FormatInt(a.EffectiveLeadS, 10))
	}
	s.Table(at)

	s.Heading("burns")
	dv := cfg.Output.DeltaVDecimals
	dd := cfg.Output.DistanceDecimals
	t := NewTable([]string{"burn s", "asset", "object", "dir", "dv m/s", "shift km", "miss km", "after km", "pc before", "pc after", "status"},
		0, 4, 5, 6, 7, 8, 9)
	for _, b := range plan.Burns {
		t.Add(strconv.FormatInt(b.BurnSeconds, 10), b.AssetID, b.ObjectID, b.Direction,
			numeric.Fixed(b.DeltaVMps, dv),
			numeric.Fixed(b.RequiredShiftKm, dd),
			numeric.Fixed(b.OriginalMissKm, dd),
			numeric.Fixed(b.PredictedMissKm, dd),
			numeric.Sci(b.OriginalProbability, cfg.Output.ProbabilityExponentDigits),
			numeric.Sci(b.PredictedProbability, cfg.Output.ProbabilityExponentDigits),
			b.Status)
	}
	s.Table(t)

	rejected := false
	for _, b := range plan.Burns {
		if b.Status != maneuver.StatusScheduled {
			rejected = true
			break
		}
	}
	if rejected {
		s.Heading("rejected burns")
		for _, b := range plan.Burns {
			if b.Status == maneuver.StatusScheduled {
				continue
			}
			detail := fmt.Sprintf("%s vs %s at t=%d: %s (%s)", b.AssetID, b.ObjectID, b.TCASeconds, b.Status, b.Reason)
			if len(b.ConflictsWith) > 0 {
				detail += " conflicts=" + fmt.Sprint(b.ConflictsWith)
			}
			s.Line("  - " + detail)
		}
	}
	if cfg.Output.IncludeAssumptions {
		s.Heading("model assumptions")
		s.Bullets(plan.Assumptions, false)
	}
	return s.String()
}

// RankingText renders the ranked removal-target list.
func RankingText(r rank.Ranking, cfg config.Config) string {
	var s Section
	s.Title(Banner)
	s.Heading("debris-removal target ranking")
	s.Field("scenario", r.ScenarioLabel)
	s.Field("configuration", r.ConfigLabel)
	s.Field("evaluated", strconv.Itoa(r.Stats.Evaluated))
	s.Field("excluded (payloads)", strconv.Itoa(r.Stats.Excluded))
	s.Field("listed", strconv.Itoa(r.Stats.Listed))
	s.Field("feasible", strconv.Itoa(r.Stats.Feasible))
	s.Field("infeasible", strconv.Itoa(r.Stats.Infeasible))
	s.Field("top score", numeric.Fixed(r.Stats.TopScore, 6))
	s.Field("total mass kg", numeric.Fixed(r.Stats.TotalMassKg, 1))
	s.Field("feasible mass kg", numeric.Fixed(r.Stats.FeasibleMassKg, 1))
	s.Field("median congestion", strconv.Itoa(r.Stats.CongestionMedian))

	s.Heading("ranked targets")
	t := NewTable([]string{"rank", "object", "class", "alt km", "inc deg", "mass kg", "cong", "pc sum", "mass s", "pc s", "cong s", "risk", "life yr", "capture"},
		0, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12)
	for _, target := range r.Targets {
		t.Add(strconv.Itoa(target.Rank), target.ObjectID, string(target.Class),
			numeric.Fixed(target.AltitudeKm, 2),
			numeric.Fixed(target.InclinationDeg, 3),
			numeric.Fixed(target.MassKg, 1),
			strconv.Itoa(target.Congestion),
			numeric.Sci(target.Probability, cfg.Output.ProbabilityExponentDigits),
			numeric.Fixed(target.MassScore, 4),
			numeric.Fixed(target.ProbabilityScore, 4),
			numeric.Fixed(target.CongestionScore, 4),
			numeric.Fixed(target.RiskScore, 6),
			numeric.Fixed(target.LifetimeYears, 2),
			captureText(target.Feasibility))
	}
	s.Table(t)

	blockers := []string{}
	for _, target := range r.Targets {
		for _, b := range target.Feasibility.Blockers {
			blockers = append(blockers, target.ObjectID+": "+b)
		}
	}
	if len(blockers) > 0 {
		s.Heading("capture blockers")
		s.Bullets(blockers, true)
	}
	if cfg.Output.IncludeAssumptions {
		s.Heading("model assumptions")
		s.Bullets(r.Assumptions, false)
	}
	return s.String()
}

// MissionText renders the removal campaign.
func MissionText(plan mission.Plan, cfg config.Config) string {
	var s Section
	s.Title(Banner)
	s.Heading("active-removal mission plan")
	s.Field("scenario", plan.ScenarioLabel)
	s.Field("configuration", plan.ConfigLabel)
	s.Field("missions", strconv.Itoa(len(plan.Missions)))
	s.Field("targets removed", strconv.Itoa(len(plan.RemovedIDs())))
	s.Field("targets skipped", strconv.Itoa(len(plan.Skipped)))

	dv := cfg.Output.DeltaVDecimals
	for _, m := range plan.Missions {
		s.Heading("mission " + m.ChaserID)
		s.Field("chaser", m.ChaserName)
		s.Field("capture slots", strconv.Itoa(m.CaptureSlots))
		s.Field("budget m/s", numeric.Fixed(m.DeltaVBudgetMps, dv))
		s.Field("reserve m/s", numeric.Fixed(m.ReserveMps, dv))
		s.Field("usable m/s", numeric.Fixed(m.UsableMps, dv))
		s.Field("planned m/s", numeric.Fixed(m.PlannedDeltaVMps, dv))
		s.Field("remaining m/s", numeric.Fixed(m.RemainingMps, dv))
		s.Field("budget ok", yesNo(m.BudgetOK))
		s.Field("legs planned", strconv.Itoa(m.Stats.LegsPlanned))
		s.Field("legs rejected", strconv.Itoa(m.Stats.LegsRejected))
		s.Field("mass removed kg", numeric.Fixed(m.Stats.MassRemovedKg, 1))
		s.Field("duration", numeric.DurationText(m.Stats.DurationSeconds))
		s.Field("reentry / graveyard", strconv.Itoa(m.Stats.ReentryCount)+" / "+strconv.Itoa(m.Stats.GraveyardCount))

		lt := NewTable([]string{"leg", "target", "rank", "from km", "to km", "plane deg", "alt dv", "plane dv", "phase dv", "capt dv", "disp dv", "leg dv", "cum dv", "wait", "status"},
			0, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12)
		for _, leg := range m.Legs {
			lt.Add(strconv.Itoa(leg.Sequence), leg.TargetID, strconv.Itoa(leg.TargetRank),
				numeric.Fixed(leg.FromAltitudeKm, 2),
				numeric.Fixed(leg.ToAltitudeKm, 2),
				numeric.Fixed(leg.PlaneAngleDeg, 4),
				numeric.Fixed(leg.AltitudeDeltaVMps, dv),
				numeric.Fixed(leg.PlaneDeltaVMps, dv),
				numeric.Fixed(leg.PhasingDeltaVMps, dv),
				numeric.Fixed(leg.CaptureDeltaVMps, dv),
				numeric.Fixed(leg.DisposalDeltaVMps, dv),
				numeric.Fixed(leg.LegDeltaVMps, dv),
				numeric.Fixed(leg.CumulativeDeltaVMps, dv),
				numeric.DurationText(leg.RAANWaitSeconds),
				leg.Status)
		}
		s.Line("")
		s.Table(lt)

		s.Line("")
		s.Line("  timeline")
		tt := NewTable([]string{"seq", "time s", "elapsed", "event", "target", "dv m/s", "detail"}, 0, 1, 5)
		for _, e := range m.Timeline {
			tt.Add(strconv.Itoa(e.Sequence),
				strconv.FormatInt(e.TimeSeconds, 10),
				numeric.DurationText(e.TimeSeconds-plan.EpochSeconds),
				e.Kind, e.TargetID,
				numeric.Fixed(e.DeltaVMps, dv),
				e.Detail)
		}
		s.Table(tt)

		s.Line("")
		s.Line("  disposal")
		dt := NewTable([]string{"target", "action", "dv m/s", "from km", "to km", "life before yr", "life after yr", "compliant"}, 2, 3, 4, 5, 6)
		for _, leg := range m.Legs {
			if leg.Status != mission.LegPlanned {
				continue
			}
			dt.Add(leg.TargetID, leg.Disposal.Action,
				numeric.Fixed(leg.Disposal.DeltaVMps, dv),
				numeric.Fixed(leg.Disposal.FromAltitudeKm, 2),
				numeric.Fixed(leg.Disposal.TargetAltitudeKm, 2),
				numeric.Fixed(leg.Disposal.LifetimeBeforeYears, 2),
				numeric.Fixed(leg.Disposal.LifetimeAfterYears, 2),
				yesNo(leg.Disposal.Compliant))
		}
		s.Table(dt)
	}

	if len(plan.Skipped) > 0 {
		s.Heading("skipped targets")
		st := NewTable([]string{"target", "risk", "reason"}, 1)
		for _, sk := range plan.Skipped {
			st.Add(sk.TargetID, numeric.Fixed(sk.RiskScore, 6), sk.Reason)
		}
		s.Table(st)
	}

	s.Heading("residual risk")
	s.Field("objects before", strconv.Itoa(plan.ResidualRisk.ObjectsBefore))
	s.Field("objects after", strconv.Itoa(plan.ResidualRisk.ObjectsAfter))
	s.Field("mass before kg", numeric.Fixed(plan.ResidualRisk.MassBeforeKg, 1))
	s.Field("mass after kg", numeric.Fixed(plan.ResidualRisk.MassAfterKg, 1))
	s.Field("risk before", numeric.Fixed(plan.ResidualRisk.RiskBefore, 6))
	s.Field("risk after", numeric.Fixed(plan.ResidualRisk.RiskAfter, 6))
	s.Field("risk reduction", numeric.Fixed(plan.ResidualRisk.RiskReduction, 6))
	s.Field("risk reduction %", numeric.Fixed(plan.ResidualRisk.RiskReductionPct, 3))
	s.Field("probability before", numeric.Sci(plan.ResidualRisk.ProbabilityBefore, cfg.Output.ProbabilityExponentDigits))
	s.Field("probability after", numeric.Sci(plan.ResidualRisk.ProbabilityAfter, cfg.Output.ProbabilityExponentDigits))

	s.Heading("compliance findings")
	s.Table(findingsTable(plan.Findings))

	if cfg.Output.IncludeAssumptions {
		s.Heading("model assumptions")
		s.Bullets(plan.Assumptions, false)
		s.Bullets(compliance.Assumptions(), false)
	}
	return s.String()
}

// VerifyText renders a store verification.
func VerifyText(v store.Verification) string {
	var s Section
	s.Title(Banner)
	s.Heading("store verification")
	s.Field("root", v.Root)
	s.Field("overall", passFail(v.OK))
	s.Field("audit entries", strconv.Itoa(v.Audit.Entries))
	s.Field("audit chain", passFail(v.Audit.OK))
	s.Field("audit head", v.Audit.HeadHash)
	s.Field("metadata", passFail(v.MetaOK))
	s.Field("catalogue replay", passFail(v.CatalogueOK))

	s.Heading("audit kinds")
	kt := NewTable([]string{"kind", "entries"}, 1)
	for _, kc := range v.Audit.KindCounts {
		kt.Add(kc.Kind, strconv.Itoa(kc.Count))
	}
	s.Table(kt)

	s.Heading("snapshots")
	st := NewTable([]string{"name", "bytes", "sha256", "in meta", "hash match", "decodes"}, 1)
	for _, snap := range v.Snapshots {
		st.Add(snap.Name, strconv.Itoa(snap.Bytes), snap.SHA256,
			yesNo(snap.InMeta), yesNo(snap.HashMatch), yesNo(snap.DecodesOK))
	}
	s.Table(st)

	s.Heading("findings")
	s.Bullets(v.Findings, true)
	return s.String()
}

func findingsTable(f compliance.Findings) *Table {
	t := NewTable([]string{"rule", "subject", "status", "detail"})
	for _, item := range f {
		t.Add(item.RuleID, item.Subject, item.Status, item.Detail)
	}
	t.SortRows(1, 0, 3)
	return t
}

func captureText(f rank.Feasibility) string {
	if f.Capturable {
		return "yes:" + f.ChaserID
	}
	return "no"
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func passFail(v bool) string {
	if v {
		return "pass"
	}
	return "fail"
}
