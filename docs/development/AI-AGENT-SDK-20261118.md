# AI 推理流、Markdown 与现成 Agent 集成

任务 AI-AGENT-SDK-20261118；会话 codex-ai-agent-sdk-20261118-01。
基线 main d71a2f0bfdd2a03cecab118ca15c73e0c81a2066。
awskr /home/admin/sf-ai-agent-sdk-20261118；feature/ai-agent-sdk-20261118。
本轮按用户要求优先验证功能可行，现有执行与权限组件直接复用，不扩展安全改造。

## 本轮行为

- Markdown：使用 react-markdown 10.1.0 与 remark-gfm 4.0.1，将模型答复、计划、
  推理文字与摘要渲染成标题、粗体、列表、表格及代码块。用户原文、工具参数与任务回执仍按原文显示。
- 推理流：接入供应商实际返回的 response.reasoning_text.delta、
  response.reasoning_summary_text.delta，以及 Chat Completions 的 reasoning_content/reasoning。
  分为“思考过程 · 模型输出”“思考摘要”“执行计划”，不再把简短计划当作模型推理。
  不解析 opaque encrypted_content；该字段仍仅用于原有模型上下文接续。
- 普通聊天通过原 POST/SSE 展示推理预览，验证完成后保存 reasoningContent/reasoningSummary。
  执行运行通过既有运行轮询显示正在生成的推理；临时预览不写入正式回执，
  完成后的 AgentStep 保存独立推理、摘要与计划。
- 协议：模型设置新增 openai-chat-completions，原 openai-responses 继续有效；
  基础 URL 可填写根地址或对应接口地址。保存协议后，聊天、Agent 与能力检测采用同一协议，
  不通过重复模型 POST 自动试错。
- 函数工具：支持 Responses function_call、Chat Completions tool_calls 与旧式
  function_call 回包、流式参数片段，以及单轮最多 8 个函数调用。
  同轮提案在 Eino 内完成工具派发后，沿原持久运行按顺序执行并逐项关联 call_id/回执，
  收齐结果后再调用模型。profile 参数兼容 JSON 字符串数组/数组与字符串/数字超时。
- 现成框架：实际装配 Eino ADK v0.9.23 的 ChatModelAgent、Runner 和工具派发。
  使用当前 BYOK 模型适配器发起真实调用，工具阶段交还既有 Controller 持久任务等待循环；
  重建运行器后按账本继续，不另建一套运行状态或用 Mock 代替生产模型。

## 验证与边界

功能样例已覆盖：Markdown 格式与代码原文、推理/概要/计划区别、API 推理与最终正文分离、
Responses 工具 SSE、Chat 分片参数、Chat 普通/流式 TLS 凭据通道、协议对应能力检测、
Eino 双工具派发与重建后的续接，以及数组/数字函数参数。
完整受影响回归、界面预览与精确 head CI 的实际结果见 PR 回执。

本轮是标准函数工具兼容，不声称支持 OpenAI 所有 hosted tools/Computer Use/MCP
或所有网关的私有变体；模型没有返回公开推理字段时，不虚构完整思考过程。
单轮 8 个函数提案、总 64 步、24 小时、历史/回复容量仍有边界；
工具批次超过剩余步数会明确停止，不静默漏掉调用。
公开推理上限 64 KiB，摘要上限 8 KiB；encrypted_content 不进入界面。

Eino 的 Sonic 在当前 Go 1.27.4 提示回退标准 encoding/json，实际功能验证通过；
未关闭警告，也不宣称该版本的 Sonic 原生性能已验证。
未用真实供应商密钥/真实节点做本轮验收，未自动更新线上程序。

来源：[Eino ChatModelAgent](https://www.cloudwego.io/docs/eino/core_modules/eino_adk/agent_implementation/chat_model/)、
[OpenAI 函数调用](https://developers.openai.com/api/docs/guides/function-calling)、
[React Markdown](https://github.com/remarkjs/react-markdown)。


提交前回执：AI 全包与 Controller server 竞态回归通过；Web 全套检查通过，
policy/contracts 通过。桌面与 390px 组件预览已核对标题、粗体、表格、代码、
三类思考/计划折叠及无横向溢出，浏览器无页面错误。
当前只有依赖内 Sonic 的标准 JSON 回退和既有前端大 chunk 提示，不把提示写为性能通过。
最终精确 head 的完整 CI 结果补 PR 评论。
