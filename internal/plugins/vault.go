package plugins

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// PackageVault stores immutable signed inputs outside the core Release. It
// never downloads a repository or loads/executes its contents on construction.
type PackageVault struct{ root *os.Root }
type PackageContent struct {
	Manifest                PackageManifest
	Raw, Signature, Payload []byte
	KeyID                   string
}

func (c PackageContent) String() string   { return "plugins.PackageContent{signed-private-artifact}" }
func (c PackageContent) GoString() string { return c.String() }
func NewPackageVault(directory string) (*PackageVault, error) {
	if !filepath.IsAbs(directory) {
		return nil, ErrPackageRejected
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, ErrPackageRejected
	}
	info, err := root.Stat(".")
	if err != nil || !info.IsDir() || runtime.GOOS == "linux" && info.Mode().Perm()&0077 != 0 {
		_ = root.Close()
		return nil, ErrPackageRejected
	}
	return &PackageVault{root}, nil
}
func (v *PackageVault) Close() error {
	if v == nil || v.root == nil {
		return nil
	}
	return v.root.Close()
}
func packageIdentity(raw, signature []byte, keyID string) string {
	encoded, _ := json.Marshal(struct {
		Raw, Signature []byte
		KeyID          string
	}{raw, signature, keyID})
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}

// VerifiedDescriptor is used by the trusted task planner to freeze the exact
// signed package identity. It does not install, load, enable or grant anything.
func VerifiedDescriptor(raw, signature, payload []byte, keyID string, trust PackageTrust) (Descriptor, error) {
	manifest, err := VerifyPackage(raw, signature, payload, keyID, trust)
	if err != nil {
		return Descriptor{}, err
	}
	return Descriptor{manifest.ID, manifest.Version, packageIdentity(raw, signature, keyID)}, nil
}
func (v *PackageVault) Put(ctx context.Context, raw, signature, payload []byte, keyID string, trust PackageTrust) (string, error) {
	if v == nil || v.root == nil || ctx == nil || ctx.Err() != nil || len(keyID) == 0 || len(keyID) > 128 {
		return "", ErrPackageRejected
	}
	if _, err := VerifyPackage(raw, signature, payload, keyID, trust); err != nil {
		return "", err
	}
	raw = bytes.Clone(raw)
	signature = bytes.Clone(signature)
	payload = bytes.Clone(payload)
	id := packageIdentity(raw, signature, keyID)
	if err := v.root.Mkdir(id, 0700); err != nil {
		if !os.IsExist(err) {
			return "", ErrPackageRejected
		}
		stored, err := v.Load(ctx, id, trust)
		if err != nil || !bytes.Equal(stored.Raw, raw) || !bytes.Equal(stored.Signature, signature) || !bytes.Equal(stored.Payload, payload) {
			return "", ErrPackageRejected
		}
		return id, nil
	}
	committed := false
	defer func() {
		if !committed {
			for _, file := range []string{"manifest.json", "signature", "key-id", "payload"} {
				_ = v.root.Remove(id + "/" + file)
			}
			_ = v.root.Remove(id)
		}
	}()
	for _, entry := range []struct {
		name    string
		content []byte
	}{{"manifest.json", raw}, {"signature", signature}, {"key-id", []byte(keyID)}, {"payload", payload}} {
		if ctx.Err() != nil {
			return "", ErrPackageRejected
		}
		file, err := v.root.OpenFile(id+"/"+entry.name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return "", ErrPackageRejected
		}
		n, writeErr := file.Write(entry.content)
		syncErr := file.Sync()
		closeErr := file.Close()
		if writeErr != nil || n != len(entry.content) || syncErr != nil || closeErr != nil {
			return "", ErrPackageRejected
		}
	}
	if runtime.GOOS == "linux" {
		for _, dir := range []string{id, "."} {
			file, err := v.root.Open(dir)
			if err != nil {
				return "", ErrPackageRejected
			}
			syncErr := file.Sync()
			_ = file.Close()
			if syncErr != nil {
				return "", ErrPackageRejected
			}
		}
	}
	if _, err := v.Load(ctx, id, trust); err != nil {
		return "", err
	}
	committed = true
	return id, nil
}
func (v *PackageVault) read(ctx context.Context, name string, max int64) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, ErrPackageRejected
	}
	file, err := v.root.Open(name)
	if err != nil {
		return nil, ErrPackageRejected
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > max {
		return nil, ErrPackageRejected
	}
	b, err := io.ReadAll(io.LimitReader(file, max+1))
	if err != nil || int64(len(b)) > max || ctx.Err() != nil {
		return nil, ErrPackageRejected
	}
	return b, nil
}
func (v *PackageVault) Load(ctx context.Context, id string, trust PackageTrust) (PackageContent, error) {
	if v == nil || v.root == nil || ctx == nil || ctx.Err() != nil || len(id) != 64 {
		return PackageContent{}, ErrPackageRejected
	}
	digest, err := hex.DecodeString(id)
	if err != nil || hex.EncodeToString(digest) != id {
		return PackageContent{}, ErrPackageRejected
	}
	raw, err := v.read(ctx, id+"/manifest.json", 64<<10)
	if err != nil {
		return PackageContent{}, err
	}
	signature, err := v.read(ctx, id+"/signature", 64)
	if err != nil {
		return PackageContent{}, err
	}
	key, err := v.read(ctx, id+"/key-id", 128)
	if err != nil {
		return PackageContent{}, err
	}
	payload, err := v.read(ctx, id+"/payload", 32<<20)
	if err != nil {
		return PackageContent{}, err
	}
	if packageIdentity(raw, signature, string(key)) != id {
		return PackageContent{}, ErrPackageRejected
	}
	manifest, err := VerifyPackage(raw, signature, payload, string(key), trust)
	if err != nil {
		return PackageContent{}, err
	}
	return PackageContent{manifest, raw, signature, payload, string(key)}, nil
}
