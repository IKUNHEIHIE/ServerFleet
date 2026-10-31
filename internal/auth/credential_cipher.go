package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"time"
)

// Private encryption component, not an export/read-plaintext API. Future service
// use must check current permission/expiry/revocation in the credential store.
// CA/recovery keys and platform authentication tokens aren't supported purposes.
type credentialBinding struct {
	ID, Owner, Purpose string
	Revision           uint64
}
type sealedCredential struct {
	KeyID      string
	Ciphertext []byte
}
type credentialCipher struct {
	active string
	keys   map[string]cipher.AEAD
}

func newCredentialCipher(active string, keys map[string][]byte) (*credentialCipher, error) {
	if active == "" || len(keys) == 0 || len(keys) > 16 {
		return nil, ErrIdentityUnavailable
	}
	c := &credentialCipher{active: active, keys: map[string]cipher.AEAD{}}
	for id, key := range keys {
		if id == "" || len(id) > 128 || len(key) != 32 {
			return nil, ErrIdentityUnavailable
		}
		block, err := aes.NewCipher(key)
		if err != nil {
			return nil, ErrIdentityUnavailable
		}
		aead, err := cipher.NewGCMWithRandomNonce(block)
		if err != nil {
			return nil, ErrIdentityUnavailable
		}
		c.keys[id] = aead
	}
	if c.keys[active] == nil {
		return nil, ErrIdentityUnavailable
	}
	return c, nil
}
func bindingAAD(binding credentialBinding, keyID string) ([]byte, error) {
	if !validPolicyID(binding.ID) || !validPolicyID(binding.Owner) || binding.Revision == 0 {
		return nil, ErrInvalidCheck
	}
	switch binding.Purpose {
	case "ai-byok", "dns-provider", "smtp-password", "qqbot-secret", "xray-user", "xray-reality":
	default:
		return nil, ErrDenied
	}
	return json.Marshal(struct {
		Domain, KeyID string
		Binding       credentialBinding
	}{"serverfleet.credential.v1", keyID, binding})
}
func (c *credentialCipher) seal(binding credentialBinding, plaintext []byte) (sealedCredential, error) {
	if c == nil || c.keys[c.active] == nil || len(plaintext) == 0 || len(plaintext) > 64<<10 {
		return sealedCredential{}, ErrIdentityUnavailable
	}
	aad, err := bindingAAD(binding, c.active)
	if err != nil {
		return sealedCredential{}, err
	}
	return sealedCredential{c.active, c.keys[c.active].Seal(nil, nil, plaintext, aad)}, nil
}
func (c *credentialCipher) open(binding credentialBinding, record sealedCredential, expires, now time.Time, revoked bool) ([]byte, error) {
	if c == nil || c.keys[record.KeyID] == nil || len(record.Ciphertext) > 64<<10+28 || len(record.Ciphertext) < 28 || now.IsZero() || expires.IsZero() || !now.Before(expires) || revoked {
		return nil, ErrDenied
	}
	aad, err := bindingAAD(binding, record.KeyID)
	if err != nil {
		return nil, err
	}
	plaintext, err := c.keys[record.KeyID].Open(nil, nil, record.Ciphertext, aad)
	if err != nil {
		return nil, ErrDenied
	}
	return plaintext, nil
}

// rotate returns new ciphertext only. Caller must atomically replace the record
// before retiring the old key; errors leave the old record untouched. Durable
// per-key usage limits (GCM <2^32 encryptions/key) belong to that reviewed store.
func (c *credentialCipher) rotate(binding credentialBinding, record sealedCredential, expires, now time.Time, revoked bool) (sealedCredential, error) {
	plaintext, err := c.open(binding, record, expires, now, revoked)
	if err != nil {
		return sealedCredential{}, err
	}
	defer clear(plaintext)
	return c.seal(binding, plaintext)
}
