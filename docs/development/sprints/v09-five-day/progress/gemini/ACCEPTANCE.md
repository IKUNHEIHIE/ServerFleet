# GM-09 浏览器验收记录（gemini-02）

验收对象：提交 `7a70ac2` 的 `apps/web/dist`（由该提交的工作树构建，`npm run build --workspace @serverfleet/web`）。
分支 `feature/v09-console-gm02`；基线 `8a7dd4707d2d7e142e137d098296788d2c80eddd`。

## 环境与边界

| 项 | 值 |
|---|---|
| 操作系统 | Windows 10.0.26300 x64 |
| 浏览器 | Chrome 156.0.8078.49 headless，CDP 127.0.0.1:9223，独立临时 profile（验收后删除） |
| Node | v24.18.0 |
| Go | **本机没有 Go 工具链**，无法运行 `apps/controller` 骨架；后端缺失场景用路由集相同的 `--healthz-only` 桩复现 |
| 前端服务 | `apps/web/acceptance/stub-server.mjs`，仅监听 127.0.0.1，同时托管 `apps/web/dist` 与候选契约的内存桩 |
| 生产代码关系 | 桩与 fixtures 位于 `apps/web/acceptance/`，不被 `apps/web/src` 引用、不进入 dist；生产请求失败只显示错误分类，不回退任何桩数据 |

为什么需要桩：真实 Controller 只实现 `GET /healthz`（`apps/controller/main.go`），管理 API 尚未实现。为了让**已交付的正式前端**在浏览器中被真实操作，用候选契约桩提供 HTTP 行为；**页面、适配器、会话与 SSE 代码全部是生产构建**。桩不是 Controller 实现，也不是验收通过的后端证据。

## 复现命令

```sh
npm run build --workspace @serverfleet/web
node apps/web/acceptance/stub-server.mjs --port 5180            # 契约桩 + dist
node apps/web/acceptance/stub-server.mjs --port 5181 --healthz-only  # 仅 /healthz

# 启动 headless Chrome（示例）
"C:\Program Files\Google\Chrome\Application\chrome.exe" --headless=new \
  --remote-debugging-port=9223 --user-data-dir=%TEMP%\gm09-profile --window-size=1280,900 about:blank

node apps/web/acceptance/cdp-driver.mjs goto "http://127.0.0.1:5180/#/overview"
node apps/web/acceptance/cdp-driver.mjs eval "<expression>"       # 读取 DOM / 点击
node apps/web/acceptance/cdp-driver.mjs shot <file.png> [w h]     # 截图（可指定视口）
```

登录凭据由桩决定：用户名 `admin`，密码 `accept123`（可用 `--password` 覆盖）。真实产品不使用该凭据。

## 已执行步骤与结果

### 1. 会话与登录（1280×900）

- 打开 `#/overview` → 匿名时显示登录页；通过**真实表单**输入用户名/密码并提交。
- 登录成功后进入概览：已授权节点 6、在线 4、近期任务 3、近期审计 3；健康接口返回 scaffold 时显示“Controller 仍为工程骨架”，不伪造管理能力。
- 强制刷新后 `GET /auth/me` 恢复身份但**不返回 CSRF**：页面显示“安全写入已锁定 / 重新登录以获取 CSRF 令牌”，安装、停用、保存等按钮禁用；点击该链接重新登录后写操作恢复。证据：`20-writes-locked-after-reload-1280x900.png`。
- 服务端撤销会话（桩 `POST /__stub/session/invalidate`）后点击刷新：401 全局门控回到登录页，提示“会话已失效或已被撤销，请重新登录。”，不循环重试。证据：`22-session-revoked-401-1280x900.png`。

### 2. 节点（W07 兼容规则）

- `#/nodes` 使用 `GET /api/v1/nodes`：在线 CPU=0 与内存=100 被原样显示（0/100 合法），并标注“无采样时间”；离线节点显示“离线，无当前读数”，不显示历史百分比。证据：`03-nodes-1280x900.png`。
- 富化视图为显式切换的候选详情视图，不默认加载。
- `#/nodes/n-1` 显示元数据/身份/能力/诊断/流量配额；暂停与吊销入口存在（写操作受 CSRF 锁定约束）。证据：`04-node-detail-1280x900.png`。

### 3. 任务与 SSE

- `#/tasks` 列表显示等待审批/执行中/已完成；`#/tasks/task-1` 打开 SSE：游标推进到 4，事件只显示投影字段（`task.state/progress`），原始 data 中不存在于 DOM（桩在 data 里放入了 `SECRET-STUB-OUTPUT-MUST-NOT-RENDER`，页面从未渲染）。证据：`06-task-detail-sse-1280x900.png`。
- 已知限制：桩的 SSE 时间线与任务快照各自独立，截图里“状态：等待审批”与事件“已完成”并存；这是桩数据不一致，不是前端把事件当权威状态（页面同时标注“状态与摘要以服务端为准”）。

### 4. 其他功能页（均读取桩的真实 HTTP 响应）

`#/files`、`#/groups`（自定义分组/自动分类、成员表）、`#/service-groups`、`#/monitoring`（明确资源历史缺口）、`#/dns`、`#/certificates`（离线节点 waiting_offline）、`#/gateways`（维护中不可调度）、`#/publishing`（预览检查项）、`#/users`、`#/audit`、`#/settings`（凭据元数据/会话/通知/系统更新四个页签）、`#/workflows`（BLOCKED 页，无规则编辑器）。

截图：`07-files`、`08-groups`、`09-monitoring`、`11-xray`。

### 5. 插件与 Xray

- `#/plugins`：目录含可安装条目（有 commit+SHA-256）与“缺少 SHA-256”的未发布条目；安装确认在未勾选全部权限时不发请求。证据：`10-plugins-1280x900.png`。
- `#/plugins/xray`：仅在 `xray-manager` 服务端状态为 enabled 时渲染声明式控制台（节点/安装/配置/用户/流量/历史六个标签）；停用后只显示“插件未启用”。证据：`11-xray-1280x900.png`。
- 启停联动：点击“启用”后侧边栏入口**无需刷新**立即出现；点击“停用”后立即消失（本次修复的 pluginEpoch 行为）。证据：`21-plugin-entry-gating-1280x900.png`。

### 6. AI 运维

- `#/ai` 显示服务端安全策略；没有会话列表端点，页面明确说明契约缺口并只提供新建。
- 新建会话后进入 `#/ai/ai-11`：零授权、scopeRevision=1、事件流 open/游标 2；SSE 原始 `output` 从未渲染。证据：`15-ai-session-1280x900.png`。

### 7. 更新与备份

- `#/updates`：检查→计划→一键更新的三段式；未检查时不显示任何版本号。证据：`12-updates-1280x900.png`。
- `#/backups`：显式说明没有备份列表/创建端点，不提供“立即备份”，只接受已知 backupId 校验。证据：`13-backups-1280x900.png`。

### 8. 故障与并发语义（桩注入）

| 场景 | 注入 | 观察 |
|---|---|---|
| 503 | `POST /__stub/faults {match:"/api/v1/nodes",status:503}` | 节点列表显示“Controller 暂时不可用。当前会话已保留，可稍后重试。”、HTTP 503 与请求 ID；导航与会话保留；清除故障后点“重新加载”恢复。证据：`19-nodes-503-retryable-1280x900.png` |
| 403 | `{match:"/api/v1/groups",status:403}` | 只有分组资源显示“无权限”，会话、侧边栏与其他页面不受影响 |
| 迟到响应 | `{match:"/api/v1/groups?",method:"GET",delayMs:2500}`，随后立即切到 `#/tasks` | 3.5 秒后任务页从未出现迟到分组数据；返回 `#/groups` 重新请求并显示最新数据 |
| 重复激活 | 连续三次点击“停用” | 服务端只收到 **1** 次 `PUT /activation`（useAction pending 门控） |
| 401 | `/__stub/session/invalidate` | 见第 1 节：回登录页并提示 |
| 后端缺失 | `--healthz-only` 桩（只实现 `/healthz`，等价于 Go 骨架的路由集） | `#/overview` 显示“无法验证管理会话 / 错误码 NOT_FOUND”，说明 404 通常表示 Controller 仍是骨架，不伪造登录。证据：`18-honest-unavailable-1280x900.png` |

### 9. 键盘、焦点与布局

- 每次路由切换后焦点落在 `#page-title`（h1），标题同步为“页面名 · Server Fleet”。
- “跳到主要内容”链接存在，`href="#main-content"`，聚焦时从 sr-only 变为可见。
- 420×900：侧边栏收起为“菜单”，`document.documentElement.scrollWidth === 420`（无横向溢出）；概览与节点列表均可读。证据：`16-overview-420x900.png`、`17-nodes-420x900.png`。
- 1280×900：侧边栏固定、内容区最大宽度正常（其余截图）。

## 未完成 / 不能声称的部分

1. **真实后端未验收**：Controller 仍是 `GET /healthz` 骨架，本记录只证明前端在候选契约响应下的行为；mTLS、鉴权、SQLite、真实任务执行均未实现。
2. **Go 工具链缺失**：本机没有 `go`，无法运行 Go 骨架本身；“仅 healthz”场景用同路由集的 Node 桩替代。
3. **契约缺口仍 BLOCKED**：Agent 更新、工作流、公网证书、上传字节/下载、资源历史指标、AI 会话列表、备份记录列表/创建（见 `SHARED-CHANGES.md` §2–§10）。
4. **真实供应商/节点行为 UNTESTED**：DNS（Cloudflare）、证书签发、网关 Nginx 安装、Xray 安装与配置部署都没有可执行后端。
5. **桩 SSE 与任务快照不同步**（见第 3 节），仅影响验收观感，不影响前端投影规则。

截图位于 `progress/gemini/evidence/`，共 22 张；均由 headless Chrome 在 127.0.0.1 上对上述构建拍摄。

---

# GM-W4-01 · 真实 Controller + SQLite WAL + HTTPS 端到端验收（gemini-04 · Wave 4）

- 验收对象：`apps/web/dist` 生产构建 + `go build ./apps/controller`（基线 `0f9a75e`，含 Wave 3 全部接线模块）。
- 脚本：`tests/browser/v09/run-live-controller-acceptance.mjs`（自动构建 → 启动真实 Controller（`-db 临时 SQLite WAL` + 自签 TLS + 同源托管 dist 并转发 `/api/*`）→ 本机安装器写入 setup token/播种节点/生成 VACUUM INTO 备份 → Headless Edge 全链路）。
- 复现：`node tests/browser/v09/run-live-controller-acceptance.mjs [--out <目录>] [--skip-build] [--keep]`
- 结果：**58/58 步 PASS**（第一轮 49/58，暴露 9 项契约差异，全部在 `apps/web` 内修复后复跑全绿；见 `SHARED-CHANGES.md` §13）。

## 步骤 31–56（本轮新增，截图 `evidence/live/live-16..32-*.png`）

| 步骤 | 模块 | 真实后端交互 | 断言要点 |
|---|---|---|---|
| 31–33 | 节点详情/流量配额 | `GET nodes/{id}/{identity,capabilities,diagnostics}`、`PUT/GET nodes/{id}/traffic-quota` | 身份/能力/诊断渲染；未配置配额显示诚实空态（404 quota_not_found）；配置 500GB 后 PUT/GET 一致 |
| 34–36 | 监控与流量风险 | `GET/POST/PATCH /api/v1/monitoring/availability-monitors`、`GET …/traffic-risks` | 授权节点行 + 风险源读取；创建 201；暂停后列表显示“已暂停”（白名单 PATCH 体） |
| 37 | 证书 | `GET /api/v1/certificates/internal/distributions` | 诚实空态 |
| 38–40 | 服务发布 | `POST /api/v1/publishing/preview`、`POST /api/v1/publishing/publications` | 向导按服务端调度资格诚实门控；同源 fetch 实测 preview eligible=true（目标快照含 node-alpha）、创建 202 queued 三阶段计划并出现在发布记录 |
| 41–46 | AI | `GET /ai/security-policy`、`POST /ai/sessions`、`POST …/grants`、`PATCH …/approval-policy`、`POST …/approvals(+decision)`、`POST /api/v1/ai/tools/execute` | 默认拒绝策略声明；零授权会话；授权 node-alpha（注册工具 tasks.command）；模式切换往返；一次性审批创建→批准（canonicalArgumentsHash）→受控执行 202 queued |
| 47–49 | 插件/Xray | `GET/PUT /api/v1/plugins{,/repository,/catalog}`、`POST …/catalog/refresh`、`GET /api/v1/plugins/xray-manager` | 来源 official、目录/已安装诚实空态；刷新目录 501 错误码可见；未启用时 Xray 控制台被门控拦截 |
| 50–52 | 更新中心 | `POST /api/v1/system/updates/{check,plan}`、`GET …/jobs` | 503 UPDATE_SOURCE_UNAVAILABLE / UPDATE_PLANNER_UNAVAILABLE 诚实展示；作业空态 |
| 53–55 | 备份恢复 | `POST /api/v1/backups/{id}/verify`、`POST /api/v1/disaster-recovery/preview` | VACUUM INTO 真实备份通过 backup_integrity；勾选 fenced 后预演仍返回 target_identity_not_observed / fencing_evidence_unavailable 未通过；创建恢复作业保持禁用（fail-closed） |
| 56 | 审计 | `GET /api/v1/audit/events` | monitoring.update / quota.policy / publishing.plan_create / ai.sessions.create / ai.sessions.grant 全部落盘 |

## 与候选契约的差异处理（全部在 `apps/web` 消化，详见 SHARED-CHANGES §13）

1. 无 `GET /api/v1/nodes/{id}` 详情路由 → 子资源独立加载 + 列表摘要诚实回退。
2. 节点生命周期拒绝 `expectedRevision` → 前端只提交 `{reason}`。
3. 监控 POST/PATCH `DisallowUnknownFields` → 白名单请求体。
4. 插件 `phase`→`status` 归一（compat 单测覆盖）。
5. 嵌套 `error.code` 提取，UI 显示具体错误码。
6. 流量配额精简响应字段诚实显示「未返回」。
7. 监控目标 SSRF 策略拒绝内网后缀（实证 `invalid_request`），验收改用公网样式目标。

## 仍然不能声称的部分（Wave 4 后依旧）

- 组合运行时未注入的适配器按服务端语义诚实失败：更新源/计划器 503、插件目录源 501、发布器缺失（计划停留 queued）、网关 Checker 缺失（路由 503、网关不可调度）、AI 模型生成 503、Agent 派发器缺失（审批后停留 queued）。
- AI 会话 approval-events / 推荐 decision 路由未在 `server.go` 挂载（CX Wave 4 集成范围），界面显示诚实 404。
- 真实供应商（Cloudflare）、真实 Nginx/Xray 实机、真实 Agent 入网与心跳、公网证书 ACME：UNTESTED。

---

# GM-W5-01/02 · Wave 4 新接口前后端打通 + 70 步真实 Controller 验收（gemini-05 · Wave 5）

- 验收对象：`apps/web` 生产构建 + `go build ./apps/controller`（基线 `3adf891`，含 Wave 4 复审合入的工作流 HTTP、监控 history/probe、文件字节通道、发布 run/rollback）。
- 脚本：`tests/browser/v09/run-live-controller-acceptance.mjs`（58 → **70 步**；自动构建 → 真实 Controller → 同源 TLS 托管 dist → Headless Edge 全链路）。
- 复现：`node tests/browser/v09/run-live-controller-acceptance.mjs [--out <目录>] [--skip-build] [--keep]`
- 结果：**70/70 步 PASS**（第一轮 69/70，唯一失败是脚本在页面 `change` 处理器同步清空 file input 之后才读取注入长度；修正读取时机后复跑全绿。前 58 步与 Wave 4 行为一致，回归无退化）。

## 步骤 57–68（本轮新增，截图 `evidence/live/live-33..38-*.png`）

| 步骤 | 模块 | 真实后端交互 | 断言要点 |
|---|---|---|---|
| 57 | 工作流控制台 | `GET /api/v1/workflows` | 授权后列表可读（`workflows.read`）；BLOCKED 占位已移除 |
| 58 | 创建工作流 | `POST /api/v1/workflows` → 201 | UI 表单（manual 触发、`tasks.command`、node-alpha、`command=uptime` 参数）；服务端列表可见 revision=1 enabled=true |
| 59 | 启停切换 | `PATCH /api/v1/workflows/{id}` ×2 → 200 | `expectedRevision` 乐观锁；停用/启用后服务端 enabled 状态与 revision 2/3 一致 |
| 60 | 手动触发运行 | `POST /api/v1/workflows/{id}/runs` → 202 | `matched=true`；页面显示服务端生成的 `tsk_` 任务 ID；运行与步骤快照以服务端为准 |
| 61 | 工作流详情 | `GET /api/v1/workflows/{id}` → 200 | 弹窗渲染工作流定义 + 运行记录（含关联任务 ID） |
| 62 | 统一任务中心 | `#/tasks/{taskId}` | 工作流提交的任务经唯一任务服务可查，无绕行执行通道 |
| 63 | 检查历史 | `GET /api/v1/availability-monitors/{id}/history` → 200 | 服务端持久化采样列表；未探测时诚实显示「暂无检查记录」 |
| 64 | 立即探测 | `POST /api/v1/availability-monitors/{id}/probe` | 未注入 NetProber → 503 `prober_unavailable` 诚实展示（HTTP 状态 + 错误码） |
| 65 | 文件字节流上传 | `POST …/files/uploads` → 201；`PUT …/content?offset=0` → 200；`POST …/complete` | DataTransfer 注入 29 B 真实文件；服务端 `bytesReceived` 与文件字节数一致、SHA-256 校验通过；传输通道未注入 → 503 `unavailable`，界面诚实区分「校验通过（29 B）」与「未送达远端」 |
| 66 | 发布执行 | `POST /api/v1/publications/{id}/run` | 未注入发布器 → 502 `stage_failed`；dns 阶段持久化为 failed、计划 partially_completed（服务端核验） |
| 67 | 阶段回滚 | `POST /api/v1/publications/{id}/rollback {stage:"dns"}` | 无补偿端口 → 502 `rollback_failed`；阶段状态刷新以服务端为准 |
| 68 | Wave 5 审计闭环 | `GET /api/v1/audit/events` | `workflows.save/trigger/task.submit`、`files.upload_create`、`publishing.stage/run/rollback` 全部落盘 |

## Wave 5 修复的既有测试问题（均在 `apps/web` 内）

1. `node-add-page.test.tsx`：「批准」按钮位于接入列表异步面板内，负载下 `getByRole` 先于渲染执行（既有竞态，与本轮改动无关）→ 改为 `findByRole` 等待出现后再断言禁用；断言语义不变。
2. `plugins-page.test.tsx` 中断言旧「BLOCKED：无工作流接口」占位的用例 → 按任务要求改为断言：无工作流接口时页面显示真实加载失败，且静态 BLOCKED 页不再存在。

## 仍然不能声称的部分（Wave 5 后依旧）

- 立即探测 503 `prober_unavailable`、上传 complete 503 `unavailable`、发布 run 502 `stage_failed`、回滚 502 `rollback_failed`：`NetProber`/`LocalStagingTransport`/DNS·证书·网关发布器均未在组合运行时注入（DS Wave 5 实现 + 总控接线范围）；界面与验收断言诚实错误，不伪造成功。
- 文件「下载」端点仍无契约；上传成功（transport 注入后）的端到端落盘读回需等总控接线后另行验收。
- `humanApproval: true` 的工作流步骤进入任务审批流的分支由任务域验收覆盖，本轮未重复断言。
- 真实供应商、实机 Nginx/Xray、真实 Agent：UNTESTED（同 Wave 4）。
