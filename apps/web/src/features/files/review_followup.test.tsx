import {fireEvent, screen, waitFor, within, act} from "@testing-library/react";
import {afterEach, beforeEach, expect, it, vi} from "vitest";
import {renderAuthenticated} from "@/test/app";
import {jsonResponse, type FetchRoutes} from "@/test/http";
beforeEach(()=>sessionStorage.clear());
afterEach(()=>{vi.unstubAllGlobals();window.location.hash="";});
const base:FetchRoutes={
 "GET /api/v1/nodes":()=>jsonResponse({items:[{id:"n-1",name:"Alpha",online:true},{id:"n-2",name:"Beta",online:true}]}),
 "GET /api/v1/nodes/n-1/files":()=>jsonResponse({nodeId:"n-1",path:"/",entries:[],nextCursor:null}),
 "GET /api/v1/nodes/n-2/files":()=>jsonResponse({nodeId:"n-2",path:"/",entries:[],nextCursor:null}),
 "GET /api/v1/transfers":()=>jsonResponse({items:[]}),
};
function seed(){sessionStorage.setItem("sf.files.pendingUploads.v2.u-admin.n-1",JSON.stringify([{uploadId:"up-r",name:"review.txt",taskId:"task-r"}]));}
it("review: old same-scope response cannot overwrite newer forbidden evidence",async()=>{
 seed();let resolveOld!:(r:Response)=>void;let reads=0;
 const old=new Promise<Response>(r=>{resolveOld=r;});
 await renderAuthenticated("/files",{...base,"GET /api/v1/nodes/n-1/files/uploads/up-r":()=> ++reads===1?old:jsonResponse({error:{code:"forbidden",message:"revoked"}},403)});
 await waitFor(()=>expect(reads).toBe(1));
 const panel=await screen.findByRole("region",{name:"待确认上传"});
 fireEvent.click(within(panel).getByRole("button",{name:"读回真实状态"}));
 await waitFor(()=>expect(within(panel).getByText("无权限读取（未核验）")).toBeVisible());
 await act(async()=>{resolveOld(jsonResponse({uploadId:"up-r",taskId:"task-r",status:"completed",bytesReceived:10}));await old;});
 expect(within(panel).queryByText("已完成")).toBeNull();
 expect(within(panel).getByText("无权限读取（未核验）")).toBeVisible();
});
it("review: switching nodes while reading task revision must abort cancellation",async()=>{
 seed();let resolveTask!:(r:Response)=>void;const old=new Promise<Response>(r=>{resolveTask=r;});
 const {calls}=await renderAuthenticated("/files",{...base,
 "GET /api/v1/nodes/n-1/files/uploads/up-r":()=>jsonResponse({uploadId:"up-r",taskId:"task-r",status:"dispatching"}),
 "GET /api/v1/tasks/task-r":()=>old,
 "POST /api/v1/tasks/task-r/cancel":()=>jsonResponse({taskId:"task-r",state:"cancelled",revision:2}),
 });
 const panel=await screen.findByRole("region",{name:"待确认上传"});
 fireEvent.click(within(panel).getByRole("button",{name:/取消/}));
 await waitFor(()=>expect(calls.some(c=>c.url==="/api/v1/tasks/task-r")).toBe(true));
 fireEvent.change(screen.getByLabelText("左侧节点"),{target:{value:"n-2"}});
 await act(async()=>{resolveTask(jsonResponse({taskId:"task-r",state:"running",revision:1}));await old;});
 expect(calls.filter(c=>c.method==="POST"&&c.url==="/api/v1/tasks/task-r/cancel")).toHaveLength(0);
});
it("review: switching nodes during upload creation does not redirect later writes",async()=>{
 let resolveCreate!:(r:Response)=>void;const creating=new Promise<Response>(r=>{resolveCreate=r;});
 const {calls}=await renderAuthenticated("/files",{...base,
 "POST /api/v1/nodes/n-1/files/uploads":()=>creating,
 });
 await waitFor(()=>expect(document.querySelector('input[type="file"]')).not.toBeDisabled());
 const input=document.querySelector<HTMLInputElement>('input[type="file"]')!;
 fireEvent.change(input,{target:{files:[new File(["hello"],"scope.txt")]}});
 await waitFor(()=>expect(calls.some(c=>c.method==="POST"&&c.url==="/api/v1/nodes/n-1/files/uploads")).toBe(true));
 fireEvent.change(screen.getByLabelText("左侧节点"),{target:{value:"n-2"}});
 await act(async()=>{resolveCreate(jsonResponse({uploadId:"up-create",status:"open"},201));await creating;});
 expect(calls.filter(c=>c.method==="PUT"&&c.url.includes("/files/uploads/"))).toHaveLength(0);
 expect(calls.filter(c=>c.method==="POST"&&c.url.includes("/up-create/complete"))).toHaveLength(0);
});
