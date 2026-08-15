package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfigIsValid(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatalf("the built-in default configuration must validate: %v", err)
	}
}

func TestLoadWithoutPathReturnsDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Label != Default().Label {
		t.Fatalf("label %q, want the default", cfg.Label)
	}
}

func TestLoadOverlaysFileOnDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := `{"label":"partial","screening":{"miss_distance_threshold_km":8.0,"required_miss_distance_km":2.0,` +
		`"max_conjunctions_per_asset":10,"skip_same_object_classes":["payload"]}}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Label != "partial" {
		t.Fatalf("label %q", cfg.Label)
	}
	if cfg.Screening.MissDistanceThresholdKm != 8 {
		t.Fatalf("threshold %v", cfg.Screening.MissDistanceThresholdKm)
	}
	// Untouched sections must keep their defaults.
	if cfg.Probability.SigmaBaseKm != Default().Probability.SigmaBaseKm {
		t.Fatalf("sigma changed to %v", cfg.Probability.SigmaBaseKm)
	}
	if !cfg.SkipClassSet()["payload"] {
		t.Fatal("skip set should contain payload")
	}
}

func TestLoadRejectsUnknownKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"label":"x","screning":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected an error for the misspelled section")
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"label":"x","propagation":{"coarse_step_s":-1}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected a validation error")
	}
	if !strings.Contains(err.Error(), "coarse_step_s") {
		t.Fatalf("error should mention the offending field: %v", err)
	}
}

func TestValidateCatchesEachSection(t *testing.T) {
	cases := map[string]func(*Config){
		"label":       func(c *Config) { c.Label = "" },
		"propagation": func(c *Config) { c.Propagation.RefineToleranceS = 100 },
		"screening":   func(c *Config) { c.Screening.RequiredMissDistanceKm = 99 },
		"probability": func(c *Config) { c.Probability.SigmaFloorKm = 99 },
		"severity":    func(c *Config) { c.Severity.HighProbability = 1 },
		"maneuver":    func(c *Config) { c.Maneuver.DeltaVMarginFactor = 0.5 },
		"ranking":     func(c *Config) { c.Ranking.MassWeight = 0.9 },
		"mission":     func(c *Config) { c.Mission.ReserveFraction = 1.5 },
		"compliance":  func(c *Config) { c.Compliance.GraveyardRaiseKm = 1 },
		"output":      func(c *Config) { c.Output.DistanceDecimals = 99 },
	}
	for name, mutate := range cases {
		cfg := Default()
		mutate(&cfg)
		if err := cfg.Validate(); err == nil {
			t.Fatalf("%s: expected a validation error", name)
		}
	}
}

func TestSeverityOrderingIsEnforced(t *testing.T) {
	cfg := Default()
	cfg.Severity.ModerateProbability = cfg.Severity.HighProbability
	if err := cfg.Validate(); err == nil {
		t.Fatal("thresholds that are not strictly decreasing must be rejected")
	}
	cfg = Default()
	cfg.Severity.ActionableSeverity = "urgent"
	if err := cfg.Validate(); err == nil {
		t.Fatal("an unknown actionable severity must be rejected")
	}
}

func TestSeverityRankOrdering(t *testing.T) {
	labels := SeverityLabels()
	for i := 1; i < len(labels); i++ {
		if SeverityRank(labels[i]) <= SeverityRank(labels[i-1]) {
			t.Fatalf("%s does not outrank %s", labels[i], labels[i-1])
		}
	}
	if SeverityRank("nonsense") <= SeverityRank("critical") {
		t.Fatal("unknown labels must rank above critical so they never trigger action")
	}
	if Default().ActionableRank() != SeverityRank("high") {
		t.Fatal("the default actionable rank should be high")
	}
}

func TestSummaryIsDeterministic(t *testing.T) {
	cfg := Default()
	first := cfg.Summary()
	for i := 0; i < 5; i++ {
		if cfg.Summary() != first {
			t.Fatal("the configuration digest must be stable")
		}
	}
	if !strings.Contains(first, "label=debrisledger-default") {
		t.Fatalf("digest should carry the label: %s", first)
	}
}

func TestSkipClassSetRejectsUnknownClass(t *testing.T) {
	cfg := Default()
	cfg.Screening.SkipSameObjectClasses = []string{"asteroid"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("an unknown class in the skip list must be rejected")
	}
}
