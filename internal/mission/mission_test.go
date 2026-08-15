package mission

import (
	"testing"

	"DebrisLedger/internal/compliance"
	"DebrisLedger/internal/config"
	"DebrisLedger/internal/model"
	"DebrisLedger/internal/rank"
	"DebrisLedger/internal/screen"
)

func object(id string, massKg, areaM2, altKm, incDeg, raanDeg, tumble float64, attach string) model.CatalogObject {
	return model.CatalogObject{
		ID: id, Name: id, Class: model.ClassRocketBody, MassKg: massKg, AreaM2: areaM2,
		Orbit:             model.Orbit{AltitudeKm: altKm, InclinationDeg: incDeg, RAANDeg: raanDeg, ArgLatDeg: 30},
		TumbleRateDegPerS: tumble, AttachPoint: attach,
	}
}

func scenario() *model.Scenario {
	sc := &model.Scenario{
		Label: "mission-unit", EpochSeconds: 0, HorizonSeconds: 259200,
		Objects: []model.CatalogObject{
			object("rb-near", 1400, 12, 705, 98.1, 210.4, 1.0, "adapter-ring"),
			object("rb-alsonear", 900, 8, 708, 98.15, 210.6, 1.2, "grapple-fixture"),
			object("rb-faraway", 1200, 10, 1400, 63.0, 15.0, 0.8, "adapter-ring"),
		},
		Chasers: []model.Chaser{
			{
				ID: "chaser-1", Name: "Chaser One", DryMassKg: 1500, DeltaVBudgetMps: 900,
				CaptureSlots: 3, DockingTimeS: 43200, MaxCaptureMassKg: 2500, MaxTumbleRateDegPerS: 3,
				Orbit: model.Orbit{AltitudeKm: 700, InclinationDeg: 98.0, RAANDeg: 210.0, ArgLatDeg: 0},
			},
		},
	}
	sc.Sort()
	return sc
}

func ranking(sc *model.Scenario, cfg config.Config) rank.Ranking {
	return rank.Build(sc, cfg, screen.Result{ScenarioLabel: sc.Label, ConfigLabel: cfg.Label})
}

func TestBuildPlansLegsWithinBudget(t *testing.T) {
	sc := scenario()
	cfg := config.Default()
	plan := Build(sc, cfg, ranking(sc, cfg))
	if len(plan.Missions) != 1 {
		t.Fatalf("expected one mission, got %d", len(plan.Missions))
	}
	m := plan.Missions[0]
	if m.Stats.LegsPlanned == 0 {
		t.Fatalf("expected at least one planned leg, legs: %+v", m.Legs)
	}
	if !m.BudgetOK {
		t.Fatalf("planned %v exceeds the usable %v", m.PlannedDeltaVMps, m.UsableMps)
	}
	if m.PlannedDeltaVMps > m.UsableMps {
		t.Fatal("planned delta-v must stay inside the usable budget")
	}
	if m.UsableMps >= m.DeltaVBudgetMps {
		t.Fatal("a reserve must be held back")
	}
	planned := 0
	for _, leg := range m.Legs {
		if leg.Status == LegPlanned {
			planned++
			if leg.LegDeltaVMps <= 0 {
				t.Fatalf("leg %d has no cost", leg.Sequence)
			}
			if leg.EndSeconds < leg.StartSeconds {
				t.Fatalf("leg %d ends before it starts", leg.Sequence)
			}
		}
	}
	if planned != m.Stats.LegsPlanned {
		t.Fatalf("planned leg count mismatch: %d vs %d", planned, m.Stats.LegsPlanned)
	}
	if planned > m.CaptureSlots {
		t.Fatal("more legs than capture slots")
	}
}

func TestCumulativeDeltaVIsMonotonic(t *testing.T) {
	sc := scenario()
	cfg := config.Default()
	m := Build(sc, cfg, ranking(sc, cfg)).Missions[0]
	previous := -1.0
	for _, leg := range m.Legs {
		if leg.Status != LegPlanned {
			continue
		}
		if leg.CumulativeDeltaVMps < previous {
			t.Fatalf("cumulative delta-v decreased at leg %d", leg.Sequence)
		}
		previous = leg.CumulativeDeltaVMps
	}
}

func TestTimelineIsOrderedAndComplete(t *testing.T) {
	sc := scenario()
	cfg := config.Default()
	m := Build(sc, cfg, ranking(sc, cfg)).Missions[0]
	if len(m.Timeline) < 3 {
		t.Fatalf("timeline too short: %+v", m.Timeline)
	}
	if m.Timeline[0].Kind != EventMissionStart {
		t.Fatalf("first event %q", m.Timeline[0].Kind)
	}
	if m.Timeline[len(m.Timeline)-1].Kind != EventMissionEnd {
		t.Fatalf("last event %q", m.Timeline[len(m.Timeline)-1].Kind)
	}
	kinds := map[string]int{}
	for i, e := range m.Timeline {
		if e.Sequence != i+1 {
			t.Fatalf("event %d carries sequence %d", i, e.Sequence)
		}
		if i > 0 && e.TimeSeconds < m.Timeline[i-1].TimeSeconds {
			t.Fatalf("timeline goes backwards at event %d", i)
		}
		if e.Detail == "" {
			t.Fatalf("event %d has no detail", i)
		}
		kinds[e.Kind]++
	}
	for _, want := range []string{EventTransferBurn, EventRendezvous, EventCapture, EventRelease} {
		if kinds[want] == 0 {
			t.Fatalf("timeline is missing a %s event", want)
		}
	}
}

func TestUnaffordableTargetIsRejected(t *testing.T) {
	sc := scenario()
	cfg := config.Default()
	plan := Build(sc, cfg, ranking(sc, cfg))
	m := plan.Missions[0]
	rejected := 0
	for _, leg := range m.Legs {
		if leg.Status == LegOverBudget {
			rejected++
			if leg.Reason == "" {
				t.Fatal("a rejected leg must carry a reason")
			}
		}
	}
	if rejected == 0 {
		t.Fatal("the distant high-inclination target should not be affordable")
	}
	if len(plan.Skipped) == 0 {
		t.Fatal("unserviced targets must be reported")
	}
	for i := 1; i < len(plan.Skipped); i++ {
		if plan.Skipped[i].RiskScore > plan.Skipped[i-1].RiskScore {
			t.Fatal("skipped targets must be sorted by descending risk")
		}
	}
}

func TestInfeasibleTargetsAreReportedAsSkipped(t *testing.T) {
	sc := scenario()
	sc.Objects = append(sc.Objects, object("rb-bare", 800, 7, 706, 98.1, 210.5, 1.0, "none"))
	sc.Sort()
	cfg := config.Default()
	plan := Build(sc, cfg, ranking(sc, cfg))
	found := false
	for _, s := range plan.Skipped {
		if s.TargetID == "rb-bare" {
			found = true
			if s.Reason == "" {
				t.Fatal("a skipped target must explain itself")
			}
		}
	}
	if !found {
		t.Fatalf("the un-capturable object must appear in the skipped list: %+v", plan.Skipped)
	}
}

func TestResidualRiskReflectsRemovals(t *testing.T) {
	sc := scenario()
	cfg := config.Default()
	plan := Build(sc, cfg, ranking(sc, cfg))
	removed := plan.RemovedIDs()
	if len(removed) == 0 {
		t.Fatal("expected at least one removal")
	}
	if plan.ResidualRisk.ObjectsAfter != plan.ResidualRisk.ObjectsBefore-len(removed) {
		t.Fatalf("residual counts %+v do not match %d removals", plan.ResidualRisk, len(removed))
	}
	if plan.ResidualRisk.RiskAfter > plan.ResidualRisk.RiskBefore {
		t.Fatal("removing objects cannot raise the residual risk")
	}
	if plan.ResidualRisk.MassAfterKg >= plan.ResidualRisk.MassBeforeKg {
		t.Fatal("removing objects must reduce the tracked mass")
	}
	for i := 1; i < len(removed); i++ {
		if removed[i] < removed[i-1] {
			t.Fatal("removed identifiers must be sorted")
		}
	}
}

func TestDisposalActionsAreAttachedToLegs(t *testing.T) {
	sc := scenario()
	cfg := config.Default()
	m := Build(sc, cfg, ranking(sc, cfg)).Missions[0]
	for _, leg := range m.Legs {
		if leg.Status != LegPlanned {
			continue
		}
		if leg.Disposal.ObjectID != leg.TargetID {
			t.Fatalf("leg %d disposal refers to %q", leg.Sequence, leg.Disposal.ObjectID)
		}
		switch leg.Disposal.Action {
		case compliance.ActionReentry, compliance.ActionGraveyard, compliance.ActionNone:
		default:
			t.Fatalf("unexpected disposal action %q", leg.Disposal.Action)
		}
		if len(leg.Disposal.Findings) == 0 {
			t.Fatalf("leg %d disposal has no findings", leg.Sequence)
		}
	}
	if m.Stats.ReentryCount+m.Stats.GraveyardCount+m.Stats.NoDisposalCount != m.Stats.LegsPlanned {
		t.Fatalf("disposal counts do not add up: %+v", m.Stats)
	}
}

func TestFindingsIncludeBudgetRule(t *testing.T) {
	sc := scenario()
	cfg := config.Default()
	plan := Build(sc, cfg, ranking(sc, cfg))
	found := false
	for _, f := range plan.Findings {
		if f.RuleID == "MSN-02-delta-v-budget" {
			found = true
			if f.Status != compliance.StatusPass {
				t.Fatalf("budget rule failed: %s", f.Detail)
			}
		}
	}
	if !found {
		t.Fatal("the mission budget rule must be evaluated")
	}
}

func TestBuildIsDeterministic(t *testing.T) {
	cfg := config.Default()
	sc := scenario()
	first := Build(sc, cfg, ranking(sc, cfg))
	for i := 0; i < 3; i++ {
		again := Build(scenario(), cfg, ranking(scenario(), cfg))
		if len(again.Missions) != len(first.Missions) {
			t.Fatalf("run %d changed the mission count", i)
		}
		for j := range again.Missions {
			a, b := again.Missions[j], first.Missions[j]
			if a.PlannedDeltaVMps != b.PlannedDeltaVMps || len(a.Legs) != len(b.Legs) {
				t.Fatalf("run %d differs for chaser %s", i, a.ChaserID)
			}
			for k := range a.Legs {
				if a.Legs[k].TargetID != b.Legs[k].TargetID || a.Legs[k].Status != b.Legs[k].Status {
					t.Fatalf("run %d differs at leg %d", i, k)
				}
			}
			for k := range a.Timeline {
				if a.Timeline[k] != b.Timeline[k] {
					t.Fatalf("run %d differs at timeline event %d", i, k)
				}
			}
		}
	}
}

func TestChaserLimitsFilterTargets(t *testing.T) {
	sc := scenario()
	sc.Chasers[0].MaxCaptureMassKg = 1000
	cfg := config.Default()
	plan := Build(sc, cfg, ranking(sc, cfg))
	for _, leg := range plan.Missions[0].Legs {
		if leg.Status != LegPlanned {
			continue
		}
		obj, ok := sc.ObjectByID(leg.TargetID)
		if !ok {
			t.Fatalf("leg refers to an unknown object %q", leg.TargetID)
		}
		if obj.MassKg > sc.Chasers[0].MaxCaptureMassKg {
			t.Fatalf("chaser captured %v kg above its %v kg limit", obj.MassKg, sc.Chasers[0].MaxCaptureMassKg)
		}
	}
}

func TestAssumptionsAreDocumented(t *testing.T) {
	if len(missionAssumptions()) == 0 {
		t.Fatal("mission assumptions must be documented")
	}
}
