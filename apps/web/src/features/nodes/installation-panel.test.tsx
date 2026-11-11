import { fireEvent, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderAuthenticated } from "@/test/app";
import { jsonResponse } from "@/test/http";
import { installationCommand } from "./installation-panel";
afterEach(() => { vi.unstubAllGlobals(); window.location.hash = ""; });
const catalog = {
 source: { origin: "https://release.example", addresses: ["192.0.2.20"], rootPem: "public-release-root", version: "1.2.3", keyId: "release", publicKey: "public-key", minimumSequence: 5 },
 identityRootPem: "public-identity-root", httpsRootPem: "public-https-root", controllerOrigin: "https://fleet.example", controllerAddresses: ["192.0.2.10"], rootPin: "a".repeat(64),
 installerScript: { url: "https://release.example/install.sh", sha256: "b".repeat(64) }, bootstrap: { amd64: { url: "https://release.example/amd64/controller", sha256: "c".repeat(64) }, arm64: { url: "https://release.example/arm64/controller", sha256: "d".repeat(64) } },
};
describe("trusted installation", () => {
 it("binds a token-free public command and blocks invalid account input", async () => {
  const { calls } = await renderAuthenticated("/nodes/new", { "GET /api/v1/installations/catalog": () => jsonResponse({ available: true, catalog }) });
  expect(calls.some(c => c.url === "/api/v1/installations/catalog")).toBe(false);
  fireEvent.click(screen.getByRole("button", { name: "生成一键安装命令" }));
  const field = await screen.findByLabelText("一键安装命令"); const command = (field as HTMLTextAreaElement).value;
  expect(command).toContain("--system-user 'serverfleet'"); expect(command).toContain("--tlsv1.3"); expect(command.indexOf("sha256sum --check")).toBeLessThan(command.indexOf('sh "$_sf_install/install.sh"'));
  expect(command).not.toContain("enroll-token"); expect(command).not.toContain("tok-secret");
  const encoded = command.match(/--config-base64 '([^']+)'/)?.[1]; expect(encoded).toBeDefined(); const config = JSON.parse(atob(encoded!));
  expect(config.provision.agent.nodeId).toBe(""); expect(config.source.publicKey).toBe("public-key"); expect(config.enrollmentTokenFile).toBeUndefined();
  fireEvent.change(screen.getByLabelText("专用服务账户"), { target: { value: "000;whoami" } }); expect(screen.getByRole("button", { name: "复制一键安装命令" })).toBeDisabled();
 });
 it("does not invent a command when trusted distribution is absent", async () => {
  await renderAuthenticated("/nodes/new", { "GET /api/v1/installations/catalog": () => jsonResponse({ available: false }) });
  fireEvent.click(screen.getByRole("button", { name: "生成一键安装命令" }));
  expect(await screen.findByText("尚未配置可信安装分发")).toBeVisible(); expect(screen.queryByLabelText("一键安装命令")).not.toBeInTheDocument();
 });
 it("keeps shell exit inside a subshell and quotes names", () => {
  const cmd = installationCommand(catalog, "serverfleet", "node ' quoted"); expect(cmd.startsWith("(")).toBe(true); expect(cmd.endsWith(")")).toBe(true); expect(cmd).not.toContain("node ' quoted");
 });
});
