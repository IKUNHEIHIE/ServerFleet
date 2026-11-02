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
