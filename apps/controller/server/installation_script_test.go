package server

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func installationScriptCatalog() *InstallationCatalog {
	return &InstallationCatalog{
		Format:              1,
		Source:              InstallationSource{Origin: "https://release.example", Addresses: []string{"192.0.2.20"}, RootPEM: "-----BEGIN CERTIFICATE-----\nRELEASE\n-----END CERTIFICATE-----\n", Version: "0.9.2", KeyID: "release", PublicKey: "cHVibGljLWtleQ==", MinimumSequence: 1},
		IdentityRootPEM:     "-----BEGIN CERTIFICATE-----\nIDENTITY\n-----END CERTIFICATE-----\n",
		HTTPSRootPEM:        "-----BEGIN CERTIFICATE-----\nHTTPS\n-----END CERTIFICATE-----\n",
		ControllerOrigin:    "https://fleet.example",
		ControllerAddresses: []string{"192.0.2.10"},
		RootPin:             strings.Repeat("a", 64),
		InstallerScript:     InstallationDownload{URL: "https://raw.githubusercontent.com/IKUNHEIHIE/ServerFleet/v0.9.2/deployment/raw-install.sh", SHA256: strings.Repeat("b", 64)},
		Bootstrap: map[string]InstallationDownload{
			"amd64": {URL: "https://release.example/amd64/controller", SHA256: strings.Repeat("c", 64)},
			"arm64": {URL: "https://release.example/arm64/controller", SHA256: strings.Repeat("d", 64)},
		},
	}
}

func TestInstallationAgentScriptKeepsTrustInScriptAndOutOfTheCommand(t *testing.T) {
	c := installationScriptCatalog()
	script, err := installationAgentScript(c)
	if err != nil {
		t.Fatal(err)
	}
	again, err := installationAgentScript(c)
	if err != nil || again != script {
		t.Fatalf("script generation must be deterministic: %v", err)
	}
	for _, want := range []string{
		installationAgentBinaryURL,
		"useradd --system",
		"User=serverfleet",
		"-file-root '/var/lib/serverfleet/files'",
		"runuser -u",
		"systemctl restart serverfleet-agent",
		"-enroll", "-serve",
		`-root-pin '` + c.RootPin + `'`,
		`'"enrolled":true'`,
		base64.StdEncoding.EncodeToString([]byte(c.IdentityRootPEM)),
		base64.StdEncoding.EncodeToString([]byte(c.HTTPSRootPEM)),
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q:\n%s", want, script)
		}
	}
	for _, forbidden := range []string{"enroll-token", "enrollmentToken", "--config-base64"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("script must not carry %q", forbidden)
		}
	}
	if len(script) > 6000 {
		t.Fatalf("script unexpectedly large: %d bytes", len(script))
	}
}

func TestInstallationScriptIsValidPOSIXShell(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh is not available")
	}
	script, err := installationAgentScript(installationScriptCatalog())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "agent.sh")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(shell, "-n", path).CombinedOutput(); err != nil {
		t.Fatalf("script has shell syntax errors: %v\n%s", err, out)
	}
}

func TestInstallationCatalogAdvertisesTheServedBytes(t *testing.T) {
	c := installationScriptCatalog()
	if err := c.setAgentScript(); err != nil {
		t.Fatal(err)
	}
	script, err := installationAgentScript(c)
	if err != nil {
		t.Fatal(err)
	}
	if c.AgentScript.URL != "https://fleet.example/install/agent.sh" {
		t.Fatalf("unexpected script url %q", c.AgentScript.URL)
	}
	if c.AgentScript.SHA256 != installationScriptDigest(script) || len(c.AgentScript.SHA256) != 64 {
		t.Fatalf("advertised digest does not match served bytes")
	}
}

func TestInstallationAgentScriptRouteServesScriptWithoutSession(t *testing.T) {
	c := installationScriptCatalog()
	s := &Server{mux: http.NewServeMux()}
	s.registerInstallationScriptRoutes(c)
	serve := func(target string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		s.mux.ServeHTTP(recorder, httptest.NewRequest("GET", target, nil))
		return recorder
	}
	expected, err := installationAgentScript(c)
	if err != nil {
		t.Fatal(err)
	}
	response := serve("/install/agent.sh")
	if response.Code != 200 || response.Body.String() != expected || response.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("unexpected script response: %d %q", response.Code, response.Header().Get("Content-Type"))
	}
	if code := serve("/install/agent.sh?name=node").Code; code != 404 {
		t.Fatalf("query parameters must not change the served script, got %d", code)
	}
	unavailable := &Server{mux: http.NewServeMux()}
	unavailable.registerInstallationScriptRoutes(nil)
	recorder := httptest.NewRecorder()
	unavailable.mux.ServeHTTP(recorder, httptest.NewRequest("GET", "/install/agent.sh", nil))
	if recorder.Code != 404 {
		t.Fatalf("expected 404 without catalog, got %d", recorder.Code)
	}
}
