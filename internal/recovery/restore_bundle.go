package recovery

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Staging is an explicit OFFLINE operation. It creates a new quarantined
// directory and never replaces a database, opens listeners, releases a latch,
// accepts Root keys, or obtains authority from an old snapshot.
type RestoreBundleOptions struct {
	Cipher                                                           *AgeCipher
	IdentityPath, BundlePath, ExpectedCipherSHA256, ExpectedRootSPKI string
	Destination, TargetControllerID                                  string
	MinimumTrustGeneration, MinimumRevocationSequence                uint64
	Validate                                                         func(context.Context, string, BackupManifest) error
}
type StagedBundle struct {
	State          string         `json:"state"`
	CipherSHA256   string         `json:"cipherSha256"`
	ManifestSHA256 string         `json:"manifestSha256"`
	Hold           QuarantineHold `json:"hold"`
	SnapshotID     string         `json:"snapshotId"`
	OldAuthorityID string         `json:"oldAuthorityId"`
}

func StageEncryptedBackup(ctx context.Context, o RestoreBundleOptions) (StagedBundle, error) {
	if ctx == nil || ctx.Err() != nil || o.Cipher == nil || !validDigest(o.ExpectedCipherSHA256) || !validDigest(o.ExpectedRootSPKI) || !validReference(o.TargetControllerID) || o.MinimumTrustGeneration == 0 || o.Validate == nil || !filepath.IsAbs(o.BundlePath) || !filepath.IsAbs(o.Destination) || filepath.Clean(o.Destination) != o.Destination {
		return StagedBundle{}, ErrRecoveryRejected
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	source, err := openRecoveryDirectory(filepath.Dir(o.BundlePath))
	if err != nil {
		return StagedBundle{}, err
	}
	defer source.Close()
	info, err := source.Lstat(filepath.Base(o.BundlePath))
	if err != nil || !privateRecoveryInfo(info, false) || info.Size() <= 0 || info.Size() > MaxBackupBundleBytes {
		return StagedBundle{}, ErrRecoveryRejected
	}
	input, err := source.Open(filepath.Base(o.BundlePath))
	if err != nil {
		return StagedBundle{}, err
	}
	defer input.Close()
	after, err := input.Stat()
	if err != nil || !os.SameFile(info, after) {
		return StagedBundle{}, ErrRecoveryRejected
	}
	// Authenticate the operator-selected ciphertext receipt before processing
	// even its public manifest. age authentication is still mandatory afterwards.
	hash := sha256.New()
	if _, err = io.Copy(hash, io.LimitReader(input, MaxBackupBundleBytes+1)); err != nil || hex.EncodeToString(hash.Sum(nil)) != o.ExpectedCipherSHA256 {
		return StagedBundle{}, ErrRecoveryRejected
	}
	if _, err = input.Seek(0, io.SeekStart); err != nil {
		return StagedBundle{}, err
	}
	parent, err := openRecoveryDirectory(filepath.Dir(o.Destination))
	if err != nil {
		return StagedBundle{}, err
	}
	defer parent.Close()
	if err = parent.Mkdir(filepath.Base(o.Destination), 0700); err != nil {
		return StagedBundle{}, ErrRecoveryRejected
	}
	target, err := openRecoveryDirectory(o.Destination)
	if err != nil {
		return StagedBundle{}, err
	}
	defer target.Close()
	latch, err := BeginQuarantine(ctx, o.Destination, RestoreIntent{SnapshotSHA256: o.ExpectedCipherSHA256, ControllerID: o.TargetControllerID, RootSPKISHA256: o.ExpectedRootSPKI, MinTrustGeneration: o.MinimumTrustGeneration, MinRevocationSequence: o.MinimumRevocationSequence}, QuarantineVerifier{})
	if err != nil {
		return StagedBundle{}, err
	}
	defer latch.Close()
	for _, dir := range []string{"pki", "credentials", "tls", "configuration"} {
		if target.Mkdir(dir, 0700) != nil {
			return StagedBundle{}, ErrRecoveryRejected
		}
	}
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	hash = sha256.New()
	go func() {
		err := o.Cipher.DecryptOffline(ctx, o.IdentityPath, io.TeeReader(io.LimitReader(input, MaxBackupBundleBytes+1), hash), writer)
		writer.CloseWithError(err)
		done <- err
	}()
	manifest, rawManifest, extractErr := extractRecoveryArchive(ctx, reader, target)
	if extractErr != nil {
		cancel()
		reader.CloseWithError(extractErr)
	} else {
		reader.Close()
	}
	decryptErr := <-done
	if extractErr != nil || decryptErr != nil || hex.EncodeToString(hash.Sum(nil)) != o.ExpectedCipherSHA256 {
		return StagedBundle{}, ErrRecoveryRejected
	}
	if manifest.RootSPKI != o.ExpectedRootSPKI || manifest.Snapshot.RootSPKISHA256 != o.ExpectedRootSPKI || manifest.AuthorityID == o.TargetControllerID || manifest.TrustGeneration >= o.MinimumTrustGeneration || manifest.Snapshot.TrustGeneration != manifest.TrustGeneration || manifest.Snapshot.RevocationSequence > o.MinimumRevocationSequence {
		return StagedBundle{}, ErrRecoveryRejected
	}
	if err = o.Validate(ctx, o.Destination, manifest); err != nil {
		return StagedBundle{}, err
	}
	sum := sha256.Sum256(rawManifest)
	receipt := StagedBundle{State: "quarantined_authenticated_bundle", CipherSHA256: o.ExpectedCipherSHA256, ManifestSHA256: hex.EncodeToString(sum[:]), Hold: latch.Hold(), SnapshotID: manifest.SnapshotID, OldAuthorityID: manifest.AuthorityID}
	raw, err := json.Marshal(receipt)
	if err != nil || writeRecoveryFile(target, "staged.json", raw) != nil {
		return StagedBundle{}, ErrRecoveryRejected
	}
	return receipt, nil
}
func extractRecoveryArchive(ctx context.Context, input io.Reader, target *os.Root) (BackupManifest, []byte, error) {
	var manifest BackupManifest
	limited := &io.LimitedReader{R: input, N: MaxBackupBundleBytes + 1}
	archive := tar.NewReader(limited)
	header, err := archive.Next()
	if err != nil || header.Name != "manifest.json" || !safeRecoveryHeader(header) || header.Size <= 0 || header.Size > 8<<20 {
		return manifest, nil, ErrRecoveryRejected
	}
	raw, err := io.ReadAll(io.LimitReader(archive, (8<<20)+1))
	if err != nil || int64(len(raw)) != header.Size {
		return manifest, nil, ErrRecoveryRejected
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&manifest) != nil || decoder.Decode(&struct{}{}) != io.EOF || manifest.Format != 1 || manifest.Encryption != "age/X25519/v1" || !validReference(manifest.SnapshotID) || !validReference(manifest.AuthorityID) || !validDigest(manifest.RootSPKI) || manifest.CreatedAt.IsZero() || manifest.CreatedAt.After(time.Now().Add(5*time.Minute)) || (len(manifest.Files) < 8 || len(manifest.Files) > 19) || len(manifest.SchemaVersions) == 0 || len(manifest.SchemaVersions) > 1024 || len(manifest.NodeIDs) > 20000 || len(manifest.Snapshot.Tasks) > 100000 {
		return manifest, nil, ErrRecoveryRejected
	}
	canonical, err := json.Marshal(manifest)
	if err != nil || !bytes.Equal(canonical, raw) {
		return manifest, nil, ErrRecoveryRejected
	}
	expected := map[string]BundleFile{}
	for _, entry := range manifest.Files {
		max := int64(128 << 10)
		if entry.Name == "controller.db" {
			max = MaxBackupDatabaseBytes
		}
		if !allowedBundleName(entry.Name) || !validDigest(entry.SHA256) || entry.Bytes <= 0 || entry.Bytes > max || expected[entry.Name].Name != "" {
			return manifest, nil, ErrRecoveryRejected
		}
		expected[entry.Name] = entry
	}
	for _, name := range []string{"controller.db", "pki/root.pem", "pki/issuer.pem", "pki/issuer-key.pem", "pki/pki.json", "credentials/credential-keys.json", "tls/controller.pem", "tls/controller-key.pem"} {
		if _, ok := expected[name]; !ok {
			return manifest, nil, ErrRecoveryRejected
		}
	}
	seen := map[string]bool{}
	for {
		if ctx.Err() != nil {
			return manifest, nil, ctx.Err()
		}
		header, err = archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil || !safeRecoveryHeader(header) {
			return manifest, nil, ErrRecoveryRejected
		}
		entry, ok := expected[header.Name]
		if !ok || seen[header.Name] || entry.Bytes != header.Size {
			return manifest, nil, ErrRecoveryRejected
		}
		name := header.Name
		if name == "controller.db" {
			name = "restored.db"
		}
		file, e := target.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return manifest, nil, ErrRecoveryRejected
		}
		hash := sha256.New()
		n, e := io.CopyN(io.MultiWriter(file, hash), archive, entry.Bytes)
		syncErr := file.Sync()
		closeErr := file.Close()
		if e != nil || syncErr != nil || closeErr != nil || n != entry.Bytes || hex.EncodeToString(hash.Sum(nil)) != entry.SHA256 {
			return manifest, nil, ErrRecoveryRejected
		}
		seen[header.Name] = true
	}
	if len(seen) != len(expected) {
		return manifest, nil, ErrRecoveryRejected
	}
	// Reject concatenated archives, hidden members and non-zero trailing data.
	tail, err := io.ReadAll(io.LimitReader(limited, 1025))
	if err != nil || len(tail) > 1024 || limited.N <= 0 {
		return manifest, nil, ErrRecoveryRejected
	}
	for _, b := range tail {
		if b != 0 {
			return manifest, nil, ErrRecoveryRejected
		}
	}
	if !sort.StringsAreSorted(manifest.SchemaVersions) {
		return manifest, nil, ErrRecoveryRejected
	}
	if err = writeRecoveryFile(target, "manifest.json", raw); err != nil {
		return manifest, nil, err
	}
	return manifest, raw, nil
}
func safeRecoveryHeader(h *tar.Header) bool {
	return h != nil && h.Typeflag == tar.TypeReg && h.Mode == 0600 && h.Linkname == "" && h.Size >= 0 && len(h.PAXRecords) == 0 && len(h.Xattrs) == 0 && h.Uid == 0 && h.Gid == 0 && h.Uname == "" && h.Gname == ""
}

// VerifyStagedFiles rechecks original archive contents before offline mutation.
// It does not trust a previous staged.json receipt as a substitute for bytes.
func VerifyStagedFiles(ctx context.Context, directory string, manifest BackupManifest) error {
	root, err := openRecoveryDirectory(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	if len(manifest.Files) < 8 || len(manifest.Files) > 19 {
		return ErrRecoveryRejected
	}
	for _, entry := range manifest.Files {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !allowedBundleName(entry.Name) || !validDigest(entry.SHA256) {
			return ErrRecoveryRejected
		}
		name := entry.Name
		if name == "controller.db" {
			name = "restored.db"
		}
		info, e := root.Lstat(name)
		if e != nil || !privateRecoveryInfo(info, false) || info.Size() != entry.Bytes {
			return ErrRecoveryRejected
		}
		file, e := root.Open(name)
		if e != nil {
			return ErrRecoveryRejected
		}
		after, e := file.Stat()
		if e != nil || !os.SameFile(info, after) {
			file.Close()
			return ErrRecoveryRejected
		}
		hash := sha256.New()
		n, e := io.Copy(hash, io.LimitReader(file, entry.Bytes+1))
		file.Close()
		if e != nil || n != entry.Bytes || hex.EncodeToString(hash.Sum(nil)) != entry.SHA256 {
			return ErrRecoveryRejected
		}
	}
	return nil
}

// WriteOfflineRecord exclusively publishes a small private offline receipt. It
// cannot overwrite a hold, a prior reconciliation or a Controller database.
func WriteOfflineRecord(directory, name string, value any) error {
	if name != "reconcile-attempt.json" && name != "reconciled.json" {
		return ErrRecoveryRejected
	}
	root, err := openRecoveryDirectory(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 16<<10 {
		return ErrRecoveryRejected
	}
	return writeRecoveryFile(root, name, raw)
}
