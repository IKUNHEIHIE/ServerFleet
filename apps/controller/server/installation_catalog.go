package server

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"serverfleet.local/serverfleet/internal/recovery"
	"serverfleet.local/serverfleet/internal/transport"
)

type InstallationDownload struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}
type InstallationSource struct {
	Origin          string   `json:"origin"`
	Addresses       []string `json:"addresses"`
	RootPEM         string   `json:"rootPem"`
	Version         string   `json:"version"`
	KeyID           string   `json:"keyId"`
	PublicKey       string   `json:"publicKey"`
	MinimumSequence uint64   `json:"minimumSequence"`
}
type InstallationCatalog struct {
	Format              int                             `json:"format"`
	Source              InstallationSource              `json:"source"`
	IdentityRootPEM     string                          `json:"identityRootPem"`
	HTTPSRootPEM        string                          `json:"httpsRootPem"`
	ControllerOrigin    string                          `json:"controllerOrigin"`
	ControllerAddresses []string                        `json:"controllerAddresses"`
	RootPin             string                          `json:"rootPin"`
	InstallerScript     InstallationDownload            `json:"installerScript"`
	Bootstrap           map[string]InstallationDownload `json:"bootstrap"`
}

func installationHTTPSURL(value string) bool {
	u, e := url.Parse(value)
	return e == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && !strings.ContainsAny(value, "\r\n\t ")
}
func installationRoot(raw string, pin string) bool {
	block, rest := pem.Decode([]byte(raw))
	if block == nil || block.Type != "CERTIFICATE" || len(bytes.TrimSpace(rest)) != 0 {
		return false
	}
	ca, e := x509.ParseCertificate(block.Bytes)
	if e != nil || !ca.IsCA || !ca.BasicConstraintsValid || ca.CheckSignatureFrom(ca) != nil || time.Now().Before(ca.NotBefore) || !time.Now().Before(ca.NotAfter) {
		return false
	}
	sum := sha256.Sum256(ca.RawSubjectPublicKeyInfo)
	return pin == "" || hex.EncodeToString(sum[:]) == pin
}
func LoadInstallationCatalog(filename, origin, pin string) (*InstallationCatalog, error) {
	raw, e := recovery.ReadPrivateFile(filename, 64<<10)
	if e != nil {
		return nil, e
	}
	var c InstallationCatalog
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if gatewayDeploymentUniqueJSON(decoder) != nil {
		return nil, ErrGatewayDeployment
	}
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&c) != nil || c.Format != 1 || c.ControllerOrigin != origin || c.RootPin != pin || !installationRoot(c.IdentityRootPEM, pin) || !installationRoot(c.HTTPSRootPEM, "") || !installationRoot(c.Source.RootPEM, "") || !installationHTTPSURL(c.Source.Origin) || !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(c.Source.Version) || len(c.Source.Version) > 64 || !transport.FileID(c.Source.KeyID) || c.Source.MinimumSequence == 0 || c.Source.MinimumSequence > 1<<53-1 || len(c.ControllerAddresses) == 0 || len(c.ControllerAddresses) > 8 || len(c.Source.Addresses) == 0 || len(c.Source.Addresses) > 8 || len(c.Bootstrap) != 2 {
		return nil, ErrGatewayDeployment
	}
	u, _ := url.Parse(c.Source.Origin)
	if u == nil || u.Path != "" || c.Source.Origin != "https://"+u.Host {
		return nil, ErrGatewayDeployment
	}
	for _, addresses := range [][]string{c.Source.Addresses, c.ControllerAddresses} {
		seen := map[string]bool{}
		for _, address := range addresses {
			ip := net.ParseIP(address)
			if ip == nil || ip.String() != address || ip.IsUnspecified() || ip.IsMulticast() || seen[address] {
				return nil, ErrGatewayDeployment
			}
			seen[address] = true
		}
	}
	key, e := base64.StdEncoding.Strict().DecodeString(c.Source.PublicKey)
	if e != nil || len(key) != ed25519.PublicKeySize {
		return nil, ErrGatewayDeployment
	}
	for _, d := range []InstallationDownload{c.InstallerScript, c.Bootstrap["amd64"], c.Bootstrap["arm64"]} {
		if !installationHTTPSURL(d.URL) || !transport.FileDigest(d.SHA256) {
			return nil, ErrGatewayDeployment
		}
	}
	return &c, nil
}
func (s *Server) registerInstallationCatalogRoutes(c *InstallationCatalog) {
	s.mux.HandleFunc("GET /api/v1/installations/catalog", func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := s.operatorPrincipal(w, r); !ok {
			return
		}
		if r.URL.RawQuery != "" || r.ContentLength != 0 {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		if c == nil {
			releaseSourceJSON(w, 200, map[string]any{"available": false})
			return
		}
		releaseSourceJSON(w, 200, map[string]any{"available": true, "catalog": c})
	})
}
