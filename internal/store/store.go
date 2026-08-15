package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"DebrisLedger/internal/model"
	"DebrisLedger/internal/strictjson"
)

// StoreVersion is bumped whenever the on-disk layout changes.
const StoreVersion = 1

// File and directory names inside a store.
const (
	catalogFile = "catalog.jsonl"
	ledgerFile  = "ledger.jsonl"
	auditFile   = "audit.jsonl"
	metaFile    = "meta.json"
	snapshotDir = "snapshots"
)

// Record kinds in the catalogue ledger.
const (
	KindScenario = "scenario"
	KindObject   = "object"
	KindAsset    = "asset"
	KindChaser   = "chaser"
	KindVolume   = "volume"
)

// Snapshot names written by the pipeline commands.
const (
	SnapshotScreening = "screening"
	SnapshotManeuver  = "maneuver"
	SnapshotRanking   = "ranking"
	SnapshotMission   = "mission"
	SnapshotReport    = "report"
)

// ScenarioHeader is the scenario-level part of an ingest.
type ScenarioHeader struct {
	Label          string `json:"label"`
	EpochSeconds   int64  `json:"epoch_s"`
	HorizonSeconds int64  `json:"horizon_s"`
	ObjectCount    int    `json:"object_count"`
	AssetCount     int    `json:"asset_count"`
	ChaserCount    int    `json:"chaser_count"`
	VolumeCount    int    `json:"volume_count"`
}

// Record is one line of the append-only catalogue ledger. Exactly one of the
// payload pointers is set, selected by Kind.
type Record struct {
	Sequence     int                    `json:"seq"`
	Kind         string                 `json:"kind"`
	EpochSeconds int64                  `json:"epoch_s"`
	Scenario     *ScenarioHeader        `json:"scenario,omitempty"`
	Object       *model.CatalogObject   `json:"object,omitempty"`
	Asset        *model.Asset           `json:"asset,omitempty"`
	Chaser       *model.Chaser          `json:"chaser,omitempty"`
	Volume       *model.ScreeningVolume `json:"volume,omitempty"`
}

// LedgerEvent records that a pipeline stage ran.
type LedgerEvent struct {
	Sequence      int    `json:"seq"`
	Kind          string `json:"kind"`
	Subject       string `json:"subject"`
	EpochSeconds  int64  `json:"epoch_s"`
	PayloadSHA256 string `json:"payload_sha256"`
	Detail        string `json:"detail"`
}

// SnapshotRef is the metadata entry describing one stored snapshot.
type SnapshotRef struct {
	Name         string `json:"name"`
	SHA256       string `json:"sha256"`
	Bytes        int    `json:"bytes"`
	EpochSeconds int64  `json:"epoch_s"`
}

// Counts is the catalogue census.
type Counts struct {
	Objects int `json:"objects"`
	Assets  int `json:"assets"`
	Chasers int `json:"chasers"`
	Volumes int `json:"volumes"`
	Records int `json:"records"`
	Events  int `json:"events"`
}

// Meta is the store metadata file.
type Meta struct {
	StoreVersion   int           `json:"store_version"`
	ScenarioLabel  string        `json:"scenario_label"`
	ConfigLabel    string        `json:"config_label"`
	EpochSeconds   int64         `json:"epoch_s"`
	HorizonSeconds int64         `json:"horizon_s"`
	Counts         Counts        `json:"counts"`
	Snapshots      []SnapshotRef `json:"snapshots"`
	AuditEntries   int           `json:"audit_entries"`
	AuditHeadHash  string        `json:"audit_head_hash"`
}

// Store is a directory-backed local store.
type Store struct {
	Root string
}

// Open prepares a store rooted at dir, creating the directory tree.
func Open(dir string) (*Store, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, fmt.Errorf("store directory must not be empty")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", dir, err)
	}
	s := &Store{Root: abs}
	for _, d := range []string{s.Root, filepath.Join(s.Root, snapshotDir)} {
		if err := os.MkdirAll(d, dirPerm); err != nil {
			return nil, fmt.Errorf("create %s: %w", d, err)
		}
	}
	return s, nil
}

// CatalogPath is the append-only catalogue ledger path.
func (s *Store) CatalogPath() string { return filepath.Join(s.Root, catalogFile) }

// LedgerPath is the append-only work ledger path.
func (s *Store) LedgerPath() string { return filepath.Join(s.Root, ledgerFile) }

// AuditPath is the hash-chained audit log path.
func (s *Store) AuditPath() string { return filepath.Join(s.Root, auditFile) }

// MetaPath is the metadata file path.
func (s *Store) MetaPath() string { return filepath.Join(s.Root, metaFile) }

// SnapshotPath is the path of a named snapshot.
func (s *Store) SnapshotPath(name string) string {
	return filepath.Join(s.Root, snapshotDir, name+".json")
}

// Ingest appends the whole scenario to the catalogue ledger, one record per
// entity, and records the ingest in the audit chain.
//
// The ledger is append-only: a second ingest of the same store adds new records
// rather than rewriting old ones, and readers reconstruct the current state by
// replaying the file with last-record-wins semantics.
func (s *Store) Ingest(sc *model.Scenario, configLabel string) (AuditEntry, error) {
	existing, err := s.Records()
	if err != nil {
		return AuditEntry{}, err
	}
	seq := len(existing)
	header := ScenarioHeader{
		Label:          sc.Label,
		EpochSeconds:   sc.EpochSeconds,
		HorizonSeconds: sc.HorizonSeconds,
		ObjectCount:    len(sc.Objects),
		AssetCount:     len(sc.Assets),
		ChaserCount:    len(sc.Chasers),
		VolumeCount:    len(sc.ScreeningVolumes),
	}
	records := make([]Record, 0, 1+len(sc.Objects)+len(sc.Assets)+len(sc.Chasers)+len(sc.ScreeningVolumes))
	seq++
	records = append(records, Record{Sequence: seq, Kind: KindScenario, EpochSeconds: sc.EpochSeconds, Scenario: &header})
	for i := range sc.Objects {
		obj := sc.Objects[i]
		seq++
		records = append(records, Record{Sequence: seq, Kind: KindObject, EpochSeconds: obj.Orbit.EpochSeconds, Object: &obj})
	}
	for i := range sc.Assets {
		asset := sc.Assets[i]
		seq++
		records = append(records, Record{Sequence: seq, Kind: KindAsset, EpochSeconds: asset.Orbit.EpochSeconds, Asset: &asset})
	}
	for i := range sc.Chasers {
		chaser := sc.Chasers[i]
		seq++
		records = append(records, Record{Sequence: seq, Kind: KindChaser, EpochSeconds: chaser.Orbit.EpochSeconds, Chaser: &chaser})
	}
	for i := range sc.ScreeningVolumes {
		volume := sc.ScreeningVolumes[i]
		seq++
		records = append(records, Record{Sequence: seq, Kind: KindVolume, EpochSeconds: sc.EpochSeconds, Volume: &volume})
	}

	var payload []byte
	for _, rec := range records {
		line, err := strictjson.MarshalCompact(rec)
		if err != nil {
			return AuditEntry{}, err
		}
		if err := AppendLine(s.CatalogPath(), line); err != nil {
			return AuditEntry{}, err
		}
		payload = append(payload, line...)
	}

	entry, err := s.appendAudit("ingest", sc.Label, sc.EpochSeconds, HashBytes(payload))
	if err != nil {
		return AuditEntry{}, err
	}
	if err := s.AppendEvent("ingest", sc.Label, sc.EpochSeconds, HashBytes(payload),
		fmt.Sprintf("%d record(s) appended", len(records))); err != nil {
		return AuditEntry{}, err
	}
	if err := s.WriteMeta(sc, configLabel); err != nil {
		return AuditEntry{}, err
	}
	return entry, nil
}

// Records replays the catalogue ledger in file order, decoding strictly.
func (s *Store) Records() ([]Record, error) {
	path := s.CatalogPath()
	if !exists(path) {
		return nil, nil
	}
	var out []Record
	err := strictjson.DecodeLinesFile(path, func(_ int, raw []byte) error {
		var rec Record
		if err := strictjson.Unmarshal(raw, &rec); err != nil {
			return err
		}
		if err := rec.validate(); err != nil {
			return err
		}
		out = append(out, rec)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r Record) validate() error {
	switch r.Kind {
	case KindScenario:
		if r.Scenario == nil {
			return fmt.Errorf("record %d of kind %q carries no scenario payload", r.Sequence, r.Kind)
		}
	case KindObject:
		if r.Object == nil {
			return fmt.Errorf("record %d of kind %q carries no object payload", r.Sequence, r.Kind)
		}
	case KindAsset:
		if r.Asset == nil {
			return fmt.Errorf("record %d of kind %q carries no asset payload", r.Sequence, r.Kind)
		}
	case KindChaser:
		if r.Chaser == nil {
			return fmt.Errorf("record %d of kind %q carries no chaser payload", r.Sequence, r.Kind)
		}
	case KindVolume:
		if r.Volume == nil {
			return fmt.Errorf("record %d of kind %q carries no screening-volume payload", r.Sequence, r.Kind)
		}
	default:
		return fmt.Errorf("record %d has unknown kind %q", r.Sequence, r.Kind)
	}
	return nil
}

// Scenario rebuilds the current scenario from the catalogue ledger. Later
// records for the same identifier replace earlier ones, and the collections are
// sorted so that the reconstruction is independent of append order.
func (s *Store) Scenario() (*model.Scenario, error) {
	records, err := s.Records()
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("store %s holds no catalogue records; run ingest first", s.Root)
	}
	sc := &model.Scenario{}
	objects := map[string]model.CatalogObject{}
	assets := map[string]model.Asset{}
	chasers := map[string]model.Chaser{}
	volumes := map[string]model.ScreeningVolume{}
	for _, rec := range records {
		switch rec.Kind {
		case KindScenario:
			sc.Label = rec.Scenario.Label
			sc.EpochSeconds = rec.Scenario.EpochSeconds
			sc.HorizonSeconds = rec.Scenario.HorizonSeconds
		case KindObject:
			objects[rec.Object.ID] = *rec.Object
		case KindAsset:
			assets[rec.Asset.ID] = *rec.Asset
		case KindChaser:
			chasers[rec.Chaser.ID] = *rec.Chaser
		case KindVolume:
			volumes[rec.Volume.Name] = *rec.Volume
		}
	}
	for _, id := range sortedKeysObjects(objects) {
		sc.Objects = append(sc.Objects, objects[id])
	}
	for _, id := range sortedKeysAssets(assets) {
		sc.Assets = append(sc.Assets, assets[id])
	}
	for _, id := range sortedKeysChasers(chasers) {
		sc.Chasers = append(sc.Chasers, chasers[id])
	}
	for _, name := range sortedKeysVolumes(volumes) {
		sc.ScreeningVolumes = append(sc.ScreeningVolumes, volumes[name])
	}
	sc.Sort()
	if err := sc.Validate(); err != nil {
		return nil, fmt.Errorf("catalogue in %s is not a valid scenario: %w", s.Root, err)
	}
	return sc, nil
}

// AppendEvent adds one work-ledger event.
func (s *Store) AppendEvent(kind, subject string, epochSeconds int64, payloadHash, detail string) error {
	events, err := s.Events()
	if err != nil {
		return err
	}
	event := LedgerEvent{
		Sequence:      len(events) + 1,
		Kind:          kind,
		Subject:       subject,
		EpochSeconds:  epochSeconds,
		PayloadSHA256: payloadHash,
		Detail:        detail,
	}
	line, err := strictjson.MarshalCompact(event)
	if err != nil {
		return err
	}
	return AppendLine(s.LedgerPath(), line)
}

// Events replays the work ledger.
func (s *Store) Events() ([]LedgerEvent, error) {
	path := s.LedgerPath()
	if !exists(path) {
		return nil, nil
	}
	var out []LedgerEvent
	err := strictjson.DecodeLinesFile(path, func(_ int, raw []byte) error {
		var event LedgerEvent
		if err := strictjson.Unmarshal(raw, &event); err != nil {
			return err
		}
		out = append(out, event)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// SaveSnapshot writes a plan snapshot atomically, appends an audit entry and a
// ledger event, and refreshes the metadata file.
func (s *Store) SaveSnapshot(name string, epochSeconds int64, payload any, detail string) (AuditEntry, error) {
	if !validSnapshotName(name) {
		return AuditEntry{}, fmt.Errorf("invalid snapshot name %q", name)
	}
	data, err := strictjson.Marshal(payload)
	if err != nil {
		return AuditEntry{}, err
	}
	if err := WriteFileAtomic(s.SnapshotPath(name), data); err != nil {
		return AuditEntry{}, err
	}
	hash := HashBytes(data)
	entry, err := s.appendAudit(name, name, epochSeconds, hash)
	if err != nil {
		return AuditEntry{}, err
	}
	if err := s.AppendEvent(name, name, epochSeconds, hash, detail); err != nil {
		return AuditEntry{}, err
	}
	if err := s.RefreshMeta(); err != nil {
		return AuditEntry{}, err
	}
	return entry, nil
}

// LoadSnapshot strictly decodes a stored snapshot.
func (s *Store) LoadSnapshot(name string, out any) error {
	if !validSnapshotName(name) {
		return fmt.Errorf("invalid snapshot name %q", name)
	}
	path := s.SnapshotPath(name)
	if !exists(path) {
		return fmt.Errorf("snapshot %q not found in %s", name, s.Root)
	}
	return strictjson.DecodeFile(path, out)
}

// HasSnapshot reports whether a snapshot exists.
func (s *Store) HasSnapshot(name string) bool {
	return validSnapshotName(name) && exists(s.SnapshotPath(name))
}

// SnapshotRefs hashes every stored snapshot, in name order.
func (s *Store) SnapshotRefs() ([]SnapshotRef, error) {
	names, err := listFiles(filepath.Join(s.Root, snapshotDir))
	if err != nil {
		return nil, err
	}
	out := make([]SnapshotRef, 0, len(names))
	for _, file := range names {
		if !strings.HasSuffix(file, ".json") {
			continue
		}
		name := strings.TrimSuffix(file, ".json")
		hash, size, err := HashFile(s.SnapshotPath(name))
		if err != nil {
			return nil, err
		}
		out = append(out, SnapshotRef{Name: name, SHA256: hash, Bytes: size, EpochSeconds: snapshotEpoch(s, name)})
	}
	return out, nil
}

// snapshotEpoch reads the epoch a snapshot was written for from the audit
// chain, returning 0 when the snapshot predates the chain.
func snapshotEpoch(s *Store, name string) int64 {
	entries, err := s.AuditEntries()
	if err != nil {
		return 0
	}
	var epoch int64
	for _, e := range entries {
		if e.Kind == name {
			epoch = e.EpochSeconds
		}
	}
	return epoch
}

// WriteMeta writes the metadata file for a freshly ingested scenario.
func (s *Store) WriteMeta(sc *model.Scenario, configLabel string) error {
	meta, err := s.buildMeta()
	if err != nil {
		return err
	}
	meta.ScenarioLabel = sc.Label
	meta.ConfigLabel = configLabel
	meta.EpochSeconds = sc.EpochSeconds
	meta.HorizonSeconds = sc.HorizonSeconds
	data, err := strictjson.Marshal(meta)
	if err != nil {
		return err
	}
	return WriteFileAtomic(s.MetaPath(), data)
}

// RefreshMeta recomputes the metadata file from the store contents, keeping the
// labels already recorded.
func (s *Store) RefreshMeta() error {
	previous, err := s.Meta()
	if err != nil {
		return err
	}
	meta, err := s.buildMeta()
	if err != nil {
		return err
	}
	meta.ScenarioLabel = previous.ScenarioLabel
	meta.ConfigLabel = previous.ConfigLabel
	meta.EpochSeconds = previous.EpochSeconds
	meta.HorizonSeconds = previous.HorizonSeconds
	data, err := strictjson.Marshal(meta)
	if err != nil {
		return err
	}
	return WriteFileAtomic(s.MetaPath(), data)
}

func (s *Store) buildMeta() (Meta, error) {
	records, err := s.Records()
	if err != nil {
		return Meta{}, err
	}
	events, err := s.Events()
	if err != nil {
		return Meta{}, err
	}
	snapshots, err := s.SnapshotRefs()
	if err != nil {
		return Meta{}, err
	}
	audit, err := s.VerifyAudit()
	if err != nil {
		return Meta{}, err
	}
	counts := Counts{Records: len(records), Events: len(events)}
	objects := map[string]bool{}
	assets := map[string]bool{}
	chasers := map[string]bool{}
	volumes := map[string]bool{}
	for _, rec := range records {
		switch rec.Kind {
		case KindObject:
			objects[rec.Object.ID] = true
		case KindAsset:
			assets[rec.Asset.ID] = true
		case KindChaser:
			chasers[rec.Chaser.ID] = true
		case KindVolume:
			volumes[rec.Volume.Name] = true
		}
	}
	counts.Objects = len(objects)
	counts.Assets = len(assets)
	counts.Chasers = len(chasers)
	counts.Volumes = len(volumes)
	return Meta{
		StoreVersion:  StoreVersion,
		Counts:        counts,
		Snapshots:     snapshots,
		AuditEntries:  audit.Entries,
		AuditHeadHash: audit.HeadHash,
	}, nil
}

// Meta reads the metadata file, returning a zero value when it is absent.
func (s *Store) Meta() (Meta, error) {
	path := s.MetaPath()
	if !exists(path) {
		return Meta{StoreVersion: StoreVersion}, nil
	}
	var meta Meta
	if err := strictjson.DecodeFile(path, &meta); err != nil {
		return Meta{}, err
	}
	return meta, nil
}

// Verification is the outcome of a whole-store integrity check.
type Verification struct {
	Root        string            `json:"root"`
	Audit       AuditVerification `json:"audit"`
	Snapshots   []SnapshotCheck   `json:"snapshots"`
	MetaOK      bool              `json:"meta_ok"`
	CatalogueOK bool              `json:"catalogue_ok"`
	OK          bool              `json:"ok"`
	Findings    []string          `json:"findings"`
}

// SnapshotCheck is the hash comparison for one snapshot.
type SnapshotCheck struct {
	Name      string `json:"name"`
	SHA256    string `json:"sha256"`
	Bytes     int    `json:"bytes"`
	InMeta    bool   `json:"in_meta"`
	HashMatch bool   `json:"hash_match"`
	DecodesOK bool   `json:"decodes_ok"`
}

// Verify checks the audit chain, the snapshot hashes recorded in the metadata,
// and that the catalogue still replays into a valid scenario.
func (s *Store) Verify() (Verification, error) {
	v := Verification{Root: s.Root, Findings: []string{}, OK: true}
	audit, err := s.VerifyAudit()
	if err != nil {
		return Verification{}, err
	}
	v.Audit = audit
	if !audit.OK {
		v.OK = false
		v.Findings = append(v.Findings, audit.Failures...)
	}

	meta, err := s.Meta()
	if err != nil {
		return Verification{}, err
	}
	metaHashes := map[string]string{}
	for _, ref := range meta.Snapshots {
		metaHashes[ref.Name] = ref.SHA256
	}
	refs, err := s.SnapshotRefs()
	if err != nil {
		return Verification{}, err
	}
	for _, ref := range refs {
		check := SnapshotCheck{Name: ref.Name, SHA256: ref.SHA256, Bytes: ref.Bytes}
		recorded, ok := metaHashes[ref.Name]
		check.InMeta = ok
		check.HashMatch = ok && recorded == ref.SHA256
		var probe map[string]any
		check.DecodesOK = strictjson.DecodeFile(s.SnapshotPath(ref.Name), &probe) == nil
		if !check.InMeta {
			v.OK = false
			v.Findings = append(v.Findings, fmt.Sprintf("snapshot %s is not recorded in the metadata", ref.Name))
		} else if !check.HashMatch {
			v.OK = false
			v.Findings = append(v.Findings,
				fmt.Sprintf("snapshot %s hash %s does not match the metadata hash %s",
					ref.Name, short(ref.SHA256), short(recorded)))
		}
		if !check.DecodesOK {
			v.OK = false
			v.Findings = append(v.Findings, fmt.Sprintf("snapshot %s does not decode as strict JSON", ref.Name))
		}
		v.Snapshots = append(v.Snapshots, check)
	}
	for name := range metaHashes {
		if !s.HasSnapshot(name) {
			v.OK = false
			v.Findings = append(v.Findings, fmt.Sprintf("snapshot %s is recorded in the metadata but missing on disk", name))
		}
	}

	v.MetaOK = meta.StoreVersion == StoreVersion && meta.AuditHeadHash == audit.HeadHash
	if !v.MetaOK {
		v.OK = false
		v.Findings = append(v.Findings,
			fmt.Sprintf("metadata head %s does not match the audit head %s",
				short(meta.AuditHeadHash), short(audit.HeadHash)))
	}
	if _, err := s.Scenario(); err != nil {
		v.CatalogueOK = false
		v.OK = false
		v.Findings = append(v.Findings, "catalogue replay failed: "+err.Error())
	} else {
		v.CatalogueOK = true
	}
	sort.Strings(v.Findings)
	return v, nil
}

func validSnapshotName(name string) bool {
	if name == "" || len(name) > 48 {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return false
		}
	}
	return true
}

func sortStrings(list []string) { sort.Strings(list) }

func sortedKeysObjects(m map[string]model.CatalogObject) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedKeysAssets(m map[string]model.Asset) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedKeysChasers(m map[string]model.Chaser) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedKeysVolumes(m map[string]model.ScreeningVolume) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
