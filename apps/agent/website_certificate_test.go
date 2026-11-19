//go:build linux

package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/transport"
)

func TestManagedWebsiteDeliveryInstallsOneGenerationAtATime(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	agent, err := newCertificateCSRCapability(root, "node-test", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	now := time.Now()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Synthetic delivery CA"}, NotBefore: now.Add(-time.Hour), NotAfter: now.AddDate(1, 0, 0), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	parsedCA, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(parsedCA)
	domain := "website.example.com"
	makeGeneration := func(n uint64) (transport.WebsiteCertificatePlan, transport.WebsiteCertificateBundle) {
		t.Helper()
		private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		key, err := x509.MarshalPKCS8PrivateKey(private)
		if err != nil {
			t.Fatal(err)
		}
		leaf := &x509.Certificate{SerialNumber: big.NewInt(int64(n) + 1), Subject: pkix.Name{CommonName: domain}, DNSNames: []string{domain}, NotBefore: now.Add(-time.Hour), NotAfter: now.AddDate(0, 0, 80+int(n)), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
		der, err := x509.CreateCertificate(rand.Reader, leaf, ca, &private.PublicKey, caKey)
		if err != nil {
			t.Fatal(err)
		}
		chain := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
		return transport.WebsiteCertificatePlan{Domain: domain, OrderID: fmt.Sprintf("order-%d", n), Generation: n, ChainSHA256: fmt.Sprintf("%x", sha256.Sum256(chain))}, transport.WebsiteCertificateBundle{KeyDER: key, ChainPEM: chain}
	}
	first, firstBundle := makeGeneration(1)
	second, secondBundle := makeGeneration(2)
	mismatch := secondBundle
	mismatch.KeyDER = firstBundle.KeyDER
	if transport.VerifyWebsiteCertificate(second, mismatch, roots, now) == nil {
		t.Fatal("accepted mismatched private key")
	}
	for _, item := range []struct {
		plan   transport.WebsiteCertificatePlan
		bundle transport.WebsiteCertificateBundle
	}{{first, firstBundle}, {second, secondBundle}, {second, secondBundle}} {
		if err := transport.VerifyWebsiteCertificate(item.plan, item.bundle, roots, now); err != nil {
			t.Fatal(err)
		}
		if err := agent.installManagedWebsite(t.Context(), item.plan, item.bundle); err != nil {
			t.Fatal(err)
		}
	}
	if err := agent.installManagedWebsite(t.Context(), first, firstBundle); err == nil {
		t.Fatal("older generation replaced current files")
	}
	path := filepath.Join(root, "managed", fmt.Sprintf("%x", sha256.Sum256([]byte(domain))), "current")
	current, err := os.Readlink(path)
	if err != nil || current != "2-order-2" {
		t.Fatalf("current=%q err=%v", current, err)
	}
	if _, err := tls.LoadX509KeyPair(filepath.Join(path, "fullchain.pem"), filepath.Join(path, "privkey.pem")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(path, "privkey.pem"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("private key mode: %v err=%v", info, err)
	}
}
