# V09-CLOUD-WB-01 · SHARED-CHANGES

本检查点**没有**修改任何中央共享文件。以下为需要主控知悉的变更面与理由。

## 1. 变更文件清单

| 文件 | 类型 | 说明 |
|---|---|---|
| `tests/browser/v09/fixturehost/product_update.go` | 新增 | 私有 0600 JSON → `updates.ProductDeploymentOptions` 的可信程序注入；严格形态校验 |
| `tests/browser/v09/fixturehost/main.go` | 修改 | 新增 `-product-update-config`；SchemaFingerprint 由真实库读取；退出路径调用 `composed.Close()` |
| `tests/browser/v09/run-product-update-acceptance.mjs` | 新增 | 原生 Linux B05 专项（P00—P19），浏览器驱动，报告 PASS/FAIL/BLOCKED |
| `apps/web/src/features/updates/updates-page.tsx` | 修改 | 移除「无 backupId 即禁止 apply」的前置阻断；改为如实提示服务端备份 |
| `apps/web/src/features/updates/updates-page.test.tsx` | 修改 | 单测同步为新行为，仍断言未确认时禁用且不发请求 |
| `docs/.../progress/workbuddy/cloud-r1/**` | 新增 | 本任务独占进度与证据 |
| `tests/browser/v09/run-recovery-prerequisite-probe.mjs` | 新增（CP-3） | B06 前置条件探测（R00—R13）；生产同构启动，不注入恢复执行器 |

## 2. 未触碰的共享面

- `apps/web/package.json`、根 `package.json`、`package-lock.json`：**未改**
- `go.mod` / `go.sum`：**未改**
- `api/openapi/**`、`proto/**`、`gen/go/**`：**未改**
- 公共 schema / 迁移序列 / `ds` 编号：**未改**
- CI、`scripts/**`、`dependencies.json`、`buf*.yaml`、`redocly.yaml`：**未改**
- 根 `AGENTS.md`、`docs/development/TASK-BOARD.md`：**未改**（按派发要求，中央任务板由主控维护）
- 其他作者进度目录：**未改**

## 3. 对产品契约的差异说明

无契约变更。`/api/v1/system/updates/{check,plan,apply,jobs}` 的请求/响应形状未改；
本检查点只消费既有端口。

## 4. 需要主控裁决的一处 UI 行为变化

原页面逻辑：`plan.backupRequired === true` 且历史作业列表中没有 `backupId` → 「一键更新」
永久禁用。该逻辑使**第一次真实升级无法发起**，与本轮实测的服务端行为（`apply` 返回 202
`awaiting_approval`，备份在 `backing_up` 阶段创建）矛盾。

现行为：提示「本次升级需要备份」，不禁用；仍需确认文本逐字匹配 + 风险勾选。

- 影响面：仅更新中心页面与其单测，未改服务端与契约。
- 回退方式：还原 `updates-page.tsx` 两处（`backupPending` 计算与对应提示），以及
  `updates-page.test.tsx` 的第一条用例。
- 证据：`evidence/CP2-product-update-results.json` 的 P12/P13/P17（计划合格 → 202 → completed）。

## 5. 生产代码导入方向

`fixturehost` 只单向导入生产包（`server`、`updates`、`ai`、`files`），生产代码不导入该包；
未新增任何无认证 HTTP 配置入口，未新增直连 Agent/DB 旁路，未引入 fixture 配置旁路。

## 6. B06 需要上游补齐的三项（精确到端点、状态码与代码接缝）

CP-3 实测后，B06 的前端与宿主侧已无缺件；剩余阻塞全部在 `internal/**`（本任务禁止修改）：

| # | 缺口 | 实测证据 | 代码接缝 | 归属 |
|---|---|---|---|---|
| 1 | 恢复预览恒不合格 | `POST /api/v1/disaster-recovery/preview` → `200`，`eligible=false`，未通过项 `target_controller_identity/target_identity_not_observed`、`old_controller_fenced/fencing_evidence_unavailable` | `internal/recovery/http.go` 的 `preview` 中两项 `Passed: false` 为常量；需要可信 Controller 身份观测与真实 fence 证据才能置真 | AgentMore |
| 2 | 创建作业恒 501 | `POST /api/v1/disaster-recovery/jobs` → `501 RECOVERY_EXECUTOR_UNAVAILABLE` | `server.go:89` 声明 `RecoveryExecute`，`updates_recovery.go:56` 接到 `recovery.Options.Execute`，但全仓**无生产赋值点** | AgentMore（执行器语义）+ 主控（装配决策） |
| 3 | 作业状态不可读回 | `GET /api/v1/disaster-recovery/jobs/{jobId}` → `404 RECOVERY_JOB_NOT_FOUND`，无条件 | `internal/recovery/http.go` 的 `job` handler 只有一行写 404 | 需契约决策：实现读回，或在 OpenAPI 标注该端点未开放 |

在 1 与 2 补齐前，任何「浏览器恢复成功」的声明都不可能成立；本任务不会以页面出现或 `202`
受理来替代证据。3 若不解决，恢复过程对管理员不可观测，验收只能停在「已受理」。

## 7. 返修轮 R1 的增量变更（仍无共享面改动）

| 文件 | 类型 | 说明 |
|---|---|---|
| `tests/browser/v09/run-recovery-prerequisite-probe.mjs` | 修改 | R08—R10 严格分类（`probeBlocked`/`problemMismatches`/`previewMismatches`）；新增 `--require-complete`；修复 `extra.status` 覆盖判定字段 |
| `tests/browser/v09/run-product-update-acceptance.mjs` | 修改 | P18 严格断言 + 合格对照；删除「计划通过则继续 apply」兜底；新增 P18B 真实回滚；P19 增加失败终态持久与作业数不增 |
| `tests/browser/v09/run-probe-classification-selftest.mjs` | 新增 | WB-R1 回归自测：vm 抽取 R08—R10 片段，10 组响应断言分类；不启动产品 |

未触碰：`package.json` / `package-lock.json`、`go.mod` / `go.sum`、`api/**`、`gen/**`、迁移、
CI、`scripts/**`、任务板、其他作者目录。`.tools/`（工具链链接与已装工具）在 `.gitignore` 内，不提交。

对产品行为无新增假设：P18B 只消费既有 `UpdateProduct` 装配路径与 `internal/updates` 的
回滚语义（与 main 的 `update_product_r3_test.go` 同源），未新增接口、未改契约、未注入恢复执行器。
