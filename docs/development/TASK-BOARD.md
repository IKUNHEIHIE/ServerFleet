# 任务登记板

更新：2026-10-29。项目负责人已要求审核首批三项并分配下一步；总控不自动启动或代发消息给其他 Agent。G2 未冻结，真实节点/生产操作仍未授权。

| 任务 | 状态 | 唯一会话 | 分支 / Worktree | 允许写入 | 审查人 / 基线 |
|---|---|---|---|---|---|
| M0-P1 | 已完成 | codex-p1-local | main / 主目录 | 原 P1 范围 | 交付 15814c6 |
| G2-R01 | 审查通过，仅接收候选/评审工件；不批准运行时提案或冻结 | codex-01 | contract/g2-review-cx01 / `D:\admin\desktop\codex\myproject\ks.worktrees\contract--g2-review-cx01` | 原三项授权目录 | 总控；被审查提交 8821555 |
| N01 / N01-R1 | 原测试通过，但边界复现失败，待修复复审，未合入 main | deepseek-01 | feature/nodes-domain-ds01 / `.worktrees/feature--nodes-domain-ds01` | `internal/nodes/`；R1 明确允许修订本分支新增 Rename 内部方法，但 ports.go 不变 | 总控；被审查提交 04c45e9 |
| W01 / W01-R1 | 原测试通过，但无效/过期指标映射失败，待修复复审，未合入 main | gemini-01 | feature/web-nodes-mock-gm01 / `.worktrees/feature--web-nodes-mock-gm01` | `apps/web/src/features/nodes/` | 总控；被审查提交 c6302a9 |
| BATCH01-REVIEW | 审查与验证完成，接收G2-R01，返修任务已分配；交接后停止 | codex-review-01 | integration/batch01-review / `.worktrees/integration--batch01-review` | 审查文档、任务板、AGENTS/README 状态、契约检查接线；只合入 G2-R01 工件 | 项目负责人验收；起始 15814c6 |
| T01-A | 已分配，待人工交给会话；尚未启动 | codex-01（原任务结束后续接，不能同时运行） | feature/proto-validation-cx01 / `.worktrees/feature--proto-validation-cx01` | `internal/transport/` 内新语义校验器、测试、testdata；ports.go 不变 | 总控独立审查；审查集成后的 main 基线 |

N01-R1/W01-R1 是原任务返修，继续原分支/Worktree，不重置或重建，保留原提交记录；不要把 main 的候选资料 merge 进返修分支来混入无关文件。需要审查材料时只读访问主目录绝对路径。

T01-A 从接收 G2-R01 工件并接好检查后的 main 创建。任意会话开始前都核对路径/分支及 `SECOND-BATCH-WORK-PACKAGES.md` 的范围。已有三个开发 Worktree 保留；不清理其他会话的依赖、报告或未保存内容。GitHub 远程、真实 PR、线上 CI 均未配置/运行。
