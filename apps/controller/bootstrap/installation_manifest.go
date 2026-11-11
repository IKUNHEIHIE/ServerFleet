package bootstrap

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"io"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"serverfleet.local/serverfleet/internal/recovery"
	"serverfleet.local/serverfleet/internal/transport"
	"serverfleet.local/serverfleet/internal/updates"
)

type installationFile struct {
	Path       string `json:"path"`
	SHA256     string `json:"sha256"`
	Bytes      int64  `json:"bytes"`
	Executable bool   `json:"executable"`
}
type installationManifest struct {
	Format       int                `json:"format"`
	Version      string             `json:"version"`
	Sequence     uint64             `json:"sequence"`
	OS           string             `json:"os"`
	Architecture string             `json:"architecture"`
	Files        []installationFile `json:"files"`
}

var installationVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

const installationSignatureDomain = "serverfleet.installation.v1\x00"

func validInstallationManifest(m installationManifest, architecture string) bool {
	if architecture != "amd64" && architecture != "arm64" {
		return false
	}
	if m.Format != 1 || m.OS != "linux" || m.Architecture != architecture || !installationVersionPattern.MatchString(m.Version) || len(m.Version) > 64 || m.Sequence == 0 || m.Sequence > 1<<53-1 || len(m.Files) < 5 || len(m.Files) > 1024 {
		return false
	}
	seen := map[string]bool{}
	var total int64
	for _, f := range m.Files {
		if seen[f.Path] || !transport.FileDigest(f.SHA256) || f.Bytes < 1 || f.Bytes > 256<<20 || path.Clean(f.Path) != f.Path || strings.Contains(f.Path, "\\") || strings.HasPrefix(f.Path, "/") {
			return false
		}
		seen[f.Path] = true
		total += f.Bytes
		switch f.Path {
		case "controller", "agent":
			if !f.Executable {
				return false
			}
		case "controller.manifest.json", "agent.manifest.json":
			if f.Executable || f.Bytes > 16384 {
				return false
			}
		default:
			if f.Executable || f.Bytes > 8<<20 || !(f.Path == "web/index.html" || f.Path == "web/favicon.ico" || strings.HasPrefix(f.Path, "web/assets/")) || !regexp.MustCompile(`^[A-Za-z0-9_.\-/]+$`).MatchString(f.Path) || strings.Count(f.Path, "/") > 2 || strings.HasPrefix(path.Base(f.Path), ".") {
				return false
			}
		}
	}
	return total <= 600<<20 && seen["controller"] && seen["agent"] && seen["controller.manifest.json"] && seen["agent.manifest.json"] && seen["web/index.html"]
}

// Offline packaging signs the entire installation, including console bytes.
// It never downloads, starts services, exposes a private key or signs a mixed build.
func packageInstallation(ctx context.Context, source, output, keyFile, keyID string, stdout io.Writer) error {
	raw, e := recovery.ReadPrivateFile(filepath.Join(source, "installation.manifest.json"), 256<<10)
	if e != nil {
		return errProvision
	}
	var m installationManifest
	if strictProvisionJSON(raw, &m) != nil || !validInstallationManifest(m, runtime.GOARCH) && !validInstallationManifest(m, "arm64") && !validInstallationManifest(m, "amd64") || filepath.Base(output) != m.Architecture || filepath.Base(filepath.Dir(output)) != m.Version {
		return errProvision
	}
	encoded, e := recovery.ReadPrivateFile(keyFile, 4096)
	if e != nil {
		return errProvision
	}
	defer clear(encoded)
	key, e := base64.StdEncoding.Strict().DecodeString(strings.TrimSpace(string(encoded)))
	if e != nil || len(key) != ed25519.PrivateKeySize {
		return errProvision
	}
	defer clear(key)
	canonical := ed25519.NewKeyFromSeed(key[:ed25519.SeedSize])
	defer clear(canonical)
	if !ed25519.PublicKey(key[32:]).Equal(canonical.Public()) {
		return errProvision
	}
	root, e := newProvisionDirectory(output)
	if e != nil {
		return e
	}
	defer root.Close()
	for _, f := range m.Files {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if strings.Contains(f.Path, "/") {
			dir := path.Dir(f.Path)
			if dir == "web/assets" {
				if e = root.Mkdir("web", 0700); e != nil && !installationDirExists(root, "web") {
					return e
				}
			}
			if e = root.Mkdir(dir, 0700); e != nil && !installationDirExists(root, dir) {
				return e
			}
		}
		if f.Executable {
			src, e := openProvisionProgram(filepath.Join(source, f.Path))
			if e != nil {
				return e
			}
			e = copyProvisionProgram(root, f.Path, src, f.SHA256, f.Bytes)
			src.Close()
			if e != nil {
				return e
			}
		} else {
			b, e := recovery.ReadPrivateFile(filepath.Join(source, f.Path), f.Bytes)
			if e != nil || int64(len(b)) != f.Bytes || recoveryDigest(b) != f.SHA256 {
				return errProvision
			}
			if e = provisionWrite(root, f.Path, b, 0600); e != nil {
				return e
			}
		}
	}
	if e = validateInstallationComponents(output, m); e != nil {
		return e
	}
	signature := ed25519.Sign(canonical, append([]byte(installationSignatureDomain), raw...))
	if e = provisionWrite(root, "installation.signature", signature, 0600); e != nil {
		return e
	}
	if e = provisionWrite(root, "installation.manifest.json", raw, 0600); e != nil {
		return e
	}
	return json.NewEncoder(stdout).Encode(map[string]any{"state": "SIGNED_INSTALLATION_NOT_DEPLOYED", "version": m.Version, "architecture": m.Architecture, "keyId": keyID, "directory": output})
}

func validateInstallationComponents(directory string, m installationManifest) error {
	// The per-component manifest and binary must agree with the signed bundle.
	for _, component := range []string{"controller", "agent"} {
		b, e := recovery.ReadPrivateFile(filepath.Join(directory, component+".manifest.json"), 16384)
		var release updates.ReleaseManifest
		if e != nil || strictProvisionJSON(b, &release) != nil || release.Format != 1 || release.OS != m.OS || release.Architecture != m.Architecture || release.Component != component || release.Version != m.Version || release.Sequence != m.Sequence {
			return errProvision
		}
		found := false
		for _, f := range m.Files {
			if f.Path == component && f.SHA256 == release.SHA256 && f.Bytes == release.Bytes {
				found = true
			}
		}
		if !found {
			return errProvision
		}
	}
	return nil
}
