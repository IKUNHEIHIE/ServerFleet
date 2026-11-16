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
	b.WriteString("# ServerFleet 节点安装器。以 root 运行：sh agent.sh <节点名称> [--system-files]\n")
	b.WriteString("# 默认：专用用户 + 文件能力限制在 /var/lib/serverfleet/files。\n")
	b.WriteString("# --system-files：显式选择整机文件浏览（服务以 root 运行，读写整机文件）。\n")
	b.WriteString("set -eu\n")
	b.WriteString("name=${1:-}\n")
	b.WriteString("mode=${2:-}\n")
	b.WriteString("case \"$name\" in ''|*[[:cntrl:]]*) echo \"用法：sh agent.sh <节点名称> [--system-files]\" >&2; exit 2;; esac\n")
	b.WriteString("case \"$mode\" in ''|--system-files) ;; *) echo \"仅支持 --system-files\" >&2; exit 2;; esac\n")
	b.WriteString("if [ \"$(id -u)\" != 0 ]; then echo \"必须以 root 运行\" >&2; exit 2; fi\n")
	b.WriteString("command -v useradd >/dev/null 2>&1 && command -v systemctl >/dev/null 2>&1 || { echo \"需要 useradd 与 systemd\" >&2; exit 2; }\n")
	b.WriteString("if [ \"$mode\" = \"--system-files\" ]; then svcuser=root; else svcuser=" + user + "; command -v runuser >/dev/null 2>&1 || { echo \"需要 runuser\" >&2; exit 2; }; fi\n")
	b.WriteString("base=" + base + "\nstate=" + state + "\nfiles=" + files + "\nbin=" + bin + "\nrun=" + run + "\nunit=" + unit + "\n")
	b.WriteString("if [ \"$svcuser\" != root ]; then getent passwd \"$svcuser\" >/dev/null 2>&1 || useradd --system --home-dir \"$base\" --shell /usr/sbin/nologin \"$svcuser\"; fi\n")
	b.WriteString("install -d -m 0755 -o root -g root \"$(dirname \"$run\")\"\n")
	b.WriteString("install -d -m 0700 -o \"$svcuser\" -g \"$svcuser\" \"$base\" \"$state\"\n")
	b.WriteString("if [ \"$svcuser\" != root ]; then install -d -m 0700 -o \"$svcuser\" -g \"$svcuser\" \"$files\"; fi\n")
	b.WriteString("curl -fsSL --proto '=https' --tlsv1.3 " + shellQuote(installationAgentBinaryURL) + ` -o "$bin.tmp"` + "\n")
	// Rename instead of writing over the target: replacing a running
	// executable in place fails with ETXTBSY on every re-run.
	b.WriteString("install -m 0755 \"$bin.tmp\" \"$bin\"\n")
	b.WriteString("rm -f \"$bin.tmp\"\n")
	b.WriteString(`printf '%s' ` + shellQuote(base64.StdEncoding.EncodeToString([]byte(c.IdentityRootPEM))) + ` | base64 -d > "$state/root.pem"` + "\n")
	b.WriteString(`printf '%s' ` + shellQuote(base64.StdEncoding.EncodeToString([]byte(c.HTTPSRootPEM))) + ` | base64 -d > "$state/controller-ca.pem"` + "\n")
	b.WriteString("chown \"$svcuser:$svcuser\" \"$state/root.pem\" \"$state/controller-ca.pem\"\n")
	b.WriteString("chmod 0600 \"$state/root.pem\" \"$state/controller-ca.pem\"\n")
	b.WriteString("printf '%s' \"$name\" > \"$state/node-name\"\n")
	b.WriteString("chown \"$svcuser:$svcuser\" \"$state/node-name\" && chmod 0600 \"$state/node-name\"\n")
	common := " -controller-url " + origin + " -root-cert " + shellQuote(state+"/root.pem") + " -controller-ca " + shellQuote(state+"/controller-ca.pem") + " -root-pin " + pin + ` -node-name "$(cat ` + shellQuote(state+"/node-name") + `)" -state-dir ` + shellQuote(state)
	b.WriteString("if [ \"$mode\" = \"--system-files\" ]; then\n")
	b.WriteString("cat > \"$run.tmp\" <<'SF_RUNNER'\n")
	b.WriteString("#!/bin/sh\n")
	b.WriteString("exec " + shellQuote(bin) + " -serve" + common + " -file-system-root\n")
	b.WriteString("SF_RUNNER\n")
	b.WriteString("else\n")
	b.WriteString("cat > \"$run.tmp\" <<'SF_RUNNER'\n")
	b.WriteString("#!/bin/sh\n")
	b.WriteString("exec " + shellQuote(bin) + " -serve" + common + " -file-root " + shellQuote(files) + "\n")
	b.WriteString("SF_RUNNER\n")
	b.WriteString("fi\n")
	b.WriteString("chmod 0755 \"$run.tmp\" && mv \"$run.tmp\" \"$run\"\n")
	b.WriteString("if [ \"$mode\" = \"--system-files\" ]; then\n")
	b.WriteString("cat > \"$unit\" <<'SF_UNIT'\n")
	b.WriteString("[Unit]\nDescription=ServerFleet Agent (system files)\nAfter=network-online.target\nWants=network-online.target\n\n")
	b.WriteString("[Service]\nType=simple\nExecStart=" + run + "\nRestart=always\nRestartSec=5\n\n")
	b.WriteString("[Install]\nWantedBy=multi-user.target\nSF_UNIT\n")
	b.WriteString("else\n")
	b.WriteString("cat > \"$unit\" <<'SF_UNIT'\n")
	b.WriteString("[Unit]\nDescription=ServerFleet Agent\nAfter=network-online.target\nWants=network-online.target\n\n")
	b.WriteString("[Service]\nType=simple\nUser=" + user + "\nGroup=" + user + "\nExecStart=" + run + "\nRestart=always\nRestartSec=5\n\n")
	b.WriteString("[Install]\nWantedBy=multi-user.target\nSF_UNIT\n")
	b.WriteString("fi\n")
	b.WriteString("if [ \"$svcuser\" = root ]; then run_as() { \"$@\"; }; else run_as() { runuser -u \"$svcuser\" -- \"$@\"; }; fi\n")
	b.WriteString("until run_as \"$bin\" -status -state-dir \"$state\" -root-pin " + pin + ` 2>/dev/null | grep -q '"enrolled":true'; do` + "\n")
	b.WriteString("  echo \"等待管理员审批接入...\"\n")
	enroll := " -controller-url " + origin + ` -root-cert "$state/root.pem" -controller-ca "$state/controller-ca.pem" -root-pin ` + pin + ` -node-name "$name" -state-dir "$state"`
	b.WriteString("  run_as \"$bin\" -enroll" + enroll + " || true\n")
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
