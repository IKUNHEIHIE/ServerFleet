# 任务登记板

更新：2026-10-29。W05-R2复审通过，接收原W05及两次返修，W05-R01/R02均CLOSED；只接收隔离加载容器，无真实来源/App接线。READ-PLAN-01因字段/权威来源混淆和遗漏onlineOnly兼容两处P2退回，未合入或提交负责人批准。DeepSeek原分支R1返修，Gemini转W06浏览器验收；Codex继续READ-SEC-01，本轮未检查其未提交内容。G2/D01–D07/P1-C10仍OPEN，不自动启动/代发其他会话消息，真实节点/生产操作未授权。

| 任务 | 状态 | 唯一会话 | 分支 / Worktree | 允许写入 | 审查人 / 基线 |
|---|---|---|---|---|---|
| READSEC01-REVIEW | 审查中，交接后停止 | codex-review-01 | integration/readsec01-review / `.worktrees/integration--readsec01-review` | 精确审查安全证据/引用原文、合入通过文档、证据/任务板/README及收口状态；不干预READ-PLAN-01-R1/W06，不启动新实现 | 项目负责人验收；起始ddf8c5b |
| FOLLOWUP03-REVIEW | 审查/组合/安全检查通过：接收W05/R2，READ-PLAN-01退回，交接后停止 | codex-review-01 | integration/followup03-review / `.worktrees/integration--followup03-review` | 精确审查/引用与UI复验、合入通过项、证据/任务板/README/P1-C10补记及两位后续安排；不干预READ-SEC-01 | 项目负责人验收；起始7ef7877 |
| N03A-REVIEW | 独立审查/组合/安全检查通过，交接后停止 | codex-review-01 | integration/n03a-review / `.worktrees/integration--n03a-review` | 精确审查N03-A/隔离探针、合入通过项、证据/任务板/README及Codex后续任务/目录；不干预READ-PLAN-01/W05-R2 | 项目负责人验收；起始98a1f6b |
| FOLLOWUP02-REVIEW | 审查/组合/安全检查完成：接收TASK-01C、W05-R1继续返修，交接后停止 | codex-review-01 | integration/followup02-review / `.worktrees/integration--followup02-review` | 核对TASK-01C实际交付、精确审查W05-R1、隔离探针/合入通过项、证据/任务板/README及两位后续安排；不干预N03-A | 项目负责人验收；起始8b2d8aa |
| AUTH01B-REVIEW | 独立审查/组合/安全检查通过，交接后停止 | codex-review-01 | integration/auth01b-review / `.worktrees/integration--auth01b-review` | 精确审查AUTH-01B/隔离探针、合入通过项、证据/任务板/README及Codex下一步任务/目录；不干预TASK-01C/W05-R1 | 项目负责人验收；起始a652696 |
| FOLLOWUP01-REVIEW | 审查完成：接收TASK-01B、W05退回；组合/安全检查通过，交接后停止 | codex-review-01 | integration/followup01-review / `.worktrees/integration--followup01-review` | 精确审查/隔离探针、合入通过项、证据/任务板/README及两位后续任务/目录；不干预AUTH-01B | 项目负责人验收；起始1f8f8cb |
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
| G2-R02A | 审查通过，仅接收评审候选/工件；D01–D07未批准，完成停止 | codex-01 | contract/nodes-read-cx01 / `D:\admin\desktop\codex\myproject\ks.worktrees\contract--nodes-read-cx01` | 原三个r03新增目录；作者未改共享文件 | 总控；8165057，起始0fef8ba |
| TASK-01A | 审查通过，接收纯快照/等待规则；完成停止 | deepseek-01 | feature/task-waiting-rules-ds01 / `.worktrees/feature--task-waiting-rules-ds01` | 原internal/tasks范围；ports.go不变 | 总控；0204d29，起始0fef8ba |
| BATCH06-PARTIAL-REVIEW | 两项部分验收通过，交接后停止；不代表整个第六批完成 | codex-review-01 | integration/batch06-partial-review / `.worktrees/integration--batch06-partial-review` | 审查TASK-01A/W04-R1、隔离验证/合入通过项、审查证据/任务板/README及两位后续目录；不干预Codex在编任务 | 项目负责人验收；起始0fef8ba |
| TASK-01B | 审查通过，接收默认分批与当前批纯判定；完成停止 | deepseek-01 | feature/task-batch-rules-ds01 / `.worktrees/feature--task-batch-rules-ds01` | 原internal/tasks范围，ports/快照/等待规则未改 | 总控任务/安全审查；e23a4c5，起始adce532 |
| W05 / W05-R1 / W05-R2 | 复审通过，接收原实现与两次返修；W05-R01/R02关闭，完成停止 | gemini-01 | feature/web-node-loader-gm01 / `.worktrees/feature--web-node-loader-gm01` | 原features/nodes范围，未接App/真实API | 总控UI/数据隔离审查；原0ebe812、R1 54df33e、最终fcb9382 |
| G2-R02A-REVIEW | 独立复审/组合检查通过；D08技术协调完成，交接后停止 | codex-review-01 | integration/batch06-contract-review / `.worktrees/integration--batch06-contract-review` | 审查/合入候选、check.mjs接线、两处Web来源注释/固定生成器重算provenance、审查/任务板/README/P1-C10及Codex后续目录 | 项目负责人验收；起始adce532 |
| AUTH-01B | 审查通过，接收离线预检/不可变范围；完成停止 | codex-01 | feature/auth-read-preflight-cx01 / `D:\admin\desktop\codex\myproject\ks.worktrees\feature--auth-read-preflight-cx01` | 原internal/auth新增文件范围，现有auth/端口/其他模块未改 | 总控授权/安全审查；14b9eac，起始1f8f8cb |
| TASK-01C | 审查通过，接收不可变顺序批次进度；完成停止 | deepseek-01 | feature/task-batch-progress-ds01 / `.worktrees/feature--task-batch-progress-ds01` | 原internal/tasks新增文件范围，现有规则/端口未改 | 总控任务/安全审查；实际交付e169afa，起始a652696 |
| N03-A | 审查通过，接收离线预检先行读取用例；完成停止 | codex-01 | feature/nodes-read-usecase-cx01 / `D:\admin\desktop\codex\myproject\ks.worktrees\feature--nodes-read-usecase-cx01` | 原internal/nodes新增文件范围；现有文件/端口/真实接线未改 | 总控授权/节点安全审查；f57fe0d，起始8b2d8aa |
| READ-PLAN-01 / READ-PLAN-01-R1 | 退回，RP01-R01/R02两个P2待修；原分支追加R1，待人工派发 | deepseek-01 | docs/node-read-plan-ds01 / `.worktrees/docs--node-read-plan-ds01` | 原三份方案文档事实/旧参数/验收/决策表述修正；不改代码/候选或批准状态 | 总控产品/契约/安全审查；原d0709fc，起始98a1f6b |
| READ-SEC-01 | 已分配，本轮未收到交付，保持原任务 | codex-01 | docs/read-security-evidence-cx01 / `D:\admin\desktop\codex\myproject\ks.worktrees\docs--read-security-evidence-cx01` | 仅docs/contracts/decisions/node-read-security-01/新增来源/安全验收/交接3份文档；不改既有文件/代码或批准状态 | 总控安全/契约审查；7ef7877 |
| W06 | 已分配，待人工派发，尚未启动 | gemini-01 | chore/web-node-browser-qa-gm01 / `.worktrees/chore--web-node-browser-qa-gm01` | 仅新增features/nodes/BROWSER-ACCEPTANCE.md，忽略reports临时预览/脚本/截图/trace；不改生产组件/App/依赖 | 总控UI/安全审查；本次FOLLOWUP03交接后的main |

原分支/Worktree保留以便追溯，不重置、不清理其他会话依赖、报告或未保存内容。W05旧目录停止，Gemini转新W06目录；DeepSeek原READ-PLAN-01目录追加R1，Codex保持READ-SEC-01原7ef7877基线/目录，不要求同步main。本轮未改作者生产源码；总控复验只写忽略reports。

DeepSeek/Gemini读`PARALLEL-FOLLOWUP-04.md`及`reviews/FOLLOWUP-03-REVIEW.md`；Codex保持原READ-SEC-01范围。N03-A/W05接收事实可从主目录复审记录核对，不代表真实接线/浏览器已验收。方案/安全证据未形成批准包，D01–D07及P1-C10仍OPEN，身份/策略存储/HTTP仍未接入。GitHub远程/真实PR/线上CI未运行，没有自动启动开发会话。
