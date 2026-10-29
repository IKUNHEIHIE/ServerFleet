# V09-5D-GM · gemini-02 进度

分支 `feature/v09-console-gm02`；目录 `D:\admin\desktop\codex\myproject\ks\.worktrees\feature--v09-console-gm02`；基线 `8a7dd4707d2d7e142e137d098296788d2c80eddd`。

## 检查点

| 检查点 | SHA | 范围 | 页面/接口 | 测试 | 阻塞 |
|---|---|---|---|---|---|
| GM-01 | 本轮首个提交（见 `git log`） | 应用壳、路由/导航、API 客户端、SSE、会话/权限上下文、登录/初始化/概览 | `/overview` ← `/healthz`、`/api/v1/nodes`、`/api/v1/tasks`、`/api/v1/audit/events`；登录 ← `POST /api/v1/auth/login`；刷新恢复 ← `GET /api/v1/auth/me` | `npm test --workspace @serverfleet/web` 207 PASS；`npm run build --workspace @serverfleet/web` PASS；`node scripts/web-boundaries.mjs` PASS | 后端管理 API 仍未实现：真实 Controller 仅 `/healthz`，登录/概览在真实环境会显示不可用 |

## 结构说明（GM-01）

- `apps/web/src/app/router.ts`：hash 路由、路径参数匹配、查询串构建；`app/navigation.ts` 保留完整 v0.9 功能地图入口。
- `apps/web/src/lib/api/client.ts`：`credentials: include`、Cookie 会话、固定错误分类、CSRF 头、超时/中止、401 全局事件；不把非 JSON 200/404 当 DTO，不回显原始错误体。
- `apps/web/src/lib/sse/event-stream.ts`：有状态 SSE 解析（跨 chunk 保留 id/event/data）、401/403/404 终止、503/断线退避重连、Last-Event-ID；`use-event-stream` 在页面隐藏时暂停。
- `apps/web/src/app/session.tsx`：Cookie 会话恢复、401 失效、403 不清会话、503 保会话、跨标签 BroadcastChannel（仅内存传递 CSRF，不落 localStorage）、BFCache/pageshow/visibility 恢复、`viewEpoch` 供页面丢弃旧数据。
- 登录响应中的 CSRF 仅存内存；刷新后 `/auth/me` 不返回 CSRF，写操作显式锁定并提示重新登录。

## 契约缺口（已确认，需 CX 协调）

1. `GET /api/v1/auth/me` 只返回 `Identity`，不返回当前会话绑定的 CSRF 令牌；刷新后同源写操作无法安全恢复。已在 UI 以“安全写入已锁定”明确说明，不用会话 SID 伪造或新增裸路由。
2. `g2-merged-candidate.yaml` 无工作流（workflows）管理操作；GM-07 的工作流页面在契约补齐前只能显示“契约缺口”，不能伪造规则 CRUD。精确请求见 `SHARED-CHANGES.md`。

## 下一步

GM-02 强化登录/会话恢复测试与浏览器验证；GM-03 接入真实 `GET /api/v1/nodes?onlineOnly=` 与 W07 legacy 列表。
