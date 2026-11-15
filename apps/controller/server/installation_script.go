package server

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
)

// The prebuilt agent published with each release. The generated installer
// embeds it so the target node never needs the distribution catalog.
const installationAgentBinaryURL = "https://raw.githubusercontent.com/IKUNHEIHIE/ServerFleet/main/releases/serverfleet-agent-linux-amd64"

var errInstallationScript = errors.New("installation script unavailable")

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func installationScriptURL(origin string) string {
	return strings.TrimSuffix(origin, "/") + "/install/agent.sh"
}

func installationScriptDigest(script string) string {
	sum := sha256.Sum256([]byte(script))
	return hex.EncodeToString(sum[:])
}

// setAgentScript advertises the console-facing download and its digest so the
// generated command can pin the exact bytes this Controller serves.
func (c *InstallationCatalog) setAgentScript() error {
	script, err := installationAgentScript(c)
	if err != nil {
		return err
	}
	c.AgentScript = InstallationDownload{URL: installationScriptURL(c.ControllerOrigin), SHA256: installationScriptDigest(script)}
	return nil
}

// installationAgentScript renders the installer served to a target node. The
// console only shows a short download-and-verify command; trust material stays
// in this script, so the command carries neither a token nor a long payload.
// The script is static per deployment: the node name arrives as the argument.
func installationAgentScript(c *InstallationCatalog) (string, error) {
	if c == nil || !installationHTTPSURL(c.ControllerOrigin) {
		return "", errInstallationScript
	}
	pin := shellQuote(c.RootPin)
	common := " -controller-url " + shellQuote(c.ControllerOrigin) + ` -root-cert "$dir/root.pem" -controller-ca "$dir/controller-ca.pem" -root-pin ` + pin + ` -node-name "$name" -state-dir "$dir"`
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("# Generated installer for one ServerFleet node. Run it as root: sh agent.sh <node-name>\n")
	b.WriteString("set -eu\n")
	b.WriteString("name=${1:-node}\n")
	b.WriteString("dir=/var/lib/serverfleet-agent\n")
	b.WriteString("bin=/usr/local/bin/serverfleet-agent\n")
	b.WriteString("curl -fsSL --proto '=https' --tlsv1.3 " + shellQuote(installationAgentBinaryURL) + ` -o "$bin"` + "\n")
	b.WriteString(`chmod +x "$bin"` + "\n")
	b.WriteString(`mkdir -p "$dir"` + "\n")
	b.WriteString(`printf '%s' ` + shellQuote(base64.StdEncoding.EncodeToString([]byte(c.IdentityRootPEM))) + ` | base64 -d > "$dir/root.pem"` + "\n")
	b.WriteString(`printf '%s' ` + shellQuote(base64.StdEncoding.EncodeToString([]byte(c.HTTPSRootPEM))) + ` | base64 -d > "$dir/controller-ca.pem"` + "\n")
	b.WriteString(`until "$bin" -status -state-dir "$dir" -root-pin ` + pin + ` 2>/dev/null | grep -q '"enrolled":true'; do` + "\n")
	b.WriteString(`  echo "等待管理员审批接入..."` + "\n")
	b.WriteString(`  "$bin" -enroll` + common + ` || true` + "\n")
	b.WriteString("  sleep 5\n")
	b.WriteString("done\n")
	b.WriteString(`echo "已批准，启动常驻服务..."` + "\n")
	b.WriteString(`exec "$bin" -serve` + common + "\n")
	return b.String(), nil
}

// The node has no credential yet, so the boundary script must be fetchable
// without a session. It contains only public trust material and the release
// URL; the enrollment token is never part of the script or the command.
func (s *Server) registerInstallationScriptRoutes(c *InstallationCatalog) {
	s.mux.HandleFunc("GET /install/agent.sh", func(w http.ResponseWriter, r *http.Request) {
		if c == nil || r.URL.RawQuery != "" || r.ContentLength != 0 {
			releaseSourceJSON(w, 404, map[string]string{"code": "INSTALLATION_UNAVAILABLE"})
			return
		}
		script, err := installationAgentScript(c)
		if err != nil {
			releaseSourceJSON(w, 404, map[string]string{"code": "INSTALLATION_UNAVAILABLE"})
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(script))
	})
}
