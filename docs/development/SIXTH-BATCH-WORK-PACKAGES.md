# 第六批：只读节点契约收口、任务等待规则、焦点返修

日期：2026-10-29；总控codex-review-01。第五批接收T01-C及N02/N02-R1；W04因W04-R01退回。本单分配独立任务，不自动启动Agent。三项可人工并行；完成本地交接即停止。

## 共同边界

读AGENTS、README、架构索引、模块归属/依赖规则、TASK-BOARD、本单及BATCH-05-REVIEW。开始记录实际路径/分支/HEAD。G2未冻结；不连接真实节点/数据库/云，不实施登录、WSS/mTLS或执行，不推送/合并main/启动其他Agent。

共享保护S：现有ports.go、根/模块AGENTS、依赖清单/锁、现有API/Proto/gen/Buf/Redocly、CI/scripts、依赖规则、数据库公共结构、原始资料和已批准ADR。不得修改；不增加依赖。G2-R02A仅获下列**新增修订目录**的单一写权限，不能改旧候选/r02或共享检查接线。W04-R1延续忽略的reports/_preview、reports/_review临时验收权限，不改生产入口/配置。

## G2-R02A · 节点只读链路的兼容候选与冻结前决策包

| 项目 | 分配 |
|---|---|
| 会话 / 审查 | Codex codex-01 / 总控、节点与Web契约审查；产品/安全决定仍待负责人批准 |
| 分支 / 目录 | contract/nodes-read-cx01 / `D:\admin\desktop\codex\myproject\ks.worktrees\contract--nodes-read-cx01` |
| 允许新增 | docs/contracts/revisions/g2-r03/、api/openapi/revisions/g2-r03/、tests/contracts/g2r03/；不修改S、Web/Go生产实现或现有修订 |
| 起点问题 | P1-C10：不能用“Core无列表”推断Merged也无列表。Merged已有GET /api/v1/nodes、operationId=listNodesForTransfer、NodeSummary，CPU/内存已存在但无采样时间；新NodeDetail是另一字段/状态/revision域。r02还含未批准运行时profile，不能直接接线 |
| 必须核对 | 对照原Merged/Core、r02、现有auth/nodes只读行为及Web局部适配，逐字段列出来源、缺失、语义和兼容影响。覆盖ID/name、online布尔与五状态、revision、平台/版本/lastSeen、可空指标及采样时间；无证据不能补成已知状态/实时值 |
| 候选交付 | 一个范围限于节点只读链路、可lint/解析的独立OpenAPI候选，加来源/操作矩阵、旧消费者兼容迁移、具体待批准清单。可提出节点列表/详情对齐及指标时间形状，但必须标为提案，不自行添加已冻结字段、恢复已删插件或扩成全量API重写 |
| 兼容保护 | 现有GET /api/v1/nodes和operationId不能无说明替换/复制；保留旧路径/行为的比较证据。不得把online=false猜成revoked/suspended，revision或采样时间缺失不得伪造。不要把r02 header/profile自动当作获批运行方案 |
| 安全与错误 | 写明身份须可信绑定、只统计授权可见集合、策略不可用不能返回成功空/部分列表、401/403/404及503等映射的来源/待定项、跨主体缓存边界；认证/会话缺口如实列出，不增加匿名可用或默认放行适配 |
| 验证 | 测试直接读取候选YAML，检查refs/operationId/参数/schema/examples/错误与兼容矩阵一致性；新增修订有独立run命令及显式lint，不改根脚本。负向样例应证明真实契约检查能拒绝坏输入；静态/模型测试不冒充真实HTTP鉴权或服务器行为 |
| 命令 | npm run check:contracts（原基线仍通过）；独立r03检查/候选lint；npm run check:policy；npm run check:security。交付固定工具的实际命令/输出及根检查尚未接入r03的说明 |

这项是接入首条真实只读流程前的契约准备，不是接口冻结授权或API开发。发现来源冲突只在本修订说明登记，交总控同步公共登记；不改原始资料或已有Web注释来掩盖范围差异。

## TASK-01A · 目标快照与未启动离线等待窗口

| 项目 | 分配 |
|---|---|
| 会话 / 审查 | DeepSeek deepseek-01 / 总控任务与安全审查 |
| 分支 / 目录 | feature/task-waiting-rules-ds01 / `D:\admin\desktop\codex\myproject\ks\.worktrees\feature--task-waiting-rules-ds01` |
| 允许写入 | internal/tasks内新增最小纯领域类型/规则/测试/说明；现有ports.go不变，不改其他模块 |
| 目标快照 | 从调用方明确提供的节点ID集合创建不可变快照；空集合/坏ID/重复ID拒绝，不trim成其他身份，不把空集合解释为全部节点。输入及返回副本均不共享可变slice，保持明确的提交顺序；后续成员增加不自动进入旧快照 |
| 权限边界 | 快照只固定目标，不证明节点存在、用户有权、AI会话授权或审批通过。输入必须由未来可信任务服务提供；当前权限、成员移除和审批仍须每次真实派发复核，本轮不实现这些服务 |
| 等待窗口 | 按项目已批准边界提供未启动离线任务默认15分钟等待窗口，所有参考时间显式注入；创建时刻/截止时刻清晰，恰好截止即过期，零时间/无效窗口不能变成无限等待。不增加生产策略系统或隐藏机器日期读取 |
| 适用范围 | TTL仅用于明确未启动的等待；评估须明确区分已启动并返回不适用，不把已启动任务改成expired/queued。结果unknown不在本模块推断，不能提供重跑或自动重试。过期判定的否定也不代表可执行许可 |
| 禁止扩围 | 不实现Submitter、完整TaskState、审批/计划摘要/调度/并发器、队列、持久化、HTTP或Agent下发，不修改公共Request/端口，不自造第二任务引擎 |
| 验收 | 真函数覆盖快照输入/输出修改隔离、空/非法/重复目标、后续集合变化；截止前/恰好截止/之后、15分钟默认、已启动不适用、坏时间拒绝、固定时钟；不得用sleep模拟15分钟，不用测试自写算法冒充实现 |
| 命令 | go test -count=1 -cover ./internal/tasks/...；npm run check:go；npm run check:policy；npm run check:security |

依据根AGENTS、现行通信/核心候选的目标冻结与未启动15分钟TTL规则。常量/局部类型不冻结公开任务合同；缺失的安全ADR不得补写成已批准条款。

## W04-R1 · 重复选择应即时聚焦，不留下延迟抢焦点请求

| 项目 | 分配 |
|---|---|
| 会话 / 审查 | Gemini gemini-01 / 总控UI与安全审查 |
| 原分支 / 目录 | feature/web-node-accessibility-gm01 / `D:\admin\desktop\codex\myproject\ks\.worktrees\feature--web-node-accessibility-gm01` |
| 精确起点 | 8e95c3cbb8bfffc497449cc2728c4dd04fe45eee；保留原提交，追加返修，不rebase/reset或合并main掩盖问题 |
| 允许写入 | 原W04范围：features/nodes最小修复/测试/说明及忽略的reports临时验收；不改App/配置/共享UI/依赖 |
| 必修W04-R01 | handleActivate对相同selectedId调用setState会被React跳过，而focusDetailNext仅写ref；无commit时effect不执行，键盘焦点留在行上。请求残留后，后台数据刷新会把焦点从搜索框抢到详情。两个独立用例已失败，源码/输出随BATCH-05归档 |
| 通过条件 | 鼠标已选中后Enter/Space激活同一行、键盘返回行再重复激活，都立即到详情；同一节点无状态变化也有效。用户转到搜索后后台刷新不能延迟执行旧请求；关闭/失效应清理待执行焦点意图，不靠定时器反复抢焦点 |
| 回归与简化 | 保留关闭/Esc返回当前行或搜索/列表兜底、来源移除/loading/error隐藏旧详情、非模态Tab和指标行为。优先事件驱动的最小修复，不为了强制effect运行新增全局状态或无条件后台focus |
| 验证 | 把两个独立失败场景加入真实组件回归；147项原测试保留；重跑真实浏览器Tab，并补重复选择与刷新焦点的实际步骤，更新预览/截图说明。不能把程序化focus断言当作原生Tab证据 |
| 命令 | npm run check:web；npm run check:policy；保留修复前失败与修复后输出，并报告实际浏览器验收范围 |

原W04未进入main。返修完成后交原提交和新HEAD，交总控审核完整差异；不要自行开始其他页面或功能。

## 交接与停止

报告路径/分支、起始与最终HEAD、文件、实际命令/输出、至少一个失败场景、兼容变化、候选依赖和未验证事项。任何FAIL/BLOCKED/UNTESTED如实保留。仅本地提交，完成交接即停止；不合并main、不推送、不自行启动下一项或其他Agent。
