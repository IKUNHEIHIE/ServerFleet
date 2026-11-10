# AI 运维循环使用与代码交付

2026-11-10；V09-CLOUD-AI-LOOP-01，接口/产品同步见[R28](../contracts/revisions/v09-r28-ai-agent-loop.md)。本轮按用户分工提供完整代码、必要集中检查；真实联调与找bug由用户进行。

## 使用

1. 按[部署说明](../../deployment/README.md)启动Controller和Web。在凭据页保存允许ai使用的ai_api_key；「AI运维」中为当前用户配置并启用openai-responses模型，配置有效窗口预算。
2. 新建会话，在「资源授权」为具体节点或服务组分配动作：nodes.read查看状态、tasks.command固定自检、tasks.exec.v1普通配置、host.root受控Root配置。后端与用户本身RBAC取交集。
3. 审批策略默认每次审批；自动审批仅对白名单只读工具生效，完全访问有限期且不免除命令配置/Root真人审批。
4. 打开「Agent运行」，输入目标并发起，核对每步节点/参数后批准工具调用。命令提交后点击「查看关联任务与审批」完成独立管理员任务审批。Agent会等待真实结果，继续规划并给出最终答复。
5. 浏览器关闭后主控内继续。主控重启后打开记录、点击继续；若中断在规划/提交阶段会停在UNKNOWN，先人工核对关联任务。取消不代表撤销已完成动作。

命令配置须先按R20安装普通/root-owned配置与helper，并在目标中提供准确profileId和policySha256。模型不能代替操作员创建策略或猜测摘要，缺少配置会说明原因。步数/期限包含等待和最终模型回合，达到上限会明确停止。

## 代码入口

- internal/ai/agent_loop.go：持久状态机、工具审批、真实任务观察、多轮继续、取消和不可变任务当前门。
- internal/ai/agent_provider.go：原生Responses函数调用/结果续传，单步单函数，无自动重试。
- internal/ai/agent_driver.go、agent_http.go：有界后台驱动、重新登录后继续、owner/CAS/严格请求门。
- apps/controller/server/ai_agent.go：实际节点观察、统一任务结果与取消、SourceAI绑定；执行沿用任务服务与mTLS准入。
- apps/web/src/features/ai/agent-run-panel.tsx：正式操作面板，记录/详情/等待/错误/取消和关联审批。

## 检查记录

固定Go1.27.3、Node24.22.1。集中检查实际结果：

| 检查 | 结果 |
|---|---|
| npm ci | PASS，未改锁文件/依赖 |
| go build ./apps/controller/... ./apps/agent/... | PASS |
| go vet ./internal/ai ./internal/tasks ./apps/controller/server | PASS |
| go test ./internal/ai ./internal/tasks ./tests/architecture -count=1 | PASS；最终恢复/披露边界调整后仅复验internal/ai，PASS |
| npm run build --workspace @serverfleet/web | PASS，含TypeScript检查；保留现有大chunk提示 |
| R28 validateContract + redocly lint | PASS，4个路径/5个操作 |
| verify-source、web-boundaries、git diff --check | PASS，原始69文件及3份字节候选不变 |

四个新增专项覆盖：实际任务结果进入下一轮/普通聊天隔离、审批前零执行与UNKNOWN不重放、中断规划和资源修订失效、原生函数拒绝越权节点及并行调用。首轮专项的审批测试替身没有管理员身份，按现有规则被拒绝；修正合成身份后复验通过，未降低产品审批要求。

专项使用合成模型与任务替身，不声称真实供应商、节点或Root执行成功。未全量执行仓库测试/Web测试/安全扫描；真实BYOK/节点、进程重启及完整产品验收由用户联调，不借旧CI标PASS。代码已保存任务分支，PR仍为待接收候选，不自动合并main。
