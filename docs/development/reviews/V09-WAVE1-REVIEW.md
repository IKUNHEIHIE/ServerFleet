# V09-WAVE1-REVIEW · v0.9 五日冲刺第一波三线交付独立审查与合入报告

日期：2026-10-31（Asia/Shanghai）；审查任务 `V09-WAVE1-REVIEW`；总控审查人 `agy-review-01`（承接 `codex-review-01` 会话 `01a183b6-bd14-71f3-ad6b-e2d88655f67e` 审查规程）；起始基线 `main` 为 `8a7dd4707d2d7e142e137d098296788d2c80eddd`。

## 1. 决定与接收边界

**ACCEPT WAVE 1 & ADVANCE TO WAVE 2**：完整审查并接收三位作者在各自独立 Worktree 提交的 v0.9 第一波复合实施代码：
- **`V09-5D-CX`（`codex-02` / `codex-03`）**：分支 `feature/v09-platform-cx02`，精确提交 `aef07e543a154cc8d907dc178cb7ecdaee957877`（38 个增量提交 R01–R38，196 个文件）。
- **`V09-5D-DS`（`deepseek-02`）**：分支 `feature/v09-services-ds02`，精确提交 `07f4154a28e794c8addb31b74100135e5d5009ea`（16 个提交，133 个文件）。
- **`V09-5D-GM`（`gemini-02`）**：分支 `feature/v09-console-gm02`，精确提交 `86e0cf5687a98ccdded3f3bf96420d2666eddf51`（6 个提交，121 个文件）。

只读核验确认三个作者工作区均干净（`git status --short` 为空），三分支修改文件集合两两交集为 **0**（共 450 个独立路径），69 个受保护原始参考文件与 3 份字节不变 OpenAPI 候选契约未发生任何改动。总控在独立集成工作区 `.worktrees/integration--v09-wave1-review` 完成三线合入、3 项共享协调修正与全量检查验证后合入 `main`，随即推进 **Wave 2（`apps/controller` 组合接线与 Web/API 端到端对齐）**。

本报告接收 Wave 1 模块实现与测试工件，**不批准 G2 契约冻结、运行时 WASM/Eino 正式 profile 或真实云/VPS 生产准入**；`D01–D07` 及 `P1-C10` 仍保持 `OPEN`。

## 2. 三线交付独立审查摘要

### 2.1 `V09-5D-CX`（平台安全、传输、插件、AI、工作流、更新恢复与 Agent）
- **目录与边界**：196 个变更文件全部位于授权的 `internal/{auth,transport,plugins,ai,workflows,updates,recovery}`、`plugins/xray-manager`、`apps/agent`、`tests/{integration,poc}/v09/platform`、`docs/contracts/revisions/v09-five-day-cx02`、`docs/development/sprints/v09-five-day/progress/codex` 内；未触碰根配置、`go.mod` 或其他作者目录。
- **架构与安全不变量**：
  - `internal/auth`：实现持久态 `IdentityService`、`SQLIdentityStore`、`CredentialService` 及候选 `IdentityHTTP`（`/api/v1/auth/*`、`users`、`roles`、`permissions`、`resource-grants`、`credentials`）。强制要求直连 TLS（`r.TLS != nil`）、`BrowserGuard`（`Origin` 精确匹配）、`__Host-sf_session`（`HttpOnly; Secure; SameSite=Strict`），写操作强制校验常数时间 `X-CSRF-Token` 与 `Idempotency-Key`。`Authorize` 默认拒绝且对管理员同样要求显式节点/组授权，无角色名通配绕过。
  - `internal/transport` 与 `apps/agent`：实现单活连接代数对账、mTLS 节点证书绑定与轮换、出站任务分派与有界输出；`apps/agent/runtime` 在保持默认无参空壳模式（兼容 P1 骨架测试）的同时，支持显式配置下的受控工作目录、mTLS/CA 校验与优雅排空。
  - `internal/plugins` 与 `plugins/xray-manager`：仅实现声明式 Manifest 校验、Ed25519 签名与 SHA-256 哈希门禁、权限确认及可选 `xray-manager` 声明式扩展；核心包零导入 `plugins/xray-manager`，未擅定 WASM 运行时。
  - `internal/{ai,workflows,updates,recovery}`：AI 默认零资源授权、快照绑定与敏感环境变量剥离，高权限/root 动作强制转人工审批；更新与恢复强制要求先决备份/隔离校验。

### 2.2 `V09-5D-DS`（SQLite WAL 存储与 12 个业务服务域）
- **目录与边界**：133 个变更文件覆盖 `internal/{storage,nodes,servicegroups,tasks,audit,files,monitoring,notifications,quota,dns,certificates,gateway,publishing}`、`tests/{integration,poc}/v09/services` 与进度文档，外加经 `SHARED-CHANGES.md` 申报的 `go.mod` / `go.sum`（纯 Go `modernc.org/sqlite v1.61.0`）。
- **架构与安全不变量**：
  - `internal/storage`：封装 SQLite WAL、`busy_timeout`、外键约束、单写事务 `InTx`、13 个域迁移与在线快照备份/恢复校验；业务包无直接导入驱动。
  - `internal/{nodes,servicegroups,tasks,audit}`：节点乐观锁 revision、预检先行 `ReadPolicy` 过滤、手动/自动分组与服务组快照；任务维持普通并发 5、root/修改类 1→5、未启动 15 分钟 TTL、未知结果（`OutcomeUnknown`）不自动重跑及 SSE 投影；审计 `SQLRecorder` 支持同事务 `AppendTx` 与敏感字段脱敏。
  - `internal/{files,monitoring,notifications,quota,dns,certificates,gateway,publishing}`：路径穿越/符号链接防护、监控新鲜度区分、通知 SSRF 防护、配额阈值动作、Cloudflare DNS 最小作用域凭据、证书预检、网关转发 preview/apply、发布五阶段向导全部具備正向与负向单测。
  - **外部凭据核查**：`tests/poc/v09/services/cloudflare/cloudflare_test.go` 与 `monitoring/live_test.go` 均通过环境变量门控（未配置即 `t.Skip`），无硬编码真实 Token 或外网直连。

### 2.3 `V09-5D-GM`（React + TypeScript 完整管理台与本地浏览器验收）
- **目录与边界**：121 个变更文件覆盖 `apps/web/src/**`（15 个功能模块、34 个测试文件共 282 项单测）与进度文档，外加 4 个本地环回浏览器验收脚本。
- **架构与安全不变量**：
  - 统一通过 `apps/web/src/lib/api/client.ts` 调用 Controller REST/SSE，自动附带 `X-Requested-With: ServerFleetWeb`、`credentials: "include"`、`cache: "no-store"` 及内存态 `X-CSRF-Token`；零 `localStorage` 凭据存储，零直连 Agent WebSocket。
  - 严格执行 401（全局跳登录且不循环重试）、403（保留会话、清空当前越权视图）、503（保留会话并提示重试）与 `viewEpoch`（`epoch:resumeEpoch`）跨账号/BFCache 视图世代隔离。

## 3. 总控协调与共享修正（`5d103a0`）

总控在 `.worktrees/integration--v09-wave1-review` 合入三分支后，对 `SHARED-CHANGES.md` 申报项及跨线检查项进行了如下精确处置（提交 `5d103a01b96e18c9cb3126312e01292733241967`）：
1. **接收 `go.mod` / `go.sum` 纯 Go SQLite 依赖**：核准 DS 引入的 `modernc.org/sqlite v1.61.0`（`CGO=0`），`go mod verify`、`go mod tidy -diff` 与 `govulncheck ./...` 全部通过。
2. **归位 GM 浏览器验收脚本至授权目录**：将 GM 置于 `apps/web/acceptance/{cdp-driver,fixtures,harness,stub-server}.mjs` 的 4 个环回验收脚本通过 `git mv` 移入 `PLAN.md` 授权的 `tests/browser/v09/` 目录，并同步修正 `tests/browser/v09/stub-server.mjs` 的 `distDir` 相对路径，保持 `apps/web` 工作区纯净。
3. **重算 `g2-r03` 节点读取候选来源哈希**：DS-02 为 `internal/nodes/node.go` 增加持久化字段（`CountryCode`、`CreatedAt`、`UpdatedAt`、`eventSeq`），改变了该源文件 SHA-256。总控按 `G2-R02A-REVIEW` 规程运行固定生成器 `node tests/contracts/g2r03/build-candidate.mjs --write` 更新 `docs/contracts/revisions/g2-r03/provenance.json`（`internalNodesModel` 哈希由 `05a65927…` 更新为 `c61c768a…`）；候选契约 `api/openapi/revisions/g2-r03/nodes-read-candidate.yaml`（SHA-256 `dcb499b108fc6eab67412e08833100f9cb13c93d843863661faab56535180cfd`）与 `MATRIX.md` 字节完全不变。
4. **登记 DS 历史提交标题多 scope 说明**：DS 分支上有 5 个中间提交使用了逗号分隔的 scope（如 `feat(gateway,publishing):`）。当前 `scripts/git-policy.mjs` 默认检查 `HEAD` 通过；后续分支与总控提交严格执行单一小写字母数字连字符 scope。

## 4. 组合验证结果

在技术代码 HEAD `5d103a01b96e18c9cb3126312e01292733241967` 上使用固定工具链（Go `1.27.3`、Node `24.22.1`、Redocly `2.72.0`、Buf `1.75.0`）实跑全套检查：

| 检查项 | 实际结果 |
|---|---|
| `npm ci` | PASS，127 个包精确安装 |
| `node scripts/check.mjs all` (`npm run check`) | PASS：`policy`、`contracts`、`g2r02`、`g2r03`、`go`、`web`、`git` 全部通过 |
| 原始资料与候选契约完整性 | PASS：69 个原始参考文件、3 份字节不变候选、`g2-r02`（147 路径/185 操作）与 `g2-r03`（2 路径/2 操作 + 14 项 JS + Go 边界）全部通过 |
| 架构与依赖方向检查 | PASS：`tests/architecture`（含 13 新业务域及禁止核心导入 `plugins/xray-manager`）与 `scripts/web-boundaries.mjs` 全部通过 |
| Go 全量测试与静态检查 | PASS：`gofmt`、`go mod verify`、`go mod tidy -diff`、`go vet`、`staticcheck` 零告警；**546 个顶层测试 + 1,116 个子测试（共 1,662 项）全部 PASS，0 失败** |
| Web 类型检查、单测与生产构建 | PASS：`tsc --noEmit` 通过；**34 个测试文件、282 项单测全部 PASS，0 失败**；Vite 生产构建成功 |
| `node scripts/check.mjs security` (`npm run check:security`) | PASS：`npm audit --audit-level=high` 0 漏洞；`govulncheck ./...` 无漏洞 |

机器可读摘要见 [V09-WAVE1-RESULTS.json](V09-WAVE1-RESULTS.json)。

## 5. 下一步：推进 Wave 2（`apps/controller` 组合接线与端到端联调）

Wave 1 各模块在各自包内已具备完整实现与单测，但 `apps/controller/main.go` 仍为仅暴露 `/healthz` 的 P1 骨架，且 `SHARED-CHANGES.md` 登记的跨模块接缝（S01–S11）需在 Wave 2 由总控完成组合接线：
1. **`apps/controller` 组合入口**：接入 `internal/storage` SQLite WAL 与全量迁移（13 个 DS 迁移 + CX 身份/平台持久化表），组装 `audit.SQLRecorder`、`auth.SQLIdentityStore`、`auth.IdentityService`（含 `ResourceResolver` 桥接 `nodes`/`servicegroups`、组合 `Authorizer` 与 `PolicyReader`）及 `auth.IdentityHTTP`，并挂载 `nodes`、`servicegroups`、`tasks`、`files`、`audit`、`dns`、`notifications`、`gateway` 真实 HTTP 路由（同时保留默认无参 `/healthz` 空壳兼容模式）。
2. **Web 会话与初始化契约对齐（S01）**：对齐 `apps/web/src/lib/api/{dto,endpoints}.ts` 与 `apps/web/src/app/session.tsx`，使 `GET /api/v1/auth/me` 在服务端返回 `csrfToken` 时安全恢复内存态 CSRF 令牌，且 `POST /api/v1/auth/bootstrap` 自动携带 `Idempotency-Key` 并兼容 `204 No Content` 响应后完成登录衔接。
3. **真实端到端集成测试**：新增 Controller HTTPS + SQLite WAL 端到端集成测试，验证初始化引导、登录、会话恢复、节点/分组/服务组授权读取、任务预览/提交/审批/SSE 与审计查询完整闭环。
