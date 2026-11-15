import { fireEvent, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderAuthenticated } from "@/test/app";
import { jsonResponse } from "@/test/http";
import { installationShortCommand } from "./installation-panel";

afterEach(() => { vi.unstubAllGlobals(); window.location.hash = ""; });

const catalog = {
  source: { origin: "https://release.example", addresses: ["192.0.2.20"], rootPem: "public-release-root", version: "1.2.3", keyId: "release", publicKey: "public-key", minimumSequence: 5 },
  identityRootPem: "public-identity-root", httpsRootPem: "public-https-root", controllerOrigin: "https://fleet.example", controllerAddresses: ["192.0.2.10"], rootPin: "a".repeat(64),
  installerScript: { url: "https://release.example/install.sh", sha256: "b".repeat(64) },
  bootstrap: { amd64: { url: "https://release.example/amd64/controller", sha256: "c".repeat(64) }, arm64: { url: "https://release.example/arm64/controller", sha256: "d".repeat(64) } },
  agentScript: { url: "https://fleet.example/install/agent.sh", sha256: "e".repeat(64) },
};

describe("one-click installation", () => {
  it("downloads the pinned installer script and keeps the token payload out of the command", async () => {
    await renderAuthenticated("/nodes/new", { "GET /api/v1/installations/catalog": () => jsonResponse({ available: true, catalog }) });
    fireEvent.change(await screen.findByLabelText("节点名称"), { target: { value: "edge-hk-01" } });
    const command = (await screen.findByLabelText("一键安装命令") as HTMLTextAreaElement).value;
    expect(command.startsWith("curl -fsSL --proto '=https' --tlsv1.3")).toBe(true);
    expect(command).toContain(`'${catalog.agentScript.url}'`);
    expect(command).toContain(`'${catalog.agentScript.sha256}'`);
    expect(command.indexOf("sha256sum --check --status")).toBeLessThan(command.indexOf("sh /tmp/sf-agent.sh"));
    expect(command).toContain("sh /tmp/sf-agent.sh 'edge-hk-01'");
    expect(command.length).toBeLessThan(400);
    expect(command).not.toContain("enroll-token");
    expect(command).not.toContain("base64");
    expect(screen.queryByLabelText("完整安装命令")).not.toBeInTheDocument();
    expect(screen.getByText("内部 Root CA 指纹：", { exact: false })).toHaveTextContent(catalog.rootPin);
    expect(screen.queryByLabelText("专用服务账户")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("安装节点名称")).not.toBeInTheDocument();
  });
  it("warns instead of inventing a command when trusted distribution is absent", async () => {
    await renderAuthenticated("/nodes/new", { "GET /api/v1/installations/catalog": () => jsonResponse({ available: false }) });
    expect(await screen.findByText("尚未配置可信安装分发")).toBeVisible();
    expect(screen.queryByLabelText("一键安装命令")).not.toBeInTheDocument();
  });
  it("verifies the downloaded script before running it with the quoted node name", () => {
    const cmd = installationShortCommand(catalog.agentScript, "node ' quoted");
    expect(cmd).toContain(`'${catalog.agentScript.sha256}'`);
    expect(cmd.indexOf("sha256sum --check --status")).toBeLessThan(cmd.indexOf("sh /tmp/sf-agent.sh"));
    expect(cmd.endsWith(`sh /tmp/sf-agent.sh 'node '"'"' quoted'`)).toBe(true);
    expect(cmd).not.toContain("enrollment");
  });
});