# V09-CLOUD-WB-01 · EVIDENCE INDEX

返修轮 R1（`V09-CLOUD-WB-01-R1`）；代码 SHA 见 `STATUS.md` 与 PR 描述（提交后回填完整 40 位 SHA）。

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
| `fixturehost`（测试宿主） | `f551e7d3f56b6f48c05011ce59b11b9f5cb57e89e662216e5a0dc6d560c3c3f6` |
| `apps/agent` 1.0.0 发布构建 | `bb0edd8fbbc15be9cc43e503f428f5b2399268c45b7310aa6b371629394422ba` |
| `apps/agent` 1.0.1 发布构建 | `91e18531634facdd0d120cf51e7db672352144079527fc33f602ae2f18aa0e13` |
| 回滚候选 1.0.3 的工件 | `bb0edd8fbbc15be9cc43e503f428f5b2399268c45b7310aa6b371629394422ba`（= 1.0.0 字节；声明版本 1.0.3，故签名/摘要合法而运行版本不符） |

安装后槽位 `active` 字节 = 1.0.1 的 SHA（P16 断言）；回滚后再次断言为 1.0.1 且 ≠ 候选 SHA（P18B）。

## P18B 回滚证据链（不依赖状态标签）

| 事实 | 观测值 |
|---|---|
| 作业终态 | `failed`（仅作为必要条件，不作为充分证据） |
| 安装账本 | `slot/update-update-176a2ebb…/receipt.json`：`State=rolled_back`、`Attempted.version=1.0.3`、`Current.version=1.0.1`、`Health.Version=1.0.1` |
| 阶段标记 | 同目录存在 `switched.json` 与 `rollback-switching.json` → 候选确实被切上去过，再由产品自己切回 |
| 槽位字节 | `91e18531…`（1.0.1），且 ≠ 候选 `bb0edd8f…` |
| 进程 | pid 1164115 → 1164306（回滚重启），`/proc/<pid>/exe` → `slot/active` |
| 健康 | `version=1.0.1`，`pid` 与 `receipt.Health.ProcessID` 一致 |

## 五层证据标注（本检查点范围）

| 层 | 本检查点状态 |
|---|---|
| 源码/模块 | `npm run check` 退出 0：policy / contracts / go / web / git 全 PASS |
| 页面正常 | P10—P13、P17：页面完成 check/plan/apply 并显示作业终态 |
| 页面负向 | P18（计划阶段 503 拒绝）、P18B（安装后版本不符 → 真实回滚）、P19（失败终态持久且不重放） |
| 真实外部 | 未覆盖：无真实供应商/外部发行源；本检查点为本地签名 inbox + 本机真实进程 |
| 发布准入 | 未闭合：Controller 组件升级/在线发行源仍缺，B05 未整体关闭 |

## 未纳入本索引的历史证据

Live70、Stub16、严格正向（31 PASS / 4 BLOCKED / 0 FAIL，退出 1）本轮未重跑，结论不因本专项变化；
不得把本专项 21 PASS 折算为上述套件通过，也不得据此关闭 B03/B04/B05/B06。
