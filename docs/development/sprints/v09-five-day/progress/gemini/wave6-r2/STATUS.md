# Wave 6-R2 · V09-R6-GM-03 检查点（gemini-06）

会话唯一 ID `gemini-06`；审查人 `codex-review-02`；分支 `feature/v09-r6-console-gm06`；已同步基线 `c9013f4231c5f0a586eb0af26386f6e265996c0c`。允许目录：GM 整包 + 新增测试专用 Go 宿主 `tests/browser/v09/fixturehost/**`（依赖方向仅 fixturehost→生产，生产绝不导入）。未改产品 Go/API/Proto/根锁/CI。

## 检查点

| 检查点 | SHA | 内容 | 验证 |
|---|---|---|---|
| GM-W6-R2-01 | `d6e58d9` | **测试专用验收宿主** `tests/browser/v09/fixturehost/`（main.go + aiupstream.go）：组合正式 `server.New`（含 enrollment/Agent WSS 链路，mTLS 监听带 ClientCAs/VerifyClientCertIfGiven）+ `Config.LocalAIProvider`（loopback TLS 注入，Roots 锚定本地 CA）+ 持久化 AIBudget；进程内本地 TLS "OpenAI responses" SSE 上游（校验 Bearer 秘密、回显请求 model、输出 `decodeProviderStream` 可解码的确定性终态文本）；控制面 GET /fixture/{health,configure,upstream}（configure 触发真实 `AIBudget.Configure`——admin gate 仍执行服务端管理员/owner/profile 校验）。套件 `--ai-host` 模式：fixturehost 替代 controller 二进制（同一 Agent 组合），PKI 增发 AI 上游证书，控制面统一走 WSL 内 curl | 正向套件 26 步：**21 PASS / 5 BLOCKED / 0 FAIL**；**B02 真实浏览器 AI 生成全链路 PASS** |
| GM-W6-R2-02 | 本提交 | 双 Agent 私有 file-root 目录（agent-file-root-01/02，0700）与映射文档（B01 `--attempt-file-flow` 已由总控版本就绪：真实路径 + skip 策略）；wave6-r2 文档 | 门禁见文末 |

## B02 · 真实浏览器 AI 生成（本轮首要交付，PASS）

完整真实链路（`--ai-host`，证据 `evidence/positive-r2/`）：

1. 浏览器 bootstrap 管理员 → 合成 `ai_api_key` 凭据（幂等键提交，秘密不落日志/不回显）。
2. `PUT /api/v1/ai/model-settings`（owner CAS）→ read-back 验证 profile id。
3. 控制面触发真实 `AIBudget.Configure`（admin gate：服务端校验 actor==owner+administrator+当前启用 profile）。
4. 会话创建 → 用户消息入队（幂等键 + scopeRevision）。
5. 显式 `POST …/generate` → **真实链路**：组合内 LocalTLSProvider 经 loopback TLS 拨打本进程上游（TLS 1.3 + Roots 校验 + loopback 解析强制）→ Bearer 秘密校验通过 → SSE 流解码（sequence 严格递增、终态 response 与 delta 逐字一致）→ 撤权/配额复查 → assistant 终态 `fixture-assistant-terminal-ok` **逐字持久化**。
6. 上游证据：`requestCount=1`、`lastBearerOk=true`、`lastModel=local-test-model`（回显）。

不实之处的排除：未用 production mock、未绕过 Cookie/CSRF/审批、未直接写消息结果表、HTTP 不能指定供应商 URL（LocalTLSProviderConfig 仅程序注入）。

## B 组其余精确状态（5 BLOCKED，全部引用服务端原始响应）

| 步骤 | 精确缺口 |
|---|---|
| B01 字节互传 | 默认 HARNESS_INCOMPLETE（CLI 未装配文件 worker/路径映射/file-root）；`--attempt-file-flow` 已就绪（真实路径+skip），CX checkpoint 后执行，失败/超时判 FAIL。两进程同 WSL 只计双端协议证据 |
| B03 Xray | 501 PLUGIN_CATALOG_SOURCE_UNAVAILABLE——无已签名发行制品；安装/配置/流量 UI 已在（GM-07），首个真实门是制品目录，之后各阶段（预检/安装/用户/流量）须逐一实测，现有探针不会自动成为完整流程 |
| B04 发布/补偿 | 网关创建 409 takeover_approval_required（真实接管审批门控）；接管审批的批准人/批准后继续流程无契约（已登记 SHARED-CHANGES） |
| B05 更新 | 503 UPDATE_SOURCE_UNAVAILABLE——发行源是第一条真实门；check→plan→apply（确认+备份门控）→健康→回滚的 UI 全链路已交付（GM-08），无发行源时后续步骤无法真实执行，不做模拟 |
| B06 灾备 | 真实备份夹具 backup_integrity 通过；target_identity_not_observed + fencing_evidence_unavailable fail-closed 正确；第二隔离 Controller/fence 属 POC-08/awskr 范围 |

## Operator 间歇 403 追踪（按 R2 要求）

保留 operator 审批人角色（未改管理员）；命中时保存 `evidence/positive-r2/p13-operator-403.json`：403 响应体与 requestId、当次角色（/auth/me）、本人授权列表、节点快照、身份存储 dump。403 必须 FAIL 的语义保留（本 26 步 run 未命中，上一 run 的证据文件已入库）。未复现根因，持续追踪。

## 门禁（本轮实际执行）

- `npm run check:web` → PASS（typecheck + **340 项** + build）。
- `npm run check:go` → PASS（gofmt、go build、`go test ./...`、`go vet ./...`、staticcheck——均覆盖 fixturehost）。
- `npm run check` / `npm run check:security` → PASS（最终提交后复核，输出见最终报告）。
- 正向套件（--ai-host）→ 21 PASS / 5 BLOCKED / 0 FAIL；Stub16 16/16、Live70 70/70（上一轮验证，本轮未改其路径）。

## 剩余依赖

- B01 执行链路：CX Wave6-R2（File* 帧/CLI 文件 worker/显式映射）——`--attempt-file-flow` 就绪。
- B02 的预算 HTTP 管理路由与配置/余额/冲突 UI：CX Wave6-R2 #4 交付后前端接入（当前预算由宿主程序级配置，真实 gate 已验证）。
- B03 依赖官方制品；B04 依赖接管审批契约 + DS 发布器；B05 依赖发行源；B06 依赖 POC-08/awskr。
