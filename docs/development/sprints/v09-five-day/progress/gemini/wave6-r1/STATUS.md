# Wave 6-R1 · V09-R6-GM-02 检查点（gemini-06）

会话唯一 ID `gemini-06`；审查人 `codex-review-02`；分支 `feature/v09-r6-console-gm06`；已同步基线 `379d5e0e4818844eb44d8ac13010f251d215d8c0`（总控接收三线 Wave6 候选后的统一 SHA，含 e7f15f7/cc36d59/9d3aa2a 三处审查修正）。允许目录沿用 GM 整包：`apps/web/src`、`tests/browser/v09`、`progress/gemini/wave6-r1`；未改 Go/契约/锁文件/CI。

## 检查点

| 检查点 | SHA | 内容 | 验证 |
|---|---|---|---|
| GM-W6-R1-01 | `fa112f2` | AI 模型设置界面 + 显式生成：`GET/PUT /api/v1/ai/model-settings` 前端接入（owner CAS/expectedRevision、凭据下拉只列 ai_api_key+ai 元数据、绝不显示 BYOK 明文、409 MODEL_REVISION_CONFLICT 诚实重读、非管理员只读）；AI 会话页每条用户消息的显式「生成回复」按钮（POST …/generate 无请求体，渲染缓冲至终态的完整脱敏消息，失败按错误码诚实显示，明确标注非逐 token 流）；`CredentialMetadata` 补服务端实际返回的 `revision`；测试助手登录响应改用注入身份 | Web 39 文件 / **338 项 PASS**（+6）；typecheck/build PASS |
| GM-W6-R1-02 | `7d52dbb` | 正向套件重构为完整流程（24 步）：P02 升级为真实 `location.reload()` CSRF 恢复（断言 /auth/me 实际返回 csrfToken + 写锁横幅消失）；P13 真实失败任务页面（异人批准的 host.patch 被真实 Agent ack CAPABILITY_REJECTED → target failed，页面渲染失败终态与逐节点执行说明）；P14 撤权隔离（任务读取 403、403 不清认证）移至末尾；P15-P18 双真实 Agent（第二个完整接入/审批/签发、双 WSS+mTLS 链路同时在线、SHA-256 可核对夹具）；B01-B06 结构化（双端字节互传/AI 设置→入队→显式 generate/服务组→网关→发布预演/真实 VACUUM INTO 备份→恢复预演 fail-closed） | 18 PASS / 6 BLOCKED / 0 FAIL；results.json status=PARTIAL |
| GM-W6-R1-03 | `b7dae0a` | P19 模型设置面板真实 UI CAS 保存（revision 推进、密钥仅显示引用）；P13 审批人流程加固（全 UI 驱动 + 会话守卫 P13b 防级联；operator 角色授权异常按状态 dump 证据记 BLOCKED 移交总控，见 SHARED-CHANGES §1）；失败取证脚本 identity-dump.mjs | 26 步：**20 PASS / 6 BLOCKED / 0 FAIL**；`--require-complete` 实测以退出码 1 拒绝 PARTIAL |
| 本提交 | 见 git log | wave6-r1 文档（本目录 STATUS/HANDOFF/SHARED-CHANGES）+ AC 矩阵增补 | 门禁输出见文末 |

## 累计验证（本轮实际执行）

- `node tests/browser/v09/run-positive-success-acceptance.mjs` → **20 PASS / 6 BLOCKED / 0 FAIL**（26 步；results.json status=PARTIAL、complete=false）。
- `node tests/browser/v09/run-positive-success-acceptance.mjs --require-complete` → **退出码 1**（BLOCKED 存在时拒绝全量通过；已实测）。
- `node tests/browser/v09/run-acceptance.mjs` → **16/16 PASS**（负向/退化回归未动）。
- `node tests/browser/v09/run-live-controller-acceptance.mjs` → **70/70 PASS**（负向/退化回归未动；总控三处修正保留）。
- `npm run check:web` → PASS（typecheck + 338 项 + vite build）。
- `npm run check` → PASS；`npm run check:security` → PASS（输出见提交说明/最终报告）。

## B 组六项的当前精确状态（全部 BLOCKED，无一人以诚实拒绝关闭）

| 步骤 | 已建立的真实结构 | 精确缺口（服务端原始响应） |
|---|---|---|
| B01 跨节点字节互传 | 两个真实在线 Agent（独立 WSS+mTLS）+ 双端私有目录夹具 + 源 SHA-256 | 传输受理（xfer_…）后 60s 未完成/403/500：WSS 文件字节链路未接线（CX File* 帧与 CLI 能力、DS 协调器默认拒绝 File*） |
| B02 AI 生成终态 | 合成 ai_api_key 凭据（幂等键提交）→ owner CAS 设置（read-back 验证）→ 用户消息入队 | 显式 generate → 503 GENERATION_OUTCOME_UNKNOWN（默认配额拒绝 + 上游端点固定 api.openai.com；本地 TLS 供应商注入属 CX-R1+总控）；入队不等于生成 |
| B03 Xray 安装 | 目录刷新探针 | 501 PLUGIN_CATALOG_SOURCE_UNAVAILABLE（无已签名发行制品） |
| B04 发布/补偿 | 服务组（在线节点）→ 网关安装 | 网关创建 409 takeover_approval_required（既有站点接管需人工准入——真实安全门控）；发布/补偿后继流程据此止步 |
| B05 更新执行 | check/plan 探针 | 503 UPDATE_SOURCE_UNAVAILABLE / UPDATE_PLANNER_UNAVAILABLE |
| B06 恢复执行 | 真实 VACUUM INTO 备份夹具 | backup_integrity 通过 + target_identity_not_observed + fencing_evidence_unavailable（fail-closed 正确）；第二隔离主控属 POC-08/awskr 范围 |

## 新增正向证据（相对 GM-02 开始时）

1. **真实页面 reload 的 CSRF 恢复**（P02）：`location.reload()` 后 /auth/me 实际返回 csrfToken、写锁不出现——此前 P02 只证明导航保持会话。
2. **实际失败任务页面**（P13）：异人批准的 host.patch 被在线真实 Agent 拒绝（CAPABILITY_REJECTED），任务 failed，页面渲染失败终态与逐节点执行说明——此前只有成功链路。
3. **撤权隔离**（P14）：撤销授权后任务读取 403、页面诚实显示、403 不清认证。
4. **两个真实在线 Agent 同时持有链路**（P17）+ 双端 SHA 夹具（P18）——B01 的接线前置条件全部就绪。
5. **AI 模型设置 owner CAS 的真实 UI 保存**（P19）：面板显示服务端状态、CAS 保存后修订推进、密钥仅显示引用。

## 剩余依赖与边界

- B01/B02 的翻转依赖 CX Wave6-R1（File* 帧接线、Agent 文件读写、本地 TLS 供应商注入）与 DS Wave6-R1（协调器授权/逐帧校验）+ 总控组合；B03 依赖官方制品发布；B04 后继依赖 DS 的 Agent apply/reload/restore 与接管审批流程；B05 依赖发布基础设施；B06 依赖 POC-08/第二隔离主控（awskr 范围，仅总控操作）。
- operator 角色资源授权异常（SHARED-CHANGES §1）属服务端 Go 域问题，已按证据移交，本套件以 BLOCKED 语义承载。
- 未自行合并 main、未操作 awskr、未使用真实外部秘密；awskr 既有服务未触碰（ENV-W6-01 为总控观测项）。
