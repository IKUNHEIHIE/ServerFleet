package auth

import "time"

// SealWebsiteKey creates a domain-separated ciphertext for a new issue task.
func (v *CredentialService) SealWebsiteKey(issue, owner string, key []byte) (string, []byte, error) {
	sealed, err := v.cipher.seal(credentialBinding{ID: issue, Owner: owner, Purpose: "website-private-key", Revision: 1}, key)
	return sealed.KeyID, sealed.Ciphertext, err
}

// OpenWebsiteKey is called after validating the task and its authenticated Agent target.
func (v *CredentialService) OpenWebsiteKey(issue, owner, keyID string, ciphertext []byte, expires, now time.Time) ([]byte, error) {
	return v.cipher.open(credentialBinding{ID: issue, Owner: owner, Purpose: "website-private-key", Revision: 1}, sealedCredential{KeyID: keyID, Ciphertext: ciphertext}, expires, now, false)
}
