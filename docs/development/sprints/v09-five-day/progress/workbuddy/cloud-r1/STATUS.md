# V09-CLOUD-WB-01 · STATUS

- 任务 ID：`V09-CLOUD-WB-01`（返修轮 `V09-CLOUD-WB-01-R2`）
- 唯一会话：`workbuddy-01`
- 分支：`feature/cloud-console-wb01`（原分支追加，无强推）
- 工作目录（云端普通用户私有）：`/home/ubuntu/ServerFleet-WB01-parent/ServerFleet-WB01`
- 起点：`1ca79072232bfdcee2b9f4582ac020d4430d0c2b`
- 已安全同步 `main`：`cce2b290536be650c8afd4a5962a1bd5a2717276`（合并提交，非强推；仅带入主控 PR #4 的 Go 测试前置）
- R1 被审 head：`6f6dc96926cc84b8f3bb47668c9eac7612b6caa9`
- 本轮 head：见 PR 描述（提交后回填完整 40 位）
- 审查与最终合并：ChatGPT（主控），本轮不自行合并、不推 main

## 返修轮 R2：审查意见 → 处置

| 审查项 | 处置 | 可复核位置 |
|---|---|---|
| **WB-R2-01** `previewMismatches` 只检查两个已知失败项存在且原因正确，既不要求 `recovery_key_reference` 通过，也不核对失败集合恰好两项；`recovery_key_reference` 失败或缺失时返回空 mismatches，把新的恢复密钥错误记成旧 BLOCKED | 重写为四层精确核对：① 检查项名字集合必须与 `PREVIEW_CHECK_NAMES` 四项**完全相等**；② 结构合法（`name` 非空字符串、`passed` 布尔、`reason` 仅字符串、无未知字段、无重复）；③ 必需成功项 `backup_integrity` 与 `recovery_key_reference` 必须 `passed=true`（通过项不得带 reason）；④ **实际失败项集合必须恰好等于已知两项**且 reason 逐字相同。任一不满足即非空 mismatches → FAIL。 | `tests/browser/v09/run-recovery-prerequisite-probe.mjs`（`previewMismatches`） |
| **WB-R2-01** 需补新增失败/未知失败/缺失/重复项负向，并扩充控制自测 | 自测由 10 例扩到 **17 例**，新增：`recovery_key_reference` 失败（审查最小复现）、缺失 `recovery_key_reference`、缺失 `backup_integrity`、检查项重复、未知失败项、未知通过项、`passed` 非布尔、已知失败项变为通过。断言同时核对三端点状态序列与 R08 失败明细关键字。 | `tests/browser/v09/run-probe-classification-selftest.mjs` |
| **WB-R2-01** 已知恢复密钥失败不得沿用旧「未装配」结论 | `recovery_key_reference` 失败时 R08 记 FAIL，明细同时给出 `recovery_key_reference 未通过（必需成功项）：reason="recovery_key_corrupted"` 与「失败集合 ≠ 已知两项」；诊断/严格两种模式的退出语义不变 | 同上 |
| **证据 head/run 对应关系** | 旧 `41526891590` 不再作为本 head 证据；本轮以最终 head 的实际运行列表为准，PR 描述里逐条标注 head→run | 本文「远端 CI」与 PR 描述 |
| **共享前置已由主控 PR #4 合并** | 正常 `git fetch` + `git merge origin/main`（`cce2b29`），合并提交 `1cf0c1b`；不改 CI、不改根锁、不混入其他 PR | `git log --oneline`、PR 提交列表 |

针对 WB-R2-01 的**控制性复现**（审查给的最小输入：保留两个已知失败项，再令
`recovery_key_reference={passed:false,reason:"recovery_key_corrupted"}`）现得到
`R08=FAIL`、`reportedFailure=true`；删去该项同样 FAIL（`缺少必需检查项 recovery_key_reference`）。

## 返修轮 R1：审查意见 → 处置（保留）

| 审查项 | 处置 | 可复核位置 |
|---|---|---|
| **WB-R1** R08—R10 对任何 HTTP 结果都记 BLOCKED | `probeBlocked()` 唯一入口：HTTP 状态 + problem `content-type/type/status/code` + 预览检查项集合全部精确匹配才 BLOCKED，其余 FAIL 并退出 1；区分「非预期响应」与「行为已变更」 | `run-recovery-prerequisite-probe.mjs` |
| **WB-R1** 严格完整模式 | `--require-complete`：BLOCKED → 退出 1；默认诊断模式在首行、结尾、JSON 三处声明不构成完整验收 | 同上 |
| **WB-R2** P18 只比 code | 503 + problem 结构精确核对 + 1.0.3 合格对照；删除「计划通过就继续 apply」兜底 | `run-product-update-acceptance.mjs`（P18） |
| **WB-R3** 缺真实回滚 | P18B：清单声明 1.0.3、工件为真实 1.0.0 字节（签名/SHA/平台/序号合法），失败落在安装后版本校验；核对槽位字节、进程、健康、账本、阶段标记 | 同上（P18B） |
| **WB-R3** 不以标签当回滚；重启要区分场景 | P19 明确为「更新均已完成后的宿主重启」，断言失败终态持久且不重放；**kill -9 中断恢复未构造，UNTESTED** | 同上（P19） |

## 检查点

| 检查点 | 内容 | 结果 |
|---|---|---|
| CP-1 | 测试宿主可信程序注入，`SchemaFingerprint` 由宿主读真实库，退出 `Close()` | P09 通过 |
| CP-2 | Agent 组件本地签名更新浏览器专项（含计划阶段拒绝、真实回滚、终态持久） | **21 PASS / 0 FAIL**（退出 0，全量构建） |
| CP-3 | B06 恢复前置条件探测（生产同构宿主，不注入 `RecoveryExecute`） | **11 PASS / 3 BLOCKED / 0 FAIL**；诊断退出 0，严格退出 1（`INCOMPLETE`） |

## 本检查点已证明（限定范围）

1. 真实发布构建：1.0.0 与 1.0.1 字节不同，SHA 记录于证据 JSON（`e41ee3fa…` / `a7542baa…`）。
2. 真实安装：槽位 `active` 字节等于 1.0.1 构建 SHA（P16），非状态伪造。
3. 真实进程替换：`health.json` 1.0.0 → 1.0.1，pid 变化，由 `internal/updates` 自有监督器启动。
4. 浏览器驱动：管理员 check/plan/确认文本/风险勾选发起 202；**异人**管理员在独立会话审批同一
   `planDigest`，服务端拒绝自批。
5. 计划阶段拒绝：篡改工件 1.0.2 被 503 拒绝（problem 结构精确核对），无作业、产品与槽位未变。
6. **真实回滚**（P18B）：合法签名但运行版本不符的 1.0.3 安装尝试后，作业 `failed`、账本
   `rolled_back`，槽位字节恢复 1.0.1、健康 1.0.1、pid 1517376 → 1517570，阶段标记含
   `switched` 与 `rollback-switching`。
7. 终态持久：重启后 1.0.1 与 `completed`/`failed` 两个作业终态均在，作业数 2 未增加。

## B06 阻塞位置（分类严格化后仍成立）

| 观测 | 端点 | 实测（精确匹配） | 性质 |
|---|---|---|---|
| 备份校验 | `POST /api/v1/backups/{id}/verify` | `200`，`verified=true`，`backup_integrity=passed` | **可用**（真实 `VACUUM INTO` 备份） |
| 恢复预览 | `POST /api/v1/disaster-recovery/preview` | `200`，`eligible=false`，`verified=true`，4 项中**恰好** 2 项不合格：`target_controller_identity/target_identity_not_observed`、`old_controller_fenced/fencing_evidence_unavailable`；`backup_integrity` 与 `recovery_key_reference` 均通过 | **产品侧关闭**：无可信 Controller 身份观测，勾选框不构成 fence 证据 |
| 创建作业 | `POST /api/v1/disaster-recovery/jobs` | `501` + `application/problem+json` + `RECOVERY_EXECUTOR_UNAVAILABLE` | **产品侧关闭**：`RecoveryExecute` 无生产装配点 |
| 作业读回 | `GET /api/v1/disaster-recovery/jobs/{jobId}` | `404` + `RECOVERY_JOB_NOT_FOUND`（无条件） | **契约缺口** |
| 未知备份 | `POST /backups/{id}/verify` | `404 BACKUP_NOT_FOUND`，无路径/SQLite 细节 | 负向通过 |
| 未认证 | `POST /disaster-recovery/preview` | `403 NOT_AUTHORIZED`，不返回检查项 | 负向通过 |

**浏览器端到端恢复在当前 main 上不可能通过**：预览恒不合格 + 执行器未装配 + 作业不可读回。
本轮仍**未**注入自制恢复执行器——那会把验证对象换成我写的适配器。

## 检查与回归（固定工具，真实命令与退出码）

| 命令 | 退出码 | 结果 |
|---|---|---|
| `npm ci` | 0 | 127 包，按 `package-lock.json` |
| `npm run tools:install` | 0 | protoc-gen-go / staticcheck / govulncheck / actionlint |
| `npm run check`（第 1 次，07:59Z） | **1** | `go` FAIL：`internal/certificates: TestACMEDNS01PollsProcessingOrder` 恰在 5.00s 超时；policy/contracts/web/git PASS |
| 同上失败用例单跑（`-run TestACMEDNS01PollsProcessingOrder`） | 0 | `ok … 0.062s` —— 全量并发下的时序抖动，非本分支改动（该文件未改） |
| `npm run check`（第 2 次，08:02Z） | 0 | policy / contracts / go / web / git 全 PASS |
| `npm run check:security` | 0 | npm audit 0 漏洞；govulncheck `No vulnerabilities found` |
| `node tests/browser/v09/run-product-update-acceptance.mjs` | 0 | **21 PASS / 0 FAIL**（全量构建） |
| `node tests/browser/v09/run-recovery-prerequisite-probe.mjs` | 0 | 11 PASS / 3 BLOCKED / 0 FAIL（诊断，`INCOMPLETE`） |
| 同上 `--require-complete` | **1** | 同结果，BLOCKED 正确推出非零 |
| `node tests/browser/v09/run-probe-classification-selftest.mjs` | 0 | **17/17** 分类符合预期 |

`npm run check` 第 1 次的失败**如实保留**：只有 1 个用例、且位于本 PR 未改动的包，单跑 0.062s 通过、
第二次全量通过。既不隐藏，也不据此声称「一直绿」。

## 同步 main 的一处操作说明

`git merge origin/main` 首次生成的提交标题是 Git 默认的 `Merge remote-tracking branch …`，
`npm run check:policy` 的 git-policy 因此报 `Invalid commit title`。该合并提交**尚未推送**，
已就地 `--amend` 为 `chore(v09): sync origin/main cce2b29 into cloud-console branch`（父提交不变：
`6f6dc96` + `cce2b29`），随后 policy PASS。属本地未推送提交的消息修正，不涉及强推或改写远端历史。

## 明确未完成（不得据此关单）

- **B03 / B04 / B05 / B06 继续 OPEN。** 21 PASS 只覆盖「Agent 组件本地签名更新」一条链路，
  不等于 B05 关闭，更不等于 B04 发布或 B06 恢复可验收。
- Controller 组件升级、在线可信发行源、分批/金丝雀：未实现、未验证。
- **B06 实际恢复**（执行、新权威、旧主控 fence、旧信任代次拒绝、撤销/过期任务不复活）：**UNTESTED**。
- **安装执行中被强杀的中断恢复**：**UNTESTED**。P19 只覆盖「所有作业已终态后重启」，
   **不等于 kill -9 恢复，也不等于 B06 完成**。
- AC-01—24 五层证据矩阵、Live70 / Stub16 / 严格正向总套件：本轮未重跑，历史结论
  （31 PASS / 4 BLOCKED / 0 FAIL，退出 1）不变，未降门槛。逐条映射见 `AC-MATRIX.md`：
  本轮只触及 AC-13（更新中心，仅 Agent 组件一条链路）与 AC-18（恢复，仅前置条件），
  **两者仍为 NOT_ACCEPTED**；共享 `ACCEPTANCE.json` 未改。
- 后续只消费经主控审查合入 main 的发布/恢复接口；本轮不改生产 Go、公共契约、迁移、CI、依赖锁。

## 环境与可复现性

- `ubuntu` uid=1000，工作根 0700；Go `1.27.3`（`.tools/go`，`GOTOOLCHAIN=local`）；Node `v24.22.1`；
  Chrome headless + CDP（`SF_BROWSER=/usr/bin/google-chrome`）；`GOPROXY=https://goproxy.cn,direct`。
- 恢复探针与更新专项都要求 `GOROOT` 指向真实工具链根（脚本按 `$GOROOT/bin/go` 构建 fixturehost）；
  未设置时会以 `spawnSync bin/go ENOENT` 在 R00 直接 FAIL——这是脚本的前置断言，不是产品问题。
- 证据：`evidence/CP2-product-update-results.json`（21 条 + 二进制 SHA + 回滚账本）、
  `evidence/CP3-recovery-probe-results.json`（`mode=complete`、`acceptanceComplete=false`）、
  截图 3 张。脱敏扫描仅命中标题性文字，无秘密值。
