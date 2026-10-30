package plugins

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
)

var ErrPackageRejected = errors.New("plugins: package rejected")
var pluginVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// PackageManifest is a proposed signed package metadata format. It is NOT the
// final Host ABI, a WASM runtime decision, or permission to install/execute.
// Signature covers exact canonical bytes with a domain separation prefix.
type PackageManifest struct {
	Format        int         `json:"format"`
	ID            string      `json:"id"`
	Version       string      `json:"version"`
	Source        string      `json:"source"`
	PayloadSHA256 string      `json:"payloadSha256"`
	PayloadBytes  int         `json:"payloadBytes"`
	Capabilities  []string    `json:"capabilities"`
	UI            []UIElement `json:"ui"`
}
type UIElement struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Label  string `json:"label"`
	Action string `json:"action,omitempty"`
}
type PackageTrust struct {
	Keys map[string]ed25519.PublicKey
	// Source URLs are exact pins configured by an administrator after reviewing
	// official/Fork provenance; HTTPS alone does not establish trust.
	Sources      map[string]bool
	Capabilities map[string]bool
}

// VerifyPackage checks signature, pinned source, digest, schema and declarative
// UI. Only xray-manager is currently an approved optional business package.
// There is no arbitrary script, HTML, external URL, shell or root Host API.
func VerifyPackage(raw, signature, payload []byte, keyID string, trust PackageTrust) (PackageManifest, error) {
	var m PackageManifest
	if len(raw) == 0 || len(raw) > 64<<10 || len(payload) > 32<<20 || len(signature) != ed25519.SignatureSize {
		return m, ErrPackageRejected
	}
	key := trust.Keys[keyID]
	if len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, append([]byte("serverfleet.plugin.v1\x00"), raw...), signature) {
		return m, ErrPackageRejected
	}
	if json.Unmarshal(raw, &m) != nil {
		return PackageManifest{}, ErrPackageRejected
	}
	canonical, _ := json.Marshal(m)
	if !bytes.Equal(raw, canonical) {
		return PackageManifest{}, ErrPackageRejected
	}
	u, err := url.Parse(m.Source)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.ForceQuery || !trust.Sources[m.Source] || m.Format != 1 || m.ID != "xray-manager" || !pluginVersion.MatchString(m.Version) || m.PayloadBytes != len(payload) || m.PayloadBytes == 0 {
		return PackageManifest{}, ErrPackageRejected
	}
	h := sha256.Sum256(payload)
	if m.PayloadSHA256 != hex.EncodeToString(h[:]) {
		return PackageManifest{}, ErrPackageRejected
	}
	if len(m.Capabilities) > 64 || len(m.UI) > 128 {
		return PackageManifest{}, ErrPackageRejected
	}
	seen := map[string]bool{}
	for _, capability := range m.Capabilities {
		if seen[capability] || !trust.Capabilities[capability] || !strings.HasPrefix(capability, "xray.") || strings.ContainsAny(capability, "*?[] \t\r\n\x00") {
			return PackageManifest{}, ErrPackageRejected
		}
		seen[capability] = true
	}
	ids := map[string]bool{}
	for _, ui := range m.UI {
		if ui.ID == "" || len(ui.ID) > 64 || ids[ui.ID] || ui.Label == "" || len(ui.Label) > 256 {
			return PackageManifest{}, ErrPackageRejected
		}
		ids[ui.ID] = true
		switch ui.Kind {
		case "text", "table", "metric":
			if ui.Action != "" {
				return PackageManifest{}, ErrPackageRejected
			}
		case "action":
			if !seen[ui.Action] {
				return PackageManifest{}, ErrPackageRejected
			}
		default:
			return PackageManifest{}, ErrPackageRejected
		}
	}
	return m, nil
}

// CheckInvocation is a dispatch-time lifecycle check, not an authorization or
// approval substitute. Caller must read the CURRENT persisted install revision
// inside the same task admission boundary. Old enabled snapshots aren't tickets.
func CheckInvocation(enabled bool, installedVersion string, revision, expectedRevision uint64, manifest PackageManifest, action string) error {
	if !enabled || revision == 0 || revision != expectedRevision || manifest.ID != "xray-manager" || manifest.Version != installedVersion {
		return ErrPackageRejected
	}
	for _, capability := range manifest.Capabilities {
		if capability == action {
			return nil
		}
	}
	return ErrPackageRejected
}
