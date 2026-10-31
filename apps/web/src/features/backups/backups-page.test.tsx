import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { jsonResponse, type FetchRoutes } from "@/test/http";
import { renderAuthenticated } from "@/test/app";

afterEach(() => {
  vi.unstubAllGlobals();
  window.location.hash = "";
});

function plan(fenced: boolean) {
  return {
    backupId: "bk-1",
    targetControllerId: "ctl-2",
    oldControllerFenced: fenced,
    expectedVersion: "0.9.0",
    expectedNodeIds: ["n-1", "n-2"],
    recoveryKeyReference: "key-ref-1",
    checks: [{ name: "校验和", passed: true }],
  };
}

function recoveryRoutes(fenced: boolean, previewChecks: { name: string; passed: boolean; reason?: string }[]): FetchRoutes {
  return {
    "POST /api/v1/backups/bk-1/verify": () => jsonResponse(plan(fenced), 202),
    "POST /api/v1/disaster-recovery/preview": () => jsonResponse({ ...plan(fenced), checks: previewChecks }),
    "POST /api/v1/disaster-recovery/jobs": () =>
      jsonResponse({ id: "dr-1", status: "running", createdAt: "2026-10-30T02:00:00Z", stages: [{ name: "fence", status: "succeeded" }], auditId: "aud-1" }, 202),
    "GET /api/v1/disaster-recovery/jobs/dr-1": () =>
      jsonResponse({ id: "dr-1", status: "succeeded", createdAt: "2026-10-30T02:00:00Z", stages: [{ name: "restore", status: "succeeded" }], auditId: "aud-1" }),
  };
}

async function verifyBackup(calls: { method: string; url: string; init?: RequestInit }[]) {
  fireEvent.change(await screen.findByLabelText("备份 ID"), { target: { value: "bk-1" } });
  fireEvent.click(screen.getByRole("button", { name: "校验备份" }));
  await waitFor(() => {
    const call = calls.find(call => call.method === "POST" && call.url === "/api/v1/backups/bk-1/verify");
    expect(call).toBeDefined();
    expect(call?.init?.body).toBeUndefined();
  });
  expect(await screen.findByText("key-ref-1")).toBeVisible();
}

describe("backup and disaster recovery", () => {
  it("states the missing backup list endpoint and never offers a fake backup", async () => {
    await renderAuthenticated("/backups", recoveryRoutes(false, []));
    expect(await screen.findByRole("heading", { level: 1, name: "备份与恢复" })).toBeVisible();
    expect(screen.getByText("契约缺口：没有备份记录列表/创建端点")).toBeVisible();
    expect(screen.queryByRole("button", { name: /立即备份|新建备份|创建备份/ })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "创建恢复作业" })).toBeDisabled();
  });

  it("blocks the recovery job when the preview reports a failing check", async () => {
    const { calls } = await renderAuthenticated("/backups", recoveryRoutes(false, [{ name: "旧主控隔离", passed: false, reason: "仍可写入" }]));
    await verifyBackup(calls);

    expect(screen.getByLabelText("目标主控 ID")).toHaveValue("ctl-2");
    fireEvent.click(screen.getByRole("button", { name: "执行预演" }));
    await waitFor(() => {
      const call = calls.find(call => call.method === "POST" && call.url === "/api/v1/disaster-recovery/preview");
      expect(call).toBeDefined();
      expect(JSON.parse(String(call?.init?.body))).toMatchObject({ backupId: "bk-1", targetControllerId: "ctl-2", oldControllerFenced: false });
    });
    expect(await screen.findByText("预演未通过")).toBeVisible();
    expect(screen.getByText("仍可写入")).toBeVisible();
    expect(screen.getByRole("button", { name: "创建恢复作业" })).toBeDisabled();
    expect(calls.some(call => call.url === "/api/v1/disaster-recovery/jobs")).toBe(false);
  });

  it("creates and refreshes a recovery job only after fencing and a passing preview", async () => {
    const { calls } = await renderAuthenticated("/backups", recoveryRoutes(true, [{ name: "旧主控隔离", passed: true }]));
    await verifyBackup(calls);

    expect(screen.getByLabelText(/旧主控已隔离/)).toBeChecked();
    fireEvent.click(screen.getByRole("button", { name: "执行预演" }));
    expect(await screen.findByText(/预演返回/)).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "创建恢复作业" }));
    await waitFor(() => {
      const call = calls.find(call => call.method === "POST" && call.url === "/api/v1/disaster-recovery/jobs");
      expect(call).toBeDefined();
      expect(JSON.parse(String(call?.init?.body))).toEqual({
        backupId: "bk-1",
        targetControllerId: "ctl-2",
        oldControllerFenced: true,
        expectedVersion: "0.9.0",
        expectedNodeIds: ["n-1", "n-2"],
        recoveryKeyReference: "key-ref-1",
        checks: [{ name: "校验和", passed: true }],
      });
    });
    expect(await screen.findByText(/恢复作业已创建/)).toBeVisible();
    expect(screen.getByText("dr-1")).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "刷新作业" }));
    await waitFor(() => {
      const call = calls.find(call => call.method === "GET" && call.url === "/api/v1/disaster-recovery/jobs/dr-1");
      expect(call).toBeDefined();
    });
    expect(await screen.findByText(/状态 成功/)).toBeVisible();
  });
});
