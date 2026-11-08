package transport

import (
	"context"
	"encoding/json"
	"serverfleet.local/serverfleet/internal/storage"
	"slices"
	"sort"
	"time"
)

type RecoveryEnrollmentState struct {
	RevocationSequence uint64
	RevokedSerials     []string
	NodeIDs            []string
}

// ReadRecoveryEnrollment reads the transport-owned aggregate from the SAME
// consistent snapshot selected by the coordinator; it exports no token or key.
func ReadRecoveryEnrollment(ctx context.Context, db storage.Queryer) (RecoveryEnrollmentState, error) {
	var raw []byte
	if db == nil || db.QueryRow(ctx, enrollmentReadSQL).Scan(&raw) != nil {
		return RecoveryEnrollmentState{}, ErrEnrollmentUnavailable
	}
	state, err := decodeEnrollment(raw)
	if err != nil {
		return RecoveryEnrollmentState{}, err
	}
	out := RecoveryEnrollmentState{RevocationSequence: state.RevocationSequence, RevokedSerials: []string{}, NodeIDs: []string{}}
	seen := map[string]bool{}
	for _, id := range state.Identities {
		if id.Revoked {
			out.RevokedSerials = append(out.RevokedSerials, id.Serial)
		}
		if !seen[id.NodeID] {
			seen[id.NodeID] = true
			out.NodeIDs = append(out.NodeIDs, id.NodeID)
		}
	}
	// A newer snapshot also carries revocations absent from its identity map.
	var tableCount int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='cx_recovery_revoked_serials'`).Scan(&tableCount); err != nil {
		return RecoveryEnrollmentState{}, err
	}
	if tableCount == 1 {
		rows, err := db.Query(ctx, `SELECT serial,revocation_sequence FROM cx_recovery_revoked_serials ORDER BY serial LIMIT 100001`)
		if err != nil {
			return RecoveryEnrollmentState{}, err
		}
		count := 0
		for rows.Next() {
			var serial string
			var sequence uint64
			if err = rows.Scan(&serial, &sequence); err != nil {
				rows.Close()
				return RecoveryEnrollmentState{}, err
			}
			count++
			if count > 100000 || serial == "" || sequence > out.RevocationSequence {
				rows.Close()
				return RecoveryEnrollmentState{}, ErrEnrollmentUnavailable
			}
			out.RevokedSerials = append(out.RevokedSerials, serial)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return RecoveryEnrollmentState{}, err
		}
	}
	sort.Strings(out.RevokedSerials)
	out.RevokedSerials = slices.Compact(out.RevokedSerials)
	sort.Strings(out.NodeIDs)
	return out, nil
}

type RestoreEnrollmentPolicy struct {
	Generation, RevocationSequence uint64
	RevokedSerials                 []string
	At                             time.Time
}

// ReconcileRestoredEnrollmentTx invalidates every old identity and enrollment
// capability. A new Intermediate never adopts an old node certificate merely
// because it chains to the same offline Root. Nodes require fresh enrollment.
func ReconcileRestoredEnrollmentTx(ctx context.Context, tx storage.Tx, p RestoreEnrollmentPolicy) error {
	if ctx == nil || tx == nil || p.Generation == 0 || p.RevocationSequence == ^uint64(0) || p.At.IsZero() || len(p.RevokedSerials) > 100000 {
		return ErrEnrollmentUnavailable
	}
	var raw []byte
	if tx.QueryRow(ctx, enrollmentReadSQL).Scan(&raw) != nil {
		return ErrEnrollmentUnavailable
	}
	state, err := decodeEnrollment(raw)
	if err != nil || p.RevocationSequence < state.RevocationSequence || p.At.Before(state.ClockFloor) {
		return ErrEnrollmentUnavailable
	}
	for fp, id := range state.Identities {
		if id.TrustGeneration >= p.Generation {
			return ErrEnrollmentUnavailable
		}
		id.Revoked = true
		id.Enabled = false
		state.Identities[fp] = id
	}
	// Root-authorized serial union is retained even for certificates not present
	// in this older snapshot: persist tombstones owned by transport below.
	serials := map[string]bool{}
	for _, serial := range p.RevokedSerials {
		if serial == "" || len(serial) > 128 {
			return ErrEnrollmentUnavailable
		}
		serials[serial] = true
	}
	for _, id := range state.Identities {
		serials[id.Serial] = true
	}
	for serial := range serials {
		if _, err = tx.Exec(ctx, `INSERT INTO cx_recovery_revoked_serials(serial,trust_generation,revocation_sequence,recorded_at) VALUES(?,?,?,?) ON CONFLICT(serial) DO UPDATE SET trust_generation=MAX(trust_generation,excluded.trust_generation),revocation_sequence=MAX(revocation_sequence,excluded.revocation_sequence)`, serial, p.Generation, p.RevocationSequence+1, storage.EncodeTime(p.At)); err != nil {
			return err
		}
	}
	state.Tokens = map[string]enrollmentTokenRecord{}
	state.Requests = map[string]enrollmentRecord{}
	state.RevocationSequence = p.RevocationSequence + 1
	state.ClockFloor = p.At.UTC()
	encoded, err := json.Marshal(state)
	if err != nil || len(encoded) > 16<<20 {
		return ErrEnrollmentUnavailable
	}
	result, err := tx.Exec(ctx, `UPDATE cx_agent_enrollment SET payload=? WHERE singleton=1`, encoded)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return ErrEnrollmentUnavailable
	}
	return nil
}
func RecoveryMigrations() []storage.Migration {
	return []storage.Migration{{Version: "cx/0012_recovery_revocations", Name: "transport-owned offline recovery revocation tombstones", Statements: []string{`CREATE TABLE cx_recovery_revoked_serials(serial TEXT PRIMARY KEY,trust_generation INTEGER NOT NULL,revocation_sequence INTEGER NOT NULL,recorded_at TEXT NOT NULL)`}}}
}
