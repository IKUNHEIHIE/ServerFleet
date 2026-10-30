package certificates

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// KeyRef is an opaque reference to key material held by the CX secret store. It
// is never the key itself.
type KeyRef string

// ParseKeyRef validates an opaque key reference.
func ParseKeyRef(value string) (KeyRef, error) {
	ref := KeyRef(value)
	if err := ref.Validate(); err != nil {
		return "", err
	}
	return ref, nil
}

// Validate accepts a non-empty token without whitespace or control characters,
// bounded to 256 bytes, and rejects anything that looks like PEM key material.
func (r KeyRef) Validate() error {
	value := string(r)
	if containsPrivateKeyMaterial(value) {
		return ErrKeyMaterialRejected
	}
	if value == "" || len(value) > 256 {
		return ErrInvalidKeyRef
	}
	if !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return ErrInvalidKeyRef
	}
	if strings.ContainsFunc(value, func(ch rune) bool {
		return unicode.IsSpace(ch) || unicode.IsControl(ch) || unicode.In(ch, unicode.Cf, unicode.Zl, unicode.Zp)
	}) {
		return ErrInvalidKeyRef
	}
	return nil
}
