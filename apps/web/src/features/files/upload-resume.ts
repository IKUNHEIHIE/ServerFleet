import type { ConflictPolicy, UploadCompletion, UploadProgress, UploadState } from "@/lib/api/dto";

/** Local identities are hints, never cached progress, authorization or success. */
export interface UploadDraft {
  readonly uploadId: string;
  readonly ownerId: string;
  readonly nodeId: string;
  readonly name: string;
  readonly size: number;
  readonly sha256: string;
  readonly conflictPolicy?: ConflictPolicy;
  readonly destinationPath: string;
  readonly createdAt: number;
}
const HASH = /^[a-f0-9]{64}$/;
const ID = /^[a-zA-Z0-9_-]{1,128}$/;
const MAX_AGE = 7 * 24 * 60 * 60 * 1000;
export const UPLOAD_CHUNK_BYTES = 1024 * 1024;
export class UploadResumeError extends Error {}
export class UploadCompleteError extends Error {
  constructor(readonly received: number, readonly cause: unknown) { super("Upload completion was not confirmed"); }
}
const key = (owner: string, node: string) => `sf.files.resume.v1.${encodeURIComponent(owner)}.${encodeURIComponent(node)}`;
const metadata = (d: UploadDraft): UploadDraft => ({ uploadId: d.uploadId, ownerId: d.ownerId, nodeId: d.nodeId,
  name: d.name, size: d.size, sha256: d.sha256, conflictPolicy: d.conflictPolicy ?? "overwrite", destinationPath: d.destinationPath, createdAt: d.createdAt });

function valid(value: unknown, owner: string, node: string, now: number): value is UploadDraft {
  if (!value || typeof value !== "object") return false;
  const d = value as UploadDraft;
  return d.ownerId === owner && d.nodeId === node && typeof d.uploadId === "string" && ID.test(d.uploadId) &&
    typeof d.name === "string" && d.name.length > 0 && d.name.length <= 255 && !/[\x00-\x1f\x7f/\\]/.test(d.name) &&
    d.name !== "." && d.name !== ".." && Number.isSafeInteger(d.size) && d.size >= 0 &&
    typeof d.sha256 === "string" && HASH.test(d.sha256) &&
    (d.conflictPolicy === undefined || ["skip", "overwrite", "rename"].includes(d.conflictPolicy)) && typeof d.destinationPath === "string" &&
    d.destinationPath.length <= 4096 && d.destinationPath.startsWith("/") && d.destinationPath.endsWith(`/${d.name}`) &&
    !d.destinationPath.includes("//") && !/[\x00-\x1f\x7f\\]/.test(d.destinationPath) &&
    !d.destinationPath.split("/").some(p => p === "." || p === "..") &&
    Number.isSafeInteger(d.createdAt) && d.createdAt <= now && now - d.createdAt <= MAX_AGE;
}
export function loadUploadDrafts(owner: string, node: string, now = Date.now()): readonly UploadDraft[] {
  try {
    const raw = localStorage.getItem(key(owner, node));
    if (!raw || raw.length > 256 * 1024) return [];
    const data: unknown = JSON.parse(raw);
    if (!Array.isArray(data)) return [];
    const seen = new Set<string>();
    return data.filter((d): d is UploadDraft => {
      if (!valid(d, owner, node, now) || seen.has(d.uploadId)) return false;
      seen.add(d.uploadId); return true;
    }).slice(0, 32).map(metadata);
  } catch { return []; }
}
function writeDrafts(owner: string, node: string, drafts: readonly UploadDraft[]): boolean {
  try { localStorage.setItem(key(owner, node), JSON.stringify(drafts.map(metadata))); return true; }
  catch { return false; }
}
export function rememberUploadDraft(d: UploadDraft): boolean {
  return valid(d, d.ownerId, d.nodeId, Date.now()) && writeDrafts(d.ownerId, d.nodeId,
    [d, ...loadUploadDrafts(d.ownerId, d.nodeId).filter(v => v.uploadId !== d.uploadId)].slice(0, 32));
}
export function forgetUploadDraft(d: UploadDraft): boolean {
  return writeDrafts(d.ownerId, d.nodeId, loadUploadDrafts(d.ownerId, d.nodeId).filter(v => v.uploadId !== d.uploadId));
}
export interface UploadResumePort {
  read(signal: AbortSignal): Promise<UploadState>;
  write(bytes: ArrayBuffer, offset: number, signal: AbortSignal): Promise<UploadProgress>;
  complete(signal: AbortSignal): Promise<UploadCompletion>;
}
export interface UploadScope { readonly signal: AbortSignal; readonly current: () => boolean }
function guard(scope: UploadScope): void {
  if (scope.signal.aborted || !scope.current()) throw new DOMException("Upload scope changed", "AbortError");
}
export async function readBoundUpload(d: UploadDraft, port: UploadResumePort, scope: UploadScope): Promise<UploadState> {
  guard(scope); const state = await port.read(scope.signal); guard(scope);
  if (!HASH.test(d.sha256) || state.uploadId !== d.uploadId || state.destinationPath !== d.destinationPath ||
    state.sha256Expected !== d.sha256 || state.conflictPolicy !== (d.conflictPolicy ?? "overwrite") || state.expectedSize !== d.size || !Number.isSafeInteger(state.bytesReceived) ||
    state.bytesReceived < 0 || state.bytesReceived > d.size) {
    throw new UploadResumeError("服务端未确认原上传目标、摘要或有效偏移，未继续写入。");
  }
  return state;
}
export interface UploadRunResult { readonly state: UploadState | UploadCompletion; readonly bytesReceived: number }
export async function continueUpload(file: File, d: UploadDraft, selectedSHA: string | undefined,
  port: UploadResumePort, scope: UploadScope, fresh = false): Promise<UploadRunResult> {
  guard(scope);
  if (file.name !== d.name || file.size !== d.size || (!fresh && (!selectedSHA || selectedSHA !== d.sha256))) {
    throw new UploadResumeError("请选择名称、大小和 SHA-256 均与原上传一致的文件。");
  }
  const state: UploadState = fresh ? { uploadId: d.uploadId, status: "awaiting_data", bytesReceived: 0, expectedSize: d.size }
    : await readBoundUpload(d, port, scope);
  // Accepted, terminal and unknown states are read-only; a lost response never
  // authorizes a second complete or a replacement Agent execution.
  if (state.status !== "awaiting_data") return { state, bytesReceived: state.bytesReceived };
  let offset = state.bytesReceived;
  while (offset < file.size) {
    guard(scope); const end = Math.min(offset + UPLOAD_CHUNK_BYTES, file.size);
    const bytes = await file.slice(offset, end).arrayBuffer(); guard(scope);
    if (bytes.byteLength !== end - offset) throw new UploadResumeError("本地文件字节长度已变化，未继续写入。");
    const progress = await port.write(bytes, offset, scope.signal); guard(scope);
    if (progress.uploadId !== d.uploadId || progress.status !== "awaiting_data" ||
      progress.bytesReceived !== end || progress.expectedSize !== d.size) {
      throw new UploadResumeError("服务端字节回执不一致；已停止，请读回真实状态。");
    }
    offset = end;
  }
  guard(scope);
  let completed: UploadCompletion;
  try { completed = await port.complete(scope.signal); }
  catch (error) { throw new UploadCompleteError(offset, error); }
  guard(scope);
  if (completed.uploadId !== d.uploadId) throw new UploadResumeError("服务端上传回执不一致，未声称完成。");
  return { state: completed, bytesReceived: offset };
}
