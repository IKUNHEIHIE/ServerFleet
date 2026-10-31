# 模块所有权

这里的 Agent 是建议岗位，不是已经启动的会话。实际分配必须在 TASK-BOARD 写唯一会话 ID；同一模型的两个会话不能共享一份写权限。

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

目前端口没有生产实现，内部类型不是 OpenAPI 完整对象；增加字段需在所属模块审查，不能用临时 Request 取代授权/审批设计。

`go.mod`、`go.sum`、根及 Web `package.json`、`package-lock.json`、`buf*.yaml`、`redocly.yaml`、依赖规则、数据库公共迁移序列、CI 与根 AGENTS 是**共享文件**。模块 Agent 提交具体需求/差异，集成负责人单独协调修改；不要由多个工作目录同时更新锁文件。

GitHub 账号未提供，CODEOWNERS 尚无真实审批人，当前依靠任务单和本地审查执行。远程仓库获批后再绑定真实账号和 main 分支保护，不能把文档规则当作 GitHub 已强制执行。

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
