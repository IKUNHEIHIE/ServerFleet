# 任务登记板

更新：2026-10-29。READ-PLAN-01-R1方案文档接收，原RP01-R01/R02关闭；总控仅补公开可省略string与局部null的事实澄清，仍PROPOSED。W06的99cd663证据已核验，原19项浏览器PASS保留，但W06-R01/R02两个P2覆盖缺口待补，报告未合入。DeepSeek/Codex完成停止，Gemini原目录追加W06-R1；详见FOLLOWUP-04-REVIEW和PARALLEL-FOLLOWUP-05。真实身份/HTTP/App来源未接入，G2/D01–D07/P1-C10仍OPEN，不自动启动/代发消息。

| 任务 | 状态 | 唯一会话 | 分支 / Worktree | 允许写入 | 审查人 / 基线 |
|---|---|---|---|---|---|
| W06R1-REVIEW | 精确复审及必要独立浏览器补验中 | codex-review-01 | integration/w06r1-review / `.worktrees/integration--w06r1-review` | 只读核对2c1c8bc/证据、忽略reports独立补验、接收通过文档及事实澄清、审查/任务状态/README/P1-C10、具体负责人选择包；不改运行时代码/候选或启动新开发 | 项目负责人验收；起始157549e；复审/验证/交接后停止 |
| FOLLOWUP04-REVIEW | 审查/组合/安全检查通过：接收返修方案文档，W06补验；交接后停止 | codex-review-01 | integration/followup04-review / `.worktrees/integration--followup04-review` | 原精确审查/集成/必要文档事实澄清、证据/任务状态和限定补验任务范围；不改运行时代码/候选，不批准新实现 | 项目负责人验收；起始69d6d43 |
| READSEC01-REVIEW | 独立审查/组合/安全检查通过，交接后停止 | codex-review-01 | integration/readsec01-review / `.worktrees/integration--readsec01-review` | 精确审查安全证据/引用原文、合入通过文档、证据/任务板/README及收口状态；不干预READ-PLAN-01-R1/W06，不启动新实现 | 项目负责人验收；起始ddf8c5b |
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
| READ-PLAN-01 / READ-PLAN-01-R1 | 文档接收，RP01-R01/R02关闭；仍PROPOSED，完成停止 | deepseek-01 | docs/node-read-plan-ds01 / `.worktrees/docs--node-read-plan-ds01` | 原3份文档；总控接收时澄清DTO省略与VM可null；不改代码/候选或批准状态 | 总控产品/契约/安全审查；原d0709fc、返修4da769c，起始98a1f6b |
| READ-SEC-01 | 审查通过，接收来源/安全证据文档；完成停止，等待收口后新分配 | codex-01 | docs/read-security-evidence-cx01 / `D:\admin\desktop\codex\myproject\ks.worktrees\docs--read-security-evidence-cx01` | 原3份新增文档范围，代码/候选/批准状态未改；本任务不再扩展 | 总控安全/契约审查；f3cdf73，起始7ef7877 |
| W06 / W06-R1 | 99cd663报告退回补验；W06-R01/R02两个P2 OPEN，原分支追加R1待人工派发 | gemini-01 | chore/web-node-browser-qa-gm01 / `.worktrees/chore--web-node-browser-qa-gm01` | 只改BROWSER-ACCEPTANCE.md，忽略reports新子目录补原生键盘/迟到失败/卸载证据；不改生产组件/App/依赖 | 总控UI/安全审查；原基线ddf8c5b，返修起点99cd663 |

原分支/Worktree保留以便追溯，不重置、不清理其他会话依赖、报告或未保存内容。Codex的READ-SEC-01、DeepSeek的READ-PLAN-01-R1完成停止；Gemini保持W06原目录，从99cd663追加补验，不要求同步main。总控只在独立集成目录澄清接收方案的公开字段形状，作者目录/提交/既有证据未改。

Gemini继续`PARALLEL-FOLLOWUP-05.md`的W06-R1；方案见`docs/contracts/decisions/node-read-01/`，安全证据见`docs/contracts/decisions/node-read-security-01/`。旧安全/方案文档的历史基线陈述与当前接收状态分别解释，W05已接收、W06仍未整项验收。W06-R1复审后再汇总具体选择包；D01–D07/P1-C10仍OPEN，身份/策略存储/HTTP未接入。GitHub远程/真实PR/线上CI未运行，没有自动启动开发会话。
