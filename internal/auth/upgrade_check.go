package auth

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Operator-only diagnostics. HTTP still maps these to sanitized identity errors.
type UpgradeError struct{ Code string }

func (e *UpgradeError) Error() string {
	return "auth: " + e.Code + "; retain the original database and credential-keys directory; see deployment/UPGRADE.md"
}
func (e *UpgradeError) Unwrap() error { return ErrIdentityUnavailable }

type IdentityUpgradeReport struct {
	Initialized         bool `json:"initialized"`
	Version             int  `json:"identityVersion"`
	CredentialsVerified bool `json:"credentialsVerified"`
}

// Read-only compatibility preflight; never regenerates keys or mutates payloads.
func CheckIdentityUpgrade(ctx context.Context, store IdentityStore, directory string) (IdentityUpgradeReport, error) {
	var report IdentityUpgradeReport
	fail := func(code string) (IdentityUpgradeReport, error) { return report, &UpgradeError{Code: code} }
	if ctx == nil || ctx.Err() != nil || store == nil {
		return fail("IDENTITY_SOURCE_UNAVAILABLE")
	}
	read := func(fn func([]byte, bool) error) error {
		return store.View(ctx, func(raw []byte) error {
			var metadata struct{ Initialized bool }
			_ = json.Unmarshal(raw, &metadata)
			return fn(raw, metadata.Initialized)
		})
	}
	if sql, ok := store.(*SQLIdentityStore); ok {
		read = func(fn func([]byte, bool) error) error { return sql.readUpgrade(ctx, fn) }
	}
	e := read(func(raw []byte, initialized bool) error {
		st, e := decodeIdentityDetailed(raw)
		if e != nil {
			return e
		}
		if st.Initialized != initialized {
			return &UpgradeError{Code: "IDENTITY_INITIALIZATION_MARKER_MISMATCH"}
		}
		if time.Now().Before(st.ClockFloor) {
			return &UpgradeError{Code: "IDENTITY_CLOCK_ROLLBACK"}
		}
		report.Initialized = st.Initialized
		report.Version = st.Version
		return nil
	})
	if e != nil {
		var issue *UpgradeError
		if errors.As(e, &issue) {
			return fail(issue.Code)
		}
		return fail("IDENTITY_PAYLOAD_OR_INITIALIZATION_MARKER_INVALID")
	}
	if !report.Initialized {
		return report, nil
	}
	info, e := os.Lstat(directory)
	if os.IsNotExist(e) {
		return fail("CREDENTIAL_KEY_DIRECTORY_MISSING")
	}
	if e != nil || !credentialPrivateOwner(info, true, directory) {
		return fail("CREDENTIAL_KEY_DIRECTORY_PERMISSIONS")
	}
	root, e := privateKeyRoot(directory)
	if e != nil {
		return fail("CREDENTIAL_KEY_DIRECTORY_UNAVAILABLE")
	}
	defer root.Close()
	info, e = root.Lstat("credential-keys.json")
	if os.IsNotExist(e) {
		return fail("CREDENTIAL_KEYRING_MISSING")
	}
	if e != nil || !credentialPrivateOwner(info, false, filepath.Join(directory, "credential-keys.json")) {
		return fail("CREDENTIAL_KEYRING_PERMISSIONS")
	}
	k, e := readKeyring(root)
	if e != nil {
		return fail("CREDENTIAL_KEYRING_FORMAT_OR_VERSION_INVALID")
	}
	defer k.Clear()
	identity, e := NewIdentityService(store)
	if e != nil {
		return fail("IDENTITY_SOURCE_UNAVAILABLE")
	}
	service, e := NewCredentialService(identity, k.Active, k.Keys, nil, nil)
	if e != nil {
		return fail("CREDENTIAL_KEYRING_FORMAT_OR_VERSION_INVALID")
	}
	if validatePersistentCredentials(ctx, identity, service) != nil {
		return fail("CREDENTIAL_KEY_BINDING_OR_CIPHERTEXT_INVALID")
	}
	report.CredentialsVerified = true
	return report, nil
}
