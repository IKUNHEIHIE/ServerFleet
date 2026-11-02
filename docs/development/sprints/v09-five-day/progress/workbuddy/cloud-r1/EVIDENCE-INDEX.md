# V09-CLOUD-WB-01 · EVIDENCE INDEX

检查点 CP-2；代码 SHA 见同目录 `STATUS.md` 与 PR 描述（提交后回填完整 40 位 SHA）。

## 证据文件

| 文件 | 内容 | 校验方式 |
|---|---|---|
| `evidence/CP2-product-update-results.json` | 20 条步骤结果、环境（Node/平台/uid）、二进制 SHA-256、端口、nodeID、PID 前后、taskId | `jq '.results \| length'` = 20；`failed:false` |
| `evidence/CP2-stage-updated.png` | 更新中心在真实数据下的页面截图（辅助说明，不替代后端证据） | 人工查看 |

## 二进制与工件 SHA-256（与结果 JSON 一致）

| 工件 | SHA-256 |
|---|---|
| `fixturehost`（测试宿主） | `5dffb2f274eeca81b13ba7e30ad5f57ba07032c3d169eed6390a2c30581ac804` |
| `apps/agent` 1.0.0 发布构建 | `9cc9c76a17fee40e13f13a28dc50cac9e185fd7150c0aafdcb881556cc38ffe2` |
| `apps/agent` 1.0.1 发布构建 | `3a613a8fc6b472dea07b0a5e6ae3d40fdd2ebdba0f8a4c0259e27838a9019bdf` |

安装后的槽位 `active` 字节等于 1.0.1 的 SHA（P16 断言），不是状态伪造。

## 五层证据标注（本检查点范围）

| 层 | 本检查点状态 |
|---|---|
| 源码/模块 | `go build` + `go vet` + `gofmt` 干净；`npm run check:web` 39 文件 / 348 测试 PASS |
| 页面正常 | P10—P13、P17：真实页面完成 check/plan/apply 并显示作业终态 |
| 页面负向 | P18：篡改工件在计划阶段被拒（`UPDATE_PLANNER_UNAVAILABLE`），无作业、产品不变 |
| 真实外部 | 未覆盖：无真实供应商/外部发行源；本检查点为本地签名 inbox + 本机真实进程 |
| 发布准入 | 未闭合：Controller 组件升级/在线发行源仍缺，B05 未整体关闭 |

## 未纳入本索引的历史证据

Live70、Stub16、严格正向（31 PASS / 4 BLOCKED / 0 FAIL，退出 1）未在本检查点重跑，
其结论不因本专项而变化；不得把本专项 20 PASS 折算为上述套件通过。
