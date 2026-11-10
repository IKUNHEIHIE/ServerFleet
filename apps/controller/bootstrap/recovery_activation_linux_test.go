//go:build linux

package bootstrap

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"serverfleet.local/serverfleet/apps/controller/localpki"
	"serverfleet.local/serverfleet/apps/controller/server"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/recovery"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/transport"
	"serverfleet.local/serverfleet/internal/updates"
)

// This child is an actual owned OS process. Its private readiness protocol
// isolates fencing/activation from the separately tested Controller HTTP server.
func TestRecoveryOwnedChild(t *testing.T) {
	if os.Getenv("SF_TEST_RECOVERY_CHILD") != "1" {
		t.Skip("private recovery subprocess only")
	}
	raw, _ := json.Marshal(updates.OwnedProcessObservation{PID: os.Getpid(), Nonce: os.Getenv("SF_SUPERVISOR_NONCE")})
	if os.WriteFile(os.Getenv("SF_CONTROLLER_HEALTH_FILE"), raw, 0600) != nil {
		os.Exit(71)
	}
	for {
		time.Sleep(time.Second)
	}
}

func TestOfflineRootMTLSFenceAndRecoveredControllerActivation(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires a genuine non-root process owner")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	base := t.TempDir()
	if e := os.Chmod(base, 0700); e != nil {
		t.Fatal(e)
	}
	private := func(name string) string {
		p := filepath.Join(base, name)
		if e := os.Mkdir(p, 0700); e != nil {
			t.Fatal(e)
		}
		return p
	}
	write := func(p string, raw []byte) {
		if e := os.WriteFile(p, raw, 0600); e != nil {
			t.Fatal(e)
		}
	}
	read := func(p string) []byte {
		raw, e := os.ReadFile(p)
		if e != nil {
			t.Fatal(e)
		}
		return raw
	}
	writeJSON := func(p string, v any) {
		raw, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		write(p, raw)
	}
	offline := filepath.Join(base, "offline-root")
	pin, e := localpki.CreateOfflineRoot(offline, "isolated recovery test Root")
	if e != nil {
		t.Fatal(e)
	}
	rootFile := filepath.Join(offline, localpki.ProvisionedRootName)
	rootPEM := read(rootFile)
	rootBlock, _ := pem.Decode(rootPEM)
	anchor, e := x509.ParseCertificate(rootBlock.Bytes)
	if e != nil {
		t.Fatal(e)
	}
	keyBlock, _ := pem.Decode(read(filepath.Join(offline, "root-key.pem")))
	rootKey, e := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if e != nil {
		t.Fatal(e)
	}
	tlsDir := private("tls")
	leaf := func(name string, usage x509.ExtKeyUsage) (string, string, string) {
		key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if e != nil {
			t.Fatal(e)
		}
		serial, e := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
		if e != nil {
			t.Fatal(e)
		}
		template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: name}, DNSNames: []string{"fence.test"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage}}
		der, e := x509.CreateCertificate(rand.Reader, template, anchor, key.Public(), rootKey)
		if e != nil {
			t.Fatal(e)
		}
		rawKey, e := x509.MarshalPKCS8PrivateKey(key)
		if e != nil {
			t.Fatal(e)
		}
		certFile, keyFile := filepath.Join(tlsDir, name+".pem"), filepath.Join(tlsDir, name+"-key.pem")
		write(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
		write(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: rawKey}))
		return certFile, keyFile, recoveryDigest(der)
	}
	serverCert, serverKey, serverPin := leaf("parent", x509.ExtKeyUsageServerAuth)
	clientCert, clientKey, clientPin := leaf("recovery-peer", x509.ExtKeyUsageClientAuth)
	wrongCert, wrongKey, _ := leaf("unprovisioned-peer", x509.ExtKeyUsageClientAuth)
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	_, port, _ := net.SplitHostPort(address)
	cache := private("authorizations")
	fenceConfig := recoveryFenceServer{Version: 1, Listen: address, Certificate: serverCert, Key: serverKey, ClientRoot: rootFile, ClientCertificateSHA256: clientPin, RootCertificate: rootFile, RootPin: pin, AuthorizationDirectory: cache, MinimumTrustGeneration: 2}
	fenceFile := filepath.Join(base, "fence.json")
	writeJSON(fenceFile, fenceConfig)
	client := recoveryFenceClient{Origin: "https://fence.test:" + port, Addresses: []string{"127.0.0.1"}, RootCertificate: rootFile, RootSHA256: recoveryDigest(rootPEM), ServerCertificateSHA256: serverPin, Certificate: clientCert, Key: clientKey}
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	image := read(exe)
	slot := private("old-slot")
	write(filepath.Join(slot, "active"), image)
	if e = os.Chmod(filepath.Join(slot, "active"), 0700); e != nil {
		t.Fatal(e)
	}
	pub, _, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	old := updates.ResidentSupervisorConfig{Format: 1, Component: "controller", NodeID: "old-controller", Directory: slot, Mailbox: private("mailbox"), Inbox: private("inbox"), StageDir: private("stage"), HealthFile: filepath.Join(private("health"), "ready.json"), Initial: updates.ReleaseManifest{Format: 1, Component: "controller", Version: "1.0.0", Sequence: 1, OS: "linux", Architecture: runtime.GOARCH, SHA256: recoveryDigest(image), Bytes: int64(len(image))}, Policy: updates.ReleasePolicy{Component: "controller", Architecture: runtime.GOARCH, MinimumSequence: 1, MaxBytes: 256 << 20, TrustedKeys: map[string]ed25519.PublicKey{"release": pub}}, Arguments: []string{"-test.run=^TestRecoveryOwnedChild$"}, Environment: []string{"SF_TEST_RECOVERY_CHILD=1"}, FenceConfiguration: fenceFile, FenceConfigurationSHA256: recoveryDigest(read(fenceFile)), RootCertificate: rootFile, RootCertificateSHA256: recoveryDigest(rootPEM), RootPin: pin}
	ready := make(chan updates.OwnedProcessObservation, 2)
	serving := make(chan struct{}, 2)
	hooks := updates.ResidentSupervisorHooks{
		Current:      func(c context.Context, _ transport.ResidentUpdateIntent, _ string) error { return c.Err() },
		BeforeStop:   func(c context.Context, _ transport.ResidentUpdateIntent) error { return c.Err() },
		Stopped:      func(c context.Context, _ transport.ResidentUpdateIntent) error { return c.Err() },
		RollbackSafe: func(c context.Context) error { return c.Err() },
		Commit: func(context.Context, transport.ResidentUpdateIntent, updates.InstallReceipt) (string, error) {
			return "unused", nil
		},
		Ready: func(c context.Context, _ updates.ReleaseManifest, p updates.OwnedProcessObservation) error {
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			for {
				var observed updates.OwnedProcessObservation
				if raw, e := os.ReadFile(old.HealthFile); e == nil && json.Unmarshal(raw, &observed) == nil && observed.PID == p.PID && observed.Nonce == p.Nonce {
					ready <- p
					return nil
				}
				select {
				case <-c.Done():
					return c.Err()
				case <-ticker.C:
				}
			}
		},
		ServeFence: func(c context.Context, control updates.ResidentFenceControl) (func() error, error) {
			close, e := serveRecoveryFence(c, old, control)
			if e == nil {
				serving <- struct{}{}
			}
			return close, e
		},
	}
	startParent := func() (context.CancelFunc, <-chan struct{}) {
		parentCtx, stop := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() { defer close(done); _ = updates.RunResidentSupervisor(parentCtx, old, hooks) }()
		t.Cleanup(func() {
			stop()
			select {
			case <-done:
			case <-time.After(6 * time.Second):
				t.Error("parent did not stop")
			}
		})
		return stop, done
	}
	stop, stopped := startParent()
	var child updates.OwnedProcessObservation
	select {
	case child = <-ready:
	case <-ctx.Done():
		t.Fatal("child not ready")
	}
	select {
	case <-serving:
	case <-ctx.Done():
		t.Fatal("fence not listening")
	}

	// Begin at the already-reconciled boundary. These files/SQL are explicit
	// fixtures; this test does not claim backup extraction/reconcile coverage.
	restored := private("restored")
	newPKI := filepath.Join(base, "new-pki")
	bundle := filepath.Join(base, "new-bundle")
	if e = localpki.RequestIntermediate(newPKI, "new-controller"); e != nil {
		t.Fatal(e)
	}
	if e = localpki.SignIntermediate(offline, newPKI, bundle, "new-controller", 2); e != nil {
		t.Fatal(e)
	}
	material, e := localpki.InstallIntermediate(newPKI, bundle, pin)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Mkdir(filepath.Join(restored, "pki"), 0700); e != nil {
		t.Fatal(e)
	}
	write(filepath.Join(restored, "pki/root.pem"), rootPEM)
	if e = auth.EnsureCredentialKeyDirectory(filepath.Join(restored, "credentials")); e != nil {
		t.Fatal(e)
	}
	initialized, e := server.New(ctx, server.Config{DBPath: filepath.Join(restored, "restored.db"), Origin: "https://restored.test", StagingDir: private("restore-stage"), CredentialKeyDir: filepath.Join(restored, "credentials")})
	if e != nil {
		t.Fatal("fixture initialized database/keyring", e)
	}
	if e = initialized.Close(); e != nil {
		t.Fatal(e)
	}
	payload := localpki.RecoveryAuthorityPayload{Version: 1, ID: "emergency-activation", RootSPKI: pin, CipherSHA256: strings.Repeat("a", 64), ManifestSHA256: strings.Repeat("b", 64), OldAuthorityID: old.NodeID, NewAuthorityID: "new-controller", IssuerSHA256: material.IssuerSHA256, TrustGeneration: 2, RevocationSequence: 3, IssuedAt: time.Now().Add(-time.Minute).UTC(), ExpiresAt: time.Now().Add(time.Hour).UTC()}
	requestFile, authorityFile := filepath.Join(base, "authority-request.json"), filepath.Join(base, "authority.json")
	writeJSON(requestFile, payload)
	if e = localpki.SignRecoveryAuthority(offline, requestFile, authorityFile); e != nil {
		t.Fatal(e)
	}
	authorization := read(authorityFile)
	request := updates.ResidentFenceRequest{AuthorizationSHA256: recoveryDigest(authorization), OldAuthorityID: old.NodeID, NewAuthorityID: payload.NewAuthorityID, TrustGeneration: 2}
	for name, attempt := range map[string]recoveryFenceClient{"unprovisioned peer": func() recoveryFenceClient { c := client; c.Certificate, c.Key = wrongCert, wrongKey; return c }(), "wrong server pin": func() recoveryFenceClient { c := client; c.ServerCertificateSHA256 = strings.Repeat("f", 64); return c }()} {
		if _, e = callRecoveryFence(ctx, attempt, request, authorization); e == nil {
			t.Fatal(name, "was accepted")
		}
	}
	invalid := append([]byte{}, authorization...)
	invalid = []byte(strings.Replace(string(invalid), "new-controller", "bad-controller", 1))
	if _, e = callRecoveryFence(ctx, client, request, invalid); e == nil {
		t.Fatal("tampered Root authorization accepted")
	}
	entries, e := os.ReadDir(cache)
	if e != nil || len(entries) != 0 {
		t.Fatal("unverified authorization poisoned immutable cache", e, len(entries))
	}
	if e = syscall.Kill(child.PID, 0); e != nil {
		t.Fatal("rejected request killed child", e)
	}
	latch, e := recovery.BeginQuarantine(ctx, restored, recovery.RestoreIntent{SnapshotSHA256: payload.CipherSHA256, ControllerID: payload.NewAuthorityID, RootSPKISHA256: pin, MinTrustGeneration: 2, MinRevocationSequence: 4}, recovery.QuarantineVerifier{})
	if e != nil {
		t.Fatal(e)
	}
	hold := latch.Hold()
	_ = latch.Close()
	staged := recovery.StagedBundle{State: "quarantined_authenticated_bundle", CipherSHA256: payload.CipherSHA256, ManifestSHA256: payload.ManifestSHA256, Hold: hold, SnapshotID: "synthetic-snapshot", OldAuthorityID: old.NodeID}
	writeJSON(filepath.Join(restored, "staged.json"), staged)
	receipt := recovery.ReconciliationReceipt{HoldID: hold.ID, AuthoritySHA256: request.AuthorizationSHA256, CipherSHA256: payload.CipherSHA256, ManifestSHA256: payload.ManifestSHA256, OldAuthorityID: old.NodeID, NewAuthorityID: payload.NewAuthorityID, RootSPKI: pin, IssuerSHA256: payload.IssuerSHA256, TrustGeneration: 2, RevocationSequence: 4, CreatedAt: time.Now().UTC()}
	writeJSON(filepath.Join(restored, "reconciled.json"), receipt)
	newConfig := old
	newConfig.NodeID, newConfig.Directory = payload.NewAuthorityID, private("new-slot")
	newConfig.FenceConfiguration, newConfig.FenceConfigurationSHA256 = "", ""
	newConfig.Database = filepath.Join(restored, "restored.db")
	newConfig.RecoveryRuntime = filepath.Join(base, "runtime.json")
	writeJSON(newConfig.RecoveryRuntime, recoveryRuntime{Version: 1, Directory: restored, Authorization: authorityFile, NewPKIDirectory: newPKI, RootPin: pin, Fence: client})
	supervisorFile := filepath.Join(base, "supervisor.json")
	writeJSON(supervisorFile, newConfig)
	db, e := storage.Open(ctx, storage.Config{Path: newConfig.Database})
	if e != nil {
		t.Fatal(e)
	}
	if e = db.InTx(ctx, func(c context.Context, tx storage.Tx) error {
		if e := recovery.RecordReconciliationTx(c, tx, receipt); e != nil {
			return e
		}
		return recovery.SuspendRestoredResidentAuthorityTx(c, tx, old.NodeID, payload.NewAuthorityID)
	}); e != nil {
		t.Fatal(e)
	}
	_ = db.Close()
	if l, e := openActivatedRecovery(ctx, newConfig.RecoveryRuntime, newConfig); e == nil {
		l.Close()
		t.Fatal("reconciled snapshot ran without activation")
	}
	loaded, e := updates.LoadResidentSupervisor(supervisorFile)
	if e != nil {
		t.Fatal("load new supervisor", e)
	}
	if _, e = loadRecoveryActivationContext(ctx, newConfig.RecoveryRuntime, loaded, time.Now()); e != nil {
		t.Fatal("load reconciled activation boundary", e)
	}
	activation, e := activateRecoveredController(ctx, newConfig.RecoveryRuntime, supervisorFile)
	if e != nil {
		t.Fatal("Root/mTLS/owned-fence activation", e)
	}
	if activation.Clearance.AuditRecordID == "" || syscall.Kill(child.PID, 0) == nil {
		t.Fatal("activation without audit or actual stop/wait")
	}
	if _, e = os.Stat(filepath.Join(restored, "activation-attempt.json")); e != nil {
		t.Fatal(e)
	}
	if e = recovery.WriteOfflineRecord(restored, "activation.json", activation); e == nil {
		t.Fatal("immutable activation overwritten")
	}
	active, e := openActivatedRecovery(ctx, newConfig.RecoveryRuntime, newConfig)
	if e != nil {
		t.Fatal("committed activation cannot reopen", e)
	}
	if e = active.Check(ctx); e != nil {
		t.Fatal(e)
	}
	_ = active.Close()
	record, e := callRecoveryFence(ctx, client, request, authorization)
	if e != nil || record.Request != request {
		t.Fatal("idempotent authenticated fence", e)
	}
	for i := 0; i < 31; i++ {
		raw, _ := json.Marshal(map[string]int{"operator-cache-fixture": i})
		if e = recovery.WriteOfflineRecord(cache, "authorization-"+recoveryDigest(raw)+".json", json.RawMessage(raw)); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = callRecoveryFence(ctx, client, request, authorization); e != nil {
		t.Fatal("full cache blocked existing authenticated fence", e)
	}
	otherPayload := payload
	otherPayload.ID = "cache-capacity-probe"
	writeJSON(requestFile, otherPayload)
	otherFile := filepath.Join(base, "other-authority.json")
	if e = localpki.SignRecoveryAuthority(offline, requestFile, otherFile); e != nil {
		t.Fatal(e)
	}
	other := read(otherFile)
	otherRequest := request
	otherRequest.AuthorizationSHA256 = recoveryDigest(other)
	if _, e = callRecoveryFence(ctx, client, otherRequest, other); e == nil {
		t.Fatal("full immutable cache accepted a new document")
	}
	if entries, e = os.ReadDir(cache); e != nil || len(entries) != 32 {
		t.Fatal("cache cap changed", len(entries), e)
	}
	stop()
	// The independent parent reopens in observation-only mode. A fresh nonce
	// must still return the same persisted fence; its old child cannot restart.
	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case <-stopped:
	}
	if l, e := openActivatedRecovery(ctx, newConfig.RecoveryRuntime, newConfig); e == nil {
		l.Close()
		t.Fatal("stale activation admitted without live fence endpoint")
	}
	_, _ = startParent()
	select {
	case <-serving:
	case <-ctx.Done():
		t.Fatal("fenced parent did not reopen")
	}
	if got, e := callRecoveryFence(ctx, client, request, nil); e != nil || got != record {
		t.Fatal("fence lost across restart", e)
	}
	select {
	case <-ready:
		t.Fatal("fenced Controller resurrected")
	default:
	}
	active, e = openActivatedRecovery(ctx, newConfig.RecoveryRuntime, newConfig)
	if e != nil {
		t.Fatal("activation lost across old-parent restart", e)
	}
	_ = active.Close()
	wrong := request
	wrong.NewAuthorityID = "different-controller"
	if _, e = callRecoveryFence(ctx, client, wrong, nil); e == nil {
		t.Fatal("fence rebound to another authority")
	}
	if _, e = activateRecoveredController(ctx, newConfig.RecoveryRuntime, supervisorFile); !errors.Is(e, recovery.ErrQuarantined) {
		t.Fatal("activation attempt replay admitted", e)
	}
}
