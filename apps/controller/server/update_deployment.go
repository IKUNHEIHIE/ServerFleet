package server

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"regexp"
	"runtime"

	"serverfleet.local/serverfleet/internal/updates"
)

var ErrUpdateDeployment = errors.New("updates: invalid or insecure startup deployment")

// Only an operator-owned startup file may supply paths and public trust anchors.
// It never grants an HTTP caller execution rights or supplies a signing secret.
type updateDeploymentDocument struct {
	Version           int                     `json:"version"`
	Directory         string                  `json:"directory"`
	Inbox             string                  `json:"inbox"`
	StageDir          string                  `json:"stageDir"`
	KeyID             string                  `json:"keyId"`
	NodeID            string                  `json:"nodeId"`
	IdentityDir       string                  `json:"identityDir"`
	ControllerOrigin  string                  `json:"controllerOrigin"`
	RootCertificate   string                  `json:"rootCertificate"`
	PublicKeys        map[string]string       `json:"publicKeys"`
	CandidateVersion  string                  `json:"candidateVersion"`
	Initial           updates.ReleaseManifest `json:"initial"`
	MinimumSequence   uint64                  `json:"minimumSequence"`
	MaxBytes          int64                   `json:"maxBytes"`
	SchemaVersion     int                     `json:"schemaVersion"`
	SchemaFingerprint string                  `json:"schemaFingerprint"`
}

func LoadUpdateDeployment(filename string) (*updates.ProductDeploymentOptions, error) {
	raw, err := readPrivateDeployment(filename)
	if err != nil {
		return nil, ErrUpdateDeployment
	}
	return parseUpdateDeployment(raw)
}

var deploymentID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)
var deploymentVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

func parseUpdateDeployment(raw []byte) (*updates.ProductDeploymentOptions, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if gatewayDeploymentUniqueJSON(decoder) != nil {
		return nil, ErrUpdateDeployment
	}
	// Require one JSON document, including when a duplicate key is nested.
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return nil, ErrUpdateDeployment
	}
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var d updateDeploymentDocument
	if decoder.Decode(&d) != nil || d.Version != 1 || !deploymentID.MatchString(d.KeyID) || !deploymentID.MatchString(d.NodeID) ||
		!deploymentVersion.MatchString(d.CandidateVersion) || d.Initial.Component != "agent" || d.Initial.OS != "linux" ||
		d.Initial.Architecture != runtime.GOARCH || d.Initial.Format != 1 || !deploymentVersion.MatchString(d.Initial.Version) ||
		d.Initial.Sequence == 0 || d.Initial.Bytes <= 0 || d.Initial.Bytes > 256<<20 || d.MaxBytes <= 0 || d.MaxBytes > 256<<20 ||
		d.SchemaVersion < 0 || len(d.PublicKeys) == 0 || len(d.PublicKeys) > 16 {
		return nil, ErrUpdateDeployment
	}
	for _, digest := range []string{d.Initial.SHA256, d.SchemaFingerprint} {
		decoded, err := hex.DecodeString(digest)
		if err != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != digest {
			return nil, ErrUpdateDeployment
		}
	}
	for _, path := range []string{d.Directory, d.Inbox, d.StageDir, d.IdentityDir, d.RootCertificate} {
		if !validGatewayDeploymentPath(path) {
			return nil, ErrUpdateDeployment
		}
	}
	if d.Directory == d.Inbox || d.Directory == d.StageDir || d.Inbox == d.StageDir {
		return nil, ErrUpdateDeployment
	}
	origin, err := url.Parse(d.ControllerOrigin)
	if err != nil || origin.Scheme != "https" || origin.Hostname() == "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || (origin.Path != "" && origin.Path != "/") || origin.Opaque != "" {
		return nil, ErrUpdateDeployment
	}
	keys := make(map[string]ed25519.PublicKey, len(d.PublicKeys))
	for id, encoded := range d.PublicKeys {
		key, err := base64.StdEncoding.Strict().DecodeString(encoded)
		if !deploymentID.MatchString(id) || err != nil || len(key) != ed25519.PublicKeySize {
			return nil, ErrUpdateDeployment
		}
		keys[id] = ed25519.PublicKey(key)
	}
	if keys[d.KeyID] == nil {
		return nil, ErrUpdateDeployment
	}
	return &updates.ProductDeploymentOptions{
		Directory: d.Directory, Inbox: d.Inbox, StageDir: d.StageDir, KeyID: d.KeyID, NodeID: d.NodeID,
		IdentityDir: d.IdentityDir, ControllerOrigin: d.ControllerOrigin, RootCertificate: d.RootCertificate,
		CandidateVersion: d.CandidateVersion, Initial: d.Initial, SchemaVersion: d.SchemaVersion, SchemaFingerprint: d.SchemaFingerprint,
		Policy: updates.ReleasePolicy{Component: "agent", Architecture: d.Initial.Architecture, MinimumSequence: d.MinimumSequence, MaxBytes: d.MaxBytes, TrustedKeys: keys},
	}, nil
}
