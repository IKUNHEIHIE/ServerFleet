import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderAuthenticated } from "@/test/app";
import { jsonResponse } from "@/test/http";

afterEach(() => { vi.unstubAllGlobals(); window.location.hash = ""; });
it("saves a CAS policy with current credential metadata and requires a preview before cleanup", async () => {
  const policy = { revision: 1, ownerId: "u-admin", updatedAt: "2026-11-10T00:00:00Z", scheduledBackups: false, intervalHours: 24, autoUpload: false, scheduleCleanup: false, localDays: 7, localKeep: 3, remoteDays: 30, remoteKeep: 7, requireVerifiedRemote: true };
  const { calls } = await renderAuthenticated("/backups", {
    "GET /api/v1/system/recovery-authority": () => jsonResponse({ mode: "standard" }),
    "GET /api/v1/backups": () => jsonResponse({ items: [], recipientFingerprint: "public", recoveryKeyConfirmed: true, automaticBackupsConfigured: false }),
    "GET /api/v1/backups/maintenance": () => jsonResponse({ policy, remoteObjects: [], cleanupReceipts: [] }),
    "GET /api/v1/credentials": () => jsonResponse([{ id: "backup-credential", name: "Backup credential", kind: "backup_storage_secret", status: "valid", revision: 5, usageScope: ["backups"] }]),
    "PUT /api/v1/backups/maintenance/policy": () => jsonResponse({ ...policy, revision: 2 }),
  });
  expect(calls.some(c => c.url === "/api/v1/backups/maintenance")).toBe(false);
  fireEvent.click(await screen.findByRole("button", { name: "打开备份维护" }));
  fireEvent.click(await screen.findByLabelText("配置异地存储"));
  fireEvent.change(screen.getByLabelText("HTTPS 存储端点"), { target: { value: "https://backup.example.com" } });
  fireEvent.change(screen.getByLabelText("Bucket"), { target: { value: "fleet-backup" } });
  fireEvent.change(screen.getByLabelText("异地存储凭据"), { target: { value: "backup-credential" } });
  fireEvent.click(screen.getByLabelText("自动上传并读回校验"));
  fireEvent.click(screen.getByRole("button", { name: "保存备份策略" }));
  await waitFor(() => expect(calls.some(c => c.method === "PUT")).toBe(true));
  const body = JSON.parse(String(calls.find(c => c.method === "PUT")?.init?.body));
  expect(body.expectedRevision).toBe(1);
  expect(body.policy).toMatchObject({ autoUpload: true, destination: { credentialRef: "backup-credential", credentialRevision: 5, kind: "s3", bucket: "fleet-backup" } });
  expect(body.policy.ownerId).toBeUndefined(); expect(body.policy.revision).toBeUndefined(); expect(body.policy.updatedAt).toBeUndefined();
  expect(calls.some(c => c.method === "POST" && c.url.includes("/cleanup"))).toBe(false);
  expect(JSON.stringify(body)).not.toContain("secretAccessKey");
});
