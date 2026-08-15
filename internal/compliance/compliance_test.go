package compliance

import (
	"math"
	"testing"

	"DebrisLedger/internal/config"
	"DebrisLedger/internal/model"
)

func object(id string, altKm, massKg, areaM2 float64, attach string) model.CatalogObject {
	return model.CatalogObject{
		ID: id, Name: id, Class: model.ClassRocketBody, MassKg: massKg, AreaM2: areaM2,
		Orbit:             model.Orbit{AltitudeKm: altKm, InclinationDeg: 53, RAANDeg: 10, ArgLatDeg: 0},
		TumbleRateDegPerS: 1, AttachPoint: attach,
	}
}

func TestAtmosphericDensityFallsWithAltitude(t *testing.T) {
	previous := math.Inf(1)
	for _, alt := range []float64{0, 100, 200, 400, 600, 800, 1000, 1100} {
		got := AtmosphericDensity(alt)
		if got <= 0 {
			t.Fatalf("density at %v km is %g", alt, got)
		}
		if got >= previous {
			t.Fatalf("density at %v km (%g) is not below the previous layer (%g)", alt, got, previous)
		}
		previous = got
	}
	if AtmosphericDensity(2000) != 0 {
		t.Fatal("the model must report vacuum above the table")
	}
	// Inside a layer the profile is exponential with the layer scale height.
	base := AtmosphericDensity(600)
	oneScaleHeight := AtmosphericDensity(600 + scaleHeightKm(600))
	if ratio := base / oneScaleHeight; math.Abs(ratio-math.E) > 1e-6 {
		t.Fatalf("one scale height should divide the density by e, got %v", ratio)
	}
}

func TestLifetimeFallsWithAltitudeAndRisesWithMass(t *testing.T) {
	low := LifetimeYears(300, 0.01)
	mid := LifetimeYears(600, 0.01)
	high := LifetimeYears(900, 0.01)
	if !(low < mid && mid < high) {
		t.Fatalf("lifetime must grow with altitude: %g %g %g", low, mid, high)
	}
	heavy := LifetimeYears(600, 0.002)
	if heavy <= mid {
		t.Fatalf("a lower area-to-mass ratio must live longer: %g vs %g", heavy, mid)
	}
	if got := LifetimeYears(600, 0); got != MaxLifetimeYears {
		t.Fatalf("a zero ballistic ratio must saturate, got %g", got)
	}
	if got := LifetimeYears(5000, 0.01); got != MaxLifetimeYears {
		t.Fatalf("above the table the lifetime must saturate, got %g", got)
	}
	// A 600 km object with a typical ballistic ratio sits in the decades band.
	if mid < 5 || mid > 200 {
		t.Fatalf("600 km lifetime %g years is implausible for this model", mid)
	}
}

func TestDecaysWithinAndNaturalDecayCeiling(t *testing.T) {
	if !DecaysWithin(300, 0.01, 25) {
		t.Fatal("a 300 km orbit should decay quickly")
	}
	if DecaysWithin(900, 0.01, 25) {
		t.Fatal("a 900 km orbit should not decay inside 25 years")
	}
	ceiling := NaturalDecayAltitudeKm(0.01, 25)
	if ceiling < 300 || ceiling > 900 {
		t.Fatalf("decay ceiling %g km is implausible", ceiling)
	}
	if !DecaysWithin(ceiling-1, 0.01, 25) {
		t.Fatal("just below the ceiling the object must still decay in time")
	}
	if DecaysWithin(ceiling+1, 0.01, 25) {
		t.Fatal("just above the ceiling the object must not decay in time")
	}
	if got := NaturalDecayAltitudeKm(1e-12, 25); got != 100 {
		t.Fatalf("an object that never decays must report the table floor, got %g", got)
	}
	if got := NaturalDecayAltitudeKm(100, 1e6); got != 1100 {
		t.Fatalf("an object that always decays must report the table ceiling, got %g", got)
	}
}

func TestPlanDisposalChoosesNoActionForShortLifetime(t *testing.T) {
	cfg := config.Default().Compliance
	d := PlanDisposal(cfg, object("rb-low", 400, 600, 6, "adapter-ring"))
	if d.Action != ActionNone {
		t.Fatalf("action %q, want %q", d.Action, ActionNone)
	}
	if d.DeltaVMps != 0 {
		t.Fatalf("no burn should be charged, got %v", d.DeltaVMps)
	}
	if !d.Compliant {
		t.Fatalf("expected compliance, findings %+v", d.Findings)
	}
	if d.LifetimeAfterYears != d.LifetimeBeforeYears {
		t.Fatal("the lifetime must be unchanged")
	}
}

func TestPlanDisposalChoosesReentryBelowCeiling(t *testing.T) {
	cfg := config.Default().Compliance
	d := PlanDisposal(cfg, object("rb-mid", 610, 1400, 8, "adapter-ring"))
	if d.Action != ActionReentry {
		t.Fatalf("action %q, want %q", d.Action, ActionReentry)
	}
	if d.DeltaVMps <= 0 {
		t.Fatalf("a reentry burn must cost something, got %v", d.DeltaVMps)
	}
	if d.LifetimeAfterYears != 0 {
		t.Fatalf("after reentry the lifetime must be zero, got %v", d.LifetimeAfterYears)
	}
	if !d.Compliant {
		t.Fatalf("expected compliance, findings %+v", d.Findings)
	}
	if d.TargetPerigeeKm != cfg.ReentryTargetPerigeeKm {
		t.Fatalf("target perigee %v", d.TargetPerigeeKm)
	}
}

func TestPlanDisposalChoosesGraveyardAboveCeiling(t *testing.T) {
	cfg := config.Default().Compliance
	d := PlanDisposal(cfg, object("rb-high", 1800, 1400, 8, "grapple-fixture"))
	if d.Action != ActionGraveyard {
		t.Fatalf("action %q, want %q", d.Action, ActionGraveyard)
	}
	if d.TargetAltitudeKm != 1800+cfg.GraveyardRaiseKm {
		t.Fatalf("target altitude %v", d.TargetAltitudeKm)
	}
	if !d.Compliant {
		t.Fatalf("a graveyard above the protected region must comply, findings %+v", d.Findings)
	}
	// Inside the protected region the graveyard option must fail.
	inside := PlanDisposal(cfg, object("rb-inside", 1500, 1400, 8, "grapple-fixture"))
	if inside.Compliant {
		t.Fatal("a graveyard inside the protected region must not comply")
	}
	found := false
	for _, f := range inside.Findings {
		if f.RuleID == RuleGraveyardClear && f.Status == StatusFail {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a graveyard-clearance failure, got %+v", inside.Findings)
	}
}

func TestPlanDisposalFlagsMissingAttachPoint(t *testing.T) {
	cfg := config.Default().Compliance
	d := PlanDisposal(cfg, object("rb-bare", 610, 1400, 8, "none"))
	if d.Compliant {
		t.Fatal("an object without an attach point must not comply")
	}
	found := false
	for _, f := range d.Findings {
		if f.RuleID == RuleCaptureIntegrity && f.Status == StatusFail {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a capture-integrity failure, got %+v", d.Findings)
	}
}

func TestFindingsSortAndFailures(t *testing.T) {
	f := Findings{
		{RuleID: "B", Subject: "z", Status: StatusPass},
		{RuleID: "A", Subject: "z", Status: StatusFail},
		{RuleID: "C", Subject: "a", Status: StatusNotApplicable},
	}
	f.Sort()
	if f[0].Subject != "a" {
		t.Fatalf("findings not sorted by subject: %+v", f)
	}
	if f[1].RuleID != "A" || f[2].RuleID != "B" {
		t.Fatalf("findings not sorted by rule: %+v", f)
	}
	failures := f.Failures()
	if len(failures) != 1 || failures[0].RuleID != "A" {
		t.Fatalf("failures %+v", failures)
	}
}

func TestComputeResidualRisk(t *testing.T) {
	inputs := []RiskInput{
		{ObjectID: "b", MassKg: 200, RiskScore: 0.5, Probability: 2e-5},
		{ObjectID: "a", MassKg: 100, RiskScore: 0.25, Probability: 1e-5},
		{ObjectID: "c", MassKg: 300, RiskScore: 0.75, Probability: 3e-5},
	}
	got := ComputeResidualRisk(inputs, []string{"c", "a"})
	if got.ObjectsBefore != 3 || got.ObjectsAfter != 1 {
		t.Fatalf("counts %+v", got)
	}
	if got.MassBeforeKg != 600 || got.MassAfterKg != 200 {
		t.Fatalf("masses %+v", got)
	}
	if math.Abs(got.RiskBefore-1.5) > 1e-9 || math.Abs(got.RiskAfter-0.5) > 1e-9 {
		t.Fatalf("risk %+v", got)
	}
	if math.Abs(got.RiskReduction-1.0) > 1e-9 {
		t.Fatalf("reduction %v", got.RiskReduction)
	}
	if math.Abs(got.RiskReductionPct-66.667) > 0.01 {
		t.Fatalf("reduction percentage %v", got.RiskReductionPct)
	}
	if len(got.RemovedIDs) != 2 || got.RemovedIDs[0] != "a" || got.RemovedIDs[1] != "c" {
		t.Fatalf("removed ids %v must be sorted", got.RemovedIDs)
	}
	if got.ProbabilityReduction <= 0 {
		t.Fatalf("probability reduction %g", got.ProbabilityReduction)
	}
	empty := ComputeResidualRisk(nil, nil)
	if empty.RiskReductionPct != 0 {
		t.Fatalf("an empty catalogue must report zero, got %v", empty.RiskReductionPct)
	}
}

func TestAssumptionsAreDocumented(t *testing.T) {
	if len(Assumptions()) == 0 {
		t.Fatal("the compliance model must document its assumptions")
	}
}
