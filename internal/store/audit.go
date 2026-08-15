package store

import (
	"fmt"
	"strconv"

	"DebrisLedger/internal/strictjson"
)

// GenesisHash is the previous-hash value of the first audit entry.
const GenesisHash = "0000000000000000000000000000000000000000000000000000000000000000"

// AuditEntry is one link of the hash chain.
//
// Hash is the SHA-256 of the canonical field join. Because PrevHash is part of
// that join, editing or reordering any earlier entry invalidates every later
// hash, which is what makes the log tamper-evident.
type AuditEntry struct {
	Sequence      int    `json:"seq"`
	Kind          string `json:"kind"`
	Subject       string `json:"subject"`
	EpochSeconds  int64  `json:"epoch_s"`
	PayloadSHA256 string `json:"payload_sha256"`
	PrevHash      string `json:"prev_hash"`
	Hash          string `json:"hash"`
}

// ComputeHash returns the hash the entry should carry.
func (e AuditEntry) ComputeHash() string {
	return HashStrings(
		strconv.Itoa(e.Sequence),
		e.Kind,
		e.Subject,
		strconv.FormatInt(e.EpochSeconds, 10),
		e.PayloadSHA256,
		e.PrevHash,
	)
}

// AuditVerification is the outcome of walking the chain.
type AuditVerification struct {
	Entries    int         `json:"entries"`
	OK         bool        `json:"ok"`
	HeadHash   string      `json:"head_hash"`
	Failures   []string    `json:"failures"`
	KindCounts []KindCount `json:"kind_counts"`
}

// KindCount is one row of the audit histogram.
type KindCount struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

// appendAudit adds one entry to the chain, linking it to the current head.
func (s *Store) appendAudit(kind, subject string, epochSeconds int64, payloadHash string) (AuditEntry, error) {
	entries, err := s.AuditEntries()
	if err != nil {
		return AuditEntry{}, err
	}
	prev := GenesisHash
	if len(entries) > 0 {
		prev = entries[len(entries)-1].Hash
	}
	entry := AuditEntry{
		Sequence:      len(entries) + 1,
		Kind:          kind,
		Subject:       subject,
		EpochSeconds:  epochSeconds,
		PayloadSHA256: payloadHash,
		PrevHash:      prev,
	}
	entry.Hash = entry.ComputeHash()
	line, err := strictjson.MarshalCompact(entry)
	if err != nil {
		return AuditEntry{}, err
	}
	if err := AppendLine(s.AuditPath(), line); err != nil {
		return AuditEntry{}, err
	}
	return entry, nil
}

// AuditEntries reads the whole chain in file order.
func (s *Store) AuditEntries() ([]AuditEntry, error) {
	path := s.AuditPath()
	if !exists(path) {
		return nil, nil
	}
	var out []AuditEntry
	err := strictjson.DecodeLinesFile(path, func(_ int, raw []byte) error {
		var entry AuditEntry
		if err := strictjson.Unmarshal(raw, &entry); err != nil {
			return err
		}
		out = append(out, entry)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// VerifyAudit recomputes every hash and every link.
func (s *Store) VerifyAudit() (AuditVerification, error) {
	entries, err := s.AuditEntries()
	if err != nil {
		return AuditVerification{}, err
	}
	v := AuditVerification{Entries: len(entries), OK: true, HeadHash: GenesisHash, Failures: []string{}}
	counts := map[string]int{}
	prev := GenesisHash
	for i, e := range entries {
		want := i + 1
		if e.Sequence != want {
			v.OK = false
			v.Failures = append(v.Failures,
				fmt.Sprintf("entry %d carries sequence %d", want, e.Sequence))
		}
		if e.PrevHash != prev {
			v.OK = false
			v.Failures = append(v.Failures,
				fmt.Sprintf("entry %d links to %s but the previous head is %s", want, short(e.PrevHash), short(prev)))
		}
		if got := e.ComputeHash(); got != e.Hash {
			v.OK = false
			v.Failures = append(v.Failures,
				fmt.Sprintf("entry %d hash mismatch: stored %s, recomputed %s", want, short(e.Hash), short(got)))
		}
		counts[e.Kind]++
		prev = e.Hash
	}
	v.HeadHash = prev
	v.KindCounts = sortedKindCounts(counts)
	return v, nil
}

func sortedKindCounts(counts map[string]int) []KindCount {
	kinds := make([]string, 0, len(counts))
	for k := range counts {
		kinds = append(kinds, k)
	}
	sortStrings(kinds)
	out := make([]KindCount, 0, len(kinds))
	for _, k := range kinds {
		out = append(out, KindCount{Kind: k, Count: counts[k]})
	}
	return out
}

func short(hash string) string {
	if len(hash) <= 12 {
		return hash
	}
	return hash[:12]
}
