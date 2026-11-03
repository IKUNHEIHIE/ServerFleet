# V09-CLOUD-WB-01 · EVIDENCE INDEX

返修轮 R2（`V09-CLOUD-WB-01-R2`）；代码 SHA 见 `STATUS.md` 与 PR 描述（提交后回填完整 40 位 SHA）。

## 证据文件

| 文件 | 内容 | 校验方式 |
|---|---|---|
| `evidence/CP2-product-update-results.json` | 21 条步骤结果（含 P18B 真实回滚）、环境（Node/平台/uid）、二进制 SHA-256、端口、nodeID、PID 前后、`rollback` 账本摘要 | `jq '.results \| length'` = 21；`jq '.failed'` = false |
| `evidence/CP2-stage-updated.png` | 更新中心真实数据页面截图（辅助，不替代后端证据） | 人工查看 |
| `evidence/CP3-recovery-probe-results.json` | 14 条结果；`mode=complete`、`acceptanceComplete=false`、`verdict=INCOMPLETE`；R08—R10 为精确匹配后的 BLOCKED | `jq '.counts'` = `{pass:11, blocked:3, fail:0}` |
| `evidence/CP3-backups-page.png` | 「备份与恢复」页面：标注契约缺口，未出现裸「恢复成功」陈述 | 人工查看 |
| `evidence/CP3-anonymous-denied.png` | 未认证会话访问恢复预览被 403 拒绝的页面实况 | 人工查看 |

## 二进制与工件 SHA-256（与结果 JSON 一致）

| 工件 | SHA-256 |
|---|---|
| `fixturehost`（测试宿主） | `99ffcf6fa0dab6072384a6c14698a5156fcdf8ceaee262d5823e58ad98230a12` |
| `apps/agent` 1.0.0 发布构建 | `e41ee3fa4762c279141c795bea439cdbe098ac4d84af3ede5436157497fe9ddd` |
| `apps/agent` 1.0.1 发布构建 | `a7542baa96bb673c60d311b7c12ee28cff776a8f205f48e060017a49de8f17f2` |
| 回滚候选 1.0.3 的工件 | `e41ee3fa4762c279141c795bea439cdbe098ac4d84af3ede5436157497fe9ddd`（= 1.0.0 字节；声明版本 1.0.3，故签名/摘要合法而运行版本不符） |

安装后槽位 `active` 字节 = 1.0.1 的 SHA（P16 断言）；回滚后再次断言为 1.0.1 且 ≠ 候选 SHA（P18B）。

## P18B 回滚证据链（不依赖状态标签）

| 事实 | 观测值 |
|---|---|
| 作业终态 | `failed`（仅作为必要条件，不作为充分证据） |
| 安装账本 | `receipt.State=rolled_back`、`Attempted.version=1.0.3`、`Current.version=1.0.1`、`Health.Version=1.0.1` |
| 阶段标记 | 同目录存在 `switched.json` 与 `rollback-switching.json` → 候选确实被切上去过，再由产品自己切回 |
| 槽位字节 | `a7542baa…`（1.0.1），且 ≠ 候选 `e41ee3fa…` |
| 进程 | pid 1517376 → 1517570（回滚重启），`/proc/<pid>/exe` → `slot/active` |
| 健康 | `version=1.0.1`，`pid` 与 `receipt.Health.ProcessID` 一致 |

## R08 预览判据（WB-R2-01 后）

`previewMismatches()` 只有同时满足下列条件才返回空数组（才允许记 BLOCKED）：

1. `HTTP 200`、body 为 JSON 对象、`eligible=false`、`verified=true`；
2. `checks` 为数组，项名集合**恰好**是 `backup_integrity`、`target_controller_identity`、
   `old_controller_fenced`、`recovery_key_reference` 四项（少一项、多一项、重复都不行）；
3. 每项结构合法：`name` 非空字符串、`passed` 布尔、`reason` 仅字符串、无未知字段；
4. 必需成功项 `backup_integrity`、`recovery_key_reference` 均为 `passed=true` 且无 reason；
5. 实际**失败项集合恰好**是 `target_controller_identity/target_identity_not_observed` 与
   `old_controller_fenced/fencing_evidence_unavailable`，reason 逐字相同。

控制性自测：`node tests/browser/v09/run-probe-classification-selftest.mjs` → **17/17 PASS**，
其中包含审查给的两个最小复现（`recovery_key_reference` 失败、删去该项），均得到 R08=FAIL。

## 五层证据标注（本检查点范围）

| 层 | 本检查点状态 |
|---|---|
| 源码/模块 | `npm run check` 退出 0：policy / contracts / go / web / git 全 PASS（第二跑；第一跑有 1 个 ACME 时序用例超时，单跑 0.062s 通过，见 `STATUS.md`） |
| 页面正常 | P10—P13、P17：页面完成 check/plan/apply 并显示作业终态 |
| 页面负向 | P18（计划阶段 503 拒绝）、P18B（安装后版本不符 → 真实回滚）、P19（失败终态持久且不重放） |
| 真实外部 | 未覆盖：无真实供应商/外部发行源；本检查点为本地签名 inbox + 本机真实进程 |
| 发布准入 | 未闭合：Controller 组件升级/在线发行源仍缺，B05 未整体关闭 |

## 未纳入本索引的历史证据

Live70、Stub16、严格正向（31 PASS / 4 BLOCKED / 0 FAIL，退出 1）本轮未重跑，结论不因本专项变化；
不得把本专项 21 PASS 折算为上述套件通过，也不得据此关闭 B03/B04/B05/B06。
