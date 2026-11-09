package bootstrap

import (
	"context"
	"encoding/json"
	"path/filepath"
	"time"

	"serverfleet.local/serverfleet/apps/controller/localpki"
	"serverfleet.local/serverfleet/apps/controller/server"
	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/recovery"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/updates"
)

type recoveryRuntime struct {
	Version         int                 `json:"version"`
	Directory       string              `json:"directory"`
	Authorization   string              `json:"authorization"`
	NewPKIDirectory string              `json:"newPkiDirectory"`
	RootPin         string              `json:"rootPin"`
	Fence           recoveryFenceClient `json:"fence"`
}
type recoveryActivation struct {
	Version                int                       `json:"version"`
	Clearance              recovery.ReleaseClearance `json:"clearance"`
	ReconciliationSHA256   string                    `json:"reconciliationSha256"`
	SupervisorPolicySHA256 string                    `json:"supervisorPolicySha256"`
	ActivatedAt            time.Time                 `json:"activatedAt"`
}
type recoveryActivationContext struct {
	config        recoveryRuntime
	staged        recovery.StagedBundle
	reconciled    recovery.ReconciliationReceipt
	receiptSHA    string
	request       updates.ResidentFenceRequest
	authorization []byte
}

func loadRecoveryActivationContext(ctx context.Context, filename string, c updates.ResidentSupervisorConfig, at time.Time) (recoveryActivationContext, error) {
	var out recoveryActivationContext
	raw, e := recovery.ReadPrivateFile(filename, 32<<10)
	if e != nil || strictRecoveryJSON(raw, &out.config) != nil || out.config.Version != 1 || !filepath.IsAbs(out.config.Directory) || c.Component != "controller" || c.RootPin != out.config.RootPin || c.RecoveryRuntime != filename || c.Database != filepath.Join(out.config.Directory, "restored.db") {
		return out, recovery.ErrRecoveryRejected
	}
	d := out.config.Directory
	if _, e = readOfflineRecord(filepath.Join(d, "staged.json"), &out.staged, 16<<10); e != nil {
		return out, e
	}
	receipt, e := readOfflineRecord(filepath.Join(d, "reconciled.json"), &out.reconciled, 16<<10)
	if e != nil {
		return out, e
	}
	out.receiptSHA = recoveryDigest(receipt)
	if at.IsZero() {
		at = time.Now()
	}
	authority, e := localpki.VerifyRecoveryAuthorityAt(filepath.Join(d, "pki/root.pem"), out.config.Authorization, out.config.RootPin, at)
	if e != nil {
		return out, e
	}
	p := authority.Payload()
	r := out.reconciled
	if r.HoldID != out.staged.Hold.ID || r.AuthoritySHA256 != authority.SHA256() || r.CipherSHA256 != out.staged.CipherSHA256 || r.ManifestSHA256 != out.staged.ManifestSHA256 || r.CipherSHA256 != p.CipherSHA256 || r.ManifestSHA256 != p.ManifestSHA256 || r.OldAuthorityID != p.OldAuthorityID || r.NewAuthorityID != p.NewAuthorityID || r.NewAuthorityID != c.NodeID || r.RootSPKI != p.RootSPKI || r.TrustGeneration != p.TrustGeneration || r.RevocationSequence != p.RevocationSequence+1 || r.IssuerSHA256 != p.IssuerSHA256 || r.CreatedAt.After(at) {
		return out, recovery.ErrRecoveryRejected
	}
	material, e := localpki.LoadProvisioned(out.config.NewPKIDirectory, out.config.RootPin)
	if e != nil || !material.Provisioned || material.AuthorityID != r.NewAuthorityID || material.IssuerSHA256 != r.IssuerSHA256 || material.TrustGeneration != r.TrustGeneration || material.RootSPKI != r.RootSPKI {
		return out, recovery.ErrRecoveryRejected
	}
	if auth.ValidateCredentialKeyDirectory(filepath.Join(d, "credentials")) != nil {
		return out, recovery.ErrRecoveryRejected
	}
	out.authorization, e = recovery.ReadPrivateFile(out.config.Authorization, 128<<10)
	if e != nil {
		return out, e
	}
	out.request = updates.ResidentFenceRequest{AuthorizationSHA256: r.AuthoritySHA256, OldAuthorityID: r.OldAuthorityID, NewAuthorityID: r.NewAuthorityID, TrustGeneration: r.TrustGeneration}
	db, e := storage.Open(ctx, storage.Config{Path: c.Database, ReadOnly: true})
	if e != nil {
		return out, e
	}
	defer db.Close()
	actual, e := recovery.ReadReconciliation(ctx, db)
	if e != nil || actual != r {
		return out, recovery.ErrRecoveryRejected
	}
	return out, nil
}
func (x recoveryActivationContext) clearance(fence updates.ResidentFenceRecord) recovery.ReleaseClearance {
	b, _ := json.Marshal(fence)
	return recovery.ReleaseClearance{HoldID: x.staged.Hold.ID, SnapshotSHA256: x.staged.CipherSHA256, ControllerID: x.reconciled.NewAuthorityID, RootSPKISHA256: x.reconciled.RootSPKI, TrustGeneration: x.reconciled.TrustGeneration, RevocationSequence: x.reconciled.RevocationSequence, FencingEvidenceSHA256: recoveryDigest(b), ReconciledStateSHA256: x.receiptSHA, AuditRecordID: "activate-" + x.staged.Hold.ID[:32]}
}
func activatedRecoveryVerifier(filename string, c updates.ResidentSupervisorConfig, activation recoveryActivation) recovery.QuarantineVerifier {
	return recovery.QuarantineVerifier{Current: func(ctx context.Context, hold recovery.QuarantineHold, clearance recovery.ReleaseClearance) error {
		if activation.Version != 1 || activation.SupervisorPolicySHA256 != updates.ResidentPolicySHA(c) || activation.ActivatedAt.IsZero() || activation.ActivatedAt.After(time.Now()) || clearance != activation.Clearance {
			return recovery.ErrQuarantined
		}
		x, e := loadRecoveryActivationContext(ctx, filename, c, activation.ActivatedAt)
		if e != nil || x.staged.Hold != hold || x.receiptSHA != activation.ReconciliationSHA256 {
			return recovery.ErrQuarantined
		}
		record, e := callRecoveryFence(ctx, x.config.Fence, x.request, nil)
		if e != nil || x.clearance(record) != clearance {
			return recovery.ErrQuarantined
		}
		db, e := storage.Open(ctx, storage.Config{Path: c.Database, ReadOnly: true})
		if e != nil {
			return e
		}
		defer db.Close()
		stored, e := recovery.ReadResidentActivation(ctx, db)
		raw, _ := json.Marshal(activation)
		if e != nil || stored != string(raw) {
			return recovery.ErrQuarantined
		}
		binding, e := recovery.ReadResidentAuthority(ctx, db)
		expected := recovery.ResidentAuthority{AuthorityID: c.NodeID, PolicySHA256: updates.ResidentPolicySHA(c), Directory: c.Directory, Mode: "owned"}
		if e != nil || binding == nil || *binding != expected {
			return recovery.ErrQuarantined
		}
		return nil
	}}
}
func openActivatedRecovery(ctx context.Context, filename string, c updates.ResidentSupervisorConfig) (*recovery.QuarantineLatch, error) {
	var activation recoveryActivation
	raw, e := recovery.ReadPrivateFile(filename, 32<<10)
	var config recoveryRuntime
	if e != nil || strictRecoveryJSON(raw, &config) != nil {
		return nil, recovery.ErrQuarantined
	}
	if _, e = readOfflineRecord(filepath.Join(config.Directory, "activation.json"), &activation, 16<<10); e != nil {
		return nil, e
	}
	x, e := loadRecoveryActivationContext(ctx, filename, c, activation.ActivatedAt)
	if e != nil {
		return nil, e
	}
	latch, e := recovery.OpenQuarantine(config.Directory, x.staged.Hold, activatedRecoveryVerifier(filename, c, activation))
	if e != nil {
		return nil, e
	}
	if latch.Check(ctx) != nil {
		latch.Close()
		return nil, recovery.ErrQuarantined
	}
	return latch, nil
}

// Explicit emergency operation: offline Root authority is checked BEFORE the
// external fence call. Neither a backup nor a caller's boolean grants release.
func activateRecoveredController(ctx context.Context, filename, supervisorFile string) (recoveryActivation, error) {
	var zero recoveryActivation
	c, e := updates.LoadResidentSupervisor(supervisorFile)
	if e != nil {
		return zero, e
	}
	x, e := loadRecoveryActivationContext(ctx, filename, c, time.Now())
	if e != nil {
		return zero, e
	}
	if recovery.WriteOfflineRecord(x.config.Directory, "activation-attempt.json", map[string]any{"authoritySha256": x.request.AuthorizationSHA256, "at": time.Now().UTC()}) != nil {
		return zero, recovery.ErrQuarantined
	}
	fence, e := callRecoveryFence(ctx, x.config.Fence, x.request, x.authorization)
	if e != nil {
		return zero, e
	}
	activation := recoveryActivation{Version: 1, Clearance: x.clearance(fence), ReconciliationSHA256: x.receiptSHA, SupervisorPolicySHA256: updates.ResidentPolicySHA(c), ActivatedAt: time.Now().UTC()}
	verifier := activatedRecoveryVerifier(filename, c, activation)
	verifier.Prepare = func(ctx context.Context, hold recovery.QuarantineHold) (recovery.ReleaseClearance, error) {
		if hold != x.staged.Hold {
			return recovery.ReleaseClearance{}, recovery.ErrQuarantined
		}
		live, e := callRecoveryFence(ctx, x.config.Fence, x.request, nil)
		if e != nil || live != fence {
			return recovery.ReleaseClearance{}, recovery.ErrQuarantined
		}
		db, e := storage.Open(ctx, storage.Config{Path: c.Database})
		if e != nil {
			return recovery.ReleaseClearance{}, e
		}
		defer db.Close()
		validator, e := audit.NewValidator(server.AuditActions(), audit.DefaultLimits())
		if e != nil {
			return recovery.ReleaseClearance{}, e
		}
		recorder, e := audit.NewSQLRecorder(db, validator, time.Now)
		if e != nil {
			return recovery.ReleaseClearance{}, e
		}
		raw, _ := json.Marshal(activation)
		e = db.InTx(ctx, func(txctx context.Context, tx storage.Tx) error {
			actual, e := recovery.ReadReconciliation(txctx, tx)
			if e != nil || actual != x.reconciled {
				return recovery.ErrQuarantined
			}
			if e = recovery.BindResidentAuthorityTx(txctx, tx, recovery.ResidentAuthority{AuthorityID: c.NodeID, PolicySHA256: updates.ResidentPolicySHA(c), Directory: c.Directory, Mode: "owned"}, true); e != nil {
				return e
			}
			if e = recovery.RecordResidentActivationTx(txctx, tx, string(raw)); e != nil {
				return e
			}
			return recorder.AppendTx(txctx, tx, audit.Event{SubjectID: x.reconciled.NewAuthorityID, Action: "recovery.activate", Result: "success", RequestID: activation.Clearance.AuditRecordID, Source: audit.SourceSystem, ReasonCode: "offline_root_authority_and_live_owned_old_controller_fence", At: activation.ActivatedAt})
		})
		if e != nil {
			return recovery.ReleaseClearance{}, e
		}
		if recovery.WriteOfflineRecord(x.config.Directory, "activation.json", activation) != nil {
			return recovery.ReleaseClearance{}, recovery.ErrQuarantined
		}
		return activation.Clearance, nil
	}
	latch, e := recovery.OpenQuarantine(x.config.Directory, x.staged.Hold, verifier)
	if e != nil {
		return zero, e
	}
	defer latch.Close()
	if latch.Release(ctx) != nil {
		return zero, recovery.ErrQuarantined
	}
	return activation, nil
}
