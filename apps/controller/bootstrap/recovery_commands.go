package bootstrap

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"path/filepath"
	"slices"

	"serverfleet.local/serverfleet/apps/controller/localpki"
	"serverfleet.local/serverfleet/apps/controller/server"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/recovery"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/tasks"
	"serverfleet.local/serverfleet/internal/transport"
	"serverfleet.local/serverfleet/internal/updates"
)

// Stage/reconcile are offline. Explicit activate requires fresh Root authority
// plus the independently provisioned live owned-process fence adapter.
func RunRecoveryCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if ctx == nil || len(args) == 0 {
		return recovery.ErrRecoveryRejected
	}
	flags := flag.NewFlagSet("controller recovery "+args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	binary := flags.String("age-binary", "", "operator-provisioned age executable")
	binarySHA := flags.String("age-sha256", "", "independently verified executable SHA-256")
	identity := flags.String("identity", "", "private OFFLINE native age identity file; never copied to target")
	bundle := flags.String("bundle", "", "private encrypted backup/challenge file")
	digest := flags.String("cipher-sha256", "", "trusted ciphertext receipt SHA-256, acquired separately")
	destination := flags.String("destination", "", "new private quarantine directory (must not exist)")
	rootPin := flags.String("root-pin", "", "independently verified original offline Root SPKI SHA-256")
	authority := flags.String("target-authority-id", "", "new unique Controller authority ID")
	generation := flags.Uint64("minimum-generation", 0, "fresh offline-authorized generation, strictly above old generation")
	revocation := flags.Uint64("minimum-revocation-sequence", 0, "fresh independently obtained revocation high-water mark")
	authorization := flags.String("authorization", "", "fresh offline-Root-signed recovery authorization")
	newPKI := flags.String("new-pki-directory", "", "new separately provisioned Intermediate/key directory")
	runtimeFile := flags.String("runtime", "", "private restored authority and independent fence client configuration")
	supervisorFile := flags.String("resident-supervisor", "", "private new Controller supervisor configuration")
	offline := flags.Bool("offline-ack", false, "confirm explicit offline recovery host, no live Controller or private Root in target")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 || !*offline {
		return errors.New("recovery: explicit offline acknowledgement required")
	}
	if args[0] == "status" {
		c, e := updates.LoadResidentSupervisor(*supervisorFile)
		if e != nil {
			return e
		}
		latch, e := openActivatedRecovery(ctx, *runtimeFile, c)
		if e != nil {
			return e
		}
		defer latch.Close()
		return json.NewEncoder(stdout).Encode(map[string]any{"state": "live_fence_verified", "released": true, "holdId": latch.Hold().ID, "newAuthorityId": c.NodeID, "agentReenrollmentRequired": true})
	}
	if args[0] == "activate" {
		receipt, e := activateRecoveredController(ctx, *runtimeFile, *supervisorFile)
		if e != nil {
			return e
		}
		return json.NewEncoder(stdout).Encode(map[string]any{"state": "released_with_continuous_owned_process_fence", "receipt": receipt, "released": true, "agentReenrollmentRequired": true})
	}
	if args[0] == "reconcile" {
		receipt, err := reconcileStagedBackup(ctx, *destination, *authorization, *newPKI, *rootPin)
		if err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(map[string]any{"state": "quarantined_reconciled_waiting_actual_fence", "receipt": receipt, "released": false})
	}
	cipher, err := recovery.NewAgeCipher(*binary, *binarySHA)
	if err != nil {
		return err
	}
	switch args[0] {
	case "prove-key":
		proof, e := recovery.ProveRecoveryKey(ctx, cipher, *identity, *bundle)
		if e != nil {
			return e
		}
		return json.NewEncoder(stdout).Encode(map[string]string{"state": "offline_key_proven", "proofSha256": proof})
	case "stage":
		receipt, e := recovery.StageEncryptedBackup(ctx, recovery.RestoreBundleOptions{Cipher: cipher, IdentityPath: *identity, BundlePath: *bundle, ExpectedCipherSHA256: *digest, ExpectedRootSPKI: *rootPin, Destination: *destination, TargetControllerID: *authority, MinimumTrustGeneration: *generation, MinimumRevocationSequence: *revocation, Validate: validateStagedBundle})
		if e != nil {
			return e
		}
		return json.NewEncoder(stdout).Encode(receipt)
	default:
		return errors.New("recovery: unsupported explicit offline command")
	}
}
func validateStagedBundle(ctx context.Context, directory string, manifest recovery.BackupManifest) error {
	material, err := localpki.VerifyArchivedProvisioning(filepath.Join(directory, "pki"), manifest.RootSPKI, manifest.CreatedAt)
	if err != nil || material.AuthorityID != manifest.AuthorityID || material.TrustGeneration != manifest.TrustGeneration {
		return recovery.ErrRecoveryRejected
	}
	certPEM, err := recovery.ReadPrivateFile(filepath.Join(directory, "tls/controller.pem"), 128<<10)
	if err != nil {
		return err
	}
	defer clear(certPEM)
	keyPEM, err := recovery.ReadPrivateFile(filepath.Join(directory, "tls/controller-key.pem"), 128<<10)
	if err != nil {
		return err
	}
	defer clear(keyPEM)
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil || len(pair.Certificate) == 0 {
		return recovery.ErrRecoveryRejected
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil || leaf.IsCA || manifest.CreatedAt.Before(leaf.NotBefore) || !manifest.CreatedAt.Before(leaf.NotAfter) {
		return recovery.ErrRecoveryRejected
	}
	if auth.ValidateCredentialKeyDirectory(filepath.Join(directory, "credentials")) != nil {
		return recovery.ErrRecoveryRejected
	}
	db, err := storage.Open(ctx, storage.Config{Path: filepath.Join(directory, "restored.db"), ReadOnly: true})
	if err != nil {
		return err
	}
	defer db.Close()
	migrator, err := storage.NewMigrator(server.Migrations()...)
	if err != nil {
		return err
	}
	if err = migrator.VerifyApplied(ctx, db, false); err != nil {
		return err
	}
	versions, err := storage.AppliedVersions(ctx, db)
	if err != nil || !slices.Equal(versions, manifest.SchemaVersions) {
		return recovery.ErrRecoveryRejected
	}
	enrollment, err := transport.ReadRecoveryEnrollment(ctx, db)
	if err != nil || enrollment.RevocationSequence != manifest.Snapshot.RevocationSequence || !slices.Equal(enrollment.NodeIDs, manifest.NodeIDs) || !slices.Equal(enrollment.RevokedSerials, manifest.Snapshot.RevokedSerials) {
		return recovery.ErrRecoveryRejected
	}
	records, err := tasks.ReadRecoveryTasks(ctx, db)
	if err != nil || len(records) != len(manifest.Snapshot.Tasks) {
		return recovery.ErrRecoveryRejected
	}
	for i, record := range records {
		stored := manifest.Snapshot.Tasks[i]
		if record.ID != stored.ID || record.State != stored.State || !record.Expires.Equal(stored.Expires) {
			return recovery.ErrRecoveryRejected
		}
	}
	return nil
}
