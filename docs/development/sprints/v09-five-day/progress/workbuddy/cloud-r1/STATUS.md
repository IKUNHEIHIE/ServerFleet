# V09-CLOUD-WB-01 · STATUS

- 任务 ID：`V09-CLOUD-WB-01`（返修轮 `V09-CLOUD-WB-01-R1`）
- 唯一会话：`workbuddy-01`
- 分支：`feature/cloud-console-wb01`（原分支追加，无强推）
- 工作目录（云端普通用户私有）：`/home/ubuntu/ServerFleet-WB01-parent/ServerFleet-WB01`
- 统一起点：`1ca79072232bfdcee2b9f4582ac020d4430d0c2b`（与派发基线一致，远端 main 未漂移）
- 被审 head：`d0dd938b01f8c7467993ade3425c13bb12847aa0`
- 审查与最终合并：ChatGPT（主控），本轮不自行合并、不推 main

## 返修轮 R1：审查意见 → 处置

| 审查项 | 处置 | 可复核位置 |
|---|---|---|
| **WB-R1** R08—R10 对任何 HTTP 结果都记 BLOCKED（500/401/403、错误 code、错误 shape 都能伪装成「未实现」） | 分类器重写：新增 `probeBlocked()` 唯一入口，只有 **HTTP 状态 + problem content-type/type/status/code + 预览检查项集合** 全部精确匹配才记 BLOCKED；其余一律 FAIL 并以 1 退出。三种成因分开标注：「非预期响应」（回归/环境）与「行为已变更」（产品已实现，B06 结论必须重估，仍是 FAIL）。附带修掉一个真 bug：`extra.status` 曾覆盖判定字段，把 FAIL 写成 HTTP 码 | `tests/browser/v09/run-recovery-prerequisite-probe.mjs`（`probeBlocked` / `problemMismatches` / `previewMismatches`） |
| **WB-R1** 需要严格完整模式，BLOCKED 返回非零；非严格模式必须声明不代表完整验收 | 新增 `--require-complete`：任何 BLOCKED 使退出码为 1。默认诊断模式的输出首行、结尾、JSON 三处都声明 `mode` / `acceptanceComplete` / `verdict`，并写明「不构成完整验收、不得作为发布准入证据」 | 同上（`mode`、`summary.modeNote`） |
| **WB-R2** P18 只比 `code=UPDATE_PLANNER_UNAVAILABLE`，不限状态 | 改为 `expectUpdateProblem()`：503 + `application/problem+json` + `type=about:blank` + `status=503` + `code` 全部核对；并增加**合格对照**（1.0.3 计划必须 200 且 eligible），否则 503 无法排除「planner 根本没装配」。原先「计划若通过就继续 apply」的兜底分支已删除——篡改工件进入 apply 即缺陷，直接 FAIL | `tests/browser/v09/run-product-update-acceptance.mjs`（P18） |
| **WB-R3** 只验证了计划阶段拒绝，没有真实回滚 | 新增 **P18B**：`1.0.3` 的 manifest 声明 1.0.3、工件却是**真实 1.0.0 字节**（签名、SHA-256、平台、序号全部合法），因此失败只能发生在安装后的运行版本校验，触发产品自己回滚。核对旧槽位字节 SHA、槽位字节 ≠ 候选、健康版本、pid 变化、`/proc/<pid>/exe` 指向 `slot/active`、安装账本 `receipt.State=rolled_back` + `Current=1.0.1` + 阶段标记 `switched`/`rollback-switching`、作业终态 `failed` | 同上（P18B） |
| **WB-R3** 不能把 failed/unknown/rolled_back 任一标签当回滚完成；重启要区分「已完成后重启」与「执行中断恢复」 | P18B 不以标签为证据，全部用文件系统与进程事实；P19 明确改名为「更新均已完成后的宿主重启」，并额外断言失败作业终态持久、作业总数不增（不重放）。**「安装执行中被 kill -9 的中断恢复」未构造，标为 UNTESTED**，不得用 P19 冒充 | 同上（P18B、P19） |
| **检查/证据** 固定工具下 `npm ci`、`tools:install`、`check`、`check:security` 与浏览器回归；基线失败准确保留；不称整包完成 | 全部执行，见下「检查与回归」。B03/B04/B05/B06 保持 OPEN | 本节与 `EVIDENCE-INDEX.md` |

针对 WB-R1 的**回归自测**已固化为可重复脚本（不启动产品、只测分类器）：
`node tests/browser/v09/run-probe-classification-selftest.mjs` → 10/10 符合预期，退出 0。
其中第一条正是审查给的复现口径（三端点全 500），现在得到 `FAIL/FAIL/FAIL`，`reportedFailure=true`。

## 检查点

| 检查点 | 内容 | 结果 |
|---|---|---|
| CP-1 | 测试宿主可信程序注入（`server.Config.UpdateProduct`），`SchemaFingerprint` 由宿主从真实库读取，退出时 `Close()` | 构建/vet 通过；P09 通过 |
| CP-2 | Agent 组件本地签名更新浏览器专项（原生 Linux，含计划阶段拒绝 + 真实回滚 + 重启持久） | **21 PASS / 0 FAIL**（退出 0，全量构建） |
| CP-3 | B06 灾难恢复前置条件探测（生产同构宿主，不注入恢复执行器） | **11 PASS / 3 BLOCKED / 0 FAIL**；诊断模式退出 0，严格模式退出 1（INCOMPLETE） |

## 本检查点已证明（限定范围）

1. 真实发布构建：两个 `apps/agent` 构建字节不同，SHA 记录在证据 JSON（`bb0edd8f…` / `91e18531…`）。
2. 真实安装：槽位 `active` 字节等于目标构建 SHA-256（P16），非状态伪造。
3. 真实进程替换：`health.json` 版本 1.0.0 → 1.0.1，`pid` 改变，进程由 `internal/updates` 自有进程监督器启动。
4. 浏览器驱动：管理员在页面 check/plan/确认文本/风险勾选发起 202；**异人**管理员在独立会话审批同一 `planDigest`；服务端拒绝自批。
5. 计划阶段拒绝：篡改工件的 1.0.2 在 plan 即被 503 拒绝（problem 结构精确核对），无作业、产品与槽位未变。
6. **真实回滚**（P18B）：合法签名但运行版本不符的 1.0.3 完成安装尝试后，作业 `failed`、账本 `rolled_back`，槽位字节恢复为 1.0.1、健康 1.0.1、pid 由 1164115 变为 1164306，进程镜像指向 `slot/active`，阶段标记含 `switched` 与 `rollback-switching`。
7. 终态持久：重启后 1.0.1 与 `completed`/`failed` 两个作业终态均在，作业总数未增加（不重放）。

## B06 阻塞位置（分类严格化后仍成立）

| 观测 | 端点 | 实测（精确匹配） | 性质 |
|---|---|---|---|
| 备份校验 | `POST /api/v1/backups/{id}/verify` | `200`，`verified=true`，`backup_integrity=passed` | **可用**（真实 `VACUUM INTO` 备份，含 `schema_migrations` 账本） |
| 恢复预览 | `POST /api/v1/disaster-recovery/preview` | `200`，`eligible=false`，`verified=true`，检查项 4 项中恰 2 项不合格：`target_controller_identity/target_identity_not_observed`、`old_controller_fenced/fencing_evidence_unavailable` | **产品侧关闭**：无可信 Controller 身份观测，勾选框不构成 fence 证据 |
| 创建作业 | `POST /api/v1/disaster-recovery/jobs` | `501` + `application/problem+json` + `RECOVERY_EXECUTOR_UNAVAILABLE` | **产品侧关闭**：`server.Config.RecoveryExecute` 无生产装配点 |
| 作业读回 | `GET /api/v1/disaster-recovery/jobs/{jobId}` | `404` + `RECOVERY_JOB_NOT_FOUND`（无条件） | **契约缺口**：即便作业创建成功也读不回状态与阶段 |
| 未知备份 | `POST /backups/{id}/verify` | `404 BACKUP_NOT_FOUND`，无路径/SQLite 细节 | 负向通过 |
| 未认证 | `POST /disaster-recovery/preview` | `403 NOT_AUTHORIZED`，不返回检查项 | 负向通过 |

**浏览器端到端恢复在当前 main 上不可能通过**：不是宿主或页面缺件，而是预览恒不合格 + 执行器未装配 + 作业不可读回。本任务**没有**注入自制恢复执行器——那会把验证对象换成我写的适配器。

## 检查与回归（固定工具，真实命令与退出码）

| 命令 | 退出码 | 结果 |
|---|---|---|
| `npm ci` | 0 | 127 包，按 `package-lock.json`；提示 `@bufbuild/buf` postinstall 未自动执行（不影响后续，`tools:install` 与 contracts 均通过） |
| `npm run tools:install` | 0 | `protoc-gen-go` / `staticcheck` / `govulncheck` / `actionlint` 装入 `.tools/bin` |
| `npm run check` | 0 | `policy PASS`、`contracts PASS`、`go PASS`、`web PASS`、`git PASS` |
| `npm run check:security` | 0 | `npm audit --audit-level=high`：0 漏洞；`govulncheck ./...`：No vulnerabilities found |
| `node tests/browser/v09/run-product-update-acceptance.mjs` | 0 | **21 PASS / 0 FAIL**（全量构建） |
| `node tests/browser/v09/run-recovery-prerequisite-probe.mjs` | 0 | 11 PASS / 3 BLOCKED / 0 FAIL（诊断模式，`verdict=INCOMPLETE`） |
| `node tests/browser/v09/run-recovery-prerequisite-probe.mjs --require-complete` | **1** | 同上结果，`mode=complete`、`acceptanceComplete=false` —— 严格模式下 BLOCKED 正确推出非零 |
| `node tests/browser/v09/run-probe-classification-selftest.mjs` | 0 | 10/10 分类符合预期 |

`check:go` 依赖真实预构建产物。本轮通过提供**真实构建**满足，不是跳过或降门槛：
`SF_R2_AGENT_BINARY`、`SF_R3_AGENT_BINARY`、`SF_R3_CONTROLLER_BINARY`、`SF_R3_AGENT_V1`、`SF_R3_AGENT_V2`
分别指向本次真实 `apps/agent` / `apps/controller` 构建。未提供这些变量时，相应用例会以
「explicit prebuilt actual Agent binary is required」失败——这是基线环境依赖，不是本分支回归。

依赖真实 Nginx 的网关用例在本容器（无 nginx）仍不执行；此前一轮的 BLOCKED 结论不变，未越界修改。

## 明确未完成（不得据此关单）

- **B03 / B04 / B05 / B06 继续 OPEN。** 本专项 21 PASS 只覆盖「Agent 组件本地签名更新」这一条链路，
  不等于 B05 关闭，更不等于 B04 发布或 B06 恢复可验收。
- Controller 组件升级、在线可信发行源、分批/金丝雀：未实现、未验证。
- **B06 实际恢复**（执行、新权威、旧主控 fence、旧信任代次拒绝、撤销/过期任务不复活）：**UNTESTED**。
- **安装执行中被强杀的中断恢复**：**UNTESTED**。P19 只覆盖「所有作业已终态后重启」。
- AC-01—24 五层证据矩阵、Live70 / Stub16 / 严格正向总套件：本轮未重跑，历史结论
  （31 PASS / 4 BLOCKED / 0 FAIL，退出 1）不变，未降门槛。
- 后续只消费经主控审查合入 main 的发布/恢复接口；本轮不改生产 Go、公共契约、迁移、CI、依赖锁。

## 环境与可复现性

- `ubuntu` uid=1000，工作根 0700；Go `1.27.3 linux/amd64`（`.tools/go` → 已下载 toolchain，`GOTOOLCHAIN=local`）；
  Node `v24.22.1`；Chrome headless + CDP（`SF_BROWSER=/usr/bin/google-chrome`）；
  `GOPROXY=https://goproxy.cn,direct`。
- 本轮容器重建后系统 Go（1.21.4）与 Node（18/22）低于 `go.mod` 与 `scripts/tool-versions.json` 要求，
  已按固定版本补齐；`.tools/` 在 `.gitignore` 内，不进入提交。
- 证据：`evidence/CP2-product-update-results.json`（21 条结果 + 二进制 SHA + 回滚账本）、
  `evidence/CP3-recovery-probe-results.json`（`mode=complete`、`acceptanceComplete=false`）、
  截图 `CP2-stage-updated.png`、`CP3-backups-page.png`、`CP3-anonymous-denied.png`。
  脱敏扫描：仅命中标题性文字（「setup token 摘要」「0600 合成 secret」），无任何秘密值。
