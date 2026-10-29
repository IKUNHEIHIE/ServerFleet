package dns

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// CredentialRef is an opaque identifier issued by the CX secret store. It is
// intentionally not a token type: no API in this package accepts raw provider
// credentials.
type CredentialRef string

// ParseCredentialRef validates an opaque secret-store reference.
func ParseCredentialRef(value string) (CredentialRef, error) {
	ref := CredentialRef(value)
	if err := ref.Validate(); err != nil {
		return "", err
	}
	return ref, nil
}

// Validate accepts a non-empty token without whitespace or control characters,
// bounded to 256 bytes.
func (r CredentialRef) Validate() error {
	value := string(r)
	if value == "" || len(value) > 256 {
		return ErrInvalidCredentialRef
	}
	if !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return ErrInvalidCredentialRef
	}
	if strings.ContainsFunc(value, func(ch rune) bool {
		return unicode.IsSpace(ch) || unicode.IsControl(ch) || unicode.In(ch, unicode.Cf, unicode.Zl, unicode.Zp)
	}) {
		return ErrInvalidCredentialRef
	}
	return nil
}

func (r CredentialRef) String() string { return string(r) }
