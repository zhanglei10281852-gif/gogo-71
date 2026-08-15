package model

import (
	"strings"
	"testing"
)

func validObject(id string) CatalogObject {
	return CatalogObject{
		ID: id, Name: "object " + id, Class: ClassFragment, MassKg: 12, AreaM2: 0.3,
		Orbit:             Orbit{AltitudeKm: 550, InclinationDeg: 53, RAANDeg: 84, ArgLatDeg: 10, EpochSeconds: 0},
		TumbleRateDegPerS: 4, AttachPoint: "none",
	}
}

func validAsset(id string) Asset {
	return Asset{
		ID: id, Name: "asset " + id, MassKg: 900, AreaM2: 7,
		Orbit:       Orbit{AltitudeKm: 552, InclinationDeg: 53, RAANDeg: 84, ArgLatDeg: 12, EpochSeconds: 0},
		Maneuver:    ManeuverCapability{DeltaVBudgetMps: 10, ThrusterType: ThrusterChemical, MinLeadTimeS: 3600, MaxBurnsPerDay: 4},
		Criticality: 4,
	}
}

func validChaser(id string) Chaser {
	return Chaser{
		ID: id, Name: "chaser " + id, DryMassKg: 1200, DeltaVBudgetMps: 800, CaptureSlots: 2,
		DockingTimeS: 3600, MaxCaptureMassKg: 2000, MaxTumbleRateDegPerS: 3,
		Orbit: Orbit{AltitudeKm: 700, InclinationDeg: 98, RAANDeg: 210, ArgLatDeg: 0, EpochSeconds: 0},
	}
}

func validScenario() *Scenario {
	return &Scenario{
		Label: "unit", EpochSeconds: 0, HorizonSeconds: 86400,
		Objects: []CatalogObject{validObject("obj-1")},
		Assets:  []Asset{validAsset("asset-1")},
		Chasers: []Chaser{validChaser("chaser-1")},
		ScreeningVolumes: []ScreeningVolume{
			{Name: "standard", RadialKm: 2, InTrackKm: 25, CrossTrackKm: 2},
		},
	}
}

func TestValidScenarioPasses(t *testing.T) {
	if err := validScenario().Validate(); err != nil {
		t.Fatalf("expected the reference scenario to validate: %v", err)
	}
}

func TestDuplicateIdentifiersAreRejected(t *testing.T) {
	sc := validScenario()
	sc.Objects = append(sc.Objects, validObject("obj-1"))
	err := sc.Validate()
	if err == nil || !strings.Contains(err.Error(), "duplicate identifier") {
		t.Fatalf("expected a duplicate-identifier error, got %v", err)
	}

	sc = validScenario()
	sc.Chasers = append(sc.Chasers, validChaser("asset-1"))
	if err := sc.Validate(); err == nil {
		t.Fatal("identifiers must be unique across entity kinds")
	}
}

func TestDuplicateVolumeNamesAreRejected(t *testing.T) {
	sc := validScenario()
	sc.ScreeningVolumes = append(sc.ScreeningVolumes, sc.ScreeningVolumes[0])
	err := sc.Validate()
	if err == nil || !strings.Contains(err.Error(), "duplicate screening volume") {
		t.Fatalf("expected a duplicate-volume error, got %v", err)
	}
}

func TestOrbitBoundsAreEnforced(t *testing.T) {
	cases := map[string]Orbit{
		"altitude too low":  {AltitudeKm: 10, InclinationDeg: 53, RAANDeg: 0, ArgLatDeg: 0},
		"altitude too high": {AltitudeKm: 99999, InclinationDeg: 53, RAANDeg: 0, ArgLatDeg: 0},
		"inclination":       {AltitudeKm: 550, InclinationDeg: 200, RAANDeg: 0, ArgLatDeg: 0},
		"raan":              {AltitudeKm: 550, InclinationDeg: 53, RAANDeg: 360, ArgLatDeg: 0},
		"arg lat":           {AltitudeKm: 550, InclinationDeg: 53, RAANDeg: 0, ArgLatDeg: -1},
		"epoch":             {AltitudeKm: 550, InclinationDeg: 53, RAANDeg: 0, ArgLatDeg: 0, EpochSeconds: -5},
	}
	for name, o := range cases {
		if issues := o.Validate("orbit"); len(issues) == 0 {
			t.Fatalf("%s: expected an issue", name)
		}
	}
}

func TestIdentifierCharactersAreChecked(t *testing.T) {
	sc := validScenario()
	sc.Objects[0].ID = "bad id!"
	err := sc.Validate()
	if err == nil || !strings.Contains(err.Error(), "unsupported rune") {
		t.Fatalf("expected an identifier error, got %v", err)
	}
	sc = validScenario()
	sc.Objects[0].ID = strings.Repeat("x", 65)
	if err := sc.Validate(); err == nil {
		t.Fatal("over-long identifiers must be rejected")
	}
}

func TestEpochsMustNotPrecedeScenarioEpoch(t *testing.T) {
	sc := validScenario()
	sc.EpochSeconds = 100
	sc.Objects[0].Orbit.EpochSeconds = 50
	err := sc.Validate()
	if err == nil || !strings.Contains(err.Error(), "precedes scenario epoch") {
		t.Fatalf("expected an epoch error, got %v", err)
	}
}

func TestLeadTimeMustFitInsideHorizon(t *testing.T) {
	sc := validScenario()
	sc.HorizonSeconds = 3600
	sc.Assets[0].Maneuver.MinLeadTimeS = 7200
	err := sc.Validate()
	if err == nil || !strings.Contains(err.Error(), "leaves no room") {
		t.Fatalf("expected a lead-time error, got %v", err)
	}
}

func TestThrusterFloorsAndEffectiveLead(t *testing.T) {
	electric := ManeuverCapability{ThrusterType: ThrusterElectric, MinLeadTimeS: 60}
	if got := electric.EffectiveLeadSeconds(); got != ThrusterElectric.MinBurnLeadSeconds() {
		t.Fatalf("electric lead %d, want the thruster floor", got)
	}
	coldGas := ManeuverCapability{ThrusterType: ThrusterColdGas, MinLeadTimeS: 9000}
	if got := coldGas.EffectiveLeadSeconds(); got != 9000 {
		t.Fatalf("declared lead should win when larger, got %d", got)
	}
	unknown := ThrusterType("ion-hybrid")
	if unknown.Valid() {
		t.Fatal("unknown thruster types must not validate")
	}
	if unknown.MinBurnLeadSeconds() != ThrusterChemical.MinBurnLeadSeconds() {
		t.Fatal("unknown thruster types should fall back to the chemical floor")
	}
}

func TestClassHelpers(t *testing.T) {
	if !ClassRocketBody.DebrisCandidate() || !ClassFragment.DebrisCandidate() {
		t.Fatal("rocket bodies and fragments are removal candidates")
	}
	if ClassPayload.DebrisCandidate() || ClassUnknown.DebrisCandidate() {
		t.Fatal("payloads and unknown objects are not removal candidates")
	}
	if ObjectClass("station").Valid() {
		t.Fatal("unknown classes must not validate")
	}
	if len(ObjectClasses()) != 4 {
		t.Fatalf("unexpected class count %d", len(ObjectClasses()))
	}
	if len(ThrusterTypes()) != 3 {
		t.Fatalf("unexpected thruster count %d", len(ThrusterTypes()))
	}
}

func TestAreaToMassAndAttachPoint(t *testing.T) {
	obj := validObject("obj-1")
	obj.MassKg = 10
	obj.AreaM2 = 2
	if got := obj.AreaToMass(); got != 0.2 {
		t.Fatalf("area to mass %v", got)
	}
	obj.MassKg = 0
	if got := obj.AreaToMass(); got != 0 {
		t.Fatalf("massless object should report zero, got %v", got)
	}
	if obj.HasAttachPoint() {
		t.Fatal("attach point \"none\" is not usable")
	}
	obj.AttachPoint = "adapter-ring"
	if !obj.HasAttachPoint() {
		t.Fatal("a named attach point is usable")
	}
	obj.AttachPoint = ""
	if obj.HasAttachPoint() {
		t.Fatal("an empty attach point is not usable")
	}
}

func TestSortMakesIterationDeterministic(t *testing.T) {
	sc := validScenario()
	sc.Objects = []CatalogObject{validObject("obj-9"), validObject("obj-1"), validObject("obj-5")}
	sc.Sort()
	if sc.Objects[0].ID != "obj-1" || sc.Objects[2].ID != "obj-9" {
		t.Fatalf("objects not sorted: %v %v %v", sc.Objects[0].ID, sc.Objects[1].ID, sc.Objects[2].ID)
	}
}

func TestLookupsAndVolumeSelection(t *testing.T) {
	sc := validScenario()
	sc.ScreeningVolumes = append(sc.ScreeningVolumes, ScreeningVolume{
		Name: "fragment-tight", RadialKm: 1, InTrackKm: 12, CrossTrackKm: 1, AppliesToClass: "fragment",
	})
	if _, ok := sc.ObjectByID("obj-1"); !ok {
		t.Fatal("expected to find obj-1")
	}
	if _, ok := sc.ObjectByID("nope"); ok {
		t.Fatal("did not expect to find a missing object")
	}
	if _, ok := sc.AssetByID("asset-1"); !ok {
		t.Fatal("expected to find asset-1")
	}
	if _, ok := sc.ChaserByID("chaser-1"); !ok {
		t.Fatal("expected to find chaser-1")
	}
	if _, ok := sc.AssetByID("x"); ok {
		t.Fatal("did not expect to find a missing asset")
	}
	if _, ok := sc.ChaserByID("x"); ok {
		t.Fatal("did not expect to find a missing chaser")
	}
	if got := sc.VolumeFor(ClassFragment); got.Name != "fragment-tight" {
		t.Fatalf("fragments should use the tightest volume, got %q", got.Name)
	}
	if got := sc.VolumeFor(ClassPayload); got.Name != "standard" {
		t.Fatalf("payloads should use the general volume, got %q", got.Name)
	}
	empty := &Scenario{}
	if got := empty.VolumeFor(ClassFragment); got.Name != "default" {
		t.Fatalf("a scenario without volumes should fall back to the default, got %q", got.Name)
	}
	if sc.EndSeconds() != sc.EpochSeconds+sc.HorizonSeconds {
		t.Fatal("EndSeconds must be the epoch plus the horizon")
	}
}

func TestScreeningVolumeValidation(t *testing.T) {
	bad := ScreeningVolume{Name: "", RadialKm: 0, InTrackKm: 0, CrossTrackKm: 0, AppliesToClass: "moon"}
	issues := bad.Validate("volume")
	if len(issues) < 5 {
		t.Fatalf("expected at least five issues, got %d", len(issues))
	}
}

func TestIssuesErrorFormatting(t *testing.T) {
	var is Issues
	if is.Err() != nil {
		t.Fatal("an empty issue list is not an error")
	}
	is.Add("b.field", "second")
	is.Add("a.field", "first")
	err := is.Err()
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "2 validation issue(s)") {
		t.Fatalf("unexpected message: %s", msg)
	}
	if strings.Index(msg, "a.field") > strings.Index(msg, "b.field") {
		t.Fatalf("issues must be sorted by path: %s", msg)
	}
}

func TestScenarioLevelBoundsAreChecked(t *testing.T) {
	sc := validScenario()
	sc.Label = " "
	sc.HorizonSeconds = 10
	sc.Objects = nil
	sc.Assets = nil
	err := sc.Validate()
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, want := range []string{"label", "horizon_s", "objects", "assets"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error should mention %s: %v", want, err)
		}
	}
}

func TestAssetAndChaserBoundsAreChecked(t *testing.T) {
	asset := validAsset("a")
	asset.Criticality = 9
	asset.MassKg = -1
	asset.AreaM2 = 0
	if issues := asset.Validate("assets[0]"); len(issues) < 3 {
		t.Fatalf("expected at least three issues, got %+v", issues)
	}
	chaser := validChaser("c")
	chaser.CaptureSlots = 0
	chaser.DeltaVBudgetMps = 0
	chaser.MaxCaptureMassKg = 0
	chaser.MaxTumbleRateDegPerS = 900
	if issues := chaser.Validate("chasers[0]"); len(issues) < 4 {
		t.Fatalf("expected at least four issues, got %+v", issues)
	}
}
