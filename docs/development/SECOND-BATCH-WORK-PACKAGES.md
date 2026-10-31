# 第二批任务：协议语义门禁 + 两项返修

分配人：总控 codex-review-01；日期：2026-10-29。依据负责人“审核并分配下一步”的请求。**已完成分配，不自动启动其他 Agent 或向其发消息。** 接到任务后只做本任务，交付后停止。源码审查结论与复现见主目录 `docs/development/reviews/BATCH-01-REVIEW.md`。

总控保留共享文件写权：根/模块 AGENTS、所有依赖清单/锁文件、API/Proto/gen、Buf/Redocly配置、CI/scripts、依赖规则、公共DB结构、原始资料、已批准ADR。下列任务都不得改这些文件。仅使用合成数据、临时文件/测试，禁止真实网络、VPS、执行器、生产或云服务操作。

## T01-A · Protobuf 解码后的语义校验

| 项目 | 分配 |
|---|---|
| 目标 / Agent | **Codex，codex-01**；将 G2-R01 已证明“可解码但不安全”的情况变为真正调用生产候选校验函数的离线负向测试。只做纯校验，无网络/执行 |
| 分支 / Worktree | `feature/proto-validation-cx01` / `D:\admin\desktop\codex\myproject\ks\.worktrees\feature--proto-validation-cx01`，由总控从审核后的 main 准备；不是原 contract Worktree |
| 基线 | 首批审查完成的 main（含8821555及根契约接线）；启动时记录精确 HEAD |
| 允许修改 | `internal/transport/` 内新增校验源文件、`*_test.go`、局部 testdata；可以使用现有 gen/go 的只读类型；不修改现有 ports.go |
| 禁止修改 | 全部共享集合、apps/agent、auth/tasks/nodes、Proto字段/枚举/生成代码；不建会话、证书库、队列或执行状态机 |
| 已有依赖 | `agentv1.Envelope`、13种 oneof、Privilege/ExecutionResult；G1与G2当前候选的方向/握手原则；G2-R01 PROTO-REVIEW 的缺口 |
| 应交付 | 独立校验入口与明确错误码/错误类型；必要的调用上下文/可注入限制策略；正反测试；说明哪些规则来自原规范，哪些仍是候选假设；模块内 README/交接说明 |
| 最小验收 | 空Envelope/缺payload、非法major/方向/握手上下文、缺必要ID、TaskOffer缺执行ID/摘要、无效参数JSON、Privilege=0/999、TaskFinished未知/未指定结果等必须有直接调用校验器的拒绝测试；13种已知payload要有对应有效样例 |
| 参数边界 | 帧/日志/动作允许列表等未冻数值必须通过显式输入或标为未实现，不硬编码成生产承诺。校验器不能单凭node_id/scope_revision批准身份、权限或执行。语法有效的JSON/摘要不等于合法动作或真实计划绑定 |
| 时间语义 | TaskOffer期限仅检验候选offer本身；不得据此丢弃已启动执行的迟到结果。时间/限制由测试注入；不猜测未知状态或证书可信性 |
| 自动验收 | `go test -count=1 ./internal/transport/...`、`npm run check:go`、`npm run check:policy`、`npm run check:contracts`；测试必须调用新函数，不能只在测试里写另一份允许/拒绝逻辑 |
| 并行 | 可与 N01-R1/W01-R1 同时做，目录不重叠；不依赖返修模块 |
| 审查 / 合并 | 总控 Codex 独立审查并合并；修改协议字段/公共端口或改变安全边界需先提出具体提案，不能自行扩围 |

T01-A 只补足解码与语义门禁之间的一层，不形成 WSS+mTLS/持久去重/授权/真实Agent闭环，也不表示 G2-C07 已完成冻结。

## N01-R1 · 修复节点领域边界

| 项目 | 分配 |
|---|---|
| 目标 / Agent | **DeepSeek，deepseek-01**；解决审查报告 N01-R01、R02、R03，不开发新业务 |
| 分支 / Worktree | 继续 `feature/nodes-domain-ds01` / `D:\admin\desktop\codex\myproject\ks\.worktrees\feature--nodes-domain-ds01` |
| 基线 | `04c45e97c76df189fa4f94b0de8034df84529442`；保留原提交，新增返修提交 |
| 允许修改 | `internal/nodes/`；明确授权修订本分支新增的 Rename 内部方法为带预期版本的原子更新，并同步调用测试；原 ports.go 不变 |
| 禁止修改 | 共享集合、auth的错误类型/端口、其他模块；不要merge main引入无关工件、重置分支或修改测试期望来掩盖问题 |
| 接口依赖 | 现有 Node/Metrics/OrdinaryGroup、auth.Authorizer；不新增外部库/HTTP/DB |
| 应交付 | ctx取消/期限传播修复；Rename原子版本前提；非有限指标拒绝；直接针对修复函数的回归测试；模块局部交接说明 |
| 自动验收 | 最后一次Authorize内取消/超时也须返回ctx错误；并发同版本Rename至多一方成功，失败不改变状态；CPU/内存分别NaN/±Inf均不可ok=true；合法0仍有效。运行 `go test -count=1 -cover ./internal/nodes/...`、`npm run check:go`、`npm run check:policy` |
| 并行 | 可与 T01-A/W01-R1 同时做；不依赖新的auth错误分类。其他授权基础设施错误的分类问题只登记，不自行扩接口 |
| 审查 / 合并 | 总控复现回归、审查后合并；作者不得自行合入main |

## W01-R1 · 指标有效性与采样时效

| 项目 | 分配 |
|---|---|
| 目标 / Agent | **Gemini，gemini-01**；修复 W01-R01/R02，保留加载/空/失败/离线与可访问交互 |
| 分支 / Worktree | 继续 `feature/web-nodes-mock-gm01` / `D:\admin\desktop\codex\myproject\ks\.worktrees\feature--web-nodes-mock-gm01` |
| 基线 | `c6302a90bd208195c24233ad790bcf4f887a68e0`；新增返修提交，保留原记录 |
| 允许修改 | `apps/web/src/features/nodes/` 内适配/ViewModel/组件、局部测试和test fixture；可新增局部README |
| 禁止修改 | 共享集合、App/main、全局CSS、components/ui、后端或真实API；不增加新依赖 |
| 接口依赖 | 当前候选DTO/局部MetricReading、Badge/cn；新鲜度是临时可注入展示策略，不能宣称G2阈值已冻结 |
| 应交付 | 明确区分新鲜/缺失/无效/陈旧（可用无有效数据或明确质量状态）；校验采样时间与百分比；注入时钟/最大年龄；组件和适配正反测试；交接说明 |
| 自动验收 | 缺/非法/未来/过期采样时间不能作为普通有效读数；CPU/内存局部0–100域之外不能显示普通百分比，也不可夹成0/100；新鲜0/100保留。历史值若保留必须显著标记及可见时间，不只用title。测试用固定注入时间，不依赖机器日期。运行 `npm run check:web`、`npm run check:policy` |
| 并行 | 可与 T01-A/N01-R1 同时做；不依赖真实REST或后端模型；继续不接入App |
| 审查 / 合并 | 总控检查数据质量、可访问性及回归后合并 |

## 所有会话交回格式

任务ID、实际工作目录、分支、基础提交、最终提交编号；变更文件清单；实际命令与结果；至少一个修复前失败/修复后通过的用例；剩余未验证事项。依赖工具缺失可按固定版本项目级安装，不能修改锁文件或用跳过检查代替修复。

N01/W01 的返修继续原来的会话；Codex 要切换到准备好的 T01-A 工作目录，先确认身份与路径。没有任何会话因本任务单生成而自动开始运行。完成后立即停止，交总控复审。
