import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { jsonResponse } from "@/test/http";
import { renderAuthenticated } from "@/test/app";

afterEach(() => { vi.unstubAllGlobals(); window.location.hash = ""; });
describe("notification setup and durable task status", () => {
 it("configures an empty channel using an encrypted credential reference and shows queued task truthfully", async () => {
  let saved = false;
  const smtp = {channel:"smtp",enabled:true,credentialStatus:"configured",smtp:{host:"smtp.example.invalid",port:465,security:"tls",sender:"a@example.invalid",recipient:"b@example.invalid"}};
  const {calls}=await renderAuthenticated("/settings", {
   "GET /api/v1/credentials":()=>jsonResponse([{id:"cred-smtp",name:"Encrypted SMTP",kind:"smtp_password",revision:4,status:"unverified",usageScope:["smtp"]}]),
   "GET /api/v1/notifications/channels":()=>jsonResponse({items:saved?[smtp]:[]}),
   "GET /api/v1/notifications/settings":()=>jsonResponse({smtpFallbackOnQQFailure:false,maxRetryAttempts:2,deduplicationWindowSeconds:60}),
   "GET /api/v1/notifications/deliveries":()=>jsonResponse({items:[{id:"d1",channel:"smtp",status:"queued",taskId:"notify-task-1"}]}),
   "PUT /api/v1/notifications/channels/smtp":()=>{saved=true;return jsonResponse(smtp);},
   "POST /api/v1/notifications/channels/smtp/credentials":()=>{saved=true;return jsonResponse({...smtp,credentialId:"cred-smtp",credentialRevision:4,backgroundReady:true});},
   "POST /api/v1/notifications/channels/smtp/test":()=>jsonResponse({id:"d1",channel:"smtp",status:"queued",taskId:"notify-task-1"}),
  });
  fireEvent.click(await screen.findByRole("tab",{name:"通知渠道"}));
  const heading=await screen.findByRole("heading",{name:"SMTP 邮箱"});
  const panel=heading.closest("section")!;
  expect(panel).toBeTruthy();
  expect(screen.queryByLabelText(/SMTP 密码\/授权码/)).not.toBeInTheDocument();
  fireEvent.change(within(panel).getByLabelText("SMTP 服务器"),{target:{value:"smtp.example.invalid"}});
  fireEvent.change(within(panel).getByLabelText("端口"),{target:{value:"465"}});
  fireEvent.change(within(panel).getByLabelText("发件邮箱"),{target:{value:"a@example.invalid"}});
  fireEvent.change(within(panel).getByLabelText("收件邮箱"),{target:{value:"b@example.invalid"}});
  fireEvent.click(within(panel).getByRole("button",{name:"保存配置"}));
  await screen.findByText(/渠道配置已保存/);
  const configuredPanel=(await screen.findByRole("heading",{name:"SMTP 邮箱"})).closest("section")!;
  fireEvent.change(within(configuredPanel).getByLabelText("加密凭据"),{target:{value:"cred-smtp"}});
  fireEvent.click(within(configuredPanel).getByRole("button",{name:"绑定凭据"}));
  await waitFor(()=>expect(calls.find(call=>call.url==="/api/v1/notifications/channels/smtp/credentials")?.init?.body).toBe(JSON.stringify({secretRef:"cred-smtp"})));
  fireEvent.click(await screen.findAllByRole("button",{name:"测试消息"}).then(buttons=>buttons[0]));
  const result=await screen.findByText(/排队 · 渠道 smtp/);
  expect(result).toBeVisible();
  expect(screen.getByRole("link",{name:"查看投递任务"})).toHaveAttribute("href","#/tasks/notify-task-1");
  expect(screen.getAllByText("排队").length).toBeGreaterThan(0);
 });
});
