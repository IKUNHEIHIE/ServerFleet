import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  activePendingUploads,
  classifyReadFailure,
  claimsRemoteCompletion,
  createPendingUpload,
  describeUploadFailure,
  describeUploadStatus,
  isCurrentScope,
  isTerminalStatus,
  loadPendingUploads,
  mergeObservations,
  mergeUploadState,
  normalizeUploadStatus,
  savePendingUploads,
  taskStateLabel,
  type PendingUpload,
  type UploadObservation,
} from "./upload-queue";

const EPOCH = "epoch-1";

function record(overrides: Partial<PendingUpload> = {}): PendingUpload {
  return {
    uploadId: "up-1",
    nodeId: "n-1",
    name: "a.bin",
    taskId: "task-1",
    bytesReceived: 5,
    expectedSize: 5,
    status: "dispatching",
    evidence: "verified",
    sequence: 1,
    epoch: EPOCH,
    observedAt: "2026-11-03T00:00:00Z",
    ...overrides,
  };
}

function verified(status: PendingUpload["status"], sequence = 2): UploadObservation {
  return { kind: "verified", status, sequence };
}

beforeEach(() => {
  sessionStorage.clear();
});

describe("upload status semantics", () => {
  it("treats dispatching as queued rather than completed", () => {
    expect(normalizeUploadStatus("dispatching")).toBe("dispatching");
    expect(claimsRemoteCompletion({ status: "dispatching", evidence: "verified" })).toBe(false);
    expect(isTerminalStatus("dispatching")).toBe(false);
    expect(describeUploadStatus({ status: "dispatching", evidence: "verified" }).label).toBe("已受理（排队）");
    expect(describeUploadStatus({ status: "dispatching", evidence: "verified" }).detail).toContain("尚未");
  });

  it("only a verified completed observation claims remote completion", () => {
    expect(claimsRemoteCompletion({ status: "completed", evidence: "verified" })).toBe(true);
    expect(isTerminalStatus("completed")).toBe(true);
    expect(claimsRemoteCompletion({ status: "failed", evidence: "verified" })).toBe(false);
    expect(claimsRemoteCompletion({ status: "cancelled", evidence: "verified" })).toBe(false);
    // 未核验/失败读回下，即使内存里的 status 是 completed 也不得声称完成。
    expect(claimsRemoteCompletion({ status: "completed", evidence: "unverified" })).toBe(false);
    expect(claimsRemoteCompletion({ status: "completed", evidence: "forbidden" })).toBe(false);
    expect(claimsRemoteCompletion({ status: "completed", evidence: "unavailable" })).toBe(false);
  });

  it("keeps unknown statuses honest instead of guessing", () => {
    expect(normalizeUploadStatus("weird")).toBe("unknown");
    expect(describeUploadStatus({ status: "unknown", evidence: "verified" }).label).toBe("状态未知");
    expect(claimsRemoteCompletion({ status: "unknown", evidence: "verified" })).toBe(false);
    expect(isTerminalStatus("unknown")).toBe(false);
  });

  it("labels every non-verified evidence as unverified instead of completed", () => {
    expect(describeUploadStatus({ status: "completed", evidence: "unverified" }).label).toBe("待核验");
    expect(describeUploadStatus({ status: "completed", evidence: "forbidden", reason: "http_403" }).label).toBe(
      "无权限读取（未核验）",
    );
    expect(describeUploadStatus({ status: "completed", evidence: "forbidden", reason: "http_403" }).detail).toContain("http_403");
    expect(describeUploadStatus({ status: "completed", evidence: "not_found" }).label).toContain("UNKNOWN");
    expect(describeUploadStatus({ status: "completed", evidence: "unavailable", reason: "network" }).label).toBe(
      "读取失败（未核验）",
    );
  });

  it("maps task states without inventing terminal success", () => {
    expect(taskStateLabel("awaiting_approval")).toBe("待审批");
    expect(taskStateLabel("running")).toBe("执行中");
    expect(taskStateLabel(undefined)).toBe("未知");
  });
});

describe("upload failure mapping", () => {
  it("distinguishes offset conflicts from 授权与大小问题", () => {
    expect(describeUploadFailure(409, "offset_mismatch").kind).toBe("conflict");
    expect(describeUploadFailure(409, "offset_mismatch").recoverable).toBe(true);
    expect(describeUploadFailure(403, null).kind).toBe("forbidden");
    expect(describeUploadFailure(413, null).kind).toBe("toolarge");
    expect(describeUploadFailure(503, null).text).toContain("未送达");
    expect(describeUploadFailure(422, "sha256_mismatch").kind).toBe("digest");
  });

  it("never reports an unrecognised response as success", () => {
    expect(describeUploadFailure(500, null).recoverable).toBe(true);
    expect(describeUploadFailure(500, null).text).toContain("未完成");
  });

  it("classifies read failures as evidence kinds, never as 'unchanged'", () => {
    expect(classifyReadFailure(403)).toBe("forbidden");
    expect(classifyReadFailure(401)).toBe("forbidden");
    expect(classifyReadFailure(404)).toBe("not_found");
    expect(classifyReadFailure(500)).toBe("unavailable");
    expect(classifyReadFailure(undefined)).toBe("unavailable");
  });
});

describe("pending upload persistence", () => {
  it("persists identity metadata only and restores nothing as verified", () => {
    savePendingUploads("n-1", [record({ status: "completed" })]);
    const raw = sessionStorage.getItem("sf.files.pendingUploads.n-1") ?? "";
    // 缓存里不得出现 status/evidence/observedAt：它们不能冒充当前事实。
    expect(raw).not.toContain("completed");
    expect(raw).not.toContain("evidence");
    expect(raw).not.toContain("observedAt");

    const restored = loadPendingUploads("n-1", EPOCH);
    expect(restored).toHaveLength(1);
    expect(restored[0].uploadId).toBe("up-1");
    expect(restored[0].taskId).toBe("task-1");
    expect(restored[0].evidence).toBe("unverified");
    expect(restored[0].status).toBe("unknown");
    expect(claimsRemoteCompletion(restored[0])).toBe(false);
  });

  it("drops a cached completed status written by an older build", () => {
    sessionStorage.setItem(
      "sf.files.pendingUploads.n-1",
      JSON.stringify([{ uploadId: "up-old", name: "old.txt", status: "completed", taskId: "task-old", updatedAt: "2026-11-03T00:00:00Z" }]),
    );
    const restored = loadPendingUploads("n-1", EPOCH);
    expect(restored).toHaveLength(1);
    expect(restored[0].status).toBe("unknown");
    expect(restored[0].evidence).toBe("unverified");
    expect(claimsRemoteCompletion(restored[0])).toBe(false);
    expect(activePendingUploads(restored)).toHaveLength(1);
  });

  it("survives removed node scopes and corrupt storage without throwing", () => {
    savePendingUploads("n-1", [record()]);
    expect(loadPendingUploads("n-2", EPOCH)).toEqual([]);
    savePendingUploads("n-1", []);
    expect(loadPendingUploads("n-1", EPOCH)).toEqual([]);

    sessionStorage.setItem("sf.files.pendingUploads.n-1", "{not json");
    expect(loadPendingUploads("n-1", EPOCH)).toEqual([]);
    sessionStorage.setItem("sf.files.pendingUploads.n-1", JSON.stringify([{ name: "no-id" }, { uploadId: "up-9", name: "ok" }]));
    expect(loadPendingUploads("n-1", EPOCH).map(item => item.uploadId)).toEqual(["up-9"]);
  });
});

describe("mergeUploadState evidence rules", () => {
  it("upgrades to a verified terminal state and keeps the task binding", () => {
    const merged = mergeUploadState(record(), verified("completed", 3));
    expect(merged.status).toBe("completed");
    expect(merged.evidence).toBe("verified");
    expect(merged.taskId).toBe("task-1");
    expect(claimsRemoteCompletion(merged)).toBe(true);
  });

  it("drops a previously verified completion when the current read is forbidden", () => {
    const completed = record({ status: "completed", evidence: "verified", sequence: 5 });
    const merged = mergeUploadState(completed, { kind: "forbidden", reason: "http_403", sequence: 6 });
    expect(merged.evidence).toBe("forbidden");
    expect(merged.status).toBe("unknown");
    expect(merged.reason).toBe("http_403");
    expect(claimsRemoteCompletion(merged)).toBe(false);
    expect(activePendingUploads([merged])).toHaveLength(1);
  });

  it("drops a completion when the current read fails or the session is gone", () => {
    const completed = record({ status: "completed", evidence: "verified", sequence: 5 });
    for (const kind of ["unavailable", "not_found"] as const) {
      const merged = mergeUploadState(completed, { kind, reason: "x", sequence: 6 });
      expect(claimsRemoteCompletion(merged)).toBe(false);
      expect(merged.status).toBe("unknown");
    }
  });

  it("does not let an in-flight older response regress a newer verified terminal state", () => {
    const completed = record({ status: "completed", evidence: "verified", sequence: 9 });
    const stale = mergeUploadState(completed, verified("dispatching", 4));
    expect(stale.status).toBe("completed");
    expect(stale.sequence).toBe(9);
  });

  it("accepts cancelled/failed terminal states but never invents completion", () => {
    expect(mergeUploadState(record(), verified("cancelled", 3)).status).toBe("cancelled");
    expect(mergeUploadState(record(), verified("failed", 3)).status).toBe("failed");
    expect(mergeUploadState(record(), { kind: "verified", status: "unknown", sequence: 3 }).status).toBe("unknown");
  });

  it("keeps only records that are not verified-terminal in the active list", () => {
    const active = activePendingUploads([
      record({ uploadId: "up-1" }),
      record({ uploadId: "up-2", status: "completed", evidence: "verified" }),
      record({ uploadId: "up-3", status: "completed", evidence: "unverified" }),
      record({ uploadId: "up-4", status: "failed", evidence: "verified" }),
    ]);
    expect(active.map(item => item.uploadId)).toEqual(["up-1", "up-3"]);
  });
});

describe("cross-node async safety", () => {
  it("scopes records to node and identity epoch", () => {
    const item = record();
    expect(isCurrentScope(item, "n-1", EPOCH)).toBe(true);
    expect(isCurrentScope(item, "n-2", EPOCH)).toBe(false);
    expect(isCurrentScope(item, "n-1", "epoch-2")).toBe(false);
    expect(isCurrentScope(item, null, EPOCH)).toBe(false);
  });

  it("keeps uploads added during a refresh instead of overwriting the list", () => {
    const during = record({ uploadId: "up-new", sequence: 7 });
    const latest = [during, record()];
    const observations = new Map<string, UploadObservation>([["up-1", verified("completed", 3)]]);
    const merged = mergeObservations(latest, observations);
    expect(merged.map(item => item.uploadId)).toEqual(["up-new", "up-1"]);
    expect(merged[0].evidence).toBe("verified");
    expect(merged[0].status).toBe("dispatching");
    expect(merged[1].status).toBe("completed");
  });

  it("keeps a record whose observation never arrived unverified", () => {
    const merged = mergeObservations([record({ evidence: "unverified", status: "unknown", sequence: 0 })], new Map());
    expect(merged[0].evidence).toBe("unverified");
    expect(claimsRemoteCompletion(merged[0])).toBe(false);
  });
});

describe("createPendingUpload", () => {
  it("records a 202 acceptance as verified dispatching, never completion", () => {
    const created = createPendingUpload(
      {
        uploadId: "up-1",
        nodeId: "n-1",
        name: "a.bin",
        taskId: "task-1",
        bytesReceived: 5,
        expectedSize: 5,
        epoch: EPOCH,
        sequence: 1,
      },
      "2026-11-03T00:00:00Z",
    );
    expect(created.status).toBe("dispatching");
    expect(created.evidence).toBe("verified");
    expect(claimsRemoteCompletion(created)).toBe(false);
    expect(created.observedAt).toBe("2026-11-03T00:00:00Z");
  });
});


it("isolates pending identities by account and never imports unowned legacy cache", () => {
  sessionStorage.clear();
  savePendingUploads("n-1", [record({name:"private.txt"})], "u-1");
  expect(loadPendingUploads("n-1", EPOCH, "u-1").map(v=>v.name)).toEqual(["private.txt"]);
  expect(loadPendingUploads("n-1", EPOCH, "u-2")).toEqual([]);
  savePendingUploads("n-1", [record({name:"legacy.txt"})]);
  expect(loadPendingUploads("n-1", EPOCH, "u-2")).toEqual([]);
});


it("keeps unavailable browser storage from breaking the pending panel", () => {
  const get = vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => { throw new Error("blocked storage"); });
  try { expect(loadPendingUploads("n-1", EPOCH, "u-1")).toEqual([]); }
  finally { get.mockRestore(); }
});
