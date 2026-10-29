# 第六批部分验收后的并行后续任务（DeepSeek / Gemini）

日期：2026-10-29；总控codex-review-01。负责人已提交TASK-01A/W04-R1先行审核，并明确Codex的G2-R02A仍在工作。本单仅给DeepSeek/Gemini派发新任务；**Codex继续原分支、原0fef8ba基线和第六批范围，不改其工作目录、不要求同步main、不启动或发送其他会话消息**。

## 共同约束

新任务从本次部分验收完成后的main建独立分支/Worktree；旧目录停止并保留。读AGENTS、README、架构索引、模块归属/依赖规则、TASK-BOARD、本单和BATCH-06-PARTIAL-REVIEW。开始记录实际目录/分支/HEAD，审查人均为总控，完成本地提交/验证后停止。

共享保护S：根/模块AGENTS、现有ports.go、所有依赖清单/锁、API/Proto/gen、Buf/Redocly、CI/scripts、依赖规则、数据库公共结构、原始资料和已批准ADR。不改S、不加依赖，不读写Codex在编的g2-r03目录；有具体公共需求先交总控协调。

G2未冻结；只做下述纯规则或注入数据源的隔离UI，不连接真实Controller/Agent/节点/数据库/云，不接App，不实现执行/真实授权，不推送/合并main或启动其他Agent。库/框架/CLI用法按项目Context7规则查文档。

## TASK-01B · 默认分批计划与后批推进的纯规则

| 项目 | 分配 |
|---|---|
| 会话 / 审查 | DeepSeek deepseek-01 / 总控任务与安全审查 |
| 分支 / Worktree | feature/task-batch-rules-ds01 / `D:\admin\desktop\codex\myproject\ks\.worktrees\feature--task-batch-rules-ds01` |
| 允许写入 | internal/tasks内新增分批计划/纯判断/测试/说明；已有快照/等待规则及测试保留，ports.go不变 |
| 计划规则 | 复用已接收TargetSnapshot；普通任务默认每批最多5个目标；root或修改类首批1个、后续每批最多5个。保持快照顺序，不丢失/重复目标；空快照/未知操作分类拒绝，不把零值当普通或全节点 |
| 可信输入 | 分类必须由未来可信动作注册/任务服务明确提供，不能由AI/客户端自报即可降成普通。计划只决定批次形状，不证明资源权限、动作安全、人工审批或实际并发已落实 |
| 推进判断 | 只做纯结果判断：当前批全部目标都有明确可靠成功证据时，后批才满足顺序门槛；任何失败/取消/超时阻止尚未启动的后批；未知、缺失、未完成也不能当成功或自动重试。已在运行的本批任务不由此规则强制回滚/取消 |
| 结果关联 | 判断针对明确当前批的目标集合；重复/批外结果、未知代码等无效输入拒绝；缺少目标的可靠完成证据不能推进。不从exitCode=0、ACK或输出截断推断成功，不设计完整TaskState或另造执行状态源 |
| 输出边界 | 输入/输出集合不共享可变slice；调用方不能通过修改返回批次改变原计划。没有隐式时钟、goroutine、I/O、回调派发、全局队列或真实执行；“满足批次顺序门槛”仍须实时鉴权/审批/TTL等后续门禁 |
| 禁止扩围 | 不实现Submitter、调度器、并发worker、真实任务状态机、审批/计划摘要、持久化、HTTP或Sender；不更改公共Request/端口、Proto或现有等待窗口来实现执行 |
| 验收 | 目标数1/5/6/11/12/13的普通/root/修改分批；顺序/复制隔离；首批及后续批失败停止更后批、混合成功与运行中/unknown不推进、结果缺失/重复/批外/未知代码拒绝或明确未完成；真实函数测试而非测试内复制判定器 |
| 命令 | go test -count=1 -cover ./internal/tasks/...；npm run check:go；npm run check:policy；npm run check:security |

依据根AGENTS与现行通信/核心候选的普通5、root/修改1→5、失败停后批和unknown不自动重跑边界。常量/局部结果类型不冻结公开任务合同；此模块没有可执行许可，也不能豁免AI任意root Shell真人管理员审批。

## W05 · 可注入只读数据源的节点加载容器

| 项目 | 分配 |
|---|---|
| 会话 / 审查 | Gemini gemini-01 / 总控UI、数据隔离与契约审查 |
| 分支 / Worktree | feature/web-node-loader-gm01 / `D:\admin\desktop\codex\myproject\ks\.worktrees\feature--web-node-loader-gm01` |
| 允许写入 | apps/web/src/features/nodes内新增加载容器/局部端口/测试/说明，最小组合现有NodeListWorkspace；已验收搜索/详情/焦点/指标测试保留 |
| 输入边界 | 由调用方显式注入异步load及不含秘密的稳定数据源/作用域标识；load返回现有局部NodeListItemView集合。只定义局部ViewModel加载接缝，不绑定未审完的公开DTO，不改CandidateNodesSource或Codex在编契约来扩围；缺少有效来源不能默认放行/返回合成成功 |
| 基本行为 | 首次加载显示loading，成功展示当前结果，失败显示固定安全提示和可用重试；重试进入loading且不保留成功节点/计数/详情。异常原文可能含路径/token，不直接展示或记录error.message。数据质量仍由受控适配层及显式时钟决定，不读取机器日期或新增采样默认 |
| 防旧响应 | source身份/作用域标识变化或重试后，旧请求的成功/失败都不能覆盖本轮状态；请求支持AbortSignal，但即使来源忽略abort也须丢弃过期响应。卸载中止本任务请求，不加入自动轮询/后台重试或跨用户缓存 |
| 渲染隔离 | 来源/作用域变化后，首个提交就不能展示旧来源节点、计数或详情，不能只靠effect稍后清空。调用者必须在身份/授权来源变化时更新标识；标识不是授权凭证，本容器不校验RBAC，不得把固定key当跨用户信任或缓存许可 |
| 集成边界 | 本轮不提供生产fetch实现，不发真实请求，不接App/main、Controller/Agent、localStorage或共享全局状态；假来源与合成数据仅放测试。后续真实适配要等G2契约/授权审查，不能假定G2-R02A已完成或获批 |
| 验收 | 可控Promise测试首次/成功/安全失败/重试、先发后到和旧错误、忽略abort、来源/作用域切换时的即时隐藏、卸载取消；保留焦点/键盘/无网络/指标回归。不得用真实网络/定时sleep制造竞态；不能仅在effect结束后的DOM断言就宣称从未提交旧数据 |
| 简化要求 | 不为单列表引入通用请求框架、复杂缓存、全局事件总线或新依赖；现有展示组件仍可独立使用，局部加载接口不当作公开API |
| 命令 | npm run check:web；npm run check:policy；说明所有负向结果、来源切换策略、兼容变化与未验证边界 |

这项为首条只读页面流程准备异步状态管理，与Codex的公开契约评审互不写入、互不依赖。pending来源的类型或字段若不够，先交差异，不读未提交契约拼接真实API。

## 交付与停止

交接实际路径/分支、起始/最终HEAD、文件、命令/真实输出、至少一个失败场景、候选依赖/兼容变化及FAIL/BLOCKED/UNTESTED。只创建本地提交，交总控审查后停止；不合并main、不推送、不启动其他Agent或擅自开始下一项。原分支/报告/预览工件全部保留。
