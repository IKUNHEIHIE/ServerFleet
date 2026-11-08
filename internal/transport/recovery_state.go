package transport

import (
	"context"
	"serverfleet.local/serverfleet/internal/storage"
	"sort"
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
	sort.Strings(out.RevokedSerials)
	sort.Strings(out.NodeIDs)
	return out, nil
}
