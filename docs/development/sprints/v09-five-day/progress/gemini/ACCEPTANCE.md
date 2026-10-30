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
