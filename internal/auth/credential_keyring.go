package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"
)

// CredentialKeyring is an operator-held private file, never an HTTP DTO. Old
// keys are retained for ciphertext and staged changes; retirement is deliberately
// not automatic. Rotation requires stopping ALL writers and restarting them.
type CredentialKeyring struct {
	Version int               `json:"version"`
	Active  string            `json:"active"`
	Keys    map[string][]byte `json:"keys"`
}

func (*CredentialKeyring) String() string     { return "auth.CredentialKeyring{redacted}" }
func (k *CredentialKeyring) GoString() string { return k.String() }
func (k *CredentialKeyring) Clear() {
	if k != nil {
		for _, key := range k.Keys {
			clear(key)
		}
		k.Keys = nil
		k.Active = ""
	}
}

func privateKeyRoot(dir string) (*os.Root, error) {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
		return nil, ErrIdentityUnavailable
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || !credentialPrivateOwner(info, true, dir) {
		return nil, ErrIdentityUnavailable
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, ErrIdentityUnavailable
	}
	info, err = root.Stat(".")
	if err != nil || !credentialPrivateOwner(info, true, dir) {
		root.Close()
		return nil, ErrIdentityUnavailable
	}
	return root, nil
}

func readKeyring(root *os.Root) (*CredentialKeyring, error) {
	info, err := root.Lstat("credential-keys.json")
	if os.IsNotExist(err) {
		return nil, os.ErrNotExist
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > 16384 || !credentialPrivateOwner(info, false, filepath.Join(root.Name(), "credential-keys.json")) {
		return nil, ErrIdentityUnavailable
	}
	f, err := root.Open("credential-keys.json")
	if err != nil {
		return nil, ErrIdentityUnavailable
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) || !credentialPrivateOwner(opened, false, filepath.Join(root.Name(), "credential-keys.json")) {
		return nil, ErrIdentityUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(f, 16385))
	defer clear(raw)
	if err != nil || len(raw) > 16384 {
		return nil, ErrIdentityUnavailable
	}
	var k CredentialKeyring
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&k) != nil {
		k.Clear()
		return nil, ErrIdentityUnavailable
	}
	var extra any
	if d.Decode(&extra) != io.EOF || k.Version != 1 {
		k.Clear()
		return nil, ErrIdentityUnavailable
	}
	// Canonical encoding also rejects duplicate/case-aliased fields and keys.
	canonical, err := json.Marshal(k)
	defer clear(canonical)
	if err != nil || !bytes.Equal(bytes.TrimSpace(raw), canonical) {
		k.Clear()
		return nil, ErrIdentityUnavailable
	}
	if _, err = newCredentialCipher(k.Active, k.Keys); err != nil {
		k.Clear()
		return nil, err
	}
	seen := map[string]bool{}
	for id, key := range k.Keys {
		fingerprint := credentialDigest(string(key))
		if !validPolicyID(id) || seen[fingerprint] {
			k.Clear()
			return nil, ErrIdentityUnavailable
		}
		seen[fingerprint] = true
	}
	return &k, nil
}

func addRandomCredentialKey(k *CredentialKeyring) error {
	if len(k.Keys) >= 16 {
		return ErrIdentityUnavailable
	}
	key, id := make([]byte, 32), make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		return ErrIdentityUnavailable
	}
	if _, err := rand.Read(id); err != nil {
		clear(key)
		return ErrIdentityUnavailable
	}
	k.Active = "key-" + hex.EncodeToString(id)
	k.Keys[k.Active] = key
	return nil
}

func publishKeyring(root *os.Root, k *CredentialKeyring, replace bool) error {
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return ErrIdentityUnavailable
	}
	name := ".credential-keys-" + hex.EncodeToString(id)
	f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return ErrIdentityUnavailable
	}
	defer root.Remove(name)
	raw, err := json.Marshal(k)
	defer clear(raw)
	if err == nil {
		_, err = f.Write(raw)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return ErrIdentityUnavailable
	}
	return publishCredentialFile(root, name, replace)
}

// EnsureCredentialKeyDirectory creates only the final directory, with private
// permissions at creation. Existing directories are verified, never repaired or
// followed through a symlink. It does not create or replace any key material.
func EnsureCredentialKeyDirectory(dir string) error {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
		return ErrIdentityUnavailable
	}
	if err := makeCredentialDirectory(dir); err != nil && !os.IsExist(err) {
		return ErrIdentityUnavailable
	}
	root, err := privateKeyRoot(dir)
	if err != nil {
		return err
	}
	return root.Close()
}

// NewPersistentCredentialService creates keys only for an untouched identity
// store. Missing keys for ANY initialized store, unknown versions or inaccessible
// ciphertext fail closed. The caller creates a dedicated 0700 directory first.
// Linux ownership/modes and Windows owner/DACL are verified. Other OSes fail closed.
func NewPersistentCredentialService(ctx context.Context, identity *IdentityService, dir string, gate CredentialOperationGate, references CredentialReferenceReader) (*CredentialService, error) {
	if ctx == nil || identity == nil || ctx.Err() != nil {
		return nil, ErrIdentityUnavailable
	}
	root, err := privateKeyRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	k, err := readKeyring(root)
	if os.IsNotExist(err) {
		// Hold the identity transaction while publishing, so bootstrap cannot
		// race creation and turn an old database into a fresh-key candidate.
		err = identity.update(ctx, func(st *identityState, _ time.Time) error {
			if st.Initialized || len(st.Credentials)+len(st.CredentialChanges)+len(st.CredentialKeyUses)+len(st.XrayMaterials) != 0 {
				return ErrIdentityUnavailable
			}
			candidate := &CredentialKeyring{Version: 1, Keys: map[string][]byte{}}
			defer candidate.Clear()
			if err := addRandomCredentialKey(candidate); err != nil {
				return err
			}
			if err := publishKeyring(root, candidate, false); err != nil && !os.IsExist(err) {
				return ErrIdentityUnavailable
			}
			return nil
		})
		if err == nil {
			k, err = readKeyring(root)
		}
	}
	if err != nil {
		return nil, ErrIdentityUnavailable
	}
	defer k.Clear()
	v, err := NewCredentialService(identity, k.Active, k.Keys, gate, references)
	if err != nil {
		return nil, err
	}
	err = identity.read(ctx, func(st *identityState, now time.Time) error {
		for key := range st.CredentialKeyUses {
			if v.cipher.keys[key] == nil {
				return ErrIdentityUnavailable
			}
		}
		for _, r := range st.Credentials {
			// Revocation deliberately erases ciphertext. It must remain revoked,
			// and cannot be treated as a corrupt live credential at startup.
			if r.Revoked && len(r.Sealed.Ciphertext) == 0 {
				continue
			}
			binding, e := credentialBindingFor(r)
			if e != nil {
				return e
			}
			plain, e := v.cipher.open(binding, r.Sealed, credentialExpiry(r.Meta), now, false)
			// Expired/revoked ciphertext still needs an authentication check.
			if !now.Before(credentialExpiry(r.Meta)) {
				plain, e = v.cipher.open(binding, r.Sealed, credentialExpiry(r.Meta), r.Meta.CreatedAt, false)
			}
			clear(plain)
			if e != nil {
				return ErrIdentityUnavailable
			}
		}
		for _, r := range st.CredentialChanges {
			purpose, _, e := credentialPurpose(r.Meta.Kind)
			if e != nil {
				return ErrIdentityUnavailable
			}
			plain, e := v.cipher.open(credentialBinding{r.BindingID, r.Owner, purpose, r.Meta.Revision}, r.Sealed, credentialExpiry(r.Meta), r.Meta.CreatedAt, false)
			clear(plain)
			if e != nil {
				return ErrIdentityUnavailable
			}
		}
		for _, r := range st.XrayMaterials {
			if v.cipher.keys[r.ReservedKeyID] == nil {
				return ErrIdentityUnavailable
			}
			if len(r.Sealed.Ciphertext) > 0 {
				plain, e := v.cipher.open(xrayMaterialBinding(r), r.Sealed, r.Meta.ExpiresAt, r.Meta.CreatedAt, false)
				clear(plain)
				if e != nil {
					return ErrIdentityUnavailable
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, ErrIdentityUnavailable
	}
	return v, nil
}

// RotateCredentialMasterKey is an OFFLINE operator operation: stop every
// Controller writer first. It atomically adds a random active key without
// removing old keys or rewriting any database. Never exposed as an HTTP route.
func RotateCredentialMasterKey(dir string) error {
	root, err := privateKeyRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	if root.Mkdir(".credential-key-rotation", 0700) != nil {
		return ErrIdentityUnavailable
	}
	defer root.Remove(".credential-key-rotation")
	k, err := readKeyring(root)
	if err != nil {
		return ErrIdentityUnavailable
	}
	defer k.Clear()
	if err = addRandomCredentialKey(k); err != nil {
		return err
	}
	if publishKeyring(root, k, true) != nil {
		return ErrIdentityUnavailable
	}
	return nil
}

// ServiceMetadata exposes one current reference to registered backend services.
// Ownership is mandatory; global administrative metadata listing is not a grant.
func (v *CredentialService) ServiceMetadata(ctx context.Context, owner, id string) (out CredentialMetadata, err error) {
	if v == nil || !validPolicyID(owner) || !validSecret(id) {
		return out, ErrDenied
	}
	err = v.identity.read(ctx, func(st *identityState, now time.Time) error {
		u, ok := st.Users[owner]
		if !ok || u.Disabled {
			return ErrDenied
		}
		r, ok := st.Credentials[id]
		if !ok || r.Owner != owner || r.Revoked || !now.Before(credentialExpiry(r.Meta)) {
			return ErrDenied
		}
		out = credentialView(r, now)
		return nil
	})
	return out, err
}

// ValidateCredentialKeyDirectory only validates an already-restored keyring.
// It neither creates keys nor starts services nor admits restored credentials.
func ValidateCredentialKeyDirectory(directory string) error {
	root, err := privateKeyRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	keys, err := readKeyring(root)
	if err != nil {
		return ErrIdentityUnavailable
	}
	keys.Clear()
	return nil
}
