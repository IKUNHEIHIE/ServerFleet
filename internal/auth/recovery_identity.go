package auth

import (
	"context"
	"encoding/json"
	"math"
	"serverfleet.local/serverfleet/internal/storage"
	"time"
)

// RestoredIdentityPolicy is fresh OFFLINE recovery authority, never read from
// the backup or browser. Reconciliation can revoke/disable, never resurrect.
type RestoredIdentityPolicy struct {
	ActorID              string
	At                   time.Time
	RevokedCredentialIDs []string
	DisabledSubjectIDs   []string
}

// InvalidateRestoredIdentity is a pure auth-owned transform for the coordinator's
// single recovery transaction. It opens no database, bypasses no quarantine, and
// grants no authority to apply its output to a live store. Every old session,
// token and approval is invalidated by increasing identity/policy epochs. Key
// usage counters and at-most-once fences are retained, staged rotations removed.
// Current revocations MUST be supplied from outside the old snapshot. New master
// keys/epochs and task reconciliation remain mandatory before release.
func InvalidateRestoredIdentity(raw []byte, policy RestoredIdentityPolicy) ([]byte, error) {
	st, err := decodeIdentity(raw)
	if err != nil || !st.Initialized || !validPolicyID(policy.ActorID) || policy.At.IsZero() || policy.At.Before(st.ClockFloor) || st.PolicyRevision == math.MaxUint64 || !validExactIDs(policy.RevokedCredentialIDs) || !validExactIDs(policy.DisabledSubjectIDs) {
		return nil, ErrIdentityUnavailable
	}
	for id, u := range st.Users {
		if u.Epoch == math.MaxUint64 {
			return nil, ErrIdentityUnavailable
		}
		u.Epoch++
		st.Users[id] = u
	}
	for digest, s := range st.Sessions {
		s.Revoked = true
		st.Sessions[digest] = s
	}
	for _, id := range policy.DisabledSubjectIDs {
		if u, ok := st.Users[id]; ok {
			u.Disabled = true
			st.Users[id] = u
		}
	}
	for _, id := range policy.RevokedCredentialIDs {
		if r, ok := st.Credentials[id]; ok {
			r.Revoked = true
			st.Credentials[id] = r
		}
	}
	st.CredentialChanges = map[string]credentialChange{}
	st.PolicyRevision++
	st.ClockFloor = policy.At.UTC()
	if appendIdentityAudit(st, policy.ActorID, "auth.recovery.invalidate", "success", policy.At.UTC()) != nil {
		return nil, ErrIdentityUnavailable
	}
	encoded, err := json.Marshal(st)
	if err != nil || len(encoded) > 16<<20 {
		return nil, ErrIdentityUnavailable
	}
	if _, err = decodeIdentity(encoded); err != nil {
		return nil, err
	}
	return encoded, nil
}

// ApplyRestoredIdentityTx is an auth-owned write on an offline, quarantined
// database transaction. The coordinator must supply verified fresh policy.
func ApplyRestoredIdentityTx(ctx context.Context, tx storage.Tx, policy RestoredIdentityPolicy) error {
	if ctx == nil || tx == nil {
		return ErrIdentityUnavailable
	}
	var raw []byte
	var initialized bool
	if err := tx.QueryRow(ctx, readIdentitySQL).Scan(&raw, &initialized); err != nil || !initialized {
		return ErrIdentityUnavailable
	}
	next, err := InvalidateRestoredIdentity(raw, policy)
	if err != nil {
		return err
	}
	defer clear(next)
	result, err := tx.Exec(ctx, `UPDATE cx_auth_identity SET payload=? WHERE singleton=1 AND ever_initialized=1`, next)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil || n != 1 {
		return ErrIdentityUnavailable
	}
	return nil
}
