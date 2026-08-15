package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"DebrisLedger/internal/model"
)

func scenario() *model.Scenario {
	sc := &model.Scenario{
		Label: "store-unit", EpochSeconds: 100, HorizonSeconds: 86400,
		Objects: []model.CatalogObject{
			{
				ID: "obj-2", Name: "Object two", Class: model.ClassFragment, MassKg: 20, AreaM2: 0.3,
				Orbit:             model.Orbit{AltitudeKm: 551, InclinationDeg: 53, RAANDeg: 84, ArgLatDeg: 10, EpochSeconds: 100},
				TumbleRateDegPerS: 3, AttachPoint: "none",
			},
			{
				ID: "obj-1", Name: "Object one", Class: model.ClassRocketBody, MassKg: 1400, AreaM2: 12,
				Orbit:             model.Orbit{AltitudeKm: 705, InclinationDeg: 98, RAANDeg: 210, ArgLatDeg: 20, EpochSeconds: 100},
				TumbleRateDegPerS: 1, AttachPoint: "adapter-ring",
			},
		},
		Assets: []model.Asset{
			{
				ID: "asset-1", Name: "Asset one", MassKg: 900, AreaM2: 7,
				Orbit:       model.Orbit{AltitudeKm: 552, InclinationDeg: 53, RAANDeg: 84, ArgLatDeg: 12, EpochSeconds: 100},
				Maneuver:    model.ManeuverCapability{DeltaVBudgetMps: 10, ThrusterType: model.ThrusterChemical, MinLeadTimeS: 3600, MaxBurnsPerDay: 4},
				Criticality: 4,
			},
		},
		Chasers: []model.Chaser{
			{
				ID: "chaser-1", Name: "Chaser one", DryMassKg: 1200, DeltaVBudgetMps: 700, CaptureSlots: 2,
				DockingTimeS: 3600, MaxCaptureMassKg: 2000, MaxTumbleRateDegPerS: 3,
				Orbit: model.Orbit{AltitudeKm: 700, InclinationDeg: 98, RAANDeg: 210, ArgLatDeg: 0, EpochSeconds: 100},
			},
		},
		ScreeningVolumes: []model.ScreeningVolume{
			{Name: "standard", RadialKm: 2, InTrackKm: 25, CrossTrackKm: 2},
		},
	}
	sc.Sort()
	return sc
}

func openTemp(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return st
}

func TestOpenRejectsEmptyDirectory(t *testing.T) {
	if _, err := Open("  "); err == nil {
		t.Fatal("expected an error for an empty directory")
	}
}

func TestIngestAndReplay(t *testing.T) {
	st := openTemp(t)
	sc := scenario()
	entry, err := st.Ingest(sc, "cfg-label")
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if entry.Sequence != 1 || entry.PrevHash != GenesisHash {
		t.Fatalf("unexpected first audit entry %+v", entry)
	}
	records, err := st.Records()
	if err != nil {
		t.Fatalf("Records: %v", err)
	}
	want := 1 + len(sc.Objects) + len(sc.Assets) + len(sc.Chasers) + len(sc.ScreeningVolumes)
	if len(records) != want {
		t.Fatalf("stored %d records, want %d", len(records), want)
	}
	for i, rec := range records {
		if rec.Sequence != i+1 {
			t.Fatalf("record %d carries sequence %d", i, rec.Sequence)
		}
	}
	back, err := st.Scenario()
	if err != nil {
		t.Fatalf("Scenario: %v", err)
	}
	if back.Label != sc.Label || back.EpochSeconds != sc.EpochSeconds {
		t.Fatalf("scenario header lost: %+v", back)
	}
	if len(back.Objects) != len(sc.Objects) || back.Objects[0].ID != "obj-1" {
		t.Fatalf("objects replayed as %+v", back.Objects)
	}
	if back.Assets[0].Maneuver.ThrusterType != model.ThrusterChemical {
		t.Fatal("asset manoeuvre capability lost in the round trip")
	}
	if back.Chasers[0].CaptureSlots != 2 {
		t.Fatal("chaser data lost in the round trip")
	}
	if back.ScreeningVolumes[0].Name != "standard" {
		t.Fatal("screening volume lost in the round trip")
	}
}

func TestIngestIsAppendOnlyAndLastWriteWins(t *testing.T) {
	st := openTemp(t)
	sc := scenario()
	if _, err := st.Ingest(sc, "cfg"); err != nil {
		t.Fatal(err)
	}
	updated := scenario()
	updated.Objects[0].MassKg = 4321
	if _, err := st.Ingest(updated, "cfg"); err != nil {
		t.Fatal(err)
	}
	records, err := st.Records()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 12 {
		t.Fatalf("expected 12 appended records, got %d", len(records))
	}
	back, err := st.Scenario()
	if err != nil {
		t.Fatal(err)
	}
	if back.Objects[0].MassKg != 4321 {
		t.Fatalf("the later record must win, got %v", back.Objects[0].MassKg)
	}
}

func TestScenarioFailsOnEmptyStore(t *testing.T) {
	st := openTemp(t)
	if _, err := st.Scenario(); err == nil {
		t.Fatal("expected an error for an empty store")
	}
}

func TestRecordsRejectMalformedLines(t *testing.T) {
	st := openTemp(t)
	if err := AppendLine(st.CatalogPath(), []byte(`{"seq":1,"kind":"object","epoch_s":0}`)); err != nil {
		t.Fatal(err)
	}
	_, err := st.Records()
	if err == nil || !strings.Contains(err.Error(), "no object payload") {
		t.Fatalf("expected a payload error, got %v", err)
	}

	st2 := openTemp(t)
	if err := AppendLine(st2.CatalogPath(), []byte(`{"seq":1,"kind":"planet","epoch_s":0}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := st2.Records(); err == nil || !strings.Contains(err.Error(), "unknown kind") {
		t.Fatalf("expected an unknown-kind error, got %v", err)
	}

	st3 := openTemp(t)
	if err := AppendLine(st3.CatalogPath(), []byte(`{"seq":1,"kind":"object","surprise":true}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := st3.Records(); err == nil {
		t.Fatal("unknown JSONL fields must be rejected")
	}
}

func TestSnapshotRoundTripAndMeta(t *testing.T) {
	st := openTemp(t)
	sc := scenario()
	if _, err := st.Ingest(sc, "cfg"); err != nil {
		t.Fatal(err)
	}
	type payload struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}
	if _, err := st.SaveSnapshot(SnapshotScreening, sc.EpochSeconds, payload{Name: "x", Value: 7}, "detail"); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}
	if !st.HasSnapshot(SnapshotScreening) {
		t.Fatal("the snapshot should exist")
	}
	var back payload
	if err := st.LoadSnapshot(SnapshotScreening, &back); err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	if back.Name != "x" || back.Value != 7 {
		t.Fatalf("snapshot round trip gave %+v", back)
	}
	meta, err := st.Meta()
	if err != nil {
		t.Fatal(err)
	}
	if meta.StoreVersion != StoreVersion {
		t.Fatalf("store version %d", meta.StoreVersion)
	}
	if meta.Counts.Objects != 2 || meta.Counts.Assets != 1 || meta.Counts.Chasers != 1 || meta.Counts.Volumes != 1 {
		t.Fatalf("counts %+v", meta.Counts)
	}
	if len(meta.Snapshots) != 1 || meta.Snapshots[0].Name != SnapshotScreening {
		t.Fatalf("snapshot refs %+v", meta.Snapshots)
	}
	if meta.Snapshots[0].SHA256 == "" || meta.Snapshots[0].Bytes == 0 {
		t.Fatalf("snapshot ref is incomplete: %+v", meta.Snapshots[0])
	}
	if meta.ScenarioLabel != sc.Label || meta.ConfigLabel != "cfg" {
		t.Fatalf("labels lost: %+v", meta)
	}
}

func TestSnapshotNameValidation(t *testing.T) {
	st := openTemp(t)
	for _, name := range []string{"", "Bad", "with space", "path/traversal", strings.Repeat("a", 49)} {
		if _, err := st.SaveSnapshot(name, 0, map[string]int{"a": 1}, "d"); err == nil {
			t.Fatalf("expected an error for snapshot name %q", name)
		}
		if err := st.LoadSnapshot(name, &struct{}{}); err == nil {
			t.Fatalf("expected an error loading snapshot name %q", name)
		}
		if st.HasSnapshot(name) {
			t.Fatalf("%q must not be reported as an existing snapshot", name)
		}
	}
	if err := st.LoadSnapshot("missing", &struct{}{}); err == nil {
		t.Fatal("expected an error for a missing snapshot")
	}
}

func TestAuditChainLinksAndVerifies(t *testing.T) {
	st := openTemp(t)
	sc := scenario()
	if _, err := st.Ingest(sc, "cfg"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{SnapshotScreening, SnapshotManeuver, SnapshotRanking} {
		if _, err := st.SaveSnapshot(name, sc.EpochSeconds, map[string]string{"name": name}, "d"); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := st.AuditEntries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 {
		t.Fatalf("expected four audit entries, got %d", len(entries))
	}
	for i, e := range entries {
		if e.Sequence != i+1 {
			t.Fatalf("entry %d carries sequence %d", i, e.Sequence)
		}
		if e.ComputeHash() != e.Hash {
			t.Fatalf("entry %d hash does not match its content", i)
		}
		if i == 0 {
			if e.PrevHash != GenesisHash {
				t.Fatal("the first entry must link to the genesis hash")
			}
		} else if e.PrevHash != entries[i-1].Hash {
			t.Fatalf("entry %d is not linked to its predecessor", i)
		}
	}
	v, err := st.VerifyAudit()
	if err != nil {
		t.Fatal(err)
	}
	if !v.OK || v.Entries != 4 {
		t.Fatalf("verification %+v", v)
	}
	if v.HeadHash != entries[3].Hash {
		t.Fatal("head hash must be the last entry hash")
	}
	if len(v.KindCounts) != 4 {
		t.Fatalf("kind counts %+v", v.KindCounts)
	}
}

func TestAuditDetectsTampering(t *testing.T) {
	st := openTemp(t)
	if _, err := st.Ingest(scenario(), "cfg"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveSnapshot(SnapshotScreening, 100, map[string]int{"a": 1}, "d"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(st.AuditPath())
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(raw), `"subject":"screening"`, `"subject":"screeninx"`, 1)
	if tampered == string(raw) {
		t.Fatal("the audit log did not contain the expected subject")
	}
	if err := os.WriteFile(st.AuditPath(), []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}
	v, err := st.VerifyAudit()
	if err != nil {
		t.Fatal(err)
	}
	if v.OK {
		t.Fatal("tampering must be detected")
	}
	if len(v.Failures) == 0 {
		t.Fatal("a failure must be reported")
	}
}

func TestVerifyDetectsSnapshotDrift(t *testing.T) {
	st := openTemp(t)
	if _, err := st.Ingest(scenario(), "cfg"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveSnapshot(SnapshotScreening, 100, map[string]int{"a": 1}, "d"); err != nil {
		t.Fatal(err)
	}
	v, err := st.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if !v.OK {
		t.Fatalf("a clean store must verify: %+v", v.Findings)
	}
	if !v.CatalogueOK || !v.MetaOK {
		t.Fatalf("unexpected verification flags: %+v", v)
	}

	// Rewrite the snapshot behind the metadata's back.
	if err := os.WriteFile(st.SnapshotPath(SnapshotScreening), []byte("{\"a\":2}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v, err = st.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if v.OK {
		t.Fatal("a modified snapshot must be detected")
	}
	joined := strings.Join(v.Findings, "|")
	if !strings.Contains(joined, "does not match the metadata hash") {
		t.Fatalf("unexpected findings: %v", v.Findings)
	}

	// Deleting the snapshot must also be caught.
	if err := os.Remove(st.SnapshotPath(SnapshotScreening)); err != nil {
		t.Fatal(err)
	}
	v, err = st.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if v.OK {
		t.Fatal("a missing snapshot must be detected")
	}
}

func TestEventLedgerGrows(t *testing.T) {
	st := openTemp(t)
	if _, err := st.Ingest(scenario(), "cfg"); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendEvent("custom", "subject", 5, "hash", "detail"); err != nil {
		t.Fatal(err)
	}
	events, err := st.Events()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("expected two events, got %d", len(events))
	}
	if events[1].Sequence != 2 || events[1].Kind != "custom" {
		t.Fatalf("unexpected event %+v", events[1])
	}
}

func TestWriteFileAtomicReplacesContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "file.txt")
	if err := WriteFileAtomic(path, []byte("first")); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}
	if err := WriteFileAtomic(path, []byte("second")); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "second" {
		t.Fatalf("file holds %q", data)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("temporary files were left behind: %d entries", len(entries))
	}
}

func TestAppendLineAddsTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.jsonl")
	if err := AppendLine(path, []byte(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := AppendLine(path, []byte("{\"a\":2}\n")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "{\"a\":1}\n{\"a\":2}\n" {
		t.Fatalf("file holds %q", data)
	}
}

func TestHashHelpers(t *testing.T) {
	if HashBytes(nil) != HashBytes([]byte{}) {
		t.Fatal("hashing must be total")
	}
	if HashBytes([]byte("a")) == HashBytes([]byte("b")) {
		t.Fatal("different inputs must hash differently")
	}
	if len(HashBytes([]byte("a"))) != 64 {
		t.Fatal("expected a hex SHA-256")
	}
	if HashStrings("a", "b") == HashStrings("ab") {
		t.Fatal("field boundaries must be part of the hash input")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	hash, size, err := HashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if size != 5 || hash != HashBytes([]byte("hello")) {
		t.Fatalf("HashFile gave %q %d", hash, size)
	}
	if _, _, err := HashFile(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

func TestListFilesSkipsTemporaries(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"b.json", "a.json", ".tmp-a.json-123"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := listFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "a.json" || got[1] != "b.json" {
		t.Fatalf("listFiles gave %v", got)
	}
	missing, err := listFiles(filepath.Join(dir, "nope"))
	if err != nil {
		t.Fatalf("a missing directory must not be an error: %v", err)
	}
	if len(missing) != 0 {
		t.Fatalf("expected no entries, got %v", missing)
	}
}
