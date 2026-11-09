package transport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"serverfleet.local/serverfleet/internal/storage"
	"sort"
	"time"
)

type CertificateSetEntry struct {
	NodeID      string `json:"nodeId"`
	TaskID      string `json:"taskId"`
	PlanSHA256  string `json:"planSha256"`
	Domain      string `json:"domain"`
	KeyRef      string `json:"keyRef"`
	LeafSHA256  string `json:"leafSha256"`
	ChainSHA256 string `json:"chainSha256"`
	CSRSHA256   string `json:"csrSha256"`
}
type CertificateSet struct {
	ID        string                `json:"id"`
	Entries   []CertificateSetEntry `json:"entries"`
	CreatedAt time.Time             `json:"createdAt"`
}

const CertificateSetPrefix = "nodecertset-"

func CertificateSetMigrations() []storage.Migration {
	return []storage.Migration{{Version: "cx/0017_node_certificate_sets", Name: "immutable node-specific certificate installation sets", Statements: []string{`CREATE TABLE cx_node_certificate_sets(id TEXT PRIMARY KEY,payload BLOB NOT NULL CHECK(length(payload)<=16384),created_at TEXT NOT NULL)`}}}
}
func FreezeCertificateSet(entries []CertificateSetEntry) (CertificateSet, error) {
	if len(entries) < 2 || len(entries) > 8 {
		return CertificateSet{}, ErrPeerIdentity
	}
	entries = append([]CertificateSetEntry{}, entries...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].NodeID < entries[j].NodeID })
	domain := ""
	for i, entry := range entries {
		if !validCSRKeyRef(entry.NodeID) || !validCSRKeyRef(entry.TaskID) || !validCSRKeyRef(entry.KeyRef) || !validCSRDomain(entry.Domain) || !canonicalDigest(entry.PlanSHA256) || !canonicalDigest(entry.LeafSHA256) || !canonicalDigest(entry.ChainSHA256) || !canonicalDigest(entry.CSRSHA256) || i > 0 && entry.NodeID == entries[i-1].NodeID {
			return CertificateSet{}, ErrPeerIdentity
		}
		if i == 0 {
			domain = entry.Domain
		} else if entry.Domain != domain {
			return CertificateSet{}, ErrPeerIdentity
		}
	}
	raw, _ := json.Marshal(entries)
	sum := sha256.Sum256(raw)
	return CertificateSet{ID: CertificateSetPrefix + hex.EncodeToString(sum[:]), Entries: entries}, nil
}
func CertificateSetEntryFromReceipt(r CertificateChainReceipt) CertificateSetEntry {
	return CertificateSetEntry{NodeID: r.NodeID, TaskID: r.TaskID, PlanSHA256: r.PlanSHA256, Domain: r.Result.Domain, KeyRef: r.Result.KeyRef, LeafSHA256: r.Result.LeafSHA256, ChainSHA256: r.Result.ChainSHA256, CSRSHA256: r.Result.CSRSHA256}
}
func RecordCertificateSetTx(ctx context.Context, tx storage.Tx, set CertificateSet) error {
	frozen, err := FreezeCertificateSet(set.Entries)
	if err != nil || frozen.ID != set.ID || set.CreatedAt.IsZero() {
		return ErrPeerIdentity
	}
	raw, _ := json.Marshal(frozen.Entries)
	for _, entry := range frozen.Entries {
		receipt, err := ReadCertificateChain(ctx, tx, entry.TaskID)
		if err != nil || CertificateSetEntryFromReceipt(receipt) != entry {
			return ErrPeerIdentity
		}
	}
	var old []byte
	err = tx.QueryRow(ctx, `SELECT payload FROM cx_node_certificate_sets WHERE id=?`, set.ID).Scan(&old)
	if err == nil {
		if !bytes.Equal(old, raw) {
			return ErrPeerIdentity
		}
		return nil
	}
	if !storage.IsNotFound(err) {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO cx_node_certificate_sets VALUES(?,?,?)`, set.ID, raw, storage.EncodeTime(set.CreatedAt))
	return err
}
func ReadCertificateSet(ctx context.Context, db storage.Queryer, id string) (CertificateSet, error) {
	var raw []byte
	var at string
	if ctx == nil || db == nil || len(id) != len(CertificateSetPrefix)+64 {
		return CertificateSet{}, ErrPeerIdentity
	}
	if err := db.QueryRow(ctx, `SELECT payload,created_at FROM cx_node_certificate_sets WHERE id=? AND length(payload)<=16384`, id).Scan(&raw, &at); err != nil {
		return CertificateSet{}, err
	}
	var entries []CertificateSetEntry
	if json.Unmarshal(raw, &entries) != nil {
		return CertificateSet{}, ErrPeerIdentity
	}
	set, err := FreezeCertificateSet(entries)
	if err != nil || set.ID != id {
		return CertificateSet{}, ErrPeerIdentity
	}
	canonical, _ := json.Marshal(set.Entries)
	if !bytes.Equal(raw, canonical) {
		return CertificateSet{}, ErrPeerIdentity
	}
	set.CreatedAt, err = storage.DecodeTime(at)
	return set, err
}
