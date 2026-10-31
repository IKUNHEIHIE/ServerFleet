# 下一批多 Agent 工作包建议

**本文件是分工建议，不是启动授权。P1 交付后所有开发暂停。** 下列任务均从审查后的 P1 main 提交创建 Worktree；负责人批准具体编号后，由集成负责人登记 TASK-BOARD。每个模型可以有多个会话，但每项只能有一个唯一写入会话。

统一共享保护集合 **S**：根/模块 AGENTS、`go.mod/go.sum`、全部 package manifest/lock、`api/`、`proto/`、`gen/`、`buf*.yaml`、`redocly.yaml`、`docs/reference`、`dependencies.json`、公共 DB schema/迁移编号、scripts、CI。除单独授权的契约/工具工作包外均不得修改。所有安全 ADR 不可覆盖。

所有任务交付：本地提交、文件清单、实际测试结果、至少一个拒绝/失败场景、未验证事项；由指定审查人审查，再由 **Codex 集成负责人** 统一合并。模块作者不得自行合并 main。以下 `<root>` 指项目主目录；Worktree 由 `node scripts/worktree.mjs create <分支> main` 创建。

## G2-R01 · 核心契约兼容收口

| 项目 | 任务定义 |
|---|---|
| 目标与 Agent | **Codex 契约/安全会话 codex-01**；评审 C01～C07，解释旧/新认证、错误、状态机和 revision 映射，审查 P1 Proto 修订 |
| 分支 / Worktree | `contract/g2-review-cx01` / `<root>/.worktrees/contract--g2-review-cx01` |
| 允许修改 | 新建 `docs/contracts/revisions/g2-r02/`、`api/openapi/revisions/g2-r02/`、隔离合同测试；如需改 proto 必须负责人明确将其加入任务范围 |
| 禁止共享文件 | S 中现有文件、原候选副本/原 ADR/兼容基线不可改；生成配置/锁文件由集成人单独处理 |
| 接口依赖 | 现有三份候选 OpenAPI、M0-P1-r1 Proto、G1 ADR、auth/tasks/nodes/audit 临时端口 |
| 交付文件 | 迁移矩阵、新增修订候选、兼容性差异、正反测试和待批准结论；不能自行宣布冻结 |
| 自动验收 | `npm run check:contracts`；新增 revision 加入显式 lint/ref/ID/路径检查；旧 151 操作变化逐项对照 |
| 并行条件 | 可与 N01/A01/W01 中互不重叠的 Mock 任务并行；所有公共合同修改只有这一个写入人 |
| 审查 / 合并 | Codex 安全+通信/任务 owner；影响产品/安全的变化交项目负责人批准；集成人合并 |

## N01 · 节点资产与普通分类的纯领域规则

| 项目 | 任务定义 |
|---|---|
| 目标与 Agent | **DeepSeek 节点会话 deepseek-01**；合成节点数据上的分类/筛选规则和 Reader 内存测试适配，区分普通分组与服务组 |
| 分支 / Worktree | `feature/nodes-domain-ds01` / `<root>/.worktrees/feature--nodes-domain-ds01` |
| 允许修改 | `internal/nodes/` 内非公共端口新增实现/测试、模块局部 testdata |
| 禁止共享文件 | S、现有 ports.go 签名、其他模块；不接 DB、不接 Agent、不改外部 API |
| 接口依赖 | `nodes.Reader/Record`、`auth.Authorizer` 的显式 mock；G2 Nodes DTO 仅做隔离适配参考 |
| 交付文件 | 领域规则、内存 reader、分类/离线缺失/可见范围失败测试；不提供真实生命周期端点 |
| 自动验收 | `go test ./internal/nodes/...`、`npm run check:policy`、`npm run check:go` |
| 并行条件 | 可与 G2-R01/A01/W01 并行；G2 未冻时仅纯领域/Mock，不开发真实注册、吊销、数据库或 HTTP |
| 审查 / 合并 | Codex 节点接口+安全审查；集成人合并 |

## A01 · 审计脱敏与只读查询的隔离适配

| 项目 | 任务定义 |
|---|---|
| 目标与 Agent | **DeepSeek 审计会话 deepseek-02**；类型化事件脱敏、合成数据分页/过滤、只追加测试适配 |
| 分支 / Worktree | `feature/audit-domain-ds02` / `<root>/.worktrees/feature--audit-domain-ds02` |
| 允许修改 | `internal/audit/` 内新实现/测试和局部 testdata |
| 禁止共享文件 | S、现有 ports.go 公共签名、业务模块；不建公共表、不宣称 SQLite 防篡改 |
| 接口依赖 | `audit.Recorder/Event`；候选审计 API 仅适配，不假定 DTO 稳定 |
| 交付文件 | 脱敏白名单、显式 memory recorder、分页/秘密不输出/错误路径测试 |
| 自动验收 | `go test ./internal/audit/...`、`npm run check:go`、`npm run check:policy` |
| 并行条件 | 可与 G2-R01/N01/W01 并行；真实持久化和授权查询须等 G2/存储评审 |
| 审查 / 合并 | Codex 安全审查 + audit owner；集成人合并 |

## W01 · 节点界面组件与候选 API Mock

| 项目 | 任务定义 |
|---|---|
| 目标与 Agent | **Gemini UI 会话 gemini-01**；节点表格/空/加载/失败/离线缺失状态，可访问交互与视觉一致性 |
| 分支 / Worktree | `feature/web-nodes-mock-gm01` / `<root>/.worktrees/feature--web-nodes-mock-gm01` |
| 允许修改 | 新建 `apps/web/src/features/nodes/` 与该目录 `*.test.*`/`test/`（前端边界规则当前使用全局 test 目录约定；需 fixture 放测试文件或全局 test 的经分配子目录） |
| 禁止共享文件 | S、App/main、全局 CSS、components/ui、其他功能目录；新依赖先提集成需求，不自行改锁文件 |
| 接口依赖 | 当前 Badge/cn、候选 Nodes DTO 的局部可替换 adapter；测试 fetch 必须 mock，不连接真实 API/Agent |
| 交付文件 | 组件、独立 ViewModel/适配接口、测试 Mock、键盘/可访问名称/错误状态测试 |
| 自动验收 | `npm run check:web`、`npm run check:policy`；离线 cpu=null 不显示 0、失败不显示成功 |
| 并行条件 | 可与 G2-R01/N01/A01 并行；G2 未冻仅 Mock/适配，暂不接 App 或实现全量页面 |
| 审查 / 合并 | Gemini UI owner + Codex 契约/安全；集成人负责 App 接入并合并 |

## S01 · SQLite WAL 与迁移基础的隔离测试（需单独授权）

| 项目 | 任务定义 |
|---|---|
| 目标与 Agent | **DeepSeek 存储会话 deepseek-03**；评估轻量驱动、临时 SQLite WAL/事务/忙等待/迁移失败测试，无业务表模型扩张 |
| 分支 / Worktree | `feature/storage-foundation-ds03` / `<root>/.worktrees/feature--storage-foundation-ds03` |
| 允许修改 | `internal/storage/` 的连接/迁移基础与临时目录测试 |
| 禁止共享文件 | S、公共业务 schema、任何真实 DB/备份；驱动依赖由集成负责人单独更新 go.mod/go.sum |
| 接口依赖 | 当前 storage 边界和 G1 SQLite WAL 原则；迁移端口需先交 owner 审查 |
| 交付文件 | 驱动选择依据、连接工厂、临时目录测试、失败恢复证据、未做性能承诺声明 |
| 自动验收 | `go test ./internal/storage/...`、`npm run check:go`、`npm run check:security`；WAL 模式真实查询断言 |
| 并行条件 | 可与纯 Mock/UI 并行；有唯一依赖/迁移写入人后启动，不与另一存储任务并行改公共结构 |
| 审查 / 合并 | Codex 集成+各模型 owner；依赖/端口审批后集成人合并 |

## T01 · 授权/任务/通信的安全闭环准备（先拆成顺序小任务）

| 项目 | 任务定义 |
|---|---|
| 目标与 Agent | **Codex 安全/核心会话 codex-02**；先做拒绝默认、作用域/审批/任务 ID 的合同测试，再逐项授权离线隔离 PoC，不能直接接生产节点 |
| 分支 / Worktree | `feature/core-safety-cx02` / `<root>/.worktrees/feature--core-safety-cx02` |
| 允许修改 | 初始只分配 `internal/auth/` 的纯策略与测试；后续 tasks、transport、apps/agent 必须拆任务和重新划定所有权 |
| 禁止共享文件 | S、安全 ADR、其他模块；不得用临时端口自行拼出真实 root 执行器 |
| 接口依赖 | G2-R01 评审结果、auth.Authorizer、tasks.Submitter、transport.Sender、Proto 候选；详细缺失 ADR 先核验 |
| 交付文件 | 默认拒绝/撤权/快照/重放/人工审批负向测试，后续按独立任务提交协议测试与安全证据 |
| 自动验收 | `go test ./internal/auth/...`、`npm run check`、`npm run check:security`；后续真实通信需 WSS+mTLS/错误证书/断线/去重测试 |
| 并行条件 | G2 未冻仅 Mock/纯策略；真实安全闭环在 C01～C07 审查和必需安全门槛后进行；不能与另一个 auth owner 同时写 |
| 审查 / 合并 | 独立 Codex 安全审查人（不能仅作者自审）+相关 owner；项目负责人批准下一步范围；集成人合并 |

## 给负责人的启动建议

首次选择 **G2-R01 + N01 + W01**，3 个独立目录最易管理；如果希望更省管理精力，先 **G2-R01 + W01** 两个任务。A01 可替换 N01，或待一个任务结束再启动。不要同时把四五个相互依赖的核心模块交给多个会话。

Codex 必须先完成 G2-R01 和安全接口审查，才能允许 DeepSeek/Gemini 接真实接口。DeepSeek 优先纯 Go 领域/数据处理/常规测试，Gemini 优先 Mock 组件与交互；WASM/Xray/Nginx/真实执行、生产部署不属于这批默认任务。当前可安全准备多个会话，但必须等项目负责人明确批准所选任务后开始。
