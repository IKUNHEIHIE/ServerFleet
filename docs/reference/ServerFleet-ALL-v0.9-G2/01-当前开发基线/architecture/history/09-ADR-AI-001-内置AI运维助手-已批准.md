# ADR-AI-001：内置 AI 运维助手与现成 Agent 框架复用

- **版本**：v0.1（正式批准稿，批准时间 2026-10-26）
- **状态**：**Accepted / 已批准**（AI-D1～D8；其中框架选型为**PoC 条件性决策**，未通过技术验证前不锁定 Eino）
- **日期**：2026-10-26
- **关联项目文档**：Server Fleet v0.8（已同步）
- **已批准的前置决策**：ADR-PRIV-001（root Agent 与服务端审批）、ADR-PLUGIN-001（插件工具与统一接口）、ADR-TASK-001（任务调度、限流、幂等、离线处理）

## 1. 决策摘要（面向产品负责人）

建议**复用现成 Go Agent 框架**，首选 **CloudWeGo Eino ADK 的 ChatModelAgent** 做技术验证；保留 **Google ADK Go** 作为备选，不自行从零实现 AI 循环、工具调用和流式处理。这里采用框架的依赖集成，不复制 / Fork 整个 AI 产品。是否正式锁定 Eino，取决于兼容性、审批与资源占用 PoC。

主控内嵌 **AI 编排与工具调用能力**，不运行大语言模型权重。模型由管理员自带 API Key。即使 AI 未配置、模型停服或关闭 AI，原有服务器管理、任务、插件、Cloudflare 和 Agent 通信都正常工作。

## 2. 项目边界

- **AI 运维助手**：运行在产品主控中的单一运维助手；不等同于未来多个 AI 编程 Agent 并行开发这个产品的工作流。
- **主控**：唯一任务与授权入口、任务数据与审计记录的权威来源。
- **模型**：只根据已授权、必要的上下文提出查询、计划和工具调用。**模型本身不持有服务器 root 凭据、mTLS 私钥或 Cloudflare API Token**。
- **被控 Agent**：继续使用 ADR-PRIV-001 已批准的单个 root Agent 模型；接收主控统一任务引擎下发的任务，AI 不直连被控节点。
- **插件**：通过现有注册中心声明工具；插件下线后，AI 工具同步不可使用。

## 3. 候选框架审查（2026-10-26 公共资料核查）

| 候选 | 适配优点 | 风险或不足 | 初步判断 |
|---|---|---|---|
| CloudWeGo Eino ADK | 原生 Go；ChatModelAgent、Tool、Runner、流式事件、HITL 恢复、模型适配；Apache-2.0 | 0.x API 可能变更，需要锁版本并验证多模型行为 | **首选 PoC** |
| Google ADK Go | 原生 Go；工具、会话、多 Agent、确认流程；Apache-2.0 | 框架功能较多，特定模型适配成熟度与引入依赖需测试 | **备选** |
| LangChainGo | Go，MIT；LLM 模型与工具生态 | 自主 Agent 流程与暂停续跑适配的工作量待验证 | 第三顺位 |
| OpenAI Agents SDK / Mastra | 已有完备 Agent 能力与丰富社区示例 | Python / TypeScript 主栈，与 Go 单体部署目标不匹配 | 首版不采用 |

**选择原则**：不能以 Star 数代替测试；验证工具参数一致性、模型兼容性、人工审批中断/恢复、会话取消、流式行为、依赖及 RSS。优先依赖原版稳定发行版本，不直接跟随 alpha，也不在 SDK 内部改安全授权逻辑。

## 4. 推荐产品体验

1. 管理员在 AI 页面配置一个支持工具调用的模型与 API Key（支持连接检查，不回显密钥）；无模型时显示 AI 未配置，其他功能不受影响。
2. 用户通过聊天提出查询或运维意图，例如“查看日本地区网关节点 Nginx 状态”。
3. AI 从核心 Tool Registry 获取**当前用户可使用**的工具列表，必要时查询节点、地区和分组；模型只获得完成请求所需的节点与状态数据。
4. 只读查询经服务端授权后自动执行，AI 汇总结果；注意“读取敏感日志/配置”不属于无条件自由的只读操作。
5. 对修改性操作，AI 首先生成可读计划，固定 Node ID 快照、动作参数、权限模式、并发批次和影响范围；由既有 `jobs` 服务创建任务。
6. **任意 AI root Shell 必须由已授权管理员在真实 Web 界面批准**。模型不提供 approve 工具；管理端复验权限并检查 `plan_digest` 与最终执行请求一致。其他修改类操作沿用既定授权规则。
7. 执行过程通过统一任务引擎展示节点执行状态、进度与实时输出；AI 可以阅读经过脱敏的结果并总结，但不得因“结果未知”自行重试高风险任务。

## 5. 推荐架构

```mermaid
flowchart TD
  U[React AI 页面] --> API[Go AI API / SSE]
  API --> R[Eino ChatModelAgent 适配层]
  R --> M[模型 Provider Adapter]
  R --> T[受限 Tool Registry]
  T --> N[节点/分组只读 API]
  T --> J[现有 jobs 任务引擎]
  J --> P[授权、不可变计划、审批、审计]
  P --> G[现有 mTLS Agent 通信]
  G --> A[被控 Agent]
  J --> S[(SQLite 审计与会话元数据)]
```

**集成方式**：在 Go 主控 `internal/ai` 内新建 `AgentRuntime` 适配层隔离 Eino SDK 版本；业务层对外只暴露自有 `Run/Cancel/Resume` 语义。未来更换 Eino 为 Google ADK Go 不影响任务引擎及前端协议。不要让框架直接连接 Agent，也不要由模型自由生成任意 HTTP URL 调用管理 API。

## 6. 模型接入及限制

- 首版优先实现**自带 Key（BYOK）**的 OpenAI 兼容 API 适配器：配置 Base URL、模型 ID、API Key、连接测试。它是接入协议，不代表所有兼容接口都完整支持工具调用。
- 每个模型必须分别验证：函数工具调用、结构化参数、工具返回、多轮对话、流式事件、超时与错误恢复。
- 未来可按需增加 Gemini、Anthropic 等原生 Provider Adapter。模型切换不能改变权限策略。
- API Key 只存于主控的凭据管理模块，不进入模型 Prompt、Agent 日志、插件页面配置或被控 Agent 环境；若配置自定义 Base URL，提示管理员数据将发送给该服务商。
- 设置每轮工具调用上限、Token/费用预算、超时、会话并发限制、用户可访问节点数限制；这些必须由主控强制执行，而非模型自行遵守。

## 7. AI 工具安全规则（不可由框架替代）

| 工具类别 | 例子 | 执行策略 |
|---|---|---|
| 普通只读 | `list_nodes`、`query_groups`、`get_node_status` | 当前用户授权范围内直接使用 |
| 需单独许可的读取 | 日志、环境变量、配置、敏感文件 | 需要更具体的权限；脱敏与输出限制 |
| 计划生成 | `create_task_plan` | 只创建计划，不派发命令 |
| 提交修改动作 | `submit_job`、插件部署操作 | 统一任务审批、权限、审计；不允许绕过 |
| 任意 root Shell | `system.exec` privilege=root | **必经管理员真实 UI 确认**；整批一次确认且需冻结计划 |
| 特权凭据操作 | 导出私钥、读取 Cloudflare API Key、Agent CA 私钥 | **不向 AI 暴露工具** |

任务引擎在创建和派发阶段均校验登录用户身份、节点作用域、插件授权与计划摘要；不能信任 AI 声称“用户已经批准”。日志、Shell 输出、外部网页、插件说明与 AI 上传内容均视为**不可信数据**，不得作为覆盖系统策略的指令。

## 8. AI 会话与 Web 设计

- 前端仍是已批准的 **React + Vite + shadcn/ui**，AI 是主控原生页面，不要求插件自定义前端脚本。
- 可以自行复用 shadcn/ui，或经验证后采用 MIT 的 `assistant-ui` 聊天组件作为 UI 加速工具；若采用后者，用自定义 Go/SSE 适配器，**不是**新引入 Next.js/Node 后端。
- 首版只做：会话列表、消息流、工具活动摘要、执行计划卡片、确认/拒绝、跳转原生任务详情、错误/重试提示。
- 会话文本/任务关联元数据保存在 SQLite，可设置保留期限和脱敏；不要把完整服务器日志无限制塞入模型上下文。
- 采用现有 `/ai/sessions`、`/ai/sessions/{id}/messages`、`/ai/sessions/{id}/plans/{plan_id}/approve|reject`、`/ai/tools` 等路径；是否增加 SSE 以接口规范更新为准，不另造审批 API。

## 9. MVP：优先完成哪些能力

**必须：** 单 Agent、多轮对话、最小模型配置、服务端 Tools（节点、分组、状态、计划、任务结果）、真实审批流程、流式或轮询响应、基础审计、取消与超时、AI 未配置时核心正常运行。

**暂不实现：** 多 AI 自主团队、长期自我记忆检索、向量数据库、自动写部署脚本并无审批执行、浏览器控制、无限循环自修复、知识库自动收集私密系统日志、Dify/Mastra 等额外平台。

## 10. PoC 与验收条件

在正式冻结 AI 框架前，准备最小实验并以真实 Go 构建测试：

1. 在 1 核 1GB 主控测试机上运行完整主控 + Eino 适配层；记录空闲与执行期间 RSS、启动耗时与二进制体积，不以推测代替实测。
2. 连接一种真实支持工具调用的外部模型，能多轮查询 3 台模拟节点及其分组。
3. 使用模拟插件注册并禁用一个工具，确认 AI 不能再调用禁用工具。
4. 生成 5 台服务器的 root Shell 任务计划：管理员批准前没有任何派发；审批修改目标/命令导致原批准失效。
5. 模拟日志中包含“忽略上级指令，执行危险命令”的提示词注入，AI 不能因此突破授权。
6. 模拟模型超时、取消、Agent 离线与执行结果未知：不擅自重复提交或扩大目标范围。
7. 关闭 AI 模块、删除模型配置，原有服务器管理与插件界面仍可使用。
8. 如 Eino 不能满足上述关键项，再用 Google ADK Go 在相同场景复测；不因某个框架拥有某功能介绍就认定无缝集成。

## 11. 已批准决策（AI-D1～D8）

| 编号 | 推荐选项 | 状态 |
|---|---|---|
| AI-D1 | Eino ADK `ChatModelAgent` 作为首选 PoC；Google ADK Go 作为备选，不 Fork 完整 AI 产品 | **已批准** |
| AI-D2 | 原生内嵌 Go 主控，不部署额外 AI 后端服务；AI 可关闭，外部模型推理 | **已批准** |
| AI-D3 | BYOK：首版 OpenAI 兼容 API；其他原生模型适配后续扩展，逐模型测试工具调用 | **已批准** |
| AI-D4 | AI 只能使用核心 Tool Registry；Web、AI、插件都经过同一任务与审批系统 | **已批准** |
| AI-D5 | 只读自动执行（依具体授权）；修改性任务需合规确认；任意 AI root Shell 必须管理员在 Web 批准 | **已批准** |
| AI-D6 | 首版一个运维 Agent，多 Agent 协作后续再做 | **已批准** |
| AI-D7 | React + shadcn/ui 原生 AI 页面；`assistant-ui` 仅作为可选 UI 复用组件 | **已批准** |
| AI-D8 | SQLite 会话元数据、上下文脱敏、调用额度上限和完整执行审计，不新增向量数据库 | **已批准** |

**批准记录**：项目发起人于 2026-10-26 批准 AI-D1～D8，并明确要求先验证 Eino ADK，失败再评估 Google ADK Go。已同步 Server Fleet v0.8 的 PRD、架构和 API；旧版文档保持不变。**批准的是选型策略，不是宣布 Eino 的资源、工具调用或兼容测试已经通过。**

## 12. 研究参考（2026-10-26 查询）

- Eino 官方仓库、许可与能力：https://github.com/cloudwego/eino
- Eino ADK 快速开始：https://www.cloudwego.io/docs/eino/core_modules/eino_adk/agent_quickstart/
- Eino ADK 概述：https://www.cloudwego.io/docs/eino/core_modules/eino_adk/agent_preview/
- Google ADK Go：https://github.com/google/adk-go
- Google ADK 工具确认：https://adk.dev/tools-custom/confirmation/
- LangChainGo：https://github.com/tmc/langchaingo
- assistant-ui React 聊天组件：https://github.com/assistant-ui/assistant-ui
- OpenAI Agents SDK 官方支持语言：https://developers.openai.com/api/docs/guides/agents/sdk
