import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { jsonResponse, type FetchRoutes } from "@/test/http";
import { renderAuthenticated } from "@/test/app";

afterEach(() => {
  vi.unstubAllGlobals();
  window.location.hash = "";
});

const CHECK = {
  currentVersion: "0.8.0",
  latestVersion: "0.9.0",
  channel: "stable",
  updateAvailable: true,
  releaseNotes: "修复会话恢复",
  publishedAt: "2026-10-20T00:00:00Z",
  signatureVerified: true,
  platformCompatible: true,
  checkedAt: "2026-10-30T01:00:00Z",
};

const PLAN = {
  planId: "pl-1",
  targetVersion: "0.9.0",
  eligible: true,
  migrationReversible: true,
  preflightChecks: [{ name: "磁盘空间", passed: true }, { name: "迁移兼容", passed: true }],
  estimatedImpact: "管理面重启约 30 秒",
  backupRequired: true,
};

const JOB_WITH_BACKUP = {
  jobId: "job-1",
  fromVersion: "0.7.0",
  toVersion: "0.8.0",
  state: "failed",
  phase: "health_check",
  errorCode: "HEALTH_FAILED",
  backupId: "bk-1",
  rollbackEligible: true,
  requestedBy: "u-admin",
  createdAt: "2026-10-29T01:00:00Z",
  updatedAt: "2026-10-29T01:05:00Z",
};

function updateRoutes(jobs: unknown[]): FetchRoutes {
  return {
    "POST /api/v1/system/updates/check": () => jsonResponse(CHECK),
    "POST /api/v1/system/updates/plan": () => jsonResponse(PLAN),
    "POST /api/v1/system/updates/apply": () =>
      jsonResponse({ ...JOB_WITH_BACKUP, jobId: "job-2", fromVersion: "0.8.0", toVersion: "0.9.0", state: "queued", errorCode: undefined }, 202),
    "GET /api/v1/system/updates/jobs": () => jsonResponse({ items: jobs }),
    "GET /api/v1/system/updates/jobs/job-1": () => jsonResponse({ ...JOB_WITH_BACKUP, healthChecks: [{ name: "控制面", passed: false }] }),
    "POST /api/v1/system/updates/jobs/job-1/rollback": () => jsonResponse({ ...JOB_WITH_BACKUP, state: "rolling_back" }, 202),
  };
}

async function planUpdate(calls: { method: string; url: string; init?: RequestInit }[]) {
  fireEvent.change(await screen.findByLabelText("发布通道"), { target: { value: "stable" } });
  fireEvent.click(screen.getByRole("button", { name: "检查更新" }));
  expect(await screen.findByText("有可用更新")).toBeVisible();
  await waitFor(() => expect(calls.some(call => call.method === "POST" && call.url === "/api/v1/system/updates/check")).toBe(true));

  fireEvent.change(await screen.findByLabelText("目标版本"), { target: { value: "0.9.0" } });
  fireEvent.click(screen.getByRole("button", { name: "生成计划" }));
  expect(await screen.findByText(/pl-1/)).toBeVisible();
  expect(screen.getByText("磁盘空间")).toBeVisible();
}

describe("update center", () => {
  it("warns that the server creates the backup instead of blocking the first upgrade", async () => {
    const { calls } = await renderAuthenticated("/updates", updateRoutes([]));
    expect(await screen.findByRole("heading", { level: 1, name: "更新中心" })).toBeVisible();
    await planUpdate(calls);

    // 备份由服务端在 backing_up 阶段创建；页面只提示，不得因“历史作业里还没有
    // backupId”把一键更新永久禁用（那会让第一次真实升级永远无法发起）。
    expect(screen.getByText("本次升级需要备份")).toBeVisible();
    expect(screen.queryByText("缺少可验证备份")).toBeNull();

    const apply = screen.getByRole("button", { name: "一键更新" });
    expect(apply).toBeDisabled();
    fireEvent.change(await screen.findByLabelText("确认文本"), { target: { value: "0.9.0" } });
    expect(apply).toBeDisabled();
    fireEvent.click(screen.getByLabelText(/我确认升级会重启主控/));
    expect(apply).toBeEnabled();
    expect(calls.some(call => call.url === "/api/v1/system/updates/apply")).toBe(false);
  });

  it("applies only after exact confirmation and rolls back with a reason", async () => {
    const { calls } = await renderAuthenticated("/updates", updateRoutes([JOB_WITH_BACKUP]));
    await planUpdate(calls);

    fireEvent.change(await screen.findByLabelText("确认文本"), { target: { value: "0.9" } });
    fireEvent.click(screen.getByLabelText(/我确认升级会重启主控/));
    expect(screen.getByRole("button", { name: "一键更新" })).toBeDisabled();

    fireEvent.change(screen.getByLabelText("确认文本"), { target: { value: "0.9.0" } });
    fireEvent.click(screen.getByRole("button", { name: "一键更新" }));
    await waitFor(() => {
      const call = calls.find(call => call.method === "POST" && call.url === "/api/v1/system/updates/apply");
      expect(call).toBeDefined();
      expect(JSON.parse(String(call?.init?.body))).toEqual({ planId: "pl-1", confirmation: "0.9.0", backupMode: "required" });
      expect(String(call?.init?.headers ? new Headers(call.init.headers).get("Idempotency-Key") : "")).not.toBe("");
    });
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByRole("heading", { name: "更新作业 job-2" })).toBeVisible();
    fireEvent.keyDown(document, { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());

    const row = (await screen.findAllByRole("row")).find(item => item.textContent?.includes("job-1"));
    fireEvent.click(within(row as HTMLElement).getByRole("button", { name: "回滚" }));
    fireEvent.change(await screen.findByLabelText("原因"), { target: { value: "健康检查失败" } });
    fireEvent.click(screen.getByRole("button", { name: "确认回滚" }));
    await waitFor(() => {
      const call = calls.find(call => call.method === "POST" && call.url === "/api/v1/system/updates/jobs/job-1/rollback");
      expect(call).toBeDefined();
      expect(JSON.parse(String(call?.init?.body))).toEqual({ reason: "健康检查失败" });
    });
    expect(await screen.findByText(/回滚请求已提交/)).toBeVisible();
  });

  it("reports the missing Agent update contract instead of faking batch upgrades", async () => {
    await renderAuthenticated("/updates", updateRoutes([]));
    expect(await screen.findByText("契约缺口：Agent 升级")).toBeVisible();
    expect(screen.queryByRole("button", { name: /分批升级|升级 Agent/ })).not.toBeInTheDocument();
  });

  it("explains an unavailable update source instead of treating the check as success", async () => {
    await renderAuthenticated("/updates", {
      "POST /api/v1/system/updates/check": () =>
        jsonResponse({ code: "UPDATE_SOURCE_UNAVAILABLE", message: "no trusted source configured" }, 503),
      "GET /api/v1/system/updates/jobs": () => jsonResponse({ items: [] }),
    });
    expect(await screen.findByRole("heading", { level: 1, name: "更新中心" })).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "检查更新" }));
    expect(await screen.findByText("发行源未配置（B05 硬依赖）")).toBeVisible();
    expect(screen.queryByText("有可用更新")).not.toBeInTheDocument();
    expect(screen.queryByText("已是最新")).not.toBeInTheDocument();
  });

  it("shares one update source with the system settings tab", async () => {
    const { calls } = await renderAuthenticated("/settings", updateRoutes([]));
    fireEvent.click(await screen.findByRole("tab", { name: "系统更新" }));
    expect(await screen.findByText("与更新中心共用同一份状态")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "检查更新" }));
    expect(await screen.findByText("有可用更新")).toBeVisible();
    await waitFor(() => expect(calls.some(call => call.method === "POST" && call.url === "/api/v1/system/updates/check")).toBe(true));
  });
});
