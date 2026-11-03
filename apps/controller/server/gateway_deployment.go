package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ErrGatewayDeployment means the trusted startup gateway document is missing,
// insecure or inconsistent with the current fleet inventory.
var ErrGatewayDeployment = errors.New("gateway: invalid or insecure deployment configuration")

type gatewayDeploymentEntry struct {
	GatewayID   string `json:"gatewayId"`
	SiteDir     string `json:"siteDir"`
	ArtifactDir string `json:"artifactDir"`
}

type certificateDeploymentEntry struct {
	CertificateID   string `json:"certificateId"`
	CertificatePath string `json:"certificatePath"`
	PrivateKeyPath  string `json:"privateKeyPath"`
}

type gatewayDeploymentDocument struct {
	Version      int                          `json:"version"`
	Gateways     []gatewayDeploymentEntry     `json:"gateways"`
	Certificates []certificateDeploymentEntry `json:"certificates"`
}

// GatewayDeployment is the immutable startup view of operator-owned Agent-side
// nginx roots and certificate install paths. It contains no secret values.
type GatewayDeployment struct {
	Gateways     map[string]GatewayInstallPaths
	Certificates map[string]CertificateInstallPaths
}

// LoadGatewayDeployment reads one private, owner-checked startup document. It is
// never reloaded from an HTTP route and never grants a task or permission.
func LoadGatewayDeployment(filename string) (*GatewayDeployment, error) {
	if !filepath.IsAbs(filename) {
		return nil, ErrGatewayDeployment
	}
	directory := filepath.Dir(filename)
	before, err := os.Lstat(directory)
	if err != nil || !gatewayDeploymentPrivate(before, true) {
		return nil, ErrGatewayDeployment
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, ErrGatewayDeployment
	}
	defer root.Close()
	anchored, err := root.Stat(".")
	if err != nil || !os.SameFile(before, anchored) {
		return nil, ErrGatewayDeployment
	}
	name := filepath.Base(filename)
	info, err := root.Lstat(name)
	if err != nil || !gatewayDeploymentPrivate(info, false) || info.Size() <= 0 || info.Size() > 128<<10 {
		return nil, ErrGatewayDeployment
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, ErrGatewayDeployment
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) || !gatewayDeploymentPrivate(opened, false) {
		return nil, ErrGatewayDeployment
	}
	raw, err := io.ReadAll(io.LimitReader(file, (128<<10)+1))
	if err != nil || len(raw) > 128<<10 {
		return nil, ErrGatewayDeployment
	}
	return parseGatewayDeployment(raw)
}

func parseGatewayDeployment(raw []byte) (*GatewayDeployment, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if gatewayDeploymentUniqueJSON(decoder) != nil {
		return nil, ErrGatewayDeployment
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, ErrGatewayDeployment
	}
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var document gatewayDeploymentDocument
	if decoder.Decode(&document) != nil || document.Version != 1 ||
		len(document.Gateways) == 0 || len(document.Gateways) > 1000 ||
		len(document.Certificates) > 1000 {
		return nil, ErrGatewayDeployment
	}
	gateways := make(map[string]GatewayInstallPaths, len(document.Gateways))
	for _, entry := range document.Gateways {
		if entry.GatewayID == "" || len(entry.GatewayID) > 128 || entry.GatewayID != strings.TrimSpace(entry.GatewayID) {
			return nil, ErrGatewayDeployment
		}
		if _, exists := gateways[entry.GatewayID]; exists {
			return nil, ErrGatewayDeployment
		}
		if !validGatewayDeploymentPath(entry.SiteDir) || !validGatewayDeploymentPath(entry.ArtifactDir) {
			return nil, ErrGatewayDeployment
		}
		gateways[entry.GatewayID] = GatewayInstallPaths{SiteDir: entry.SiteDir, ArtifactDir: entry.ArtifactDir}
	}
	certificates := make(map[string]CertificateInstallPaths, len(document.Certificates))
	for _, entry := range document.Certificates {
		if entry.CertificateID == "" || len(entry.CertificateID) > 128 || entry.CertificateID != strings.TrimSpace(entry.CertificateID) {
			return nil, ErrGatewayDeployment
		}
		if _, exists := certificates[entry.CertificateID]; exists {
			return nil, ErrGatewayDeployment
		}
		if !validGatewayDeploymentPath(entry.CertificatePath) || !validGatewayDeploymentPath(entry.PrivateKeyPath) {
			return nil, ErrGatewayDeployment
		}
		certificates[entry.CertificateID] = CertificateInstallPaths{
			CertificatePath: entry.CertificatePath, PrivateKeyPath: entry.PrivateKeyPath,
		}
	}
	return &GatewayDeployment{Gateways: gateways, Certificates: certificates}, nil
}

func validGatewayDeploymentPath(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path &&
		len(path) <= 1024 && !strings.Contains(path, "..") &&
		!strings.ContainsAny(path, "\\\x00") && strings.TrimSpace(path) == path
}

func gatewayDeploymentUniqueJSON(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	if delim != '{' && delim != '[' {
		return ErrGatewayDeployment
	}
	seen := map[string]bool{}
	for decoder.More() {
		if delim == '{' {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return ErrGatewayDeployment
			}
			seen[name] = true
		}
		if err := gatewayDeploymentUniqueJSON(decoder); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}
