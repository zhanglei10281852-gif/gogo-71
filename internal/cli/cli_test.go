package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"DebrisLedger/internal/strictjson"
)

func examplePath(name string) string {
	return filepath.Join("..", "..", "examples", name)
}

// run executes a command line and returns the exit code with both streams.
func run(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := Run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestUsageAndVersion(t *testing.T) {
	code, out, _ := run("--help")
	if code != 0 {
		t.Fatalf("--help exit code %d", code)
	}
	for _, want := range []string{"usage: debrisledger", "validate", "verify", "not suitable for operational"} {
		if !strings.Contains(out, want) {
			t.Fatalf("usage is missing %q", want)
		}
	}
	if code, out, _ := run("--version"); code != 0 || !strings.Contains(out, Version) {
		t.Fatalf("--version gave %d %q", code, out)
	}
	if code, _, err := run(); code != 2 || !strings.Contains(err, "usage") {
		t.Fatalf("an empty command line should print usage and exit 2, got %d", code)
	}
	if code, _, err := run("teleport"); code != 2 || !strings.Contains(err, "unknown subcommand") {
		t.Fatalf("unknown subcommand gave %d %q", code, err)
	}
}

func TestValidateSubcommand(t *testing.T) {
	code, out, _ := run("validate", "--quiet", "--config", examplePath("config.json"),
		"--scenario", examplePath("scenario.json"))
	if code != 0 {
		t.Fatalf("validate exit code %d", code)
	}
	if !strings.Contains(out, "validation") || !strings.Contains(out, "fictional-leo-study") {
		t.Fatalf("unexpected validate output:\n%s", out)
	}

	code, out, _ = run("validate", "--quiet", "--format", "json",
		"--config", examplePath("config.json"), "--scenario", examplePath("scenario.json"))
	if code != 0 {
		t.Fatalf("validate --format json exit code %d", code)
	}
	var parsed validateOutput
	if err := strictjson.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("validate JSON is not strictly decodable: %v", err)
	}
	if parsed.Status != "ok" || parsed.Objects == 0 || len(parsed.ObjectIDs) != parsed.Objects {
		t.Fatalf("unexpected JSON payload: %+v", parsed)
	}
	if parsed.Assets != len(parsed.AssetIDs) || parsed.Chasers != len(parsed.ChaserIDs) {
		t.Fatalf("identifier lists do not match the counts: %+v", parsed)
	}
}

func TestValidateRejectsBadInput(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "scenario.json")
	if err := os.WriteFile(bad, []byte(`{"label":"x","epoch_s":0,"horizon_s":600,"surprise":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, err := run("validate", "--quiet", "--scenario", bad); code != 1 || !strings.Contains(err, "surprise") {
		t.Fatalf("expected a strict-decoding failure, got %d %q", code, err)
	}
	if code, _, _ := run("validate", "--quiet"); code != 1 {
		t.Fatal("validate without a scenario must fail")
	}
	if code, _, err := run("validate", "--quiet", "--format", "yaml",
		"--scenario", examplePath("scenario.json")); code != 1 || !strings.Contains(err, "unknown --format") {
		t.Fatalf("expected a format error, got %d %q", code, err)
	}
	if code, _, err := run("validate", "--quiet", "--scenario", examplePath("scenario.json"), "extra"); code != 1 ||
		!strings.Contains(err, "positional") {
		t.Fatalf("expected a positional-argument error, got %d %q", code, err)
	}
}

func TestPipelineEndToEnd(t *testing.T) {
	store := filepath.Join(t.TempDir(), "store")
	cfg := examplePath("config.json")
	scenario := examplePath("scenario.json")

	if code, _, err := run("ingest", "--quiet", "--config", cfg, "--scenario", scenario, "--store", store); code != 0 {
		t.Fatalf("ingest failed: %d %s", code, err)
	}
	for _, stage := range []string{"screen", "maneuver", "rank", "mission"} {
		code, out, errOut := run(stage, "--quiet", "--config", cfg, "--store", store)
		if code != 0 {
			t.Fatalf("%s failed: %d %s", stage, code, errOut)
		}
		if !strings.Contains(out, "DebrisLedger") {
			t.Fatalf("%s output is missing the banner", stage)
		}
	}
	code, out, errOut := run("verify", "--quiet", "--store", store)
	if code != 0 {
		t.Fatalf("verify failed: %d %s", code, errOut)
	}
	if !strings.Contains(out, "overall:") || strings.Contains(out, "overall:                   fail") {
		t.Fatalf("verify should pass:\n%s", out)
	}
	code, out, errOut = run("report", "--quiet", "--config", cfg, "--store", store)
	if code != 0 {
		t.Fatalf("report failed: %d %s", code, errOut)
	}
	for _, want := range []string{"campaign report", "summary", "disclaimer"} {
		if !strings.Contains(out, want) {
			t.Fatalf("report is missing %q", want)
		}
	}

	// Every snapshot must be present and strictly decodable.
	for _, name := range []string{"screening", "maneuver", "ranking", "mission", "report"} {
		path := filepath.Join(store, "snapshots", name+".json")
		var probe map[string]any
		if err := strictjson.DecodeFile(path, &probe); err != nil {
			t.Fatalf("snapshot %s is not strict JSON: %v", name, err)
		}
	}
	for _, name := range []string{"catalog.jsonl", "ledger.jsonl", "audit.jsonl", "meta.json"} {
		if _, err := os.Stat(filepath.Join(store, name)); err != nil {
			t.Fatalf("store file %s missing: %v", name, err)
		}
	}
}

func TestPipelineIsByteIdenticalAcrossRuns(t *testing.T) {
	cfg := examplePath("config.json")
	scenario := examplePath("scenario.json")
	root := t.TempDir()
	var digests [2]map[string][]byte

	for i := range digests {
		store := filepath.Join(root, "run", string(rune('a'+i)))
		if code, _, err := run("ingest", "--quiet", "--config", cfg, "--scenario", scenario, "--store", store); code != 0 {
			t.Fatalf("run %d ingest failed: %s", i, err)
		}
		// The report stage is deliberately left out of the comparison: it
		// embeds the absolute store path in its verification block, which
		// differs between the two temporary directories by construction.
		for _, stage := range []string{"screen", "maneuver", "rank", "mission"} {
			if code, _, err := run(stage, "--quiet", "--config", cfg, "--store", store); code != 0 {
				t.Fatalf("run %d %s failed: %s", i, stage, err)
			}
		}
		digests[i] = map[string][]byte{}
		for _, name := range []string{"screening", "maneuver", "ranking", "mission"} {
			data, err := os.ReadFile(filepath.Join(store, "snapshots", name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			digests[i][name] = data
		}
		data, err := os.ReadFile(filepath.Join(store, "audit.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		digests[i]["audit"] = data
	}
	for name, first := range digests[0] {
		if !bytes.Equal(first, digests[1][name]) {
			t.Fatalf("artefact %s differs between two identical runs", name)
		}
	}
}

func TestStageOrderIsEnforced(t *testing.T) {
	store := filepath.Join(t.TempDir(), "store")
	cfg := examplePath("config.json")
	scenario := examplePath("scenario.json")
	if code, _, err := run("ingest", "--quiet", "--config", cfg, "--scenario", scenario, "--store", store); code != 0 {
		t.Fatalf("ingest failed: %s", err)
	}
	if code, _, err := run("maneuver", "--quiet", "--config", cfg, "--store", store); code != 1 ||
		!strings.Contains(err, "run the screen subcommand first") {
		t.Fatalf("maneuver before screen gave %d %q", code, err)
	}
	if code, _, err := run("mission", "--quiet", "--config", cfg, "--store", store); code != 1 ||
		!strings.Contains(err, "run the rank subcommand first") {
		t.Fatalf("mission before rank gave %d %q", code, err)
	}
}

func TestStageRequiresStore(t *testing.T) {
	for _, stage := range []string{"screen", "maneuver", "rank", "mission", "verify", "report"} {
		if code, _, err := run(stage, "--quiet"); code != 1 || !strings.Contains(err, "--store is required") {
			t.Fatalf("%s without a store gave %d %q", stage, code, err)
		}
	}
}

func TestScreenAcceptsScenarioOverrideAndWritesOut(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, "store")
	cfg := examplePath("config.json")
	scenario := examplePath("scenario.json")
	if code, _, err := run("ingest", "--quiet", "--config", cfg, "--scenario", scenario, "--store", store); code != 0 {
		t.Fatalf("ingest failed: %s", err)
	}
	out := filepath.Join(dir, "screen.json")
	code, stdout, errOut := run("screen", "--config", cfg, "--scenario", scenario,
		"--store", store, "--format", "json", "--out", out)
	if code != 0 {
		t.Fatalf("screen failed: %d %s", code, errOut)
	}
	if stdout != "" {
		t.Fatalf("with --out nothing should reach stdout, got %q", stdout)
	}
	if !strings.Contains(errOut, "wrote ") {
		t.Fatalf("the write should be reported on stderr, got %q", errOut)
	}
	var probe map[string]any
	if err := strictjson.DecodeFile(out, &probe); err != nil {
		t.Fatalf("written artefact is not strict JSON: %v", err)
	}
	if _, ok := probe["conjunctions"]; !ok {
		t.Fatalf("screening JSON has no conjunctions key: %v", probe)
	}
}

func TestVerifyFailsOnTamperedStore(t *testing.T) {
	store := filepath.Join(t.TempDir(), "store")
	cfg := examplePath("config.json")
	scenario := examplePath("scenario.json")
	if code, _, err := run("ingest", "--quiet", "--config", cfg, "--scenario", scenario, "--store", store); code != 0 {
		t.Fatalf("ingest failed: %s", err)
	}
	if code, _, err := run("screen", "--quiet", "--config", cfg, "--store", store); code != 0 {
		t.Fatalf("screen failed: %s", err)
	}
	path := filepath.Join(store, "snapshots", "screening.json")
	if err := os.WriteFile(path, []byte("{\"tampered\":true}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, err := run("verify", "--quiet", "--store", store); code != 1 ||
		!strings.Contains(err, "verification failed") {
		t.Fatalf("verify should have failed, got %d %q", code, err)
	}
}

func TestCommandTableAndFormatHelpers(t *testing.T) {
	names := map[string]bool{}
	for _, c := range Commands() {
		if c.Name == "" || c.Summary == "" || c.Run == nil {
			t.Fatalf("incomplete command entry %+v", c)
		}
		if names[c.Name] {
			t.Fatalf("duplicate command %q", c.Name)
		}
		names[c.Name] = true
	}
	for _, want := range []string{"validate", "ingest", "screen", "maneuver", "rank", "mission", "verify", "report"} {
		if !names[want] {
			t.Fatalf("command %q is missing", want)
		}
	}
	if err := checkFormat("text"); err != nil {
		t.Fatalf("text must be accepted: %v", err)
	}
	if err := checkFormat("json"); err != nil {
		t.Fatalf("json must be accepted: %v", err)
	}
	if err := checkFormat("xml"); err == nil {
		t.Fatal("xml must be rejected")
	}
	if got := formatNames(); len(got) != 2 || got[0] != "json" {
		t.Fatalf("format names %v", got)
	}
}

func TestConfigErrorsArePropagated(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "config.json")
	if err := os.WriteFile(bad, []byte(`{"label":"x","output":{"distance_decimals":99}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, err := run("validate", "--quiet", "--config", bad,
		"--scenario", examplePath("scenario.json")); code != 1 || !strings.Contains(err, "distance_decimals") {
		t.Fatalf("expected a configuration error, got %d %q", code, err)
	}
}
