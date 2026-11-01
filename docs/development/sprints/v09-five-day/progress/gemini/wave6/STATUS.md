# Wave 6 · V09-R6-GM 检查点（gemini-06）

会话唯一 ID `gemini-06`；审查人 `codex-review-02`；分支 `feature/v09-r6-console-gm06`；目录 `D:\admin\desktop\codex\myproject\ks\.worktrees\feature--v09-r6-console-gm06`；起始基线 `73d5dd46fb29b3cf4a445b43b48186d5938fe781`（main，总控接回+awskr 授权后）。

任务单：`docs/development/sprints/v09-five-day/WAVE6.md` §GM。允许目录内工作：`apps/web/src`、`apps/web/public`（未改）、`tests/browser/v09`、`progress/gemini/wave6`（本目录）；未改 Go、契约、锁文件、CI。

## 检查点

| 检查点 | SHA | 内容 | 验证 | 状态 |
|---|---|---|---|---|
| GM-W6-01 | `9a04c87` | 新增 `tests/browser/v09/run-positive-success-acceptance.mjs`（18 步）：正常成功用例独立成套，与 70 步负向回归并存。真实链路：WSL 内交叉编译运行真实 linux/amd64 Controller+Agent（SQLite WAL 原生语义、Agent 平台上报 linux），按 `localpki` 加载格式预置本地 Agent CA 并签发 localhost 服务端证书，UI 生成一次性接入令牌 → Agent enroll(pending) → UI 审批 → enroll(issued) → UI 资源授权 → Agent 主动 WSS+mTLS（心跳/指标、节点在线）→ UI 提交 `tasks.command` 标准任务 → 真实派发/TaskOffer/TaskFinished → 任务 completed、执行 succeeded、一次性结果 tasksExecuted=1 → 审计闭环；B01–B06 为正向尝试（跨节点字节互传/AI 生成/Xray 安装/发布补偿/更新执行/恢复执行），未接线适配器按服务端精确响应记 BLOCKED，不以诚实拒绝关闭功能 | `node tests/browser/v09/run-positive-success-acceptance.mjs` → **12 PASS / 6 BLOCKED / 0 FAIL**（exit 0）；截图 `progress/gemini/evidence/positive/` | 完成 |
| GM-W6-02 | `1e16ba5` | 任务详情「逐节点执行」表新增「错误」列（真实 Controller 终态执行返回 `error`，此前被 UI 丢弃）；新增 `task-detail-page.test.tsx` 3 项，把正向套件达成的服务端现实固定为渲染契约：completed+succeeded 按真实最小 DTO 渲染（审计字段缺失诚实「—」、终态不订阅 SSE）、failed 显示服务端错误文本且绝不渲染为成功、unknown 保持「未知结果不等于成功」 | `npm test --workspace @serverfleet/web` → 38 文件 / **332 项 PASS**（+3）；typecheck/build PASS | 完成 |
| GM-W6-03 | 本提交 | 接入令牌有效期提示与真实服务端约束对齐（UI 曾写「1–60 分钟」，`ENROLLMENT_TOKEN_POLICY` 实际硬约束 1–10，`max` 属性同步改 10）；`progress/gemini/wave6/` STATUS/HANDOFF/SHARED-CHANGES 与 AC-01—24 五列证据矩阵；`progress/gemini/STATUS.md` 增 Wave 6 检查点节 | node-add-page 3 项 PASS；全量见下 | 完成 |

## 累计验证（本轮实际执行输出）

- `npm test --workspace @serverfleet/web` → **38 文件 / 332 项 PASS**（基线 329 + 本轮 3）。
- `npm run check:web` → PASS（typecheck + 332 项 + vite build）。
- `node tests/browser/v09/run-acceptance.mjs` → **16/16 PASS**（起点基线运行，防破坏回归）。
- `node tests/browser/v09/run-live-controller-acceptance.mjs` → **70/70 PASS**（Windows 真实 Controller 组合，负向/退化全量回归；起点基线运行，防破坏回归；最终增量上复跑见文末）。
- `node tests/browser/v09/run-positive-success-acceptance.mjs` → **12 PASS / 6 BLOCKED / 0 FAIL**。

## 本轮确立的真实正向事实（此前所有波次均为"诚实失败"口径）

1. **节点接入全链路真实可用**：浏览器 UI 生成一次性令牌（仅显示一次、列表响应不含令牌）→ 真实 Agent CLI（WSL Ubuntu，linux/amd64，`platform=linux` 通过 handshake 结构校验）enroll → pending → UI 审批 → 同一令牌二次调用取得签发身份（nodeId）→ 主动 WSS+mTLS 连接 → 心跳/指标使节点在线 → 任务执行后一次性退出、节点按服务端快照诚实离线。
2. **实际运维任务完成（AC-05 正向）**：`tasks.command` 标准级、无审批、经唯一任务服务派发（`agentLinkBridge.Dispatch` → TaskOffer → Agent 自检执行 → TaskFinished → `RecordResult`）→ 任务 `completed`、执行 `succeeded`、Agent 一次性结果 `tasksExecuted=1`。
3. **诚实缺口清单**（B01–B06，未把 501/503/500 反向通过）：跨节点文件字节通道（500 internal_error，另登记 SHARED-CHANGES §2）、AI 模型生成（503 `MESSAGE_SOURCE_UNAVAILABLE`）、Xray 发行制品（501 `PLUGIN_CATALOG_SOURCE_UNAVAILABLE`）、服务发布/补偿（本套件无服务组/网关/DNS 数据集；70 步已覆盖其诚实门控与补偿失败）、更新执行（503 `UPDATE_SOURCE_UNAVAILABLE`）、恢复执行（404 `BACKUP_NOT_FOUND`，且需第二隔离主控）。

## 与 Wave 1–5 口径的差异说明

- Wave 5 及之前，AC-05 的"无 Agent 派发器，审批后停留 queued"是组合运行时真实行为；本轮同一组合入口（`bootstrap.Compose`）在提供本地 PKI 与 authority 时自动挂载 Agent WSS/enrollment 且 `agentBridge` 充当默认任务派发器——因此正常成功链路在本机即可真实达成，无需改 Go 代码或接线新适配器。
- 70 步负向回归未做任何修改（保留全部"正确拒绝"验证）；正向套件与其互为补充，脚本头注释写明分工。

## 剩余依赖与边界

- CX Wave 6 未合并（凭据密钥/Agent 常驻/模型执行器/Xray 执行器/更新源仍开发中，其 worktree 至 `73d5dd4` 尚无已提交差异）；DS Wave 6 同。前端对齐与对应正向成功用例的翻转依赖总控集成后的统一 SHA。
- awskr 实机、双架构/1C1G、外部供应商（Cloudflare/ACME/QQ/SMTP）、Xray 真实实例、更新签名基础设施、第二隔离主控：未授权/未提供，相应 AC 条目如实留缺口，见 `AC-EVIDENCE-WAVE6.md`。
