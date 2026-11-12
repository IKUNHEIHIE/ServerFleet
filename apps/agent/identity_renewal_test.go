package main

import (
	"bytes"
	"context"
	"crypto/x509"
	"math/big"
	"os"
	"path/filepath"
	agentruntime "serverfleet.local/serverfleet/apps/agent/runtime"
	"testing"
	"time"
)

func TestAgentIdentityRenewalAtomicReplacementAndRestart(t *testing.T) {
	s, root, key, directory := identityStoreFixture(t)
	ctx := context.Background()
	prepared, e := s.Prepare(ctx, "0.9.1-test")
	if e != nil {
		t.Fatal(e)
	}
	initial := issuedIdentityFixture(t, prepared, root, key, nil)
	if e = s.Accept(ctx, initial); e != nil {
		t.Fatal(e)
	}
	prior, _, e := s.Status(ctx)
	if e != nil {
		t.Fatal(e)
	}
	originalKey, _ := os.ReadFile(filepath.Join(directory, "identity-key.pem"))
	foreign := issuedIdentityFixture(t, prepared, root, key, func(c *x509.Certificate) {
		c.Subject.CommonName = "other-node"
		c.SerialNumber = big.NewInt(3)
		c.NotAfter = time.Now().Add(time.Hour)
	})
	foreign.NodeID = "other-node"
	if e = s.replaceIssued(ctx, prior, foreign); e == nil {
		t.Fatal("changed node accepted")
	}
	current, _, _ := s.Status(ctx)
	if current != prior {
		t.Fatal("failed renewal changed identity")
	}
	renewed := issuedIdentityFixture(t, prepared, root, key, func(c *x509.Certificate) { c.SerialNumber = big.NewInt(4); c.NotAfter = time.Now().Add(time.Hour) })
	if e = s.replaceIssued(ctx, prior, renewed); e != nil {
		t.Fatal(e)
	}
	if e = s.replaceIssued(ctx, prior, renewed); e != nil {
		t.Fatal("identical retry rejected", e)
	}
	if changed, e := installedAgentIdentityChanged(ctx, agentruntime.ConnectRequest{StateDir: directory, RootPin: s.pin}, prior); e != nil || !changed {
		t.Fatal("could not detect an already installed successor", e)
	}
	reopened, e := openAgentIdentity(directory, s.pin, 3)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	facts, enrolled, e := reopened.Status(ctx)
	if e != nil || !enrolled || facts.Fingerprint == prior.Fingerprint || !facts.ExpiresAt.After(prior.ExpiresAt) {
		t.Fatal("restart lost renewed identity", e)
	}
	after, _ := os.ReadFile(filepath.Join(directory, "identity-key.pem"))
	if !bytes.Equal(originalKey, after) {
		t.Fatal("renewal replaced the private key")
	}
	info, _ := os.Stat(filepath.Join(directory, "issued.json"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("identity file not private")
	}
}
