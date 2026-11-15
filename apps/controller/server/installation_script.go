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
// The script creates a dedicated service account, installs a systemd unit with
// an explicit file root (file capability requires a non-root identity) and
// waits for administrator approval before starting the resident service.
func installationAgentScript(c *InstallationCatalog) (string, error) {
	if c == nil || !installationHTTPSURL(c.ControllerOrigin) {
		return "", errInstallationScript
	}
	const (
		user  = "serverfleet"
		base  = "/var/lib/serverfleet"
		state = base + "/agent"
		files = base + "/files"
		bin   = "/usr/local/bin/serverfleet-agent"
		run   = "/usr/local/lib/serverfleet/agent-run"
		unit  = "/etc/systemd/system/serverfleet-agent.service"
	)
	pin := shellQuote(c.RootPin)
	origin := shellQuote(c.ControllerOrigin)
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("# ServerFleet 节点安装器。以 root 运行：sh agent.sh <节点名称>\n")
	b.WriteString("# 创建专用用户与 systemd 服务，等待管理员接入审批后自动上线。\n")
	b.WriteString("set -eu\n")
	b.WriteString("name=${1:-}\n")
	b.WriteString("if [ \"$(id -u)\" != 0 ]; then echo \"必须以 root 运行\" >&2; exit 2; fi\n")
	b.WriteString("case \"$name\" in ''|*[[:cntrl:]]*) echo \"用法：sh agent.sh <节点名称>\" >&2; exit 2;; esac\n")
	b.WriteString("command -v useradd >/dev/null 2>&1 && command -v runuser >/dev/null 2>&1 && command -v systemctl >/dev/null 2>&1 || { echo \"需要 useradd、runuser 与 systemd\" >&2; exit 2; }\n")
	b.WriteString("user=" + user + "\nbase=" + base + "\nstate=" + state + "\nfiles=" + files + "\nbin=" + bin + "\nrun=" + run + "\nunit=" + unit + "\n")
	b.WriteString("getent passwd \"$user\" >/dev/null 2>&1 || useradd --system --home-dir \"$base\" --shell /usr/sbin/nologin \"$user\"\n")
	b.WriteString("install -d -m 0755 -o root -g root \"$(dirname \"$run\")\"\n")
	b.WriteString("install -d -m 0700 -o \"$user\" -g \"$user\" \"$base\" \"$state\" \"$files\"\n")
	b.WriteString("curl -fsSL --proto '=https' --tlsv1.3 " + shellQuote(installationAgentBinaryURL) + ` -o "$bin"` + "\n")
	b.WriteString("chmod 0755 \"$bin\"\n")
	b.WriteString(`printf '%s' ` + shellQuote(base64.StdEncoding.EncodeToString([]byte(c.IdentityRootPEM))) + ` | base64 -d > "$state/root.pem"` + "\n")
	b.WriteString(`printf '%s' ` + shellQuote(base64.StdEncoding.EncodeToString([]byte(c.HTTPSRootPEM))) + ` | base64 -d > "$state/controller-ca.pem"` + "\n")
	b.WriteString("chown \"$user:$user\" \"$state/root.pem\" \"$state/controller-ca.pem\"\n")
	b.WriteString("chmod 0600 \"$state/root.pem\" \"$state/controller-ca.pem\"\n")
	common := " -controller-url " + origin + ` -root-cert "$state/root.pem" -controller-ca "$state/controller-ca.pem" -root-pin ` + pin + ` -node-name "$name" -state-dir "$state"`
	// The runner keeps every value shell-quoted; systemd executes it directly.
	b.WriteString("cat > \"$run.tmp\" <<'SF_RUNNER'\n")
	b.WriteString("#!/bin/sh\n")
	b.WriteString("exec " + shellQuote(bin) + " -serve" + common + " -file-root " + shellQuote(files) + "\n")
	b.WriteString("SF_RUNNER\n")
	b.WriteString("chmod 0755 \"$run.tmp\" && mv \"$run.tmp\" \"$run\"\n")
	b.WriteString("cat > \"$unit\" <<'SF_UNIT'\n")
	b.WriteString("[Unit]\nDescription=ServerFleet Agent\nAfter=network-online.target\nWants=network-online.target\n\n")
	b.WriteString("[Service]\nType=simple\nUser=serverfleet\nGroup=serverfleet\nExecStart=" + run + "\nRestart=always\nRestartSec=5\n\n")
	b.WriteString("[Install]\nWantedBy=multi-user.target\nSF_UNIT\n")
	b.WriteString("run_as() { runuser -u \"$user\" -- \"$@\"; }\n")
	b.WriteString("until run_as \"$bin\" -status -state-dir \"$state\" -root-pin " + pin + ` 2>/dev/null | grep -q '"enrolled":true'; do` + "\n")
	b.WriteString("  echo \"等待管理员审批接入...\"\n")
	b.WriteString("  run_as \"$bin\" -enroll" + common + " || true\n")
	b.WriteString("  sleep 5\n")
	b.WriteString("done\n")
	b.WriteString("systemctl daemon-reload\n")
	b.WriteString("systemctl enable serverfleet-agent >/dev/null 2>&1 || true\n")
	b.WriteString("systemctl restart serverfleet-agent\n")
	b.WriteString("echo \"已批准，常驻服务已启动（systemctl status serverfleet-agent）。\"\n")
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
