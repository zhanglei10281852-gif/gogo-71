package maneuver

import (
	"math"
	"testing"

	"DebrisLedger/internal/config"
	"DebrisLedger/internal/model"
	"DebrisLedger/internal/screen"
)

func asset(id string, budget float64, thruster model.ThrusterType, lead int64, burns int) model.Asset {
	return model.Asset{
		ID: id, Name: id, MassKg: 1000, AreaM2: 9,
		Orbit: model.Orbit{AltitudeKm: 552, InclinationDeg: 53.1, RAANDeg: 84.5, ArgLatDeg: 12, EpochSeconds: 0},
		Maneuver: model.ManeuverCapability{
			DeltaVBudgetMps: budget, ThrusterType: thruster, MinLeadTimeS: lead, MaxBurnsPerDay: burns,
		},
		Criticality: 5,
	}
}

func conjunction(assetID, objectID string, tca int64, miss, radial, inTrack, cross float64) screen.Conjunction {
	return screen.Conjunction{
		AssetID: assetID, AssetName: assetID, ObjectID: objectID, ObjectName: objectID,
		ObjectClass: model.ClassFragment, TCASeconds: tca, LeadSeconds: tca,
		MissKm: miss, RadialKm: radial, InTrackKm: inTrack, CrossTrackKm: cross,
		RelSpeedKmS: 0.0003, CombinedRadiusM: 7.5, SigmaKm: 0.4,
		Probability: 6e-5, Severity: "high", SeverityRank: config.SeverityRank("high"), Actionable: true,
		VolumeName: "standard", InsideVolume: true,
	}
}

func scenarioWith(assets ...model.Asset) *model.Scenario {
	return &model.Scenario{Label: "maneuver-unit", EpochSeconds: 0, HorizonSeconds: 259200, Assets: assets}
}

func resultWith(list ...screen.Conjunction) screen.Result {
	return screen.Result{
		ScenarioLabel: "maneuver-unit", ConfigLabel: "unit", EpochSeconds: 0, HorizonSeconds: 259200,
		Conjunctions: list,
	}
}

func TestBurnRaisesMissAboveThreshold(t *testing.T) {
	cfg := config.Default()
	a := asset("asset-1", 12, model.ThrusterChemical, 5400, 4)
	c := conjunction("asset-1", "frag-1", 43200, 0.5, 0.5, 0, 0)
	plan := Build(scenarioWith(a), cfg, resultWith(c))
	if len(plan.Burns) != 1 {
		t.Fatalf("expected one burn, got %d", len(plan.Burns))
	}
	b := plan.Burns[0]
	if b.Status != StatusScheduled {
		t.Fatalf("status %q: %s", b.Status, b.Reason)
	}
	if b.PredictedMissKm < cfg.Screening.RequiredMissDistanceKm {
		t.Fatalf("predicted miss %v does not reach the required %v",
			b.PredictedMissKm, cfg.Screening.RequiredMissDistanceKm)
	}
	if b.PredictedProbability >= b.OriginalProbability {
		t.Fatalf("probability must fall: %g then %g", b.OriginalProbability, b.PredictedProbability)
	}
	// The required shift solves sqrt(radial^2 + shift^2) = required.
	wantShift := math.Sqrt(cfg.Screening.RequiredMissDistanceKm*cfg.Screening.RequiredMissDistanceKm - 0.25)
	if math.Abs(b.RequiredShiftKm-wantShift) > 1e-3 {
		t.Fatalf("required shift %v, want %v", b.RequiredShiftKm, wantShift)
	}
	if b.LeadSeconds != cfg.Maneuver.PlanningLeadSeconds {
		t.Fatalf("lead %d, want %d", b.LeadSeconds, cfg.Maneuver.PlanningLeadSeconds)
	}
	if b.BurnSeconds != c.TCASeconds-b.LeadSeconds {
		t.Fatalf("burn time %d", b.BurnSeconds)
	}
	if b.DeltaVMps <= 0 || b.DeltaVMps > cfg.Maneuver.MaxDeltaVPerBurnMps {
		t.Fatalf("delta-v %v out of range", b.DeltaVMps)
	}
	if b.SemiMajorAxisDeltaKm <= 0 {
		t.Fatalf("semi-major axis change %v", b.SemiMajorAxisDeltaKm)
	}
}

func TestBurnDirectionFollowsInTrackSign(t *testing.T) {
	cfg := config.Default()
	a := asset("asset-1", 12, model.ThrusterChemical, 5400, 4)
	forward := Build(scenarioWith(a), cfg, resultWith(conjunction("asset-1", "frag-1", 43200, 0.5, 0.3, 0.4, 0)))
	if forward.Burns[0].Direction != "prograde" {
		t.Fatalf("positive in-track should burn prograde, got %q", forward.Burns[0].Direction)
	}
	backward := Build(scenarioWith(a), cfg, resultWith(conjunction("asset-1", "frag-1", 43200, 0.5, 0.3, -0.4, 0)))
	if backward.Burns[0].Direction != "retrograde" {
		t.Fatalf("negative in-track should burn retrograde, got %q", backward.Burns[0].Direction)
	}
	if math.Abs(forward.Burns[0].DeltaVMps-backward.Burns[0].DeltaVMps) > 1e-12 {
		t.Fatal("the two directions must cost the same")
	}
}

func TestExistingInTrackSeparationReducesTheBurn(t *testing.T) {
	cfg := config.Default()
	a := asset("asset-1", 12, model.ThrusterChemical, 5400, 4)
	head := Build(scenarioWith(a), cfg, resultWith(conjunction("asset-1", "frag-1", 43200, 0.5, 0.5, 0, 0)))
	partly := Build(scenarioWith(a), cfg, resultWith(conjunction("asset-1", "frag-1", 43200, 1.1, 0.5, 1.0, 0)))
	if partly.Burns[0].DeltaVMps >= head.Burns[0].DeltaVMps {
		t.Fatalf("an existing in-track offset must reduce the burn: %v vs %v",
			partly.Burns[0].DeltaVMps, head.Burns[0].DeltaVMps)
	}
}

func TestConflictingWindowsAreDetected(t *testing.T) {
	cfg := config.Default()
	a := asset("asset-1", 12, model.ThrusterChemical, 5400, 4)
	res := resultWith(
		conjunction("asset-1", "frag-1", 43200, 0.5, 0.5, 0, 0),
		conjunction("asset-1", "frag-2", 46800, 0.4, 0.4, 0, 0),
	)
	plan := Build(scenarioWith(a), cfg, res)
	if plan.Stats.Scheduled != 1 || plan.Stats.Conflicts != 1 {
		t.Fatalf("expected one scheduled and one conflict, got %+v", plan.Stats)
	}
	var conflicted Burn
	for _, b := range plan.Burns {
		if b.Status == StatusConflict {
			conflicted = b
		}
	}
	if conflicted.ObjectID != "frag-2" {
		t.Fatalf("the later conjunction should lose, got %q", conflicted.ObjectID)
	}
	if len(conflicted.ConflictsWith) != 1 {
		t.Fatalf("conflict references %v", conflicted.ConflictsWith)
	}
	// Widely separated windows must not conflict.
	spread := Build(scenarioWith(a), cfg, resultWith(
		conjunction("asset-1", "frag-1", 43200, 0.5, 0.5, 0, 0),
		conjunction("asset-1", "frag-2", 150000, 0.4, 0.4, 0, 0),
	))
	if spread.Stats.Conflicts != 0 || spread.Stats.Scheduled != 2 {
		t.Fatalf("unexpected conflict: %+v", spread.Stats)
	}
}

func TestBudgetIsRespected(t *testing.T) {
	cfg := config.Default()
	a := asset("asset-1", 0.05, model.ThrusterChemical, 5400, 4)
	plan := Build(scenarioWith(a), cfg, resultWith(conjunction("asset-1", "frag-1", 43200, 0.5, 0.5, 0, 0)))
	b := plan.Burns[0]
	if b.Status != StatusOverBudget {
		t.Fatalf("status %q, want %q", b.Status, StatusOverBudget)
	}
	if plan.Assets[0].PlannedMps != 0 {
		t.Fatalf("nothing should have been charged, got %v", plan.Assets[0].PlannedMps)
	}
	if plan.Assets[0].RemainingMps != plan.Assets[0].BudgetMps {
		t.Fatal("the whole budget must remain")
	}
}

func TestBurnAllowanceIsRespected(t *testing.T) {
	cfg := config.Default()
	cfg.Maneuver.ConflictGuardSeconds = 0
	a := asset("asset-1", 50, model.ThrusterColdGas, 900, 1)
	plan := Build(scenarioWith(a), cfg, resultWith(
		conjunction("asset-1", "frag-1", 43200, 0.5, 0.5, 0, 0),
		conjunction("asset-1", "frag-2", 72000, 0.4, 0.4, 0, 0),
	))
	statuses := map[string]int{}
	for _, b := range plan.Burns {
		statuses[b.Status]++
	}
	if statuses[StatusScheduled] != 1 || statuses[StatusAllowance] != 1 {
		t.Fatalf("expected one scheduled and one allowance rejection, got %v", statuses)
	}
}

func TestInsufficientLeadTimeIsRejected(t *testing.T) {
	cfg := config.Default()
	a := asset("asset-1", 12, model.ThrusterElectric, 21600, 4)
	plan := Build(scenarioWith(a), cfg, resultWith(conjunction("asset-1", "frag-1", 3000, 0.5, 0.5, 0, 0)))
	if plan.Burns[0].Status != StatusNoLeadTime {
		t.Fatalf("status %q, want %q", plan.Burns[0].Status, StatusNoLeadTime)
	}
	if plan.Burns[0].LeadSeconds != model.ThrusterElectric.MinBurnLeadSeconds() {
		t.Fatalf("electric propulsion should use its own floor, got %d", plan.Burns[0].LeadSeconds)
	}
}

func TestPerBurnCapIsRespected(t *testing.T) {
	cfg := config.Default()
	cfg.Maneuver.MaxDeltaVPerBurnMps = 0.001
	a := asset("asset-1", 500, model.ThrusterChemical, 5400, 4)
	plan := Build(scenarioWith(a), cfg, resultWith(conjunction("asset-1", "frag-1", 43200, 0.5, 0.5, 0, 0)))
	if plan.Burns[0].Status != StatusOverPerBurn {
		t.Fatalf("status %q, want %q", plan.Burns[0].Status, StatusOverPerBurn)
	}
}

func TestAlreadySafeConjunctionNeedsNoBurn(t *testing.T) {
	cfg := config.Default()
	a := asset("asset-1", 12, model.ThrusterChemical, 5400, 4)
	c := conjunction("asset-1", "frag-1", 43200, 3.0, 0.2, 2.99, 0)
	plan := Build(scenarioWith(a), cfg, resultWith(c))
	if plan.Burns[0].Status != StatusNotRequired {
		t.Fatalf("status %q, want %q", plan.Burns[0].Status, StatusNotRequired)
	}
}

func TestNonActionableConjunctionsAreIgnored(t *testing.T) {
	cfg := config.Default()
	a := asset("asset-1", 12, model.ThrusterChemical, 5400, 4)
	c := conjunction("asset-1", "frag-1", 43200, 0.5, 0.5, 0, 0)
	c.Actionable = false
	c.Severity = "low"
	c.SeverityRank = config.SeverityRank("low")
	plan := Build(scenarioWith(a), cfg, resultWith(c))
	if len(plan.Burns) != 0 {
		t.Fatalf("expected no candidates, got %d", len(plan.Burns))
	}
	if plan.Stats.Candidates != 0 {
		t.Fatalf("candidate count %d", plan.Stats.Candidates)
	}
}

func TestScheduleIsSortedAndAccounted(t *testing.T) {
	cfg := config.Default()
	plan := Build(
		scenarioWith(
			asset("asset-b", 12, model.ThrusterChemical, 5400, 4),
			asset("asset-a", 12, model.ThrusterChemical, 5400, 4),
		),
		cfg,
		resultWith(
			conjunction("asset-b", "frag-2", 120000, 0.5, 0.5, 0, 0),
			conjunction("asset-a", "frag-1", 43200, 0.5, 0.5, 0, 0),
		),
	)
	if len(plan.Burns) != 2 {
		t.Fatalf("expected two burns, got %d", len(plan.Burns))
	}
	if plan.Burns[0].BurnSeconds > plan.Burns[1].BurnSeconds {
		t.Fatal("burns must be sorted by burn time")
	}
	if plan.Assets[0].AssetID != "asset-a" || plan.Assets[1].AssetID != "asset-b" {
		t.Fatalf("asset summaries not sorted: %v", plan.Assets)
	}
	total := 0.0
	for _, b := range plan.Scheduled() {
		total += b.DeltaVMps
	}
	if math.Abs(total-plan.Stats.TotalDeltaVMps) > 1e-9 {
		t.Fatalf("total delta-v %v does not match the sum %v", plan.Stats.TotalDeltaVMps, total)
	}
	if plan.Stats.ProbabilityReduction <= 0 {
		t.Fatalf("probability reduction %g", plan.Stats.ProbabilityReduction)
	}
	for _, s := range plan.Assets {
		if s.UtilizationPct < 0 || s.UtilizationPct > 100 {
			t.Fatalf("utilisation %v out of range", s.UtilizationPct)
		}
	}
}

func TestQuantiseRoundsUpToResolution(t *testing.T) {
	if got := quantise(0.0011, 0.001); math.Abs(got-0.002) > 1e-12 {
		t.Fatalf("quantise = %v", got)
	}
	if got := quantise(0.002, 0.001); math.Abs(got-0.002) > 1e-12 {
		t.Fatalf("an exact multiple must be preserved, got %v", got)
	}
	if got := quantise(0, 0.001); math.Abs(got-0.001) > 1e-12 {
		t.Fatalf("a zero request must still command one increment, got %v", got)
	}
	if got := quantise(0.5, 0); got != 0.5 {
		t.Fatalf("a zero resolution must pass through, got %v", got)
	}
}

func TestWindowAndAssumptions(t *testing.T) {
	b := Burn{BurnSeconds: 1000, TCASeconds: 5000}
	lo, hi := b.Window(100)
	if lo != 900 || hi != 5100 {
		t.Fatalf("window (%d,%d)", lo, hi)
	}
	if len(planAssumptions()) == 0 {
		t.Fatal("assumptions must be documented")
	}
}

func TestAllowanceRejectsZeroAllowance(t *testing.T) {
	if allowanceOK(nil, Burn{}, 0) {
		t.Fatal("a zero allowance must reject every burn")
	}
	if !allowanceOK(nil, Burn{BurnSeconds: 0}, 1) {
		t.Fatal("the first burn must fit inside an allowance of one")
	}
}
