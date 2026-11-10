package server

import (
	"context"
	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/recovery"
	"serverfleet.local/serverfleet/internal/storage"
	"time"
)

// PrepareLocalBootstrap is a local installer port, never an HTTP endpoint or
// password reset. Existing setup tokens last ten minutes; initialized stores
// and quarantined/fenced/maintenance authorities cannot issue one.
func PrepareLocalBootstrap(ctx context.Context, database string) (string, error) {
	gate, e := recovery.RuntimeGuard(ctx, database, nil)
	if e != nil || gate(ctx) != nil {
		return "", recovery.ErrQuarantined
	}
	db, e := storage.Open(ctx, storage.Config{Path: database})
	if e != nil {
		return "", e
	}
	defer db.Close()
	binding, e := recovery.ReadResidentAuthority(ctx, db)
	if e != nil {
		return "", e
	}
	policy := ""
	if binding != nil {
		policy = binding.PolicySHA256
	}
	gate = recovery.ResidentAuthorityGate(db, binding, policy, gate)
	if binding != nil {
		gate = recovery.ResidentMaintenanceGate(binding.Directory, gate)
	}
	validator, e := audit.NewValidator(AuditActions(), audit.DefaultLimits())
	if e != nil {
		return "", e
	}
	recorder, e := audit.NewSQLRecorder(db, validator, time.Now)
	if e != nil {
		return "", e
	}
	store, e := newIdentityStore(db, recorder)
	if e != nil {
		return "", e
	}
	identity, e := auth.NewIdentityService(store)
	if e != nil {
		return "", e
	}
	if gate(ctx) != nil {
		return "", recovery.ErrQuarantined
	}
	token, e := identity.PrepareSetup(ctx)
	if e != nil {
		return "", e
	}
	if gate(ctx) != nil {
		return "", recovery.ErrQuarantined
	}
	return token, nil
}
