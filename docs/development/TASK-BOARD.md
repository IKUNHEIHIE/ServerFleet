# 任务登记板

更新：2026-10-29。负责人已提供第二批精确提交进行复审；三项交付通过，首批5个问题闭环，第三批离线任务已分配。总控不自动启动或代发消息给其他Agent。G2未冻结，真实节点/生产操作仍未授权。

| 任务 | 状态 | 唯一会话 | 分支 / Worktree | 允许写入 | 审查人 / 基线 |
|---|---|---|---|---|---|
| M0-P1 | 已完成 | codex-p1-local | main / 主目录 | 原 P1 范围 | 交付 15814c6 |
| G2-R01 | 审查通过，仅接收候选/评审工件；不批准运行时提案或冻结 | codex-01 | contract/g2-review-cx01 / `D:\admin\desktop\codex\myproject\ks.worktrees\contract--g2-review-cx01` | 原三项授权目录 | 总控；被审查提交 8821555 |
| N01 / N01-R1 | 复审通过，3个P2关闭，接收原实现与返修 | deepseek-01 | feature/nodes-domain-ds01 / `.worktrees/feature--nodes-domain-ds01` | 原任务范围，已完成停止 | 总控；原04c45e9、返修b848104、最终0fa6f88 |
| W01 / W01-R1 | 复审通过，2个P2关闭，接收原组件与返修 | gemini-01 | feature/web-nodes-mock-gm01 / `.worktrees/feature--web-nodes-mock-gm01` | 原任务范围，已完成停止 | 总控；原c6302a9、返修86d4722 |
| BATCH01-REVIEW | 审查与验证完成，接收G2-R01，返修任务已分配；交接后停止 | codex-review-01 | integration/batch01-review / `.worktrees/integration--batch01-review` | 审查文档、任务板、AGENTS/README 状态、契约检查接线；只合入 G2-R01 工件 | 项目负责人验收；起始 15814c6 |
| T01-A | 复审通过，接收离线校验候选；没有真实通信/执行准入 | codex-01 | feature/proto-validation-cx01 / `D:\admin\desktop\codex\myproject\ks.worktrees\feature--proto-validation-cx01` | 原任务范围，已完成停止 | 总控；fd918d1，基线36ab5cf |
| BATCH02-REVIEW | 三项独立复审/组合检查通过，交接后停止 | codex-review-01 | integration/batch02-review / `.worktrees/integration--batch02-review` | 集成三项已审查提交；审查文档/任务板/README，准备下一批Worktree | 项目负责人验收；起始36ab5cf |
| AUTH-01A | 已分配，待人工派发，尚未启动 | codex-01 | feature/auth-policy-cx01 / `.worktrees/feature--auth-policy-cx01` | `internal/auth/`新增策略/错误/测试，不改ports.go | 总控；第二批集成后的main |
| A01 | 已分配，待人工派发，尚未启动 | deepseek-01 | feature/audit-domain-ds01 / `.worktrees/feature--audit-domain-ds01` | `internal/audit/`新增校验/视图/测试适配，不改ports.go | 总控；第二批集成后的main |
| W02 | 已分配，待人工派发，尚未启动 | gemini-01 | feature/web-node-filters-gm01 / `.worktrees/feature--web-node-filters-gm01` | `apps/web/src/features/nodes/`搜索/筛选包装及测试 | 总控；第二批集成后的main |

第二批已完成，原分支/Worktree保留以便追溯，不重置、不清理其他会话的依赖、报告或未保存内容。T01-A的实际移动路径已经只读核对并同步，未由总控移动。第三批每项使用新分支/Worktree，不在旧任务目录继续叠加无关代码。

开始前核对路径、分支和HEAD，阅读 `THIRD-BATCH-WORK-PACKAGES.md`。第三批任务目录从第二批审查集成后的main准备。GitHub远程、真实PR、线上CI均未配置/运行；没有自动启动开发会话。
