package report

import (
	"strings"
	"testing"

	"DebrisLedger/internal/compliance"
	"DebrisLedger/internal/config"
	"DebrisLedger/internal/maneuver"
	"DebrisLedger/internal/mission"
	"DebrisLedger/internal/model"
	"DebrisLedger/internal/rank"
	"DebrisLedger/internal/screen"
	"DebrisLedger/internal/store"
)

func sampleScenario() *model.Scenario {
	sc := &model.Scenario{
		Label: "report-unit", EpochSeconds: 0, HorizonSeconds: 86400,
		Objects: []model.CatalogObject{{
			ID: "rb-1", Name: "Body one", Class: model.ClassRocketBody, MassKg: 1400, AreaM2: 12,
			Orbit:             model.Orbit{AltitudeKm: 705, InclinationDeg: 98, RAANDeg: 210, ArgLatDeg: 20},
			TumbleRateDegPerS: 1, AttachPoint: "adapter-ring",
		}},
		Assets: []model.Asset{{
			ID: "asset-1", Name: "Asset one", MassKg: 900, AreaM2: 7,
			Orbit:       model.Orbit{AltitudeKm: 706, InclinationDeg: 98, RAANDeg: 210, ArgLatDeg: 21},
			Maneuver:    model.ManeuverCapability{DeltaVBudgetMps: 10, ThrusterType: model.ThrusterElectric, MinLeadTimeS: 21600, MaxBurnsPerDay: 2},
			Criticality: 4,
		}},
		Chasers: []model.Chaser{{
			ID: "chaser-1", Name: "Chaser one", DryMassKg: 1200, DeltaVBudgetMps: 700, CaptureSlots: 1,
			DockingTimeS: 3600, MaxCaptureMassKg: 2000, MaxTumbleRateDegPerS: 3,
			Orbit: model.Orbit{AltitudeKm: 700, InclinationDeg: 98, RAANDeg: 210},
		}},
		ScreeningVolumes: []model.ScreeningVolume{
			{Name: "standard", RadialKm: 2, InTrackKm: 25, CrossTrackKm: 2},
			{Name: "fragment-tight", RadialKm: 1, InTrackKm: 12, CrossTrackKm: 1, AppliesToClass: "fragment"},
		},
	}
	sc.Sort()
	return sc
}

func TestValidationTextContainsEverySection(t *testing.T) {
	text := ValidationText(sampleScenario(), config.Default())
	for _, want := range []string{Banner, "validation", "catalogue", "assets", "chasers", "screening volumes",
		"rb-1", "asset-1", "chaser-1", "(all classes)"} {
		if !strings.Contains(text, want) {
			t.Fatalf("validation text is missing %q\n%s", want, text)
		}
	}
	if !strings.HasSuffix(text, "\n") {
		t.Fatal("text output must end with a newline")
	}
}

func TestScreeningTextIsStable(t *testing.T) {
	cfg := config.Default()
	res, err := screen.Run(sampleScenario(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	first := ScreeningText(res, cfg)
	for i := 0; i < 3; i++ {
		if again := ScreeningText(res, cfg); again != first {
			t.Fatalf("render %d differs from the first", i)
		}
	}
	for _, want := range []string{"conjunction screening", "severity histogram", "model assumptions"} {
		if !strings.Contains(first, want) {
			t.Fatalf("screening text is missing %q", want)
		}
	}
	cfg.Output.IncludeAssumptions = false
	if strings.Contains(ScreeningText(res, cfg), "model assumptions") {
		t.Fatal("assumptions must be suppressible")
	}
}

func TestManeuverTextShowsRejections(t *testing.T) {
	cfg := config.Default()
	sc := sampleScenario()
	res, err := screen.Run(sc, cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Force an actionable conjunction so the schedule is not empty.
	if len(res.Conjunctions) == 0 {
		res.Conjunctions = []screen.Conjunction{{
			AssetID: "asset-1", ObjectID: "rb-1", ObjectClass: model.ClassRocketBody,
			TCASeconds: 3000, LeadSeconds: 3000, MissKm: 0.4, RadialKm: 0.4,
			CombinedRadiusM: 8, SigmaKm: 0.3, Probability: 5e-5,
			Severity: "high", SeverityRank: config.SeverityRank("high"), Actionable: true,
			VolumeName: "standard",
		}}
	}
	plan := maneuver.Build(sc, cfg, res)
	text := ManeuverText(plan, cfg)
	for _, want := range []string{"collision-avoidance manoeuvre schedule", "per-asset budget", "burns"} {
		if !strings.Contains(text, want) {
			t.Fatalf("manoeuvre text is missing %q", want)
		}
	}
	if plan.Stats.Rejected > 0 && !strings.Contains(text, "rejected burns") {
		t.Fatal("rejected burns must be listed")
	}
}

func TestRankingAndMissionText(t *testing.T) {
	cfg := config.Default()
	sc := sampleScenario()
	res, err := screen.Run(sc, cfg)
	if err != nil {
		t.Fatal(err)
	}
	ranking := rank.Build(sc, cfg, res)
	rankingText := RankingText(ranking, cfg)
	for _, want := range []string{"debris-removal target ranking", "ranked targets", "rb-1"} {
		if !strings.Contains(rankingText, want) {
			t.Fatalf("ranking text is missing %q", want)
		}
	}
	campaign := mission.Build(sc, cfg, ranking)
	missionText := MissionText(campaign, cfg)
	for _, want := range []string{"active-removal mission plan", "mission chaser-1", "timeline",
		"residual risk", "compliance findings"} {
		if !strings.Contains(missionText, want) {
			t.Fatalf("mission text is missing %q", want)
		}
	}
}

func TestVerifyTextRendersFindings(t *testing.T) {
	v := store.Verification{
		Root: "/tmp/store", OK: false, MetaOK: true, CatalogueOK: true,
		Audit: store.AuditVerification{
			Entries: 2, OK: false, HeadHash: strings.Repeat("a", 64),
			Failures:   []string{"entry 2 hash mismatch"},
			KindCounts: []store.KindCount{{Kind: "ingest", Count: 1}, {Kind: "screening", Count: 1}},
		},
		Snapshots: []store.SnapshotCheck{{Name: "screening", SHA256: strings.Repeat("b", 64), Bytes: 12, InMeta: true}},
		Findings:  []string{"entry 2 hash mismatch"},
	}
	text := VerifyText(v)
	for _, want := range []string{"store verification", "fail", "audit kinds", "snapshots", "findings",
		"entry 2 hash mismatch"} {
		if !strings.Contains(text, want) {
			t.Fatalf("verify text is missing %q\n%s", want, text)
		}
	}
}

func TestBuildCombinedSummarises(t *testing.T) {
	cfg := config.Default()
	sc := sampleScenario()
	res, err := screen.Run(sc, cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan := maneuver.Build(sc, cfg, res)
	ranking := rank.Build(sc, cfg, res)
	campaign := mission.Build(sc, cfg, ranking)
	verification := store.Verification{OK: true, Audit: store.AuditVerification{OK: true, Entries: 4}}
	meta := store.Meta{
		ScenarioLabel: sc.Label, ConfigLabel: cfg.Label, EpochSeconds: sc.EpochSeconds,
		Counts:       store.Counts{Objects: 1, Assets: 1, Chasers: 1},
		AuditEntries: 4,
	}
	combined := BuildCombined(cfg, meta, &res, &plan, &ranking, &campaign, &verification)
	if combined.Tool != ToolName {
		t.Fatalf("tool %q", combined.Tool)
	}
	if combined.Summary.CatalogueObjects != 1 || combined.Summary.Assets != 1 {
		t.Fatalf("summary counts %+v", combined.Summary)
	}
	if combined.Summary.RankedTargets != len(ranking.Targets) {
		t.Fatalf("ranked targets %d", combined.Summary.RankedTargets)
	}
	if combined.Summary.RemovedTargets != len(campaign.RemovedIDs()) {
		t.Fatalf("removed targets %d", combined.Summary.RemovedTargets)
	}
	if !combined.Summary.AuditOK {
		t.Fatal("audit status must be carried into the summary")
	}
	if len(combined.Disclaimer) != len(Disclaimer()) {
		t.Fatal("the disclaimer must be embedded")
	}

	text := CombinedText(combined, cfg)
	for _, want := range []string{"campaign report", "summary", "model caveats", "disclaimer",
		"independent original project"} {
		if !strings.Contains(text, want) {
			t.Fatalf("combined text is missing %q", want)
		}
	}
	if again := CombinedText(combined, cfg); again != text {
		t.Fatal("combined rendering must be deterministic")
	}
}

func TestBuildCombinedToleratesMissingStages(t *testing.T) {
	cfg := config.Default()
	combined := BuildCombined(cfg, store.Meta{}, nil, nil, nil, nil, nil)
	if combined.Screening != nil || combined.Mission != nil {
		t.Fatal("absent stages must stay nil")
	}
	text := CombinedText(combined, cfg)
	if !strings.Contains(text, "campaign report") {
		t.Fatal("a report without stages must still render")
	}
}

func TestTableRendering(t *testing.T) {
	tbl := NewTable([]string{"name", "value"}, 1)
	if got := tbl.Render("  "); got != "  (none)\n" {
		t.Fatalf("empty table rendered as %q", got)
	}
	tbl.Add("beta", "2")
	tbl.Add("alpha", "10")
	tbl.Add("gamma")
	tbl.SortRows(0)
	out := tbl.Render("")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("expected header, separator and three rows, got %d lines:\n%s", len(lines), out)
	}
	if !strings.HasPrefix(lines[2], "alpha") {
		t.Fatalf("rows not sorted: %q", lines[2])
	}
	if !strings.Contains(lines[2], " 10") {
		t.Fatalf("numeric column should be right aligned: %q", lines[2])
	}
	if strings.HasSuffix(lines[4], " ") {
		t.Fatalf("trailing whitespace must be trimmed: %q", lines[4])
	}
	tbl.SortRows(9)
	if len(tbl.Rows) != 3 {
		t.Fatal("sorting by an out-of-range column must not drop rows")
	}
}

func TestSectionHelpers(t *testing.T) {
	var s Section
	s.Title("Title")
	s.Heading("Head")
	s.Field("key", "value")
	s.Line("plain")
	s.Bullets([]string{"b", "a"}, true)
	s.Bullets(nil, false)
	got := s.String()
	for _, want := range []string{"Title\n=====", "Head\n----", "key:", "plain", "  - a\n  - b", "(none)"} {
		if !strings.Contains(got, want) {
			t.Fatalf("section output is missing %q\n%s", want, got)
		}
	}
	if strings.Index(got, "  - a") > strings.Index(got, "  - b") {
		t.Fatal("bullets must be sorted when requested")
	}
}

func TestFindingsTableSorts(t *testing.T) {
	tbl := findingsTable(compliance.Findings{
		{RuleID: "B", Subject: "z", Status: "pass", Detail: "d"},
		{RuleID: "A", Subject: "a", Status: "fail", Detail: "d"},
	})
	out := tbl.Render("")
	if strings.Index(out, "a") > strings.Index(out, "z") {
		t.Fatalf("findings not sorted by subject:\n%s", out)
	}
}

func TestSmallFormatters(t *testing.T) {
	if yesNo(true) != "yes" || yesNo(false) != "no" {
		t.Fatal("yesNo is wrong")
	}
	if passFail(true) != "pass" || passFail(false) != "fail" {
		t.Fatal("passFail is wrong")
	}
	if got := captureText(rank.Feasibility{Capturable: true, ChaserID: "c1"}); got != "yes:c1" {
		t.Fatalf("captureText = %q", got)
	}
	if got := captureText(rank.Feasibility{}); got != "no" {
		t.Fatalf("captureText = %q", got)
	}
}
