package localpki

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
)

// CreateACMEAccountKey creates only a fresh account key in a new private
// directory. It is not a Root/Intermediate and cannot overwrite existing data.
func CreateACMEAccountKey(directory string) (string, error) {
	root, err := newPrivatePKIDirectory(directory)
	if err != nil {
		return "", err
	}
	defer root.Close()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", err
	}
	raw, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", err
	}
	defer clear(raw)
	encoded := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: raw})
	defer clear(encoded)
	if err = privatePKIWrite(root, "account-key.pem", encoded); err != nil {
		return "", err
	}
	public, err := x509.MarshalPKIXPublicKey(key.Public())
	if err != nil {
		return "", err
	}
	return provisionedDigest(public), nil
}
