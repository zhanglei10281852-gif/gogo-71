package rank

import (
	"testing"

	"DebrisLedger/internal/config"
	"DebrisLedger/internal/model"
	"DebrisLedger/internal/orbit"
	"DebrisLedger/internal/screen"
)

func object(id string, class model.ObjectClass, massKg, areaM2, altKm, tumble float64, attach string) model.CatalogObject {
	return model.CatalogObject{
		ID: id, Name: id, Class: class, MassKg: massKg, AreaM2: areaM2,
		Orbit:             model.Orbit{AltitudeKm: altKm, InclinationDeg: 53, RAANDeg: 84, ArgLatDeg: 0},
		TumbleRateDegPerS: tumble, AttachPoint: attach,
	}
}

func chaser(id string, maxMass, maxTumble float64) model.Chaser {
	return model.Chaser{
		ID: id, Name: id, DryMassKg: 1200, DeltaVBudgetMps: 800, CaptureSlots: 2,
		DockingTimeS: 3600, MaxCaptureMassKg: maxMass, MaxTumbleRateDegPerS: maxTumble,
		Orbit: model.Orbit{AltitudeKm: 700, InclinationDeg: 98, RAANDeg: 210},
	}
}

func scenario() *model.Scenario {
	sc := &model.Scenario{
		Label: "rank-unit", EpochSeconds: 0, HorizonSeconds: 86400,
		Objects: []model.CatalogObject{
			object("rb-heavy", model.ClassRocketBody, 2000, 18, 550, 1.0, "adapter-ring"),
			object("rb-light", model.ClassRocketBody, 400, 5, 552, 0.5, "adapter-ring"),
			object("frag-spin", model.ClassFragment, 30, 0.3, 551, 90, "none"),
			object("rb-huge", model.ClassRocketBody, 5000, 30, 553, 0.4, "grapple-fixture"),
			object("pay-live", model.ClassPayload, 1200, 9, 554, 0.1, "adapter-ring"),
		},
		Chasers: []model.Chaser{chaser("chaser-a", 2500, 3), chaser("chaser-b", 3000, 5)},
	}
	sc.Sort()
	return sc
}

func result() screen.Result {
	return screen.Result{
		ScenarioLabel: "rank-unit", ConfigLabel: "unit",
		Conjunctions: []screen.Conjunction{
			{AssetID: "asset-1", ObjectID: "frag-spin", Probability: 8e-5, Severity: "high"},
			{AssetID: "asset-1", ObjectID: "rb-light", Probability: 2e-5, Severity: "moderate"},
		},
	}
}

func TestBuildExcludesPayloadsAndRanksByScore(t *testing.T) {
	r := Build(scenario(), config.Default(), result())
	if r.Stats.Excluded != 1 {
		t.Fatalf("expected one excluded payload, got %d", r.Stats.Excluded)
	}
	if r.Stats.Evaluated != 4 {
		t.Fatalf("expected four evaluated objects, got %d", r.Stats.Evaluated)
	}
	if r.Stats.Listed != len(r.Targets) {
		t.Fatalf("listed %d but %d targets present", r.Stats.Listed, len(r.Targets))
	}
	for _, target := range r.Targets {
		if target.Class == model.ClassPayload {
			t.Fatal("payloads must not be ranked")
		}
		if target.RiskScore < 0 || target.RiskScore > 1 {
			t.Fatalf("risk score %v out of range", target.RiskScore)
		}
	}
	for i := 1; i < len(r.Targets); i++ {
		if r.Targets[i].RiskScore > r.Targets[i-1].RiskScore {
			t.Fatalf("targets not sorted by descending risk: %+v", r.Targets)
		}
		if r.Targets[i].Rank != i+1 {
			t.Fatalf("rank %d at index %d", r.Targets[i].Rank, i)
		}
	}
	if r.Targets[0].ObjectID != "rb-huge" {
		t.Fatalf("the heaviest congested object should lead, got %s", r.Targets[0].ObjectID)
	}
}

func TestScoreTermsAreNormalisedAndWeighted(t *testing.T) {
	cfg := config.Default()
	r := Build(scenario(), cfg, result())
	target, ok := r.TargetByID("frag-spin")
	if !ok {
		t.Fatal("expected frag-spin in the ranking")
	}
	if target.ProbabilityScore <= 0 {
		t.Fatalf("probability score %v", target.ProbabilityScore)
	}
	want := cfg.Ranking.MassWeight*target.MassScore +
		cfg.Ranking.ProbabilityWeight*target.ProbabilityScore +
		cfg.Ranking.CongestionWeight*target.CongestionScore
	if diff := target.RiskScore - want; diff > 1e-6 || diff < -1e-6 {
		t.Fatalf("risk score %v does not match the weighted sum %v", target.RiskScore, want)
	}
	heavy, _ := r.TargetByID("rb-huge")
	if heavy.MassScore != 1 {
		t.Fatalf("mass score must clamp at one, got %v", heavy.MassScore)
	}
	if _, ok := r.TargetByID("pay-live"); ok {
		t.Fatal("payloads must not be present")
	}
}

func TestFeasibilityChecksEveryLimit(t *testing.T) {
	r := Build(scenario(), config.Default(), result())
	spin, _ := r.TargetByID("frag-spin")
	if spin.Feasibility.Capturable {
		t.Fatal("a fast tumbler without an attach point cannot be captured")
	}
	if len(spin.Feasibility.Blockers) < 2 {
		t.Fatalf("expected attach-point and tumble blockers, got %v", spin.Feasibility.Blockers)
	}
	huge, _ := r.TargetByID("rb-huge")
	if huge.Feasibility.Capturable {
		t.Fatal("an object heavier than every chaser cannot be captured")
	}
	light, _ := r.TargetByID("rb-light")
	if !light.Feasibility.Capturable {
		t.Fatalf("rb-light should be capturable, blockers %v", light.Feasibility.Blockers)
	}
	if light.Feasibility.ChaserID != "chaser-a" {
		t.Fatalf("the first suitable chaser in identifier order should be recorded, got %q",
			light.Feasibility.ChaserID)
	}
	if len(r.FeasibleTargets()) != r.Stats.Feasible {
		t.Fatal("feasible count and feasible list must agree")
	}
}

func TestFeasibilityWithoutChasers(t *testing.T) {
	sc := scenario()
	sc.Chasers = nil
	r := Build(sc, config.Default(), result())
	for _, target := range r.Targets {
		if target.Feasibility.Capturable {
			t.Fatal("without chasers nothing is capturable")
		}
	}
	found := false
	for _, target := range r.Targets {
		for _, b := range target.Feasibility.Blockers {
			if b == "scenario declares no chaser vehicles" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("the missing-chaser blocker must be reported")
	}
}

func TestMaxTargetsTruncatesTheList(t *testing.T) {
	cfg := config.Default()
	cfg.Ranking.MaxTargets = 2
	r := Build(scenario(), cfg, result())
	if len(r.Targets) != 2 {
		t.Fatalf("expected two targets, got %d", len(r.Targets))
	}
	if r.Stats.Listed != 2 {
		t.Fatalf("listed %d", r.Stats.Listed)
	}
}

func TestBuildIsDeterministic(t *testing.T) {
	cfg := config.Default()
	first := Build(scenario(), cfg, result())
	for i := 0; i < 3; i++ {
		again := Build(scenario(), cfg, result())
		if len(again.Targets) != len(first.Targets) {
			t.Fatalf("run %d changed the target count", i)
		}
		for j := range again.Targets {
			if again.Targets[j].ObjectID != first.Targets[j].ObjectID {
				t.Fatalf("run %d differs at position %d", i, j)
			}
			if again.Targets[j].RiskScore != first.Targets[j].RiskScore {
				t.Fatalf("run %d changed the score of %s", i, again.Targets[j].ObjectID)
			}
		}
	}
}

func TestRiskInputsMirrorTargets(t *testing.T) {
	r := Build(scenario(), config.Default(), result())
	inputs := r.RiskInputs()
	if len(inputs) != len(r.Targets) {
		t.Fatalf("risk inputs %d, targets %d", len(inputs), len(r.Targets))
	}
	for i, in := range inputs {
		if in.ObjectID != r.Targets[i].ObjectID || in.RiskScore != r.Targets[i].RiskScore {
			t.Fatalf("risk input %d does not mirror the target", i)
		}
	}
}

func TestStatsAggregate(t *testing.T) {
	r := Build(scenario(), config.Default(), result())
	if r.Stats.Feasible+r.Stats.Infeasible != len(r.Targets) {
		t.Fatalf("feasibility counts do not add up: %+v", r.Stats)
	}
	if r.Stats.TotalMassKg <= r.Stats.FeasibleMassKg {
		t.Fatalf("total mass %v should exceed the feasible mass %v", r.Stats.TotalMassKg, r.Stats.FeasibleMassKg)
	}
	if r.Stats.TopScore != r.Targets[0].RiskScore {
		t.Fatalf("top score %v does not match the leading target %v", r.Stats.TopScore, r.Targets[0].RiskScore)
	}
	if r.Stats.CongestionMedian < 0 {
		t.Fatalf("congestion median %d", r.Stats.CongestionMedian)
	}
	if len(r.Assumptions) == 0 {
		t.Fatal("assumptions must be recorded")
	}
}

func TestMedianOfIntegers(t *testing.T) {
	if got := median(nil); got != 0 {
		t.Fatalf("median(nil) = %d", got)
	}
	if got := median([]int{3, 1, 2}); got != 2 {
		t.Fatalf("odd median = %d", got)
	}
	if got := median([]int{4, 1, 2, 3}); got != 2 {
		t.Fatalf("even median = %d", got)
	}
}

func TestPlaneOffsetDeg(t *testing.T) {
	prop := orbit.NewPropagator(false)
	ch := chaser("chaser-a", 2500, 3)
	target := Target{AltitudeKm: 700, InclinationDeg: 98, RAANDeg: 210}
	if got := PlaneOffsetDeg(prop, ch, target, 0); got > 1e-9 {
		t.Fatalf("identical planes should have no offset, got %v", got)
	}
	target.InclinationDeg = 96
	if got := PlaneOffsetDeg(prop, ch, target, 0); got < 1.9 || got > 2.1 {
		t.Fatalf("a two degree inclination difference gave %v", got)
	}
}
