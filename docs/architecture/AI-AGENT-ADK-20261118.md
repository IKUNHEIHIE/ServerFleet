# Eino ADK 在 AI 模型与工具规划中的接入范围

本轮用户明确要求采用现成 Agent 框架，并优先验证新功能可行性。
本决策针对 Go Controller 中的 AI 模型规划与函数工具派发，不改变节点 Agent 架构。

采用 Eino ADK v0.9.23：它提供 Go 原生 ChatModelAgent/Runner/ToolsNode，
可直接嵌入当前模块化单体，无需另开 Python/Node Agent 服务。
由现有模型适配器连接 Responses 或 Chat Completions；模型实际请求仍由已配置的 BYOK 发出。
Eino 每次处理一个模型/工具派发回合，工具提案交给原持久任务运行器；
下一回合从已记录的调用与真实回执构造上下文。
因此这是现成 Agent 的有界接入，现有审批、异步任务、重启接续等执行底座继续复用。

生产装配入口是 internal/ai/agent_eino.go 和 AgentLoop.think，
不是把归档 Eino PoC/Mock 加入 Release。原实验资料保持不变，不能将其结论扩大为实机验收。
WASM/业务插件运行时、未知工具自动注册与外部工具服务器不在本轮范围。

验证证据与限制见 [功能记录](../development/AI-AGENT-SDK-20261118.md)；
同轮多函数及重建续接已经由真实 Eino Runner 的功能样例覆盖。
版本锁定在根模块；Go 1.27 上 Sonic 的 JSON 回退警告保留，性能结论不扩大。
