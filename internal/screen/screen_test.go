package screen

import (
	"math"
	"testing"

	"DebrisLedger/internal/config"
	"DebrisLedger/internal/model"
	"DebrisLedger/internal/orbit"
)

// leaderFollower builds an object whose closest approach against the asset
// happens at wantTCA with a purely radial separation of deltaAltKm.
func leaderFollower(id string, class model.ObjectClass, asset model.Asset, deltaAltKm, wantTCA float64) model.CatalogObject {
	dn := orbit.MeanMotionRadPerS(asset.Orbit.AltitudeKm) -
		orbit.MeanMotionRadPerS(asset.Orbit.AltitudeKm+deltaAltKm)
	o := asset.Orbit
	o.AltitudeKm = asset.Orbit.AltitudeKm + deltaAltKm
	o.ArgLatDeg = orbit.WrapAngleDeg(asset.Orbit.ArgLatDeg + orbit.Deg(dn*wantTCA))
	return model.CatalogObject{
		ID: id, Name: id, Class: class, MassKg: 50, AreaM2: 0.4, Orbit: o,
		TumbleRateDegPerS: 2, AttachPoint: "adapter-ring",
	}
}

func testAsset() model.Asset {
	return model.Asset{
		ID: "asset-1", Name: "Asset One", MassKg: 1000, AreaM2: 9,
		Orbit:       model.Orbit{AltitudeKm: 552, InclinationDeg: 53.1, RAANDeg: 84.5, ArgLatDeg: 12, EpochSeconds: 0},
		Maneuver:    model.ManeuverCapability{DeltaVBudgetMps: 12, ThrusterType: model.ThrusterChemical, MinLeadTimeS: 5400, MaxBurnsPerDay: 4},
		Criticality: 5,
	}
}

func testScenario() *model.Scenario {
	asset := testAsset()
	sc := &model.Scenario{
		Label: "screen-unit", EpochSeconds: 0, HorizonSeconds: 172800,
		Assets: []model.Asset{asset},
		Objects: []model.CatalogObject{
			leaderFollower("frag-close", model.ClassFragment, asset, 0.5, 43200),
			leaderFollower("rb-mid", model.ClassRocketBody, asset, 1.2, 86400),
			leaderFollower("frag-far", model.ClassFragment, asset, 40, 100000),
		},
		ScreeningVolumes: []model.ScreeningVolume{
			{Name: "standard", RadialKm: 2, InTrackKm: 25, CrossTrackKm: 2},
		},
	}
	sc.Sort()
	return sc
}

func TestCombinedHardBodyRadius(t *testing.T) {
	// Two 1 m^2 discs have equivalent radii of 0.5642 m each.
	got := CombinedHardBodyRadiusM(1, 1, 0)
	if math.Abs(got-2*math.Sqrt(1/math.Pi)) > 1e-12 {
		t.Fatalf("combined radius %v", got)
	}
	if withMargin := CombinedHardBodyRadiusM(1, 1, 5); math.Abs(withMargin-got-5) > 1e-12 {
		t.Fatalf("margin not applied: %v", withMargin)
	}
	if CombinedHardBodyRadiusM(-1, -1, -1) != 0 {
		t.Fatal("negative inputs must be clamped to zero")
	}
}

func TestPositionSigmaGrowsWithLeadTime(t *testing.T) {
	cfg := config.Default().Probability
	atEpoch := PositionSigmaKm(cfg, 0)
	afterDay := PositionSigmaKm(cfg, 86400)
	if afterDay <= atEpoch {
		t.Fatalf("sigma must grow with lead time: %v then %v", atEpoch, afterDay)
	}
	if math.Abs(afterDay-(cfg.SigmaBaseKm+cfg.SigmaGrowthKmPerDay)) > 1e-12 {
		t.Fatalf("sigma after one day = %v", afterDay)
	}
	cfg.SigmaBaseKm = 0.01
	cfg.SigmaFloorKm = 0.05
	if got := PositionSigmaKm(cfg, 0); got != 0.05 {
		t.Fatalf("the floor must apply, got %v", got)
	}
	if got := PositionSigmaKm(cfg, -100); got != 0.05 {
		t.Fatalf("negative lead times must clamp, got %v", got)
	}
}

func TestCollisionProbabilityShape(t *testing.T) {
	radius := 10.0
	sigma := 0.25
	atZero := CollisionProbability(0, radius, sigma)
	atHalf := CollisionProbability(0.5, radius, sigma)
	atFar := CollisionProbability(5, radius, sigma)
	if !(atZero > atHalf && atHalf > atFar) {
		t.Fatalf("probability must fall with the miss distance: %g %g %g", atZero, atHalf, atFar)
	}
	if atZero > 1 || atFar < 0 {
		t.Fatalf("probability out of range: %g %g", atZero, atFar)
	}
	bigger := CollisionProbability(0.5, 2*radius, sigma)
	if bigger <= atHalf {
		t.Fatalf("a larger hard body must raise the probability: %g vs %g", bigger, atHalf)
	}
	if CollisionProbability(0.5, radius, 0) != 0 {
		t.Fatal("a zero sigma must yield zero probability")
	}
	if CollisionProbability(0.5, 0, sigma) != 0 {
		t.Fatal("a zero hard-body radius must yield zero probability")
	}
	if CollisionProbability(-0.5, radius, sigma) != atHalf {
		t.Fatal("the sign of the miss distance must not matter")
	}
	// The closed form must match a direct evaluation.
	want := math.Exp(-0.25/(2*sigma*sigma)) * (1 - math.Exp(-(0.01*0.01)/(2*sigma*sigma)))
	if math.Abs(atHalf-want) > 1e-12*want {
		t.Fatalf("closed form mismatch: %g vs %g", atHalf, want)
	}
}

func TestClassifyTakesTheWorseVerdict(t *testing.T) {
	cfg := config.Default().Severity
	if got := Classify(cfg, 1e-12, 100); got != "none" {
		t.Fatalf("got %q", got)
	}
	if got := Classify(cfg, 5e-6, 100); got != "moderate" {
		t.Fatalf("got %q", got)
	}
	if got := Classify(cfg, 1e-7, 100); got != "low" {
		t.Fatalf("got %q", got)
	}
	if got := Classify(cfg, 1e-3, 100); got != "critical" {
		t.Fatalf("got %q", got)
	}
	// A tiny miss distance dominates a diluted probability.
	if got := Classify(cfg, 1e-12, 0.1); got != "critical" {
		t.Fatalf("got %q", got)
	}
	if got := Classify(cfg, 1e-12, 0.5); got != "high" {
		t.Fatalf("got %q", got)
	}
	if got := Classify(cfg, 5e-8, 10); got != "low" {
		t.Fatalf("got %q", got)
	}
}

func TestRunFindsExpectedConjunctions(t *testing.T) {
	sc := testScenario()
	cfg := config.Default()
	res, err := Run(sc, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Stats.PairsScreened != 3 {
		t.Fatalf("screened %d pairs, want 3", res.Stats.PairsScreened)
	}
	if res.Stats.ConjunctionCount != 2 {
		t.Fatalf("expected two conjunctions, got %d", res.Stats.ConjunctionCount)
	}
	first := res.Conjunctions[0]
	if first.ObjectID != "frag-close" {
		t.Fatalf("the closest conjunction should sort first, got %s", first.ObjectID)
	}
	// The radial separation is 0.5 km; the small excess comes from the
	// differential nodal drift of the two orbits over half a day.
	if first.MissKm < 0.5 || first.MissKm > 0.51 {
		t.Fatalf("miss distance %v, want just above 0.5", first.MissKm)
	}
	if first.TCASeconds < 43100 || first.TCASeconds > 43300 {
		t.Fatalf("closest approach at %d s, want about 43200", first.TCASeconds)
	}
	if !first.Actionable || first.Severity != "high" {
		t.Fatalf("expected an actionable high-severity conjunction, got %+v", first)
	}
	if first.VolumeName != "standard" {
		t.Fatalf("volume %q", first.VolumeName)
	}
	if first.CombinedRadiusM <= cfg.Probability.HardBodyMarginM {
		t.Fatalf("combined radius %v should exceed the margin", first.CombinedRadiusM)
	}
	if !first.Diagnostics.Bracketed || first.Diagnostics.CoarseSamples == 0 {
		t.Fatalf("unexpected diagnostics %+v", first.Diagnostics)
	}
	if first.Key() == "" {
		t.Fatal("the conjunction key must not be empty")
	}
}

func TestRunIsDeterministic(t *testing.T) {
	cfg := config.Default()
	first, err := Run(testScenario(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		again, err := Run(testScenario(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		if len(again.Conjunctions) != len(first.Conjunctions) {
			t.Fatalf("run %d produced a different conjunction count", i)
		}
		for j := range again.Conjunctions {
			if again.Conjunctions[j] != first.Conjunctions[j] {
				t.Fatalf("run %d differs at conjunction %d", i, j)
			}
		}
	}
}

func TestRunHonoursSkipListAndCap(t *testing.T) {
	sc := testScenario()
	cfg := config.Default()
	cfg.Screening.SkipSameObjectClasses = []string{"fragment"}
	res, err := Run(sc, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if res.Stats.PairsSkipped != 2 {
		t.Fatalf("skipped %d pairs, want 2", res.Stats.PairsSkipped)
	}
	for _, c := range res.Conjunctions {
		if c.ObjectClass == model.ClassFragment {
			t.Fatal("fragments should have been skipped")
		}
	}

	cfg = config.Default()
	cfg.Screening.MaxConjunctionsPerAsset = 1
	res, err = Run(sc, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Conjunctions) != 1 {
		t.Fatalf("cap not applied, got %d conjunctions", len(res.Conjunctions))
	}
	if len(res.Stats.TruncatedAssets) != 1 {
		t.Fatalf("truncation should be reported, got %v", res.Stats.TruncatedAssets)
	}
}

func TestRunRejectsScenarioWithoutAssets(t *testing.T) {
	sc := testScenario()
	sc.Assets = nil
	if _, err := Run(sc, config.Default()); err == nil {
		t.Fatal("expected an error for a scenario with no assets")
	}
}

func TestResultHelpers(t *testing.T) {
	res, err := Run(testScenario(), config.Default())
	if err != nil {
		t.Fatal(err)
	}
	grouped := res.ByAsset()
	if len(grouped["asset-1"]) != len(res.Conjunctions) {
		t.Fatalf("grouping lost rows: %d", len(grouped["asset-1"]))
	}
	ids := res.AssetIDs()
	if len(ids) != 1 || ids[0] != "asset-1" {
		t.Fatalf("asset ids %v", ids)
	}
	byObject := res.ProbabilityByObject()
	if len(byObject) != len(res.Conjunctions) {
		t.Fatalf("probability map has %d entries", len(byObject))
	}
	for id, p := range byObject {
		if p <= 0 {
			t.Fatalf("object %s has probability %g", id, p)
		}
	}
	if len(res.Assumptions) == 0 {
		t.Fatal("assumptions must be recorded with every result")
	}
	if len(res.Stats.BySeverity) != len(config.SeverityLabels()) {
		t.Fatalf("histogram should cover every label, got %d rows", len(res.Stats.BySeverity))
	}
}

func TestCongestionCountsNeighbours(t *testing.T) {
	cfg := config.Default().Ranking
	sc := &model.Scenario{
		Objects: []model.CatalogObject{
			{ID: "a", Orbit: model.Orbit{AltitudeKm: 550, InclinationDeg: 53}},
			{ID: "b", Orbit: model.Orbit{AltitudeKm: 560, InclinationDeg: 53.5}},
			{ID: "c", Orbit: model.Orbit{AltitudeKm: 900, InclinationDeg: 53}},
			{ID: "d", Orbit: model.Orbit{AltitudeKm: 552, InclinationDeg: 80}},
		},
	}
	got := Congestion(sc, cfg)
	if got["a"] != 1 || got["b"] != 1 {
		t.Fatalf("neighbour counts: %v", got)
	}
	if got["c"] != 0 || got["d"] != 0 {
		t.Fatalf("distant objects should have no neighbours: %v", got)
	}
}

func TestProbabilityAssumptionsAreStable(t *testing.T) {
	first := ProbabilityAssumptions()
	second := ProbabilityAssumptions()
	if len(first) == 0 {
		t.Fatal("assumptions must not be empty")
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatal("assumption text must be stable")
		}
	}
}
