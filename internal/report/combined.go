package report

import (
	"strconv"

	"DebrisLedger/internal/compliance"
	"DebrisLedger/internal/config"
	"DebrisLedger/internal/maneuver"
	"DebrisLedger/internal/mission"
	"DebrisLedger/internal/numeric"
	"DebrisLedger/internal/rank"
	"DebrisLedger/internal/screen"
	"DebrisLedger/internal/store"
)

// ToolName is embedded in stored reports.
const ToolName = "DebrisLedger"

// Summary is the cross-stage digest of a campaign.
type Summary struct {
	CatalogueObjects       int     `json:"catalogue_objects"`
	Assets                 int     `json:"assets"`
	Chasers                int     `json:"chasers"`
	Conjunctions           int     `json:"conjunctions"`
	ActionableConjunctions int     `json:"actionable_conjunctions"`
	ScheduledBurns         int     `json:"scheduled_burns"`
	AvoidanceDeltaVMps     float64 `json:"avoidance_delta_v_mps"`
	RankedTargets          int     `json:"ranked_targets"`
	FeasibleTargets        int     `json:"feasible_targets"`
	RemovedTargets         int     `json:"removed_targets"`
	RemovalDeltaVMps       float64 `json:"removal_delta_v_mps"`
	RiskReductionPct       float64 `json:"risk_reduction_pct"`
	ComplianceFailures     int     `json:"compliance_failures"`
	AuditEntries           int     `json:"audit_entries"`
	AuditOK                bool    `json:"audit_ok"`
}

// Combined is the artefact written by the report command.
type Combined struct {
	Tool          string              `json:"tool"`
	ScenarioLabel string              `json:"scenario_label"`
	ConfigLabel   string              `json:"config_label"`
	ConfigDigest  string              `json:"config_digest"`
	EpochSeconds  int64               `json:"epoch_s"`
	Summary       Summary             `json:"summary"`
	Screening     *screen.Result      `json:"screening,omitempty"`
	Maneuver      *maneuver.Plan      `json:"maneuver,omitempty"`
	Ranking       *rank.Ranking       `json:"ranking,omitempty"`
	Mission       *mission.Plan       `json:"mission,omitempty"`
	Verification  *store.Verification `json:"verification,omitempty"`
	Disclaimer    []string            `json:"disclaimer"`
}

// Disclaimer is repeated in every combined report.
func Disclaimer() []string {
	return []string{
		"DebrisLedger is an independent original project with no affiliation to any real company, agency, constellation or product.",
		"All bundled data is fictional and was written for this repository.",
		"The physics is a deliberately simplified circular-orbit approximation and is not suitable for operational spaceflight decisions.",
	}
}

// BuildCombined assembles the cross-stage report from whichever snapshots exist.
func BuildCombined(cfg config.Config, meta store.Meta, screening *screen.Result, avoidance *maneuver.Plan,
	ranking *rank.Ranking, campaign *mission.Plan, verification *store.Verification) Combined {
	c := Combined{
		Tool:          ToolName,
		ScenarioLabel: meta.ScenarioLabel,
		ConfigLabel:   cfg.Label,
		ConfigDigest:  cfg.Summary(),
		EpochSeconds:  meta.EpochSeconds,
		Screening:     screening,
		Maneuver:      avoidance,
		Ranking:       ranking,
		Mission:       campaign,
		Verification:  verification,
		Disclaimer:    Disclaimer(),
	}
	sum := Summary{
		CatalogueObjects: meta.Counts.Objects,
		Assets:           meta.Counts.Assets,
		Chasers:          meta.Counts.Chasers,
		AuditEntries:     meta.AuditEntries,
	}
	if screening != nil {
		sum.Conjunctions = screening.Stats.ConjunctionCount
		sum.ActionableConjunctions = screening.Stats.ActionableCount
	}
	if avoidance != nil {
		sum.ScheduledBurns = avoidance.Stats.Scheduled
		sum.AvoidanceDeltaVMps = avoidance.Stats.TotalDeltaVMps
	}
	if ranking != nil {
		sum.RankedTargets = len(ranking.Targets)
		sum.FeasibleTargets = ranking.Stats.Feasible
	}
	if campaign != nil {
		sum.RemovedTargets = len(campaign.RemovedIDs())
		removal := make([]float64, 0, len(campaign.Missions))
		for _, m := range campaign.Missions {
			removal = append(removal, m.PlannedDeltaVMps)
		}
		sum.RemovalDeltaVMps = numeric.Round(numeric.SumFloats(removal), cfg.Output.DeltaVDecimals)
		sum.RiskReductionPct = campaign.ResidualRisk.RiskReductionPct
		sum.ComplianceFailures = len(campaign.Findings.Failures())
	}
	if verification != nil {
		sum.AuditOK = verification.Audit.OK
	}
	c.Summary = sum
	return c
}

// CombinedText renders the cross-stage report.
func CombinedText(c Combined, cfg config.Config) string {
	var s Section
	s.Title(Banner)
	s.Heading("campaign report")
	s.Field("tool", c.Tool)
	s.Field("scenario", c.ScenarioLabel)
	s.Field("configuration", c.ConfigLabel)
	s.Field("epoch (s)", strconv.FormatInt(c.EpochSeconds, 10))
	s.Field("configuration digest", c.ConfigDigest)

	s.Heading("summary")
	t := NewTable([]string{"metric", "value"}, 1)
	t.Add("catalogue objects", strconv.Itoa(c.Summary.CatalogueObjects))
	t.Add("operator assets", strconv.Itoa(c.Summary.Assets))
	t.Add("chaser vehicles", strconv.Itoa(c.Summary.Chasers))
	t.Add("conjunctions", strconv.Itoa(c.Summary.Conjunctions))
	t.Add("actionable conjunctions", strconv.Itoa(c.Summary.ActionableConjunctions))
	t.Add("scheduled avoidance burns", strconv.Itoa(c.Summary.ScheduledBurns))
	t.Add("avoidance delta-v m/s", numeric.Fixed(c.Summary.AvoidanceDeltaVMps, cfg.Output.DeltaVDecimals))
	t.Add("ranked removal targets", strconv.Itoa(c.Summary.RankedTargets))
	t.Add("feasible removal targets", strconv.Itoa(c.Summary.FeasibleTargets))
	t.Add("removed targets", strconv.Itoa(c.Summary.RemovedTargets))
	t.Add("removal delta-v m/s", numeric.Fixed(c.Summary.RemovalDeltaVMps, cfg.Output.DeltaVDecimals))
	t.Add("risk reduction %", numeric.Fixed(c.Summary.RiskReductionPct, 3))
	t.Add("compliance failures", strconv.Itoa(c.Summary.ComplianceFailures))
	t.Add("audit entries", strconv.Itoa(c.Summary.AuditEntries))
	t.Add("audit chain valid", yesNo(c.Summary.AuditOK))
	s.Table(t)

	if c.Screening != nil {
		s.Heading("screening extract")
		et := NewTable([]string{"asset", "object", "miss km", "pc", "severity"}, 2, 3)
		limit := 0
		for _, conj := range c.Screening.Conjunctions {
			if limit >= 12 {
				break
			}
			limit++
			et.Add(conj.AssetID, conj.ObjectID,
				numeric.Fixed(conj.MissKm, cfg.Output.DistanceDecimals),
				numeric.Sci(conj.Probability, cfg.Output.ProbabilityExponentDigits),
				conj.Severity)
		}
		s.Table(et)
	}
	if c.Maneuver != nil {
		s.Heading("avoidance extract")
		bt := NewTable([]string{"burn s", "asset", "object", "dv m/s", "status"}, 0, 3)
		for _, b := range c.Maneuver.Burns {
			bt.Add(strconv.FormatInt(b.BurnSeconds, 10), b.AssetID, b.ObjectID,
				numeric.Fixed(b.DeltaVMps, cfg.Output.DeltaVDecimals), b.Status)
		}
		s.Table(bt)
	}
	if c.Ranking != nil {
		s.Heading("ranking extract")
		rt := NewTable([]string{"rank", "object", "risk", "mass kg", "capture"}, 0, 2, 3)
		for _, target := range c.Ranking.Targets {
			rt.Add(strconv.Itoa(target.Rank), target.ObjectID,
				numeric.Fixed(target.RiskScore, 6),
				numeric.Fixed(target.MassKg, 1),
				captureText(target.Feasibility))
		}
		s.Table(rt)
	}
	if c.Mission != nil {
		s.Heading("mission extract")
		mt := NewTable([]string{"chaser", "legs", "planned m/s", "remaining m/s", "mass removed kg", "budget ok"}, 1, 2, 3, 4)
		for _, m := range c.Mission.Missions {
			mt.Add(m.ChaserID, strconv.Itoa(m.Stats.LegsPlanned),
				numeric.Fixed(m.PlannedDeltaVMps, cfg.Output.DeltaVDecimals),
				numeric.Fixed(m.RemainingMps, cfg.Output.DeltaVDecimals),
				numeric.Fixed(m.Stats.MassRemovedKg, 1),
				yesNo(m.BudgetOK))
		}
		s.Table(mt)

		s.Heading("compliance failures")
		s.Table(findingsTable(c.Mission.Findings.Failures()))
	}
	if c.Verification != nil {
		s.Heading("store integrity")
		s.Field("audit head", c.Verification.Audit.HeadHash)
		s.Field("overall", passFail(c.Verification.OK))
		s.Bullets(c.Verification.Findings, true)
	}

	s.Heading("model caveats")
	s.Bullets(screen.ProbabilityAssumptions(), false)
	s.Bullets(compliance.Assumptions(), false)

	s.Heading("disclaimer")
	s.Bullets(c.Disclaimer, false)
	return s.String()
}
