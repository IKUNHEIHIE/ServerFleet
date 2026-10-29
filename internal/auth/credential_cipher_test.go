package auth

import (
	"bytes"
	"testing"
	"time"
)

func TestCredentialCipherBindingExpiryRotationAndTamper(t *testing.T) {
	keys := map[string][]byte{"old": bytes.Repeat([]byte{1}, 32), "new": bytes.Repeat([]byte{2}, 32)}
	c, e := newCredentialCipher("old", keys)
	if e != nil {
		t.Fatal(e)
	}
	binding := credentialBinding{"credential-1", "user-1", "ai-byok", 1}
	plaintext := []byte("synthetic-test-only-value")
	now := time.Now()
	expires := now.Add(time.Hour)
	record, e := c.seal(binding, plaintext)
	if e != nil || bytes.Contains(record.Ciphertext, plaintext) {
		t.Fatal(e)
	}
	for _, change := range []func(*credentialBinding){func(b *credentialBinding) { b.ID = "other" }, func(b *credentialBinding) { b.Owner = "other" }, func(b *credentialBinding) { b.Purpose = "smtp-password" }, func(b *credentialBinding) { b.Revision++ }} {
		bad := binding
		change(&bad)
		if _, e = c.open(bad, record, expires, now, false); e == nil {
			t.Fatal("unbound decryption")
		}
	}
	if _, e = c.open(binding, record, expires, expires, false); e == nil {
		t.Fatal("expired decrypted")
	}
	if _, e = c.open(binding, record, expires, now, true); e == nil {
		t.Fatal("revoked decrypted")
	}
	newCipher, _ := newCredentialCipher("new", keys)
	rotated, e := newCipher.rotate(binding, record, expires, now, false)
	if e != nil || rotated.KeyID != "new" {
		t.Fatal(e)
	}
	retired, _ := newCredentialCipher("new", map[string][]byte{"new": keys["new"]})
	if _, e = retired.open(binding, record, expires, now, false); e == nil {
		t.Fatal("retired key accepted")
	}
	value, e := retired.open(binding, rotated, expires, now, false)
	if e != nil || !bytes.Equal(value, plaintext) {
		t.Fatal(e)
	}
	rotated.Ciphertext[0] ^= 1
	if _, e = retired.open(binding, rotated, expires, now, false); e == nil {
		t.Fatal("tamper accepted")
	}
	binding.Purpose = "root-ca"
	if _, e = c.seal(binding, plaintext); e == nil {
		t.Fatal("CA secret accepted")
	}
}
