package platform_test

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"serverfleet.local/serverfleet/apps/controller/server"
	"serverfleet.local/serverfleet/internal/recovery"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/storage/testkit"
)

func TestR2PlatformMigrationsUpgradeFromAdopted21AndReapply(t *testing.T) {
	ctx := context.Background()
	groups := server.Migrations()
	var old [][]storage.Migration
	var total int
	for _, group := range groups {
		filtered := []storage.Migration{}
		for _, migration := range group {
			total++
			if completionOnlyMigration(migration.Version) {
				continue
			}
			if migration.Version != "ds/0041_file_upload_task" && migration.Version != "ds/0043_upload_policies" && migration.Version != "cx/0008_file_execution_bindings" && migration.Version != "cx/0009_update_journal" && migration.Version != "ds/0062_acme_cleanup" && migration.Version != "ds/0063_cleanup_claim" && migration.Version != "ds/0042_file_distribution" && migration.Version != "cx/0010_gateway_artifact_bindings" {
				filtered = append(filtered, migration)
			}
		}
		if len(filtered) > 0 {
			old = append(old, filtered)
		}
	}
	legacyCount := 0
	for _, group := range old {
		legacyCount += len(group)
	}
	if legacyCount != 21 {
		t.Fatal("historical fixture is not actual adopted21", legacyCount)
	}
	if total != 45 {
		t.Fatal("unexpected composition migration count", total)
	}
	db := testkit.NewDB(t, old...)
	migrator, err := storage.NewMigrator(groups...)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = migrator.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err = migrator.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"cx_file_execution_bindings", "cx_update_installed", "cx_update_receipts"} {
		var n int
		if err = db.QueryRow(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&n); err != nil || n != 1 {
			t.Fatal(table, n, err)
		}
	}
}

func TestR2RestoredControllerRequiresOfflineReleaseAndLiveRuntimeGuard(t *testing.T) {
	ctx := context.Background()
	original, err := server.New(ctx, server.Config{DBPath: filepath.Join(t.TempDir(), "original.db"), Origin: "https://localhost"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	dbpath := filepath.Join(dir, "restored.db")
	if err = original.DB.BackupTo(ctx, dbpath); err != nil {
		t.Fatal(err)
	}
	original.Close()
	raw, err := os.ReadFile(dbpath)
	if err != nil {
		t.Fatal(err)
	}
	intent := recovery.RestoreIntent{SnapshotSHA256: sha256Hex(raw), ControllerID: "new-isolated-controller", RootSPKISHA256: strings.Repeat("a", 64), MinTrustGeneration: 4, MinRevocationSequence: 9}
	held, err := recovery.BeginQuarantine(ctx, dir, intent, recovery.QuarantineVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	hold := held.Hold()
	held.Close()
	cfg := server.Config{DBPath: dbpath, Origin: "https://localhost"}
	if unexpected, e := server.New(ctx, cfg); e == nil {
		unexpected.Close()
		t.Fatal("restored Controller started without mandatory latch")
	}
	var fenced atomic.Bool
	fenced.Store(true)
	// This verifier is an explicit offline-authority/fence DOUBLE. Actual
	// Controller/DB/HTTP enforcement is tested; remote fence is NOT claimed.
	latch, err := recovery.OpenQuarantine(dir, hold, recovery.QuarantineVerifier{Prepare: func(_ context.Context, h recovery.QuarantineHold) (recovery.ReleaseClearance, error) {
		return recovery.ReleaseClearance{HoldID: h.ID, SnapshotSHA256: h.SnapshotSHA256, ControllerID: h.ControllerID, RootSPKISHA256: h.RootSPKISHA256, TrustGeneration: 4, RevocationSequence: 9, FencingEvidenceSHA256: strings.Repeat("b", 64), ReconciledStateSHA256: strings.Repeat("c", 64), AuditRecordID: "fixture-clearance"}, nil
	}, Current: func(context.Context, recovery.QuarantineHold, recovery.ReleaseClearance) error {
		if !fenced.Load() {
			return recovery.ErrQuarantined
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer latch.Close()
	cfg.RecoveryLatch = latch
	if unexpected, e := server.New(ctx, cfg); e == nil {
		unexpected.Close()
		t.Fatal("restored Controller implicitly released quarantine")
	}
	if err = latch.Release(ctx); err != nil {
		t.Fatal(err)
	}
	restored, err := server.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	endpoint := httptest.NewTLSServer(restored)
	defer endpoint.Close()
	client := endpoint.Client()
	client.Transport.(*http.Transport).TLSClientConfig.MinVersion = tls.VersionTLS13
	response, err := client.Get(endpoint.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	fenced.Store(false)
	for _, route := range []string{"/healthz", "/api/v1/auth/session", "/api/v1/agent/enroll", "/api/v1/agent/link", "/api/v1/tasks", "/api/v1/transfers", "/api/v1/ai/sessions", "/api/v1/system/updates/jobs", "/api/v1/backups/anything/verify", "/api/v1/plugins"} {
		response, err = client.Get(endpoint.URL + route)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 503 {
			t.Fatal("quarantine route bypass", route, response.StatusCode)
		}
	}
	if _, err = restored.Tasks.CheckExecution(ctx, "missing-task", "missing-plan"); err != recovery.ErrQuarantined {
		t.Fatal("execution lookup bypassed quarantine", err)
	}
	t.Log("actual restored SQLite/Controller refuses startup before explicit release; live revocation blocks all HTTP route families and execution admission")
}

// Versions introduced only by the concentrated completion branch, after the
// accepted 29-version baseline. Historical SQL/checksums are left unchanged.
func completionOnlyMigration(v string) bool {
	switch v {
	case "cx/0011_encrypted_backups", "cx/0012_recovery_revocations", "cx/0013_recovery_reconciliation", "cx/0014_release_acquisition", "cx/0015_agent_certificate_csr", "cx/0016_node_certificate_chains", "cx/0017_node_certificate_sets", "cx/0018_public_certificate_orders", "cx/0019_controller_supervision", "cx/0020_public_certificate_renewals", "cx/0021_backup_maintenance", "cx/0022_operator_incidents", "cx/0023_incident_closure_evidence", "cx/0024_renewal_delivery", "ds/0083_certificate_replacements", "ds/0082_publishing_dns_tasks":
		return true
	}
	return false
}
