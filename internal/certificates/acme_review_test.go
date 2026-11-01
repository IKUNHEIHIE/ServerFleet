package certificates_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/certificates"
)

type acmeReviewTransport func(*http.Request) (*http.Response, error)

func (f acmeReviewTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestWave6ACMERejectsCertificateForDifferentKeyOrName(t *testing.T) {
	for _, mode := range []string{"wrong-key", "wrong-name", "expired"} {
		t.Run(mode, func(t *testing.T) {
			fake := newFakeACMEServer(t)
			signer := newTestKeySigner(t, "account-key", "cert-key")
			client := fake.server.Client()
			base := client.Transport
			client.Transport = acmeReviewTransport(func(r *http.Request) (*http.Response, error) {
				response, err := base.RoundTrip(r)
				if err != nil || r.URL.Path != "/certificate" {
					return response, err
				}
				defer response.Body.Close()
				key := &signer.keys["cert-key"].PublicKey
				if mode == "wrong-key" {
					other, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
					if e != nil {
						return nil, e
					}
					key = &other.PublicKey
				}
				name := "app.example.com"
				if mode == "wrong-name" {
					name = "other.example.com"
				}
				end := time.Now().Add(time.Hour)
				if mode == "expired" {
					end = time.Now().Add(-time.Hour)
				}
				leaf := &x509.Certificate{SerialNumber: big.NewInt(123), DNSNames: []string{name},
					NotBefore: time.Now().Add(-2 * time.Hour), NotAfter: end,
					KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
				der, e := x509.CreateCertificate(rand.Reader, leaf, fake.caCert, key, fake.caKey)
				if e != nil {
					return nil, e
				}
				chain := append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), fake.caPEM...)
				response.Body = io.NopCloser(bytes.NewReader(chain))
				response.ContentLength = int64(len(chain))
				return response, nil
			})
			trust := x509.NewCertPool()
			trust.AddCert(fake.caCert)
			acme, err := certificates.NewACMEClient(certificates.ACMEConfig{DirectoryURL: fake.server.URL + "/directory", Client: client,
				AccountKey: "account-key", Signer: signer, Challenge: newACMEPublisher(fake), TrustRoots: trust,
				PollInterval: time.Millisecond, Timeout: 5 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			issued, err := acme.IssueDNS01(context.Background(), certificates.ACMERequest{CommonName: "app.example.com", KeyRef: "cert-key"})
			if err == nil || len(issued.CertificatePEM) != 0 {
				t.Fatal("accepted unbound or expired certificate as issued")
			}
		})
	}
}
