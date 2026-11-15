import { fireEvent, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderAuthenticated } from "@/test/app";
import { jsonResponse } from "@/test/http";
import { installationCommand } from "./installation-panel";

afterEach(() => { vi.unstubAllGlobals(); window.location.hash = ""; });

const catalog = {
  source: { origin: "https://release.example", addresses: ["192.0.2.20"], rootPem: "public-release-root", version: "1.2.3", keyId: "release", publicKey: "public-key", minimumSequence: 5 },
  identityRootPem: "public-identity-root", httpsRootPem: "public-https-root", controllerOrigin: "https://fleet.example", controllerAddresses: ["192.0.2.10"], rootPin: "a".repeat(64),
  installerScript: { url: "https://release.example/install.sh", sha256: "b".repeat(64) },
  bootstrap: { amd64: { url: "https://release.example/amd64/controller", sha256: "c".repeat(64) }, arm64: { url: "https://release.example/arm64/controller", sha256: "d".repeat(64) } },
};

const AGENT_BINARY_URL = "https://raw.githubusercontent.com/IKUNHEIHIE/ServerFleet/main/releases/serverfleet-agent-linux-amd64";

describe("one-click installation", () => {
  it("builds a token-free command that downloads the prebuilt agent and enters approval", async () => {
    await renderAuthenticated("/nodes/new", { "GET /api/v1/installations/catalog": () => jsonResponse({ available: true, catalog }) });
    fireEvent.change(await screen.findByLabelText("节点名称"), { target: { value: "edge-hk-01" } });
    const command = (await screen.findByLabelText("一键安装命令") as HTMLTextAreaElement).value;
    expect(command).toContain(AGENT_BINARY_URL);
    expect(command).toContain("-enroll");
    expect(command).toContain("-serve");
    expect(command).toContain("-node-name 'edge-hk-01'");
    expect(command).toContain(`-root-pin '${"a".repeat(64)}'`);
    expect(command).toContain('"enrolled":true');
    expect(command).not.toContain("enroll-token");
    expect(command.startsWith("(")).toBe(true);
    expect(command.endsWith(")")).toBe(true);
    expect(screen.getByText("内部 Root CA 指纹：", { exact: false })).toHaveTextContent(catalog.rootPin);
    expect(screen.queryByLabelText("专用服务账户")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("安装节点名称")).not.toBeInTheDocument();
  });
  it("warns instead of inventing a command when trusted distribution is absent", async () => {
    await renderAuthenticated("/nodes/new", { "GET /api/v1/installations/catalog": () => jsonResponse({ available: false }) });
    expect(await screen.findByText("尚未配置可信安装分发")).toBeVisible();
    expect(screen.queryByLabelText("一键安装命令")).not.toBeInTheDocument();
  });
  it("quotes node names and keeps the whole command inside a subshell", () => {
    const cmd = installationCommand(catalog, "node ' quoted");
    expect(cmd.startsWith("(")).toBe(true);
    expect(cmd.endsWith(")")).toBe(true);
    expect(cmd).not.toContain("node ' quoted");
    expect(cmd).toContain(AGENT_BINARY_URL);
    expect(cmd).toContain("-enroll");
  });
});
