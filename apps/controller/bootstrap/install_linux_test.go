//go:build linux

package bootstrap

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"serverfleet.local/serverfleet/internal/updates"
	"strings"
	"testing"
	"time"
)

func installationFixture(t *testing.T) (installationManifest, map[string][]byte) {
	t.Helper()
	files := map[string][]byte{"controller": []byte("#!/bin/sh\nexit 99\n"), "agent": []byte("#!/bin/sh\nexit 98\n"), "web/index.html": []byte("<html>signed console</html>")}
	m := installationManifest{Format: 1, Version: "1.2.3", Sequence: 5, OS: "linux", Architecture: runtime.GOARCH}
	for _, component := range []string{"controller", "agent"} {
		raw, _ := json.Marshal(updates.ReleaseManifest{Format: 1, Component: component, Version: m.Version, Sequence: m.Sequence, OS: m.OS, Architecture: m.Architecture, SHA256: recoveryDigest(files[component]), Bytes: int64(len(files[component]))})
		files[component+".manifest.json"] = raw
	}
	for _, name := range []string{"controller", "agent", "controller.manifest.json", "agent.manifest.json", "web/index.html"} {
		b := files[name]
		m.Files = append(m.Files, installationFile{Path: name, SHA256: recoveryDigest(b), Bytes: int64(len(b)), Executable: name == "controller" || name == "agent"})
	}
	return m, files
}

func TestInstallationExplicitComponentBindingBeforeChanges(t *testing.T) {
	base := installationPrivateTemp(t)
	cfg := installationDocument{Format: 1, Directory: filepath.Join(base, "new-install"),
		Source:      installationSource{Origin: "https://releases.test", Addresses: []string{"192.0.2.1"}, Version: "0.9.2", KeyID: "test", PublicKey: base64.StdEncoding.EncodeToString(make([]byte, 32)), MinimumSequence: 3},
		ServiceMode: "external", WaitSeconds: 10, Provision: provisionDocument{Component: "controller", RootPin: strings.Repeat("a", 64)}}
	raw, _ := json.Marshal(cfg)
	file := filepath.Join(base, "install.json")
	if err := os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	for _, component := range []string{"agent", "other"} {
		if err := RunInstallCommand(context.Background(), []string{"-config", file, "-component", component, "-check"}, io.Discard, io.Discard); err == nil {
			t.Fatal("wrong component accepted")
		}
		if _, err := os.Stat(cfg.Directory); !os.IsNotExist(err) {
			t.Fatal("component mismatch changed runtime")
		}
	}
	if os.Geteuid() != 0 {
		if err := RunInstallCommand(context.Background(), []string{"-config", file, "-component", "controller", "-check"}, io.Discard, io.Discard); err != nil {
			t.Fatal(err)
		}
	}
}
func TestInstallationRejectsUntrustedBundlesBeforeExecution(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires ordinary user")
	}
	for _, which := range []string{"signature", "hash", "traversal", "platform", "sequence", "component"} {
		t.Run(which, func(t *testing.T) {
			m, files := installationFixture(t)
			marker := filepath.Join(installationPrivateTemp(t), "executed")
			for _, component := range []string{"controller", "agent"} {
				files[component] = []byte("#!/bin/sh\nprintf x > '" + marker + "'\nexit 98\n")
				files[component+".manifest.json"], _ = json.Marshal(updates.ReleaseManifest{Format: 1, Component: component, Version: m.Version, Sequence: m.Sequence, OS: m.OS, Architecture: m.Architecture, SHA256: recoveryDigest(files[component]), Bytes: int64(len(files[component]))})
			}
			for i := range m.Files {
				m.Files[i].SHA256 = recoveryDigest(files[m.Files[i].Path])
				m.Files[i].Bytes = int64(len(files[m.Files[i].Path]))
			}
			pub, key, _ := ed25519.GenerateKey(rand.Reader)
			switch which {
			case "traversal":
				m.Files[4].Path = "../escape"
			case "platform":
				m.Architecture = "other"
			case "sequence":
				m.Sequence = 1
			case "component":
				files["agent.manifest.json"] = bytes.ReplaceAll(files["agent.manifest.json"], []byte("1.2.3"), []byte("1.2.2"))
				m.Files[3].SHA256 = recoveryDigest(files["agent.manifest.json"])
			}
			raw, _ := json.Marshal(m)
			sig := ed25519.Sign(key, append([]byte(installationSignatureDomain), raw...))
			if which == "signature" {
				sig[0] ^= 1
			}
			if which == "hash" {
				files["agent"] = []byte("wrong executable")
			}
			hits := 0
			ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits++
				name := strings.TrimPrefix(r.URL.Path, "/1.2.3/"+runtime.GOARCH+"/")
				switch name {
				case "installation.manifest.json":
					_, _ = w.Write(raw)
				case "installation.signature":
					_, _ = w.Write(sig)
				default:
					if b, ok := files[name]; ok {
						_, _ = w.Write(b)
					} else {
						http.NotFound(w, r)
					}
				}
			}))
			defer ts.Close()
			ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ts.Certificate().Raw})
			cfg := installationDocument{Format: 1, Directory: filepath.Join(installationPrivateTemp(t), "new-install"), Source: installationSource{Origin: ts.URL, Addresses: []string{ts.Listener.Addr().(*net.TCPAddr).IP.String()}, RootPEM: string(ca), Version: "1.2.3", KeyID: "release", PublicKey: base64.StdEncoding.EncodeToString(pub), MinimumSequence: 5}, ServiceMode: "external", WaitSeconds: 10, Provision: provisionDocument{Component: "controller", RootPin: strings.Repeat("a", 64)}}
			config := filepath.Join(installationPrivateTemp(t), "install.json")
			b, _ := json.Marshal(cfg)
			if e := os.WriteFile(config, b, 0600); e != nil {
				t.Fatal(e)
			}
			var out bytes.Buffer
			e := RunInstallCommand(context.Background(), []string{"-config", config}, &out, io.Discard)
			if e == nil {
				t.Fatal("untrusted bundle accepted")
			}
			if _, err := os.Lstat(marker); !os.IsNotExist(err) {
				t.Fatal("downloaded executable ran before verification", err)
			}
			if hits == 0 {
				t.Fatal("did not reach pinned HTTPS verification", e)
			}
			if _, e = os.Lstat(filepath.Join(cfg.Directory, "runtime")); !os.IsNotExist(e) {
				t.Fatal("executed/provisioned before full bundle verification", e)
			}
		})
	}
}
func TestInstallationOfflinePackageBindsConsoleAndComponentManifests(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires ordinary user")
	}
	m, files := installationFixture(t)
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	source := installationPrivateTemp(t)
	os.Chmod(source, 0700)
	for _, f := range m.Files {
		p := filepath.Join(source, f.Path)
		os.MkdirAll(filepath.Dir(p), 0700)
		mode := os.FileMode(0600)
		if f.Executable {
			mode = 0700
		}
		if e := os.WriteFile(p, files[f.Path], mode); e != nil {
			t.Fatal(e)
		}
	}
	raw, _ := json.Marshal(m)
	os.WriteFile(filepath.Join(source, "installation.manifest.json"), raw, 0600)
	keyFile := filepath.Join(installationPrivateTemp(t), "signing.key")
	os.WriteFile(keyFile, []byte(base64.StdEncoding.EncodeToString(key)), 0600)
	version := filepath.Join(installationPrivateTemp(t), m.Version)
	os.Mkdir(version, 0700)
	output := filepath.Join(version, m.Architecture)
	var receipt bytes.Buffer
	if e := packageInstallation(context.Background(), source, output, keyFile, "release", &receipt); e != nil {
		t.Fatal(e)
	}
	sig, e := os.ReadFile(filepath.Join(output, "installation.signature"))
	if e != nil || !ed25519.Verify(pub, append([]byte(installationSignatureDomain), raw...), sig) {
		t.Fatal("signature mismatch", e)
	}
	for _, f := range m.Files {
		if e := installationVerifyFile(filepath.Join(output, f.Path), f); e != nil {
			t.Fatal(e)
		}
	}
	if bytes.Contains(receipt.Bytes(), []byte(base64.StdEncoding.EncodeToString(key))) {
		t.Fatal("signing key disclosure")
	}
	files["agent.manifest.json"] = bytes.ReplaceAll(files["agent.manifest.json"], []byte("1.2.3"), []byte("1.2.2"))
	os.WriteFile(filepath.Join(output, "agent.manifest.json"), files["agent.manifest.json"], 0600)
	if validateInstallationComponents(output, m) == nil {
		t.Fatal("mixed component releases accepted")
	}
}
func TestInstallationFileAndLockRejectUnsafeReuse(t *testing.T) {
	dir := installationPrivateTemp(t)
	os.Chmod(dir, 0700)
	root, e := os.OpenRoot(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	release, e := installationLock(root)
	if e != nil {
		t.Fatal(e)
	}
	defer release()
	if _, e = installationLock(root); e == nil {
		t.Fatal("concurrent install allowed")
	}
	target := filepath.Join(dir, "target")
	os.WriteFile(target, []byte("safe"), 0600)
	os.Symlink("target", filepath.Join(dir, "alias"))
	if installationEnsureFile(root, "alias", []byte("safe")) == nil {
		t.Fatal("symlink accepted")
	}
	if installationEnsureFile(root, "target", []byte("other")) == nil {
		t.Fatal("existing file overwritten")
	}
}

func installationPrivateTemp(t *testing.T) string {
	t.Helper()
	p := t.TempDir()
	if e := os.Chmod(p, 0700); e != nil {
		t.Fatal(e)
	}
	return p
}

func TestInstallationAgentFileProfileOptOut(t *testing.T) {
	dir := installationPrivateTemp(t)
	root, e := os.OpenRoot(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	defaulted := &provisionAgent{}
	if e := defaultAgentFileProfile(root, defaulted, dir); e != nil || defaulted.FileRoot != filepath.Join(dir, "files") {
		t.Fatalf("default profile = %q, %v", defaulted.FileRoot, e)
	}
	if !installationDirExists(root, "files") {
		t.Fatal("default files dir not private 0700")
	}
	optOut := &provisionAgent{DisableFiles: true}
	if e := defaultAgentFileProfile(root, optOut, dir); e != nil || optOut.FileRoot != "" {
		t.Fatalf("opt-out profile = %q, %v", optOut.FileRoot, e)
	}
	for _, bad := range []*provisionAgent{
		{DisableFiles: true, FileRoot: filepath.Join(dir, "custom")},
		{DisableFiles: true, GatewayPrefix: filepath.Join(dir, "gw")},
	} {
		if e := defaultAgentFileProfile(root, bad, dir); e == nil {
			t.Fatalf("contradictory opt-out accepted: %+v", bad)
		}
	}
	explicit := &provisionAgent{FileRoot: filepath.Join(dir, "custom")}
	if e := defaultAgentFileProfile(root, explicit, dir); e != nil || explicit.FileRoot != filepath.Join(dir, "custom") {
		t.Fatalf("explicit root = %q, %v", explicit.FileRoot, e)
	}
}

func provisionAgentFixture(t *testing.T) string {
	t.Helper()
	base := installationPrivateTemp(t)
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	der, e := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	caPath := filepath.Join(base, "ca.pem")
	if e = os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); e != nil {
		t.Fatal(e)
	}
	program := []byte("#!/bin/sh\nexit 0\n")
	artPath, supPath := filepath.Join(base, "agent"), filepath.Join(base, "supervisor")
	for _, p := range []string{artPath, supPath} {
		if e = os.WriteFile(p, program, 0700); e != nil {
			t.Fatal(e)
		}
	}
	manifest, _ := json.Marshal(updates.ReleaseManifest{Format: 1, Component: "agent", OS: "linux", Architecture: runtime.GOARCH, Version: "1.2.3", Sequence: 5, SHA256: recoveryDigest(program), Bytes: int64(len(program))})
	manPath := filepath.Join(base, "agent.manifest.json")
	if e = os.WriteFile(manPath, manifest, 0600); e != nil {
		t.Fatal(e)
	}
	pub, _, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	doc := provisionDocument{Format: 1, Component: "agent", Directory: filepath.Join(base, "runtime"), Artifact: artPath, Manifest: manPath, SupervisorExecutable: supPath, RootCertificate: caPath, RootPin: strings.Repeat("a", 64), ControllerOrigin: "https://releases.test", ControllerAddresses: []string{"192.0.2.1"}, ReleaseKeys: map[string]string{"release": base64.StdEncoding.EncodeToString(pub)}, Agent: &provisionAgent{NodeID: "node-a", IdentityDir: filepath.Join(base, "identity"), DisableFiles: true, FileRoot: filepath.Join(base, "files")}}
	raw, _ := json.Marshal(doc)
	cfgPath := filepath.Join(base, "provision.json")
	if e = os.WriteFile(cfgPath, raw, 0600); e != nil {
		t.Fatal(e)
	}
	return cfgPath
}

func TestProvisionRejectsDisableFilesWithFileRoot(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires ordinary user")
	}
	cfg := provisionAgentFixture(t)
	if e := RunProvisionCommand(context.Background(), []string{"-config", cfg}, io.Discard, io.Discard); e == nil {
		t.Fatal("disableFiles with explicit fileRoot accepted")
	}
}
