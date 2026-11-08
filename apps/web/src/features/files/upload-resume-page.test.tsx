import { webcrypto, createHash } from "node:crypto";
import { act, cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, afterEach, expect, it, vi } from "vitest";
import { renderAuthenticated } from "@/test/app";
import { IDENTITY, jsonResponse, type FetchRoutes } from "@/test/http";
import { rememberUploadDraft, loadUploadDrafts, UPLOAD_CHUNK_BYTES } from "./upload-resume";

beforeEach(() => { localStorage.clear(); sessionStorage.clear(); vi.stubGlobal("crypto", webcrypto); });
afterEach(() => { vi.unstubAllGlobals(); window.location.hash = ""; });
const base: FetchRoutes = {
 "GET /api/v1/nodes": () => jsonResponse({ items: [{ id:"n-1",name:"Alpha",online:true },{ id:"n-2",name:"Beta",online:true }] }),
 "GET /api/v1/nodes/n-1/files": () => jsonResponse({ nodeId:"n-1",path:"/",entries:[],nextCursor:null }),
 "GET /api/v1/nodes/n-2/files": () => jsonResponse({ nodeId:"n-2",path:"/",entries:[],nextCursor:null }),
 "GET /api/v1/transfers": () => jsonResponse({ items:[] }),
};
function seed(content = "abcdefg") {
 const bytes = new TextEncoder().encode(content); const sha256 = createHash("sha256").update(bytes).digest("hex");
 const d = { uploadId:"up-resume",ownerId:"u-admin",nodeId:"n-1",name:"resume.bin",size:bytes.length,
  sha256,destinationPath:"/resume.bin",createdAt:Date.now() };
 expect(rememberUploadDraft(d)).toBe(true);
 return { d, bytes, state: { uploadId:d.uploadId,status:"awaiting_data",bytesReceived:2,expectedSize:d.size,destinationPath:d.destinationPath,sha256Expected:sha256 } };
}
const reselection = async () => screen.findByLabelText("继续上传 resume.bin");
it("remounts after an interrupted upload and resumes from the server offset, preserving 202 semantics", async () => {
 const bytes = new Uint8Array(2 * UPLOAD_CHUNK_BYTES + 3).fill(65);
 const sha256 = createHash("sha256").update(bytes).digest("hex");
 const first = await renderAuthenticated("/files", { ...base,
  "POST /api/v1/nodes/n-1/files/uploads": () => jsonResponse({uploadId:"up-resume",status:"awaiting_data"},201),
  "PUT /api/v1/nodes/n-1/files/uploads/up-resume/content": (_init,url) => {
   if (Number(url.searchParams.get("offset")) > 0) throw new Error("connection interrupted");
   return jsonResponse({uploadId:"up-resume",status:"awaiting_data",bytesReceived:UPLOAD_CHUNK_BYTES,expectedSize:bytes.length});
  },
 });
 await waitFor(() => expect(document.querySelector('input[type="file"]')).not.toBeDisabled());
 fireEvent.change(document.querySelector('input[type="file"]')!,{target:{files:[new File([bytes],"resume.bin")]}});
 await screen.findByText(/字节与任务状态未确认/);
 expect(first.calls.filter(c=>c.method==="PUT")).toHaveLength(2);
 expect(loadUploadDrafts("u-admin","n-1")).toHaveLength(1);
 cleanup(); sessionStorage.clear(); window.location.hash="";
 const next = await renderAuthenticated("/files", { ...base,
  "GET /api/v1/nodes/n-1/files/uploads/up-resume": () => jsonResponse({uploadId:"up-resume",status:"awaiting_data",bytesReceived:UPLOAD_CHUNK_BYTES,expectedSize:bytes.length,destinationPath:"/resume.bin",sha256Expected:sha256}),
  "PUT /api/v1/nodes/n-1/files/uploads/up-resume/content": (init,url) => jsonResponse({uploadId:"up-resume",status:"awaiting_data",bytesReceived:Number(url.searchParams.get("offset"))+(init?.body as Uint8Array).byteLength,expectedSize:bytes.length}),
  "POST /api/v1/nodes/n-1/files/uploads/up-resume/complete": () => jsonResponse({uploadId:"up-resume",status:"dispatching",taskId:"task-resume"},202),
 });
 fireEvent.change(await reselection(),{target:{files:[new File([bytes],"resume.bin")]}});
 await screen.findByText(/已受理进入统一任务队列（任务 task-resume），尚未完成/);
 expect(next.calls.filter(c=>c.method==="PUT").map(c=>new URL(c.url,"http://localhost").searchParams.get("offset"))).toEqual([String(UPLOAD_CHUNK_BYTES),String(2*UPLOAD_CHUNK_BYTES)]);
 expect(next.calls.filter(c=>c.method==="POST"&&c.url.endsWith("/complete"))).toHaveLength(1);
 expect(next.calls.some(c=>c.method==="POST"&&c.url.endsWith("/uploads"))).toBe(false);
 expect(screen.queryByText(/上传完成，服务端校验通过/)).toBeNull();
});
it("rejects different file contents before asking the server to write", async () => {
 const { d }=seed(); const {calls}=await renderAuthenticated("/files",base);
 fireEvent.change(await reselection(),{target:{files:[new File(["zzzzzzz"],d.name)]}});
 await screen.findByText(/请选择名称、大小和 SHA-256/);
 expect(calls.some(c=>c.url.includes("up-resume"))).toBe(false);
});
it("discards an old node's deferred resume and never redirects bytes to the new node", async () => {
 const {state,bytes}=seed(); let release!:(r:Response)=>void;const delayed=new Promise<Response>(r=>{release=r;});
 const {calls}=await renderAuthenticated("/files",{...base,"GET /api/v1/nodes/n-1/files/uploads/up-resume":()=>delayed});
 fireEvent.change(await reselection(),{target:{files:[new File([bytes],"resume.bin")]}});
 await waitFor(()=>expect(calls.some(c=>c.url.includes("/up-resume?resumeBinding=true"))).toBe(true));
 fireEvent.change(screen.getByLabelText("左侧节点"),{target:{value:"n-2"}});
 await act(async()=>{release(jsonResponse(state));await delayed;});
 expect(calls.some(c=>c.method==="PUT"||c.url.endsWith("/complete"))).toBe(false);
 expect(screen.queryByLabelText("继续上传 resume.bin")).toBeNull();
});
it("does not duplicate a resume on a second selection while its read is pending", async () => {
 const {state,bytes}=seed();let release!:(r:Response)=>void;const delayed=new Promise<Response>(r=>{release=r;});
 const {calls}=await renderAuthenticated("/files",{...base,
  "GET /api/v1/nodes/n-1/files/uploads/up-resume":()=>delayed,
  "PUT /api/v1/nodes/n-1/files/uploads/up-resume/content":()=>jsonResponse({uploadId:"up-resume",status:"awaiting_data",bytesReceived:7,expectedSize:7}),
  "POST /api/v1/nodes/n-1/files/uploads/up-resume/complete":()=>jsonResponse({uploadId:"up-resume",status:"dispatching",taskId:"task-resume"},202),
 });
 const input=await reselection(); const file=new File([bytes],"resume.bin");fireEvent.change(input,{target:{files:[file]}});
 await waitFor(()=>expect(input).toBeDisabled());fireEvent.change(input,{target:{files:[file]}});
 await act(async()=>{release(jsonResponse(state));await delayed;});
 await screen.findByText(/已受理进入统一任务队列/);
 expect(calls.filter(c=>c.method==="GET"&&c.url.includes("/up-resume?resumeBinding=true"))).toHaveLength(1);
 expect(calls.filter(c=>c.url.endsWith("/complete"))).toHaveLength(1);
});
it("reads an already accepted task without writing or submitting it again", async () => {
 const {state,bytes}=seed();const {calls}=await renderAuthenticated("/files",{...base,
  "GET /api/v1/nodes/n-1/files/uploads/up-resume":()=>jsonResponse({...state,status:"dispatching",taskId:"existing-task"}),
 });
 fireEvent.change(await reselection(),{target:{files:[new File([bytes],"resume.bin")]}});
 await screen.findByText(/已受理进入统一任务队列（任务 existing-task）/);
 expect(calls.some(c=>c.method==="PUT"||c.url.endsWith("/complete"))).toBe(false);
});
it("keeps current 403 unverified and offers no cached success fallback", async () => {
 seed();const {calls}=await renderAuthenticated("/files",{...base,
  "GET /api/v1/nodes/n-1/files/uploads/up-resume":()=>jsonResponse({error:{code:"forbidden"}},403),
 });
 fireEvent.click(await screen.findByRole("button",{name:"读回状态 resume.bin"}));
 const panel=await screen.findByRole("region",{name:"待确认上传"});
 await within(panel).findByText("无权限读取（未核验）");
 expect(within(panel).queryByText("已完成")).toBeNull();
 expect(calls.some(c=>c.method==="PUT"||c.url.endsWith("/complete"))).toBe(false);
});
it("does not show another account's persisted upload names or identities",async()=>{
 seed();await renderAuthenticated("/files",base,{...IDENTITY,userId:"another-user"});
 expect(screen.queryByRole("region",{name:"可恢复上传"})).toBeNull();expect(screen.queryByText(/resume.bin/)).toBeNull();
});
