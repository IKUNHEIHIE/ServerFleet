# 任务登记板

更新：2026-10-29。第六批TASK-01A/W04-R1部分验收通过，W04-R01关闭，组合与安全检查通过。负责人确认Codex的G2-R02A仍在工作，本轮未审查、不改原任务或目录。DeepSeek/Gemini后续TASK-01B/W05已分配，待人工派发。G2未冻结，真实节点/生产操作未授权；总控不自动启动或代发其他会话消息。

| 任务 | 状态 | 唯一会话 | 分支 / Worktree | 允许写入 | 审查人 / 基线 |
|---|---|---|---|---|---|
| M0-P1 | 已完成 | codex-p1-local | main / 主目录 | 原 P1 范围 | 交付 15814c6 |
| G2-R01 | 审查通过，仅接收候选/评审工件；不批准运行时提案或冻结 | codex-01 | contract/g2-review-cx01 / `D:\admin\desktop\codex\myproject\ks.worktrees\contract--g2-review-cx01` | 原三项授权目录 | 总控；被审查提交 8821555 |
| N01 / N01-R1 | 复审通过，3个P2关闭，接收原实现与返修 | deepseek-01 | feature/nodes-domain-ds01 / `.worktrees/feature--nodes-domain-ds01` | 原任务范围，已完成停止 | 总控；原04c45e9、返修b848104、最终0fa6f88 |
| W01 / W01-R1 | 复审通过，2个P2关闭，接收原组件与返修 | gemini-01 | feature/web-nodes-mock-gm01 / `.worktrees/feature--web-nodes-mock-gm01` | 原任务范围，已完成停止 | 总控；原c6302a9、返修86d4722 |
| BATCH01-REVIEW | 审查与验证完成，接收G2-R01，返修任务已分配；交接后停止 | codex-review-01 | integration/batch01-review / `.worktrees/integration--batch01-review` | 审查文档、任务板、AGENTS/README 状态、契约检查接线；只合入 G2-R01 工件 | 项目负责人验收；起始 15814c6 |
| T01-A | 复审通过，接收离线校验候选；没有真实通信/执行准入 | codex-01 | feature/proto-validation-cx01 / `D:\admin\desktop\codex\myproject\ks.worktrees\feature--proto-validation-cx01` | 原任务范围，已完成停止 | 总控；fd918d1，基线36ab5cf |
| BATCH02-REVIEW | 三项独立复审/组合检查通过，交接后停止 | codex-review-01 | integration/batch02-review / `.worktrees/integration--batch02-review` | 集成三项已审查提交；审查文档/任务板/README，准备下一批Worktree | 项目负责人验收；起始36ab5cf |
| AUTH-01A | 审查通过，接收离线策略/错误分类；已完成停止 | codex-01 | feature/auth-policy-cx01 / `D:\admin\desktop\codex\myproject\ks.worktrees\feature--auth-policy-cx01` | 原任务范围，ports.go不变 | 总控；8e87570，起始a103c5f |
| A01 | 审查通过，接收校验/受限视图/测试适配；已完成停止 | deepseek-01 | feature/audit-domain-ds01 / `.worktrees/feature--audit-domain-ds01` | 原任务范围，ports.go不变 | 总控；d99d7ef，起始a103c5f |
| W02 | 审查通过，接收局部搜索/筛选；已完成停止 | gemini-01 | feature/web-node-filters-gm01 / `.worktrees/feature--web-node-filters-gm01` | 原任务范围 | 总控；5c6ceff，起始a103c5f |
| BATCH03-REVIEW | 三项独立审查及组合/安全检查通过，交接后停止 | codex-review-01 | integration/batch03-review / `.worktrees/integration--batch03-review` | 只读审查精确提交、集成通过项；审查证据/任务板/README，准备下一批目录 | 项目负责人验收；起始a103c5f |
| T01-B | 审查通过，接收有界离线帧解码候选；完成停止 | codex-01 | feature/frame-decoder-cx01 / `D:\admin\desktop\codex\myproject\ks.worktrees\feature--frame-decoder-cx01` | 原任务范围；ports/Proto/gen不变 | 总控；3488197，起始ff66dea |
| N02 / N02-R1 | 复审通过，N02-R01关闭，接收原N02及返修；完成停止 | deepseek-01 | feature/nodes-auth-errors-ds01 / `.worktrees/feature--nodes-auth-errors-ds01` | 原范围，两个testkit文件调整获批 | 总控；原ea7ed5c、返修fdeb3e6，保留历史 |
| W03 | 审查通过，接收只读详情/列表选择；完成停止 | gemini-01 | feature/web-node-details-gm01 / `.worktrees/feature--web-node-details-gm01` | 原任务范围 | 总控；64465c4，起始ff66dea |
| BATCH04-REVIEW | 审查完成：接收T01-B/W03，N02退回；交接后停止 | codex-review-01 | integration/batch04-review / `.worktrees/integration--batch04-review` | 独立审查/隔离探针、集成通过项；审查证据/任务板/README及下一批目录 | 项目负责人验收；起始ff66dea |
| T01-C | 审查通过，接收离线单连接对账/序列候选；完成停止 | codex-01 | feature/connection-guard-cx01 / `D:\admin\desktop\codex\myproject\ks.worktrees\feature--connection-guard-cx01` | 原任务范围 | 总控；b118aa6，起始189112e |
| W04 / W04-R1 | 复审通过，W04-R01关闭，接收原W04与返修；完成停止 | gemini-01 | feature/web-node-accessibility-gm01 / `.worktrees/feature--web-node-accessibility-gm01` | 原features/nodes及忽略reports临时验收范围 | 总控；原8e95c3c、返修b315046，保留历史 |
| BATCH05-REVIEW | 审查完成：接收T01-C及N02-R1，W04退回；交接后停止 | codex-review-01 | integration/batch05-review / `.worktrees/integration--batch05-review` | 精确提交审查/隔离验证、合入通过项；证据/任务板/README、P1-C10登记及下一批目录 | 项目负责人验收；起始189112e |
| G2-R02A | 进行中（负责人本轮明确确认），尚未交付审查，原范围不变 | codex-01 | contract/nodes-read-cx01 / `D:\admin\desktop\codex\myproject\ks.worktrees\contract--nodes-read-cx01` | 仅新增docs/contracts/revisions/g2-r03、api/openapi/revisions/g2-r03、tests/contracts/g2r03 | 总控/节点/Web契约审查；保持原基线0fef8ba，不要求同步main |
| TASK-01A | 审查通过，接收纯快照/等待规则；完成停止 | deepseek-01 | feature/task-waiting-rules-ds01 / `.worktrees/feature--task-waiting-rules-ds01` | 原internal/tasks范围；ports.go不变 | 总控；0204d29，起始0fef8ba |
| BATCH06-PARTIAL-REVIEW | 两项部分验收通过，交接后停止；不代表整个第六批完成 | codex-review-01 | integration/batch06-partial-review / `.worktrees/integration--batch06-partial-review` | 审查TASK-01A/W04-R1、隔离验证/合入通过项、审查证据/任务板/README及两位后续目录；不干预Codex在编任务 | 项目负责人验收；起始0fef8ba |
| TASK-01B | 已分配，待人工派发，尚未启动 | deepseek-01 | feature/task-batch-rules-ds01 / `.worktrees/feature--task-batch-rules-ds01` | internal/tasks新增默认分批/后批推进纯规则及测试；不改ports.go或调度实现 | 总控任务/安全审查；本次部分验收后的main |
| W05 | 已分配，待人工派发，尚未启动 | gemini-01 | feature/web-node-loader-gm01 / `.worktrees/feature--web-node-loader-gm01` | features/nodes注入只读来源的局部加载容器/测试/说明；不接App/真实API | 总控UI/数据隔离审查；本次部分验收后的main |

原分支/Worktree保留以便追溯，不重置、不清理其他会话依赖、报告或未保存内容。TASK-01B/W05用新目录，不在已完成目录叠加无关任务；Codex继续原G2-R02A目录、分支和基线，不读取未提交契约供新任务使用。

DeepSeek/Gemini开始前核对路径、分支和HEAD，读`PARALLEL-FOLLOWUP-01.md`及`reviews/BATCH-06-PARTIAL-REVIEW.md`。Codex仍以SIXTH-BATCH-WORK-PACKAGES的G2-R02A为准。N02及W04问题已关闭，真实身份/策略存储/HTTP仍未接入；GitHub远程/真实PR/线上CI未配置或运行，没有自动启动开发会话。
