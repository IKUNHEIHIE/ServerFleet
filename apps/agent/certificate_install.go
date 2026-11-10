package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"os"
	"time"

	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/transport"
)

func (c *certificateCSRCapability) readPrivate(name string, max int64) ([]byte, error) {
	if !c.current() {
		return nil, errExecutionRejected
	}
	info, err := c.root.Lstat(name)
	if err != nil || !csrPrivateInfo(info, false) || info.Size() <= 0 || info.Size() > max {
		return nil, errExecutionRejected
	}
	file, err := c.root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(info, after) {
		return nil, errExecutionRejected
	}
	raw, err := io.ReadAll(io.LimitReader(file, max+1))
	if err != nil || int64(len(raw)) != info.Size() {
		clear(raw)
		return nil, errExecutionRejected
	}
	return raw, nil
}
func (c *certificateCSRCapability) InstallAction() agentAction {
	return agentAction{
		Validate: func(raw []byte) error {
			if !c.current() || c.roots == nil {
				return errExecutionRejected
			}
			_, _, err := transport.ParseCertificateInstallRequest(raw)
			return err
		}, Run: c.install,
		FilterOutput: func(ctx context.Context, raw []byte) ([]byte, error) {
			if ctx.Err() != nil || !c.current() {
				return nil, errExecutionRejected
			}
			var result transport.CertificateInstallResult
			if json.Unmarshal(raw, &result) != nil || result.NodeID != c.nodeID || !result.NotAfter.After(time.Now()) {
				return nil, errExecutionRejected
			}
			canonical, _ := json.Marshal(result)
			if !bytes.Equal(canonical, raw) {
				return nil, errExecutionRejected
			}
			return bytes.Clone(raw), nil
		}}
}
func (c *certificateCSRCapability) install(ctx context.Context, raw []byte) executionResult {
	failed := executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_FAILED}
	unknown := executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN}
	request, chain, err := transport.ParseCertificateInstallRequest(raw)
	if err != nil || ctx == nil || ctx.Err() != nil || !c.current() || c.roots == nil {
		return failed
	}
	child, err := c.root.Lstat(request.KeyRef)
	if err != nil || !csrPrivateInfo(child, true) {
		return failed
	}
	public, err := c.readPrivate(request.KeyRef+"/csr.json", 8<<10)
	if err != nil {
		return failed
	}
	csr, err := transport.ValidateCertificateCSRResult(public, c.nodeID, request.Domain, request.KeyRef)
	if err != nil || csr.SHA256 != request.CSRSHA256 {
		return failed
	}
	csrDER, err := base64.StdEncoding.Strict().DecodeString(csr.CSRDER)
	if err != nil {
		return failed
	}
	leaf, err := transport.VerifyNodeCertificateChain(chain, csrDER, request.Domain, c.roots, time.Now())
	if err != nil {
		return failed
	}
	keyPEM, err := c.readPrivate(request.KeyRef+"/website-key.pem", 16<<10)
	if err != nil {
		return failed
	}
	defer clear(keyPEM)
	block, rest := pem.Decode(keyPEM)
	if block == nil || block.Type != "PRIVATE KEY" || len(bytes.TrimSpace(rest)) != 0 {
		return failed
	}
	defer clear(block.Bytes)
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return failed
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return failed
	}
	publicKey, err := x509.MarshalPKIXPublicKey(key.Public())
	if err != nil || !bytes.Equal(publicKey, leaf.RawSubjectPublicKeyInfo) {
		return failed
	}
	leafHash := sha256.Sum256(leaf.Raw)
	result := transport.CertificateInstallResult{NodeID: c.nodeID, Domain: request.Domain, KeyRef: request.KeyRef, CSRSHA256: request.CSRSHA256, ChainSHA256: request.ChainSHA256, LeafSHA256: hex.EncodeToString(leafHash[:]), NotAfter: leaf.NotAfter}
	name := request.KeyRef + "/chain-" + result.LeafSHA256 + ".pem"
	// Existing exact bytes are reconciled read-only, never overwritten. A
	// different/truncated/symlink target remains an operator-visible failure.
	if _, err = c.root.Lstat(name); err == nil {
		old, e := c.readPrivate(name, 8<<10)
		if e != nil || !bytes.Equal(old, chain) {
			return failed
		}
	} else if !os.IsNotExist(err) {
		return failed
	} else {
		if ctx.Err() != nil || !c.current() {
			return failed
		}
		file, e := c.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return unknown
		}
		n, e := file.Write(chain)
		syncErr := file.Sync()
		closeErr := file.Close()
		if e != nil || syncErr != nil || closeErr != nil || n != len(chain) {
			return unknown
		}
		directory, e := c.root.Open(request.KeyRef)
		if e != nil {
			return unknown
		}
		e = directory.Sync()
		directory.Close()
		if e != nil {
			return unknown
		}
	}
	actual, err := c.readPrivate(name, 8<<10)
	if err != nil || !bytes.Equal(actual, chain) || ctx.Err() != nil {
		return unknown
	}
	out, err := json.Marshal(result)
	if err != nil {
		return unknown
	}
	if _, err = transport.ValidateCertificateInstallResult(out, c.nodeID, request); err != nil {
		return unknown
	}
	return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED, Output: out}
}
