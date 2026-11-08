import { beforeEach, afterEach, expect, it, vi } from "vitest";
import { continueUpload, readBoundUpload, rememberUploadDraft, forgetUploadDraft, loadUploadDrafts,
  UploadCompleteError, type UploadDraft, type UploadResumePort, UPLOAD_CHUNK_BYTES } from "./upload-resume";
const hash = "a".repeat(64);
const draft = (size = 7): UploadDraft => ({ uploadId: "up-1", ownerId: "u-1", nodeId: "n-1", name: "f.bin",
  size, sha256: hash, destinationPath: "/srv/f.bin", createdAt: Date.now() });
const scope = () => ({ signal: new AbortController().signal, current: () => true });
function file(size: number): File {
  const bytes = new Uint8Array(size); bytes.forEach((_, i) => { bytes[i] = i % 251; });
  return { name: "f.bin", size, slice: (a: number, b: number) => ({ arrayBuffer: async () => bytes.slice(a, b).buffer }) } as File;
}
function ports(d = draft(), start = 2) {
  const read = vi.fn(async () => ({ uploadId: d.uploadId, status: "awaiting_data", bytesReceived: start,
    expectedSize: d.size, destinationPath: d.destinationPath, sha256Expected: d.sha256 }));
  const write = vi.fn(async (bytes: ArrayBuffer, offset: number) => ({ uploadId: d.uploadId, status: "awaiting_data" as const,
    bytesReceived: offset + bytes.byteLength, expectedSize: d.size }));
  const complete = vi.fn(async () => ({ uploadId: d.uploadId, status: "dispatching" as const, taskId: "task-1" }));
  return { read, write, complete };
}
beforeEach(() => localStorage.clear());
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); });
it("persists identities across reload, isolated by account/node, and projects away forged cached state", () => {
  const d = draft(); expect(rememberUploadDraft({ ...d, status: "completed", token: "never-store" } as UploadDraft)).toBe(true);
  expect(loadUploadDrafts("u-1", "n-1")).toEqual([d]);
  expect(loadUploadDrafts("u-2", "n-1")).toEqual([]); expect(loadUploadDrafts("u-1", "n-2")).toEqual([]);
  expect(localStorage.getItem(localStorage.key(0)!)).not.toMatch(/completed|token|bytesReceived/);
  expect(forgetUploadDraft(d)).toBe(true); expect(loadUploadDrafts("u-1", "n-1")).toEqual([]);
});
it("rejects malformed/expired metadata and reports unavailable storage", () => {
  expect(rememberUploadDraft({ ...draft(), createdAt: Date.now() - 8 * 86400000 })).toBe(false);
  expect(rememberUploadDraft({ ...draft(), destinationPath: "/srv/../f.bin" })).toBe(false);
  expect(rememberUploadDraft({ ...draft(), uploadId: undefined } as unknown as UploadDraft)).toBe(false);
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => { throw new Error("quota"); });
  expect(rememberUploadDraft(draft())).toBe(false);
});
it("resumes only confirmed bytes with bounded chunks then submits exactly once", async () => {
  const d = draft(2 * UPLOAD_CHUNK_BYTES + 19); const p = ports(d, 17);
  const result = await continueUpload(file(d.size), d, hash, p, scope());
  expect(p.write.mock.calls.map(c => c[1])).toEqual([17, 17 + UPLOAD_CHUNK_BYTES, 17 + 2 * UPLOAD_CHUNK_BYTES]);
  expect(p.write.mock.calls.every(c => c[0].byteLength <= UPLOAD_CHUNK_BYTES)).toBe(true);
  expect(new Uint8Array(p.write.mock.calls[0][0])[0]).toBe(17);
  expect(p.complete).toHaveBeenCalledTimes(1); expect(result.state.status).toBe("dispatching");
});
it.each(["dispatching", "completed", "failed", "cancelled", "unknown", "unexpected"])("reads %s without replaying any write or complete", async status => {
  const d = draft(); const p = ports(d); p.read.mockResolvedValue({ ...(await p.read()), status }); p.read.mockClear();
  const result = await continueUpload(file(d.size), d, hash, p, scope());
  expect(result.state.status).toBe(status); expect(p.write).not.toHaveBeenCalled(); expect(p.complete).not.toHaveBeenCalled();
});
it.each([
  { destinationPath: "/other/f.bin" }, { sha256Expected: "b".repeat(64) }, { sha256Expected: undefined },
  { expectedSize: 8 }, { bytesReceived: -1 }, { bytesReceived: 8 }, { bytesReceived: 1.5 }, { uploadId: "other" },
])("refuses an unverified or inconsistent server binding %j", async changed => {
  const d = draft(); const p = ports(d); const state = await p.read(); p.read.mockResolvedValue({ ...state, ...changed } as typeof state);
  await expect(continueUpload(file(d.size), d, hash, p, scope())).rejects.toThrow("服务端未确认");
  expect(p.write).not.toHaveBeenCalled(); expect(p.complete).not.toHaveBeenCalled();
});
it("rejects a different selected file before any server request", async () => {
  const d = draft(); const p = ports(d);
  await expect(continueUpload(file(d.size), d, "b".repeat(64), p, scope())).rejects.toThrow("请选择");
  expect(p.read).not.toHaveBeenCalled();
});
it("does not retry a write error or an ambiguous completion", async () => {
  const d = draft(); const p = ports(d); p.write.mockRejectedValue(new Error("409"));
  await expect(continueUpload(file(d.size), d, hash, p, scope())).rejects.toThrow("409");
  expect(p.write).toHaveBeenCalledTimes(1); expect(p.complete).not.toHaveBeenCalled();
  const q = ports(d); q.complete.mockRejectedValue(new Error("receipt lost"));
  await expect(continueUpload(file(d.size), d, hash, q, scope())).rejects.toBeInstanceOf(UploadCompleteError);
  expect(q.complete).toHaveBeenCalledTimes(1);
});
it("rechecks scope after a delayed read and after each acknowledged chunk", async () => {
  const d = draft(UPLOAD_CHUNK_BYTES + 5); const p = ports(d); let current = true;
  const s = { ...scope(), current: () => current };
  const originalRead = p.read.getMockImplementation()!;
  p.read.mockImplementation(async () => { const value = await originalRead(); current = false; return value; });
  await expect(continueUpload(file(d.size), d, hash, p, s)).rejects.toMatchObject({ name: "AbortError" });
  expect(p.write).not.toHaveBeenCalled(); current = true;
  const q = ports(d, 0); const originalWrite = q.write.getMockImplementation()!;
  q.write.mockImplementation(async (b, o) => { const value = await originalWrite(b, o); current = false; return value; });
  await expect(continueUpload(file(d.size), d, hash, q, s)).rejects.toMatchObject({ name: "AbortError" });
  expect(q.write).toHaveBeenCalledTimes(1); expect(q.complete).not.toHaveBeenCalled();
});
it("refuses a wrong byte acknowledgement instead of guessing a new offset", async () => {
  const d = draft(); const p = ports(d); p.write.mockResolvedValue({ uploadId: d.uploadId, status: "awaiting_data", bytesReceived: 3, expectedSize: d.size });
  await expect(continueUpload(file(d.size), d, hash, p, scope())).rejects.toThrow("回执不一致"); expect(p.complete).not.toHaveBeenCalled();
});
it("supports zero-byte fresh files without sending an empty PUT", async () => {
  const d = draft(0); const p = ports(d, 0); await continueUpload(file(0), d, hash, p, scope(), true);
  expect(p.read).not.toHaveBeenCalled(); expect(p.write).not.toHaveBeenCalled(); expect(p.complete).toHaveBeenCalledTimes(1);
});
it("a failed current read never falls back to local cached progress", async () => {
  const p = ports(); p.read.mockRejectedValue(new Error("403"));
  await expect(readBoundUpload(draft(), p as UploadResumePort, scope())).rejects.toThrow("403"); expect(p.write).not.toHaveBeenCalled();
});
