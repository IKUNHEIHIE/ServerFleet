# TASK-01B / W05审查后的两位后续任务

日期：2026-10-29；总控codex-review-01。TASK-01B接收，W05因两个P2退回；Codex继续AUTH-01B原任务、原目录和1f8f8cb基线，本单不改变其范围，也不启动/代发其他会话消息。所有任务仅离线开发，G2未冻结。

## 共同约束

先读AGENTS、README、架构索引、模块归属/依赖规则、TASK-BOARD、本单和reviews/FOLLOWUP-01-REVIEW.md。开始记录任务ID、唯一会话、实际目录/分支/起始HEAD；审查人均为总控，完成本地交接即停止。

共享保护S：根/模块AGENTS、现有ports.go、依赖清单/锁、API/Proto/gen、Buf/Redocly、CI/scripts、依赖规则、数据库公共结构、原始资料和已批准ADR。不改S、不加依赖，不修改AUTH-01B或其他作者目录。不连接真实节点/Controller/Agent/数据库/云，不实现生产默认放行mock，不推送/合并main、不启动其他Agent。原提交/分支/报告保留，不reset/rebase或同步main掩盖差异。库/框架/CLI用法遵循Context7规则；代码审查和纯业务规则无需查库文档。

## TASK-01C · 顺序批次进度的不可变纯值

| 项目 | 分配 |
|---|---|
| 会话 / 审查 | DeepSeek deepseek-01 / 总控任务与安全审查 |
| 分支 / Worktree | feature/task-batch-progress-ds01 / `D:\admin\desktop\codex\myproject\ks\.worktrees\feature--task-batch-progress-ds01` |
| 基线 | 本次审查交接后的main（精确起点以目录HEAD为准）；旧TASK-01B目录停止保留 |
| 允许新增 | internal/tasks下批次进度纯值、测试及独立TASK-01C说明；现有batch_plan/batch_gate、快照/等待规则、HANDOFF及其测试和ports.go均不改 |
| 目标 | TASK-01B的EvaluateBatch只评价调用者指定的一批，不记前批结论。新增最小不可变进度值，复用BatchPlan/EvaluateBatch表达“从首批起按序走到哪一批”，减少未来调用方自行复制顺序规则；不是完整任务状态机或调度器 |
| 构造与访问 | 只从有效非空BatchPlan开始；零计划/零进度拒绝，起点只能首批。暴露当前批索引、目标副本及明确进度状态；调用者不能传任意新索引跳过前批，不解释空目标为全部节点 |
| 纯推进 | 对当前批提交可信结果并返回新进度值/判断；复用现有EvaluateBatch，不复制结果判定。incomplete停在本批；advance只移动紧邻下一批；末批成功终结complete；blocked终结并阻止此进度继续。非法证据返回错误且不改变旧值；终结后不能重开/跳批 |
| 复制与边界 | 旧进度/计划与输入/返回集合不受后续修改影响。它是可复制的纯值，没有持久状态、执行ID绑定或防回滚/防重放保证；重用旧副本可能重算，绝不能当作一次性派发票据。只返回本地顺序结论，不发送、重试、取消或回滚任务；unknown仍不自动重跑 |
| 禁止扩围 | 不实现Submitter、完整TaskState、审批/计划摘要、执行许可、权限布尔凭证、并发worker/队列/锁/隐式时钟、I/O、HTTP、Sender、数据库或恢复服务；不接App/Controller/Agent，不调整公共端口或契约 |
| 验收 | 普通/root/修改、多批成功、首批/中间批失败、取消/超时终结、running/unknown/缺证据原地等待、重复/批外/未知代码拒绝；不能跨批套旧结果或在终结值继续；零值拒绝、集合复制、旧进度不变。测试调用实际新类型和现有判定函数，不在测试里自写推进器冒充实现；说明纯值不能解决实际并发/重启/旧副本重放 |
| 命令 | go test -count=1 -cover ./internal/tasks/...；npm run check:go；npm run check:policy；npm run check:security |

顺序满足后仍须真实派发链路的当前授权、审批、离线TTL和可靠结果证据；本项不关闭G2或执行准入缺口。作者自行选择能清楚表达上述语义的最小局部API，不为未来完整引擎预建框架。

## W05-R1 · 来源失效恢复与精确作用域隔离返修

| 项目 | 分配 |
|---|---|
| 会话 / 审查 | Gemini gemini-01 / 总控UI与数据隔离审查 |
| 原分支 / Worktree | feature/web-node-loader-gm01 / `D:\admin\desktop\codex\myproject\ks\.worktrees\feature--web-node-loader-gm01` |
| 精确起点 | 0ebe81224e63065d40a608e9eccb15123d9c8d60；保留原提交，追加返修，不合并main或改写历史 |
| 允许写入 | 原W05 features/nodes内加载容器/测试/说明的最小修复；忽略reports/_review临时验证；不改已验收组件、App/配置/共享契约或依赖 |
| 必修W05-R01 | A成功→source=null→恢复A：无效来源effect提前return，snapshot仍是A的ready；恢复时key又相等，首个提交重新渲染旧8节点/计数，随后effect才清空。恢复必须在首个提交就loading/无旧节点计数详情；不能依赖effect事后修补或仅凭最终DOM断言 |
| 必修W05-R02 | key被trim后用作请求依赖和快照身份；从`scope-a`换成` scope-a `仍被当同一作用域，旧节点持续可见，新来源load不调用。身份比较应保留精确值，或明确拒绝首尾空白key并安全失败；不得静默规范化成另一来源 |
| 回归要求 | 加入两项总控失败用例，保留原18个加载测试及151项既有回归；用布局阶段观察恢复/切换的首个提交。覆盖ready→无效来源→原key恢复、pending失效后恢复且旧请求迟到、不同精确key、无效load恢复、旧成功/旧错误不覆盖、abort/卸载/重试、筛选/键盘/指标。稳定的相同有效key重复渲染仍不应无故请求 |
| 测试有效性 | 原“已reject的同一个Promise再resolve”用例不会产生迟到成功，不能独立证明重试竞态。增加真实仍pending的旧请求→切换到B并失败→重试B→旧请求成功/失败等可控交错，验证本轮loading/结果均不被覆盖；不靠sleep/真实网络 |
| 简化与边界 | 优先最小状态隔离修复，不引入通用缓存、请求框架或全局状态。固定安全失败提示、无网络、key非授权凭证等原边界保留；不能用固定key声称跨用户安全，不能顺带接真实API |
| 验证 | npm run check:web；npm run check:policy（先加载主目录scripts/env.ps1；原Go路径/工具缺失FAIL已保留，总控补齐固定工具后PASS）；重跑总控外置探针，保留原FAIL和新PASS。若选择拒绝空白key，第二用例允许安全失败，不要求一定请求新来源 |

总控探针源码和配置以文本归档于reviews/evidence/followup01-loader-probe*；可在原reports/_review/followup01直接运行。原FAIL不删除，返修输出写新文件。W05尚未进入main。

## 交接与停止

提交实际路径/分支、起始与最终HEAD、文件、命令/真实输出、至少一个负向场景、候选依赖/兼容变化和剩余风险。任何FAIL/BLOCKED/UNTESTED明确记录。只创建本地提交，交总控复审后停止，不自行开始新功能或启动其他会话。
