# 模块所有权

这里的初始模块表记录早期职责/端口来源，包含当时的占位描述；实际实现状态见CURRENT-PROGRESS，现行写权限见本文末尾Cloud R2及TASK-BOARD。登记的READY不代表平台已运行；必须有唯一会话ID，同一模型的两个会话不能共享一份写权限。

| 目录/模块 | 职责与当前端口 | 数据模型归属 | 允许依赖（自身包除外） | 禁止直接访问 | 建议执行人 / 公共接口审查人 |
|---|---|---|---|---|---|
| `apps/controller` | 程序生命周期、依赖组合、当前 health HTTP | 不持有业务权威模型 | 全部 internal、gen | 可选业务插件源码、测试 mock | Codex 集成负责人 / 安全负责人+涉及模块 owner |
| `apps/agent` | 独立 Agent 入口，当前离线退出 | 本地执行记录未来独立设计 | transport、gen | Controller 的 auth/tasks/storage、可选插件 | Codex 通信负责人 / 安全负责人 |
| `internal/auth` | 身份/RBAC/资源作用域；Authorizer.Authorize | 用户、角色、会话、授权版本 | audit、storage | tasks、transport、plugins | Codex 安全负责人 / 集成负责人 |
| `internal/nodes` | 资产、普通分组、生命周期；Reader.Get | Record；节点/普通分组模型；服务组未来单独子域 | auth、audit、storage | tasks、transport、plugins | DeepSeek 节点负责人 / Codex 安全+集成 |
| `internal/tasks` | 唯一任务计划、审批、调度；Submitter.Submit | 任务、执行、计划/目标快照、任务事件 | auth、nodes、audit、transport、storage | 插件业务包、Web、Agent 入口 | Codex 任务负责人 / 安全+通信负责人 |
| `internal/transport` | WSS/mTLS 身份、帧、重连；Sender.Send | 连接/协商状态；消息结构属 proto owner | audit、storage、gen | auth/tasks/nodes/plugins 业务层 | Codex 通信负责人 / 安全+契约负责人 |
| `internal/audit` | 脱敏写入/只读查询；Recorder.Append | Event、审计过滤/保留视图 | storage | 其他业务模块、执行通道 | DeepSeek 审计负责人 / Codex 安全 |
| `internal/plugins` | 清单/来源、生命周期、未来受控 Host；Catalog.Lookup | Descriptor、平台安装元数据；不拥有 Xray 模型 | auth、tasks、audit、storage | transport、Agent、可选插件源码 | Codex 插件平台负责人 / 安全+任务负责人 |
| `internal/storage` | SQLite 连接/WAL/事务/迁移基础，当前仅占位 | 迁移台账/连接配置；业务模型仍由各领域拥有 | 无其他领域模块 | 反向依赖业务、执行命令 | DeepSeek 存储负责人 / Codex 集成+相关模型 owner |
| `apps/web` | REST/SSE 界面、组件、API 适配层；当前空壳 | ViewModel、临时测试 fixture；不得另造服务端权威状态 | 前端包及 Controller 候选适配 | Agent、DB、Go internal/gen、生产导入 test | Gemini UI 负责人 / Codex 契约+安全、UI owner |
| `api/openapi`、`proto`、`gen/go` | 外部合同、消息和派生产物 | 公共 DTO、字段号、版本 | 不依赖实现或历史目录 | 自动手改生成文件、静默改源候选 | Codex 契约负责人 / 所有受影响 owner+安全 |
| `plugins` | 后续独立可选业务包，当前 README | Xray 等插件业务模型（仅 Xray 已批准） | 受控 Host SDK（尚未冻结） | 核心直接编入 Release、直连 Agent/秘密 | 后续独立插件任务 / 平台+安全负责人 |
| `tests`、`scripts`、CI、根配置 | 跨模块基础设施与边界规则 | 合成测试数据、工具版本 | 测试可依赖模块，生产不可反向引用 | 降低门槛掩盖失败 | Codex 集成负责人 / 涉及 owner |

已有多个领域端口、生产目录实现与已验证检查点，仍不等于全部能力已正式装配或完整验收。内部类型不自动成为冻结的OpenAPI对象；增加字段须在所属模块审查，不能用临时Request取代授权/审批设计。

`go.mod`、`go.sum`、根及 Web `package.json`、`package-lock.json`、`buf*.yaml`、`redocly.yaml`、依赖规则、数据库公共迁移序列、CI 与根 AGENTS 是**共享文件**。模块 Agent 提交具体需求/差异，集成负责人单独协调修改；不要由多个工作目录同时更新锁文件。

仓库现为IKUNHEIHIE/ServerFleet私有仓库。三平台使用同一GitHub账号不构成三位独立审核人；主控以实际diff/证据及COMMENT记录审查，不自我APPROVE。保护规则以GitHub实际设置为准，不擅改CODEOWNERS/分支保护，也不能把文档要求当作平台已经强制执行。

## 完整v0.9五日复合包的新增目录归属

负责人2026-10-30授权的实施分工见`sprints/v09-five-day/PLAN.md`。总控本次已登记下表13个新域的最小方向，见`dependencies.json`和DEPENDENCY-RULES，作者可以直接使用；不允许任意跨域import。额外依赖边、公开端口与共享patch仍由总控独立协调。

| 新目录 | 唯一实施会话 | 责任及边界 |
|---|---|---|
| internal/ai、internal/workflows | codex-02 | AI/工作流只通过当前授权、统一任务/审批/审计；不直连Agent/DB，不反向强迫auth依赖任务 |
| internal/updates、internal/recovery | codex-02 | 更新/信任/恢复平台，业务数据快照经DS存储接缝；不得复制或暴露离线恢复/CA秘密 |
| plugins/xray-manager | codex-02 | 唯一官方可选业务包，核心不得import/编入；运行时/Host需PoC后审查 |
| internal/files、internal/monitoring、internal/notifications、internal/quota | deepseek-02 | 文件/探测/通知/估算周期业务，节点操作经统一受控任务；路径、SSRF与通知秘密等负向必测 |
| internal/dns、internal/certificates、internal/gateway、internal/servicegroups、internal/publishing | deepseek-02 | 供应商/证书/网关/业务成员及发布编排；不自造鉴权/执行/私钥旁路或共享全部私表 |
| apps/web/src各正式业务模块 | gemini-02 | 全管理台/REST-SSE适配/声明式插件UI与浏览器验证；只消费Controller，后端缺口不补假成功 |

三个复合包的测试和进度目录也独立：CX为tests/integration/v09/platform、tests/poc/v09/platform及progress/codex；DS为对应services与progress/deepseek；GM为tests/browser/v09与progress/gemini。根配置/依赖锁/API/Proto/迁移编号/CI仍总控单写，作者给精确diff；未授权新增目录以外的跨模块改动不能因期限而覆盖他人文件。

## Wave6-R2具体任务调整

以`sprints/v09-five-day/WAVE6-R2.md`为当前授权：CX-03/codex-06接管`internal/files`与新增Controller file_*.go/ai_budget_http.go，DS-03暂停该模块写入，避免文件执行生命周期分散；DS可写新增Agent gateway_capability.go及同名前缀测试、Controller gateway_tasks.go。GM-03可写测试专用`tests/browser/v09/fixturehost` Go宿主，不得从生产引用。三位各自新增v09-r7候选文件范围见任务单，中央server/enrollment/bootstrap/Agent注册、迁移登记/Proto/gen/根锁/CI仍由总控接收patch。依赖白名单和统一任务/审批边界未放宽。

## 2026-11-03 Cloud R2当前所有权（覆盖上方旧平台分工）

完整精确文件清单和验收见`sprints/v09-five-day/cloud-r2/PLAN.md`及三份提示词。任务均READY，开工/提交尚待实际回报核验，不表示已运行；起点为经CI验证的51e0ee57。旧历史保留，按用户最新授权转交，不改原始资料/ADR或依赖白名单。

| owner | 当前独占范围 | 中央与共享边界 |
|---|---|---|
| agentmore-02 / AM-02 | files/tasks/transport/updates/recovery/ai/workflows；列明文件/平台程序与platform非插件测试 | 不承担插件/代理组件，不写Web或gateway；其他中央字段交精确候选，不能争改HW窗口 |
| codearts-02 / HW-02 | DNS/证书/网关/服务组/发布/监控/通知/配额；gateway_*.go与services测试 | 独立integration/cloud-gateway-wiring-hw02唯一候选窗口：网关所需server/agent_links/Controller main/Agent connect-main-runtime与测试、迁移/全局断言；主控审查接收前不称正式能力 |
| workbuddy-02 / WB-02 | apps/web（锁/配置除外）、tests/browser/fixturehost；plugins/xray-manager、internal/plugins、auth的xray_materials*.go、server/plugins.go与新增xray_*.go；明确Xray测试与脚本 | 旧“禁止生产Go”只在此Xray域内被本次授权覆盖；不写其他Go/中央入口。Host/注册/消息/schema/锁/CI交主控协调；Runtime未获准入不得运行或默认启用 |
| chatgpt-review-02 / 总控 | 公共接口/编号/生成/锁/CI/独立审查/顺序合并/任务板 | 不与HW候选窗口同时修改同批中央文件；窗口以明确接收或释放结束。其余两家中央请求排队成精确patch，不能形成隐藏overlay |

Xray所有工作由WorkBuddy承担；这不是Runtime批准。可选插件不编入核心Release/被核心直接导入，WASM/Eino未准入。预留网关候选cx/0010_gateway_artifact_bindings仅作唯一编号预约，当前正式迁移仍26，必须主控审查与升级验证后才登记，禁止改旧SQL/hash。

## 2026-11-08 单会话接手（覆盖上方平台写权限）

用户授权ChatGPT直接返修#7/#8/#9并独立继续完整项目。V09-CLOUD-TAKEOVER-01/chatgpt-takeover-01取得三包剩余模块及主控协调范围；原三平台/Windows作者不自动复活，原中央窗口转交本会话单写。历史交付/分支/进度保留，不把转交标完成。源资料/ADR不可改写，公共接口/迁移/生成/锁/CI仍须协调审查；统一任务/审批/审计、未知不重放、可选Runtime未准入等条件不变。见TASK-BOARD与progress/chatgpt/cloud-r3/TAKEOVER.md。

2026-11-11 R33：internal/netprobe归主控共享低层网络代码；仅标准库。monitoring维持业务授权/状态/存储，Agent只消费公开探测类型，Controller独占任务与来源装配。当前任务协调以上明确边界，不开放通配写入或插件Runtime。
