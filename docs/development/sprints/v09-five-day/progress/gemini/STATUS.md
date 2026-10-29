# V09-5D-GM · gemini-02 进度

分支 `feature/v09-console-gm02`；目录 `D:\admin\desktop\codex\myproject\ks\.worktrees\feature--v09-console-gm02`；基线 `8a7dd4707d2d7e142e137d098296788d2c80eddd`。

## 检查点

| 检查点 | SHA | 范围 | 页面/接口 | 测试 | 阻塞 |
|---|---|---|---|---|---|
| GM-01 | 4155b9c | 应用壳、路由/导航、API 客户端、SSE、会话/权限上下文、登录/初始化/概览 | `/overview` ← `/healthz`、`/api/v1/nodes`、`/api/v1/tasks`、`/api/v1/audit/events`；登录 ← `POST /api/v1/auth/login`；刷新恢复 ← `GET /api/v1/auth/me` | `npm test --workspace @serverfleet/web` 207 PASS；`npm run build --workspace @serverfleet/web` PASS；`node scripts/web-boundaries.mjs` PASS | 后端管理 API 仍未实现：真实 Controller 仅 `/healthz`，登录/概览在真实环境会显示不可用 |
| GM-02/03 | f49e637 | 会话失效/恢复语义、W07 legacy 列表、真实节点列表 | `/nodes` ← `GET /api/v1/nodes?onlineOnly=`；富化视图 ← `GET /api/v1/nodes/{nodeId}`（候选详情，需显式切换） | 235 PASS；build PASS；boundaries PASS | 详情契约 D02–D04 未冻结；真实管理 API 未提供，浏览器验证待 GM-09 |
| GM-04 | ffb2a14 | 节点接入/详情、分组双视图与成员抽屉、服务组、用户/角色/授权/会话、审计、安全凭据 | `/nodes/new` ← enrollment tokens/enrollments；`/nodes/{id}` ← detail/identity/capabilities/diagnostics/traffic-quota/suspend/revoke；`/groups` ← groups/classifications/node-results/members；`/service-groups[/:id]` ← CRUD/members/scheduling/preview；`/users` ← users/roles/permissions/grants/sessions；`/audit`；`/settings` ← credentials/sessions | 253 PASS；build PASS；boundaries PASS | 无安装命令/接入历史/密码重置/角色 CRUD 端点；真实 API 未部署，浏览器验收待 GM-09 |
| GM-05 | 本轮后续提交（见 `git log`） | 任务中心/创建预演/审批/取消/恢复/SSE、双栏文件浏览/文件夹/重命名/删除/大小/上传元数据/跨节点传输 | `/tasks` `/tasks/:id` ← tasks/preview/approve/cancel/resume/executions/events；`/files` ← nodes/files/directories/rename/size/uploads/transfers | 259 PASS；build PASS；boundaries PASS | 无下载端点与上传字节通道；`/tasks` 路径在候选契约中重复；分批/金丝雀参数缺失；真实字节验收 BLOCKED |

## GM-02/03 交付细节

### 会话与权限（GM-02）

- 401：全局终止门控，清空身份并回到登录页；同一时刻只处理一次，不自动重试或循环。
- 403：只清当前资源，保留 Cookie 会话与导航；手动刷新再次 403 也不登出。
- 503：保留会话；初次加载显示可重试错误，静默刷新失败保留上次成功数据并显式提示“可能已过期”。
- 刷新恢复：`/auth/me` 成功补回身份；因契约不含 CSRF，写操作锁定并展示原因，不用会话 SID 伪造。
- 跨标签：BroadcastChannel 内存同步登录/退出/失效；CSRF 不写 localStorage/sessionStorage。
- BFCache/pageshow/visibility：暂停 SSE、复核身份、`viewEpoch` 自增使当前资源重新请求，旧响应按序号丢弃。
- 登录表单不持久化密码/令牌；退出请求带 `X-CSRF-Token`，失败时本地清除并提示服务器撤销未确认。

### 节点列表（GM-03 / W07）

- 默认兼容视图只消费 `GET /api/v1/nodes` 的已批准字段：`id/name/online` 与可空指标，`onlineOnly` 由服务端过滤。
- 适配规则：缺 `items`/非数组、非对象行、坏 `id`/`online`、重复 `id` 整次失败且无部分表格/计数；name 空/非法回退 ID 并固定提示；region/provider 非法显示缺失。
- 指标：离线一律不显示历史百分比；在线有限 0–100（含 0/100）显示并标“无采样时间”；`null`→无读数，非有限→读数非法，越界→超出范围；不夹取、不补 0、不补造采样时间。
- 搜索仅作用于已加载 name/id；服务端空集与本地无命中是不同文案；行不可聚焦、无详情/执行入口、无行内按钮。
- 富化视图需用户显式切换，才逐节点请求候选详情；若任一详情缺失/重复/ID 不匹配则整次失败。列表 DTO 无采样时间，CPU/内存不会被显示为当前读数。
- 保留原有 179 个测试与富化组件；新增 legacy/rich 适配测试与页面集成测试。

## 结构说明

- `apps/web/src/app/router.ts`：hash 路由、路径参数匹配、查询串构建；`app/navigation.ts` 保留完整 v0.9 功能地图入口。
- `apps/web/src/lib/api/client.ts`：`credentials: include`、Cookie 会话、固定错误分类、CSRF 头、超时/中止、401 全局事件；不把非 JSON 200/404 当 DTO，不回显原始错误体。
- `apps/web/src/lib/sse/event-stream.ts`：有状态 SSE 解析（跨 chunk 保留 id/event/data）、401/403/404 终止、503/断线退避重连、Last-Event-ID；`use-event-stream` 在页面隐藏时暂停。
- `apps/web/src/app/session.tsx`：Cookie 会话恢复、401 失效、403 不清会话、503 保会话、跨标签 BroadcastChannel（仅内存传递 CSRF，不落 localStorage）、BFCache/pageshow/visibility 恢复、`viewEpoch` 供页面丢弃旧数据。
- 登录响应中的 CSRF 仅存内存；刷新后 `/auth/me` 不返回 CSRF，写操作显式锁定并提示重新登录。

## 契约缺口（已确认，需 CX 协调）

1. `GET /api/v1/auth/me` 只返回 `Identity`，不返回当前会话绑定的 CSRF 令牌；刷新后同源写操作无法安全恢复。已在 UI 以“安全写入已锁定”明确说明，不用会话 SID 伪造或新增裸路由。
2. `g2-merged-candidate.yaml` 无工作流（workflows）管理操作；GM-07 的工作流页面在契约补齐前只能显示“契约缺口”，不能伪造规则 CRUD。精确请求见 `SHARED-CHANGES.md`。

## 下一步

GM-05：统一任务中心/审批/SSE 进度、文件互传双栏/上传/下载/传输任务。随后 GM-06—08 页面与 GM-09 浏览器证据。
