# R28：AI 运维完整 Agent 执行循环

2026-11-10，V09-CLOUD-AI-LOOP-01 / chatgpt-ai-loop-20261110；独立 `feature/cloud-ai-loop-20261110`，基于 `d4f54ba`。复用已有Go/BYOK架构，不引入未准入的Eino/WASM等Runtime。

## 新增

正式Web AI会话新增「Agent运行」页签：输入目标、规划步数、期限，查看逐步工具、节点、规范参数、工具审批、关联任务、真实脱敏结果和最终答复。后台在当前主控进程中继续，关闭页面不默认取消；重启后由当前登录用户显式继续，不持久保存Cookie或恢复系统身份。

模型原生函数调用 → 当前资源/动作/参数验证 → 必要的真人工具审批 → 唯一任务服务提交 → 持续观察真实终态 → function_call_output → 下一轮规划或最终答复。queued不代表业务成功；运行completed仅表示推理流程结束，实际操作成败以任务结果为准。

| 模型函数 | 动作 | 实际执行 |
|---|---|---|
| inspect_nodes | nodes.read | 已授权节点资料与最新可用监控数据，单次最多50台 |
| self_check | tasks.command | 固定Agent自检，空参数，不是任意命令 |
| run_profile | tasks.exec.v1 | 已安装的普通用户命令配置；精确profileId、policySha256、arguments(JSON字符串数组)、timeoutSeconds(十进制字符串) |
| run_root_profile | host.root | 已安装root-owned配置及受控helper；同上参数，始终真人管理员AI审批及独立任务审批 |

普通命令配置也保留两层真人审批。完全访问不扩大资源，不免除RequiresHuman/Root审批。模型不能自行安装配置、发现未授权资源、增加授权、指定任意Shell/SSH/URL或把自称的taskId当作执行结果。`host.patch`旧注册保留，但因没有本轮执行器，不进入新循环目录。

## 接口与兼容

独立候选：[ai-agent-loop-candidate.yaml](../../../api/openapi/revisions/v09-r28/ai-agent-loop-candidate.yaml)。历史候选、Proto/生成文件、已批准ADR与40项迁移保持。

以下路径均在`/api/v1/ai/sessions/{sessionId}`下：

| 方法与路径 | 行为 |
|---|---|
| GET /runs | 当前owner、当前资源修订下仍有权读取的记录 |
| POST /runs | clientRunId与规范输入/当前模型配置绑定；精确重试返回同一持久运行，并启动有界后台驱动 |
| GET /runs/{runId} | 当前权限重新核对后的步骤/结果/后台状态 |
| POST /runs/{runId}/advance | expectedRevision CAS，使用当前登录身份继续或复用已有驱动 |
| POST /runs/{runId}/cancel | CAS停止规划或请求统一任务取消，等待实际终态 |

HTTPS、现有认证/CSRF和当前owner门保持。新请求拒绝重复/大小写别名/未知字段和额外查询。目标上限24000 UTF-8字节，Web输入8000字符；每会话最多16次运行，一次仅一个当前修订的非终态；默认8步/15分钟，最多12步/1小时，期限含等待审批。单主控最多16个活动驱动，目录最多200台授权节点，结果32KiB，函数历史128KiB。每轮经过原凭据修订/借用围栏、当前权限和窗口预算，不默认启用模型或读取环境密钥兜底。

消息新增只读`agentRunId`元数据；普通消息列表对内部消息只返回运行引用说明，不披露原生规划JSON/不透明推理续传项。普通聊天生成不能重放内部Agent消息，两类模型上下文分开。手动只读工具执行响应可新增`observation`；真实任务仍保留taskId/state。

## 状态与安全规则

状态包括queued、thinking、waiting_approval、submitting、waiting_task、canceling及completed/failed/canceled/unknown/limit_reached。工具审批与统一任务审批分别展示、分别生效，不能互相替代。

规划/提交阶段在外部I/O前持久化；中断后记unknown，不重放。审批或任务等待可以重新登录继续观察。任一目标UNKNOWN、结果不可读取、提交无法确认均停止；相同工具/节点集合/规范参数的重复修改动作也停止。取消不承诺回滚，提交中取消记unknown，已知任务先持久化canceling再发送取消，观察终态后才结束取消。

每次计划/执行/派发/命令准入及结果披露重新检查资源修订、服务组快照与实时成员交集、动作权限和审批绑定。已消费AI审批只供同一不可变任务，不可复用。Root仍走独立管理员任务审批和root-owned helper。授权变化使旧运行失效，新目标不继承旧计划；旧修订非终态记录不会永久堵住新修订。

输出作为不可信数据，使用现有秘密过滤。模型思维链不披露；原生Responses不透明推理续传项只在内部保存。任务结果只回传既有安全状态/日志，不放开任意stdout。

## 修改与删除

修改：tasks.command收紧为空参数固定自检；普通/Root配置执行对绑定本次循环的SourceAI任务开放原有路径。新增统一任务当前门只约束内部`ai-agent-`幂等键，其余Web/已有工作流门保持。无产品功能删除。实现和检查见[交付说明](../../development/AI-AGENT-LOOP.md)，不以源码完成代替真实模型/节点/Root联调、G2或发布准入。
