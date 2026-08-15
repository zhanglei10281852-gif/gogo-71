package mission

import (
	"testing"

	"DebrisLedger/internal/compliance"
	"DebrisLedger/internal/config"
	"DebrisLedger/internal/model"
	"DebrisLedger/internal/rank"
	"DebrisLedger/internal/screen"
)

// reserveScenario holds one chaser and one reachable target, so a mission plan
// contains exactly one candidate leg whose cost can be measured first and
// budgeted afterwards.
func reserveScenario(budgetMps float64) *model.Scenario {
	sc := &model.Scenario{
		Label: "reserve-regression", EpochSeconds: 0, HorizonSeconds: 259200,
		Objects: []model.CatalogObject{{
			ID: "rb-reserve-target", Name: "rb-reserve-target", Class: model.ClassRocketBody,
			MassKg: 1300, AreaM2: 11,
			Orbit:             model.Orbit{AltitudeKm: 712, InclinationDeg: 98.6, RAANDeg: 210.9, ArgLatDeg: 40},
			TumbleRateDegPerS: 1.1, AttachPoint: "adapter-ring",
		}},
		Chasers: []model.Chaser{{
			ID: "chaser-reserve", Name: "chaser-reserve", DryMassKg: 1500, DeltaVBudgetMps: budgetMps,
			CaptureSlots: 2, DockingTimeS: 43200, MaxCaptureMassKg: 2500, MaxTumbleRateDegPerS: 3,
			Orbit: model.Orbit{AltitudeKm: 700, InclinationDeg: 98.0, RAANDeg: 210.0, ArgLatDeg: 0},
		}},
	}
	sc.Sort()
	return sc
}

// reservePlan plans the single-chaser campaign for the given budget and reserve.
func reservePlan(t *testing.T, budgetMps, reserveFraction float64) Mission {
	t.Helper()
	cfg := config.Default()
	cfg.Mission.ReserveFraction = reserveFraction
	sc := reserveScenario(budgetMps)
	ranking := rank.Build(sc, cfg, screen.Result{ScenarioLabel: sc.Label, ConfigLabel: cfg.Label})
	plan := Build(sc, cfg, ranking)
	if len(plan.Missions) != 1 {
		t.Fatalf("expected exactly one mission, got %d", len(plan.Missions))
	}
	return plan.Missions[0]
}

func TestPlannedDeltaVStaysOutOfTheChaserReserve(t *testing.T) {
	const reserveFraction = 0.2

	// Measure what the single candidate leg costs when nothing constrains it.
	probe := reservePlan(t, 5000, reserveFraction)
	if probe.Stats.LegsPlanned != 1 {
		t.Fatalf("the probe run should plan the only leg, got %d planned out of %d leg(s)",
			probe.Stats.LegsPlanned, len(probe.Legs))
	}
	cost := probe.Legs[0].LegDeltaVMps
	if cost <= 0 {
		t.Fatalf("leg cost %v m/s is not usable as a reference", cost)
	}

	// Give the chaser a budget that covers the leg in total but not once the
	// reserve is held back: usable = 0.8 * 1.05 * cost = 0.84 * cost.
	m := reservePlan(t, cost*1.05, reserveFraction)
	if m.DeltaVBudgetMps < cost {
		t.Fatalf("setup error: total budget %v m/s should still cover the %v m/s leg", m.DeltaVBudgetMps, cost)
	}
	if m.UsableMps >= cost {
		t.Fatalf("setup error: usable budget %v m/s should be below the %v m/s leg", m.UsableMps, cost)
	}

	if m.Stats.LegsPlanned != 0 {
		t.Fatalf("a %v m/s leg must not be flown against a %v m/s usable budget (%v m/s total, %v m/s reserve); planned %v m/s",
			cost, m.UsableMps, m.DeltaVBudgetMps, m.ReserveMps, m.PlannedDeltaVMps)
	}
	if m.PlannedDeltaVMps > m.UsableMps {
		t.Fatalf("planned %v m/s exceeds the usable %v m/s", m.PlannedDeltaVMps, m.UsableMps)
	}
	if m.RemainingMps < 0 {
		t.Fatalf("remaining budget %v m/s must not go negative", m.RemainingMps)
	}
	if !m.BudgetOK {
		t.Fatalf("the mission must stay inside its usable budget, got planned %v of usable %v",
			m.PlannedDeltaVMps, m.UsableMps)
	}
	if len(m.Legs) != 1 {
		t.Fatalf("expected exactly one reported leg, got %d", len(m.Legs))
	}
	if m.Legs[0].Status != LegOverBudget {
		t.Fatalf("the unaffordable leg must be reported as %q, got %q", LegOverBudget, m.Legs[0].Status)
	}
	if m.Legs[0].Reason == "" {
		t.Fatal("a rejected leg must carry a reason")
	}

	found := false
	for _, f := range m.Findings {
		if f.RuleID != "MSN-02-delta-v-budget" {
			continue
		}
		found = true
		if f.Status != compliance.StatusPass {
			t.Fatalf("the budget rule must pass when nothing was flown: %+v", f)
		}
	}
	if !found {
		t.Fatalf("the mission budget rule must be evaluated: %+v", m.Findings)
	}
}
