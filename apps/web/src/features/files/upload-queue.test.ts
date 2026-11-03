import { beforeEach, describe, expect, it } from "vitest";
import {
  activePendingUploads,
  claimsRemoteCompletion,
  describeUploadFailure,
  describeUploadStatus,
  isTerminalStatus,
  loadPendingUploads,
  mergeUploadState,
  normalizeUploadStatus,
  savePendingUploads,
  taskStateLabel,
  type PendingUpload,
} from "./upload-queue";

const RECORD: PendingUpload = {
  uploadId: "up-1",
  nodeId: "n-1",
  name: "a.bin",
  status: "dispatching",
  taskId: "task-1",
  bytesReceived: 5,
  expectedSize: 5,
  updatedAt: "2026-11-03T00:00:00Z",
};

beforeEach(() => {
  sessionStorage.clear();
});

describe("upload status semantics", () => {
  it("treats dispatching as queued rather than completed", () => {
    expect(normalizeUploadStatus("dispatching")).toBe("dispatching");
    expect(claimsRemoteCompletion("dispatching")).toBe(false);
    expect(isTerminalStatus("dispatching")).toBe(false);
    expect(describeUploadStatus("dispatching").label).toBe("已受理（排队）");
    expect(describeUploadStatus("dispatching").detail).toContain("尚未");
  });

  it("only completed claims remote completion", () => {
    expect(claimsRemoteCompletion("completed")).toBe(true);
    expect(isTerminalStatus("completed")).toBe(true);
    expect(claimsRemoteCompletion("failed")).toBe(false);
    expect(claimsRemoteCompletion("cancelled")).toBe(false);
  });

  it("keeps unknown statuses honest instead of guessing", () => {
    expect(normalizeUploadStatus("weird")).toBe("unknown");
    expect(describeUploadStatus("unknown").label).toBe("状态未知");
    expect(claimsRemoteCompletion("unknown")).toBe(false);
    expect(isTerminalStatus("unknown")).toBe(false);
  });

  it("maps task states without inventing terminal success", () => {
    expect(taskStateLabel("awaiting_approval")).toBe("待审批");
    expect(taskStateLabel("running")).toBe("执行中");
    expect(taskStateLabel(undefined)).toBe("未知");
  });
});

describe("upload failure mapping", () => {
  it("distinguishes offset conflicts from授权与大小问题", () => {
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
});

describe("pending upload persistence", () => {
  it("round-trips records and survives removed node scopes", () => {
    savePendingUploads("n-1", [RECORD]);
    expect(loadPendingUploads("n-1")).toEqual([RECORD]);
    expect(loadPendingUploads("n-2")).toEqual([]);
    savePendingUploads("n-1", []);
    expect(loadPendingUploads("n-1")).toEqual([]);
  });

  it("ignores corrupt storage instead of throwing", () => {
    sessionStorage.setItem("sf.files.pendingUploads.n-1", "{not json");
    expect(loadPendingUploads("n-1")).toEqual([]);
    sessionStorage.setItem("sf.files.pendingUploads.n-1", JSON.stringify([{ name: "no-id" }, { uploadId: "up-9", name: "ok" }]));
    expect(loadPendingUploads("n-1").map(item => item.uploadId)).toEqual(["up-9"]);
  });
});

describe("mergeUploadState", () => {
  it("does not regress a terminal state on a stale read", () => {
    const merged = mergeUploadState({ ...RECORD, status: "completed" }, { status: "dispatching" });
    expect(merged.status).toBe("completed");
  });

  it("upgrades to the terminal state reported by the server and keeps task binding", () => {
    const merged = mergeUploadState(RECORD, { status: "completed", bytesReceived: 5 });
    expect(merged.status).toBe("completed");
    expect(merged.taskId).toBe("task-1");
  });

  it("accepts a cancelled terminal state and keeps active filtering honest", () => {
    const merged = mergeUploadState({ ...RECORD, status: "completed" }, { status: "cancelled" });
    expect(merged.status).toBe("cancelled");
    const active = activePendingUploads([RECORD, { ...RECORD, uploadId: "up-2", status: "completed" }]);
    expect(active.map(item => item.uploadId)).toEqual(["up-1"]);
  });
});
