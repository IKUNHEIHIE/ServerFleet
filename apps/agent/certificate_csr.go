package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"

	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/transport"
)

type certificateCSRCapability struct {
	root   *os.Root
	nodeID string
}

func newCertificateCSRCapability(directory, nodeID, fileRoot string) (*certificateCSRCapability, error) {
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory || !executionIDPattern.MatchString(nodeID) || directory == fileRoot || fileRoot != "" && (agentPathWithinRoot(fileRoot, directory) || agentPathWithinRoot(directory, fileRoot)) {
		return nil, errExecutionRejected
	}
	info, err := os.Lstat(directory)
	if err != nil || !csrPrivateInfo(info, true) {
		return nil, errExecutionRejected
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	after, err := root.Stat(".")
	if err != nil || !os.SameFile(info, after) {
		root.Close()
		return nil, errExecutionRejected
	}
	return &certificateCSRCapability{root, nodeID}, nil
}
func (c *certificateCSRCapability) Close() error {
	if c == nil || c.root == nil {
		return nil
	}
	return c.root.Close()
}
func (c *certificateCSRCapability) Action() agentAction {
	return agentAction{
		Validate: func(raw []byte) error {
			if !c.current() {
				return errExecutionRejected
			}
			_, err := transport.ParseCertificateCSRRequest(raw)
			return err
		}, Run: c.run,
		FilterOutput: func(ctx context.Context, raw []byte) ([]byte, error) {
			if ctx.Err() != nil || !c.current() {
				return nil, errExecutionRejected
			}
			var value transport.CertificateCSRResult
			if json.Unmarshal(raw, &value) != nil {
				return nil, errExecutionRejected
			}
			if _, err := transport.ValidateCertificateCSRResult(raw, c.nodeID, value.Domain, value.KeyRef); err != nil {
				return nil, errExecutionRejected
			}
			return bytes.Clone(raw), nil
		}}
}
func (c *certificateCSRCapability) run(ctx context.Context, raw []byte) executionResult {
	failed := executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_FAILED}
	unknown := executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN}
	request, err := transport.ParseCertificateCSRRequest(raw)
	if err != nil || c == nil || !c.current() || ctx == nil || ctx.Err() != nil {
		return failed
	}
	// A key reference is reserved once. Existing/partial keys are never replaced,
	// exported, reused by a different request, or regenerated after UNKNOWN.
	if c.root.Mkdir(request.KeyRef, 0700) != nil {
		return failed
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return unknown
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return unknown
	}
	defer clear(private)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})
	defer clear(keyPEM)
	write := func(name string, value []byte) error {
		if ctx.Err() != nil || !c.current() {
			return errExecutionRejected
		}
		child, e := c.root.Lstat(request.KeyRef)
		if e != nil || !csrPrivateInfo(child, true) {
			return errExecutionRejected
		}
		file, e := c.root.OpenFile(request.KeyRef+"/"+name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return e
		}
		n, e := file.Write(value)
		syncErr := file.Sync()
		closeErr := file.Close()
		if e != nil {
			return e
		}
		if syncErr != nil || closeErr != nil || n != len(value) {
			return errExecutionRejected
		}
		return nil
	}
	if write("website-key.pem", keyPEM) != nil {
		return unknown
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: request.Domain}, DNSNames: []string{request.Domain}, SignatureAlgorithm: x509.ECDSAWithSHA256}, key)
	if err != nil {
		return unknown
	}
	sum := sha256.Sum256(csr)
	result := transport.CertificateCSRResult{NodeID: c.nodeID, Domain: request.Domain, KeyRef: request.KeyRef, CSRDER: base64.StdEncoding.EncodeToString(csr), SHA256: hex.EncodeToString(sum[:])}
	out, err := json.Marshal(result)
	if err != nil || write("csr.json", out) != nil {
		return unknown
	}
	for _, name := range []string{request.KeyRef, "."} {
		directory, e := c.root.Open(name)
		if e != nil {
			return unknown
		}
		e = directory.Sync()
		directory.Close()
		if e != nil {
			return unknown
		}
	}
	if ctx.Err() != nil {
		return unknown
	}
	if _, err = transport.ValidateCertificateCSRResult(out, c.nodeID, request.Domain, request.KeyRef); err != nil {
		return unknown
	}
	return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED, Output: out}
}

func (c *certificateCSRCapability) current() bool {
	if c == nil || c.root == nil {
		return false
	}
	info, err := c.root.Stat(".")
	return err == nil && csrPrivateInfo(info, true)
}
