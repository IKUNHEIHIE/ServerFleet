# T01-A · 解码后的离线语义校验候选

日期：2026-10-29（Asia/Shanghai）；会话：`codex-01`；审查人：总控 `codex-review-01`。**本层实现可调用的纯校验，不代表协议冻结或真实通信安全闭环。**

## 任务、范围和文件

基线 `36ab5cfbb425fdd0b71f69c03e07d59b67cc9ccd`；分支 `feature/proto-validation-cx01`；实际工作目录 `D:\admin\desktop\codex\myproject\ks.worktrees\feature--proto-validation-cx01`。总控原已在 `ks\.worktrees\feature--proto-validation-cx01` 准备相同分支/基线；核对干净后通过 `git worktree move` 移到用户本次明确指定的位置。没有重建/重置分支或改动其他会话目录。任务板等共享文件中的原路径未直接修改，交总控同步。

仅新增以下文件：

- `validation.go`：`ValidateEnvelope`、显式调用上下文/限制、错误类型和消息规则。
- `validation_fixtures_test.go`：13种消息的合成有效样例和注入参数；没有生产默认配置。
- `validation_test.go`：外部测试包直接调用校验器，包含真实Protobuf编码/解码后的验证。
- 本说明：规则来源、候选假设、检查证据和剩余问题。

现有 `ports.go`、Proto、gen/go、原始资料、公共契约、依赖/锁文件、共享脚本/配置保持基线字节不变。没有接入Sender、Controller或Agent入口，没有增加依赖。

## 调用与错误

```go
err := transport.ValidateEnvelope(decodedEnvelope, validationContext, validationLimits)
```

调用方显式传入 `Direction`、`HandshakePhase`、期望 `ProtocolMajor/Minor` 和 `Now`，以及消息/字符串/参数/输出大小、指标/核对/能力条目数、最大超时和offer有效窗口等正数限制。任何零值/非法上下文或非正限制返回 `INVALID_CONTEXT`，没有隐式时钟/环境读取、网络、执行或持久化。`AllowedActions` 是action ID→精确schema版本的局部结构允许列表；nil或空列表拒绝所有TaskOffer，不是授权兜底。

错误为 `*ValidationError{Code, Field}`，可用 `errors.As` 检查；只返回固定字段路径和以下错误码，不包含输入值、参数、ID或原始日志：

`INVALID_CONTEXT`、`MISSING_PAYLOAD`、`INVALID_PROTOCOL`、`INVALID_DIRECTION`、`INVALID_HANDSHAKE`、`MISSING_ID`、`INVALID_FIELD`、`UNKNOWN_ENUM`、`LIMIT_EXCEEDED`、`EXPIRED_OFFER`、`ACTION_NOT_ALLOWED`。

**nil错误只表示本层检查通过**。调用方不能把它当作身份认证、权限/审批通过、计划摘要真实匹配、动作可执行或任务已成功。消息、允许列表均只读；并发调用时调用方也不可同时修改它们。

## 规则来源与候选假设

来源：`docs/reference/ServerFleet-ALL-v0.9-G2/01-当前开发基线/g2/AGENT-TRANSPORT-AND-STATE-MACHINE.md` §1～5，`G2-CORE-CONTRACT-RULES.md` §5，以及 `docs/contracts/revisions/g2-r02/PROTO-REVIEW.md`。已批准G1/TASK/AI安全边界继续有效；G2规则和本层细化仍是候选，不能借此批准wire或运行时提案。

| 范围 | 实现规则 | 来源/限制 |
|---|---|---|
| Envelope | nil、无已知payload、typed-nil wrapper/payload均拒绝；message/connection/node ID非空白且UTF-8；正sequence、正sent_at | 缺payload/ID为本任务最小要求；三种ID在握手期也必填、sequence从正数起和正毫秒时间为**局部候选约定**，不是Proto本身的required语义 |
| 协议/握手 | major/minor必须精确匹配调用方期望；AgentHello只在AwaitAgentHello且A→C；ControllerHello只在AwaitControllerHello且C→A；业务消息只在Established | 方向/初始握手顺序来自原候选；minor精确比较和阶段enum是本层候选接口，不实施版本协商或自动迁移阶段 |
| ControlError | 双向；可在AwaitControllerHello/Established；不能替代首条AgentHello | 双向来自原候选；允许在等待ControllerHello时报告拒绝是局部假设；校验不关闭连接 |
| AgentHello | 版本/平台/架构/boot ID非空；正且有序major范围包含期望major；能力字符串有界非空且不重复 | major范围来自原消息；字段非空和去重是局部细化；未知能力广告不自动授权，能力名称/交集协商未实现 |
| ControllerHello | 接受时selected版本一致、heartbeat/frame/time为正，声明frame不超过显式限制；拒绝时必须有reason | 有效拒绝本身可通过校验；accepted=false时协商结果被忽略，调用方**不得**因此进入Established |
| Heartbeat/Metrics | status非空；batch ID、非空有界points、name/unit、正采样时间；拒绝nil point和NaN/±Inf | 有界消息和采样时间方向来自候选；非空字段为局部约定。不锁定status/name/unit词表、单位范围或指标新鲜度；通用指标不一律限制0～100或非负 |
| TaskOffer | task/execution ID、64位十六进制SHA256形状、action/schema版本；NORMAL或ROOT；UTF-8单个JSON object；参数大小/正且受限timeout；未来且在显式窗口内的expiry | 必要ID/摘要/动作/期限来自任务及原候选；摘要编码和object根形状为本轮候选假设。允许列表仅核对ID/版本，不校验动作参数schema、真实计划绑定或资源权限 |
| ACK/Started | task/execution ID；拒绝ACK需要reason；Started时间为正 | ACK不是成功；拒绝reason必填为局部规则，不创建执行状态 |
| Output | task/execution ID；仅STDOUT/STDERR；正stream_sequence；片大小上限；空片仅允许truncated=true | 片大小外部注入；空截断通知为局部约定。字节不强制UTF-8；不检测内容是否文件或秘密，不验证序号单调/流控 |
| Finished | task/execution ID、非UNSPECIFIED且已知result、正完成时间；可选error_code有界 | 0/999/-1均拒绝，UNKNOWN是合法终态；exit_code=0不被解释为成功。不比对offer期限，不推断执行状态或结果/退出码的完整业务一致性 |
| Cancel | task/execution ID、reason、正请求时间 | 仅验证消息；不终止进程，不宣称回滚 |
| StateSync | 请求/响应sync ID；请求max为正且受限，since≥0；响应执行数受限、ID齐全且不重复、正更新时间、已知结果 | 同步记录UNSPECIFIED允许表示未完成（started可真/假），**不能映射为成功**；SUCCEEDED+未启动拒绝。允许空/截断回复，不将其理解为完整清单或重派发许可 |
| 未知字段 | 只有未知oneof而没有已知payload拒绝；已知消息中的未知扩展字段保持不透明 | 不从未知字段推导权限或新消息；未来字段兼容策略仍需通信owner评审 |

字符串上限按UTF-8**字节数**计；不要求UUID或固定ID格式。`ScopeRevision`值域未冻结，本层允许0但不将任意值作为授权证明。JSON仅检查UTF-8/语法/object根，不证明规范化，不拒绝重复key，不重算摘要，不执行JSON Schema或动作语义；这些仍须受控动作层完成。

## 时间和大小的明确边界

`TaskOffer.expires_at <= Now` 返回EXPIRED_OFFER；未来差值超过显式MaxOfferLifetime返回LIMIT_EXCEEDED。配置和测试使用固定注入时间，生产代码不调用time.Now。本轮不会拿这条规则丢弃已启动执行的ACK/Started/Output/Finished/Cancel/StateSync证据；其时间只检查正毫秒值，没有历史年龄或未来偏移策略。执行超时、离线15分钟、恢复隔离和真正调度仍归统一任务服务。

MaxMessageBytes仅对 `proto.Size(decoded)` 的**重新编码大小**检查；无法代表原始帧大小，也无法阻止解码前的超大输入分配。真实帧限额、解码递归限制、速率/背压、重复oneof/拼接编码的原始wire识别均未实现，不能使用本函数宣称具备这些防护。MaxFrameBytes/输出片/采样/心跳不使用原候选1MiB/64KiB/30秒作为硬编码生产值；测试数值仅为合成边界。

## 实测交接

工具：Go1.27.3、Node24.22.1、npm11.19.1；依赖固定于现有锁文件。运行时只读使用主目录现有工具，依赖/固定Go工具安装在本任务Worktree的忽略目录。没有开放Buf安装脚本的全局权限。

修复前实际探针只调用原Protobuf解码器（没有伪造默认放行的生产校验器）：

```text
go test -count=1 ./internal/transport/...
--- FAIL: TestDecodedEmptyMessageMustBeRejected
decoder accepted an empty Envelope; semantic rejection is missing
FAIL (exit 1)
```

修复后同一个空字节输入仍能解码，但同名测试直接调用新增ValidateEnvelope并断言MISSING_PAYLOAD。有效消息先真实marshal/unmarshal再调用函数；不再用测试内的另一套允许/拒绝算法作验证。

| 检查 | 实际结果 |
|---|---|
| `npm ci`、`npm run tools:install` | PASS；安装127个npm包，固定4个Go工具 |
| `go test -count=1 ./internal/transport/...` | PASS；按任务单原命令执行，`ok .../internal/transport 2.571s` |
| `go test -count=1 -cover ./internal/transport/...` | PASS；`coverage: 99.0% of statements`，14个顶层测试、124个命名子测试（Go全包JSON复核）；13种已知消息有效样例齐全 |
| `npm run check:go` | PASS；gofmt、module verify/tidy差异、双入口构建、全包测试、vet/staticcheck |
| `npm run check:policy` | PASS；69原始文件/3候选哈希、8工具测试、Go/Web边界、分支及actionlint |
| `npm run check:contracts` | PASS；原三份OpenAPI、Buf lint/format/build/FILE兼容、生成一致性、协议负向变异，以及已接线的r02显式检查（26个Node样例、Go13种oneof往返、对原协议WIRE兼容）全部通过 |

实际工具输出位于本任务忽略目录 `reports/t01a-before.log`、`t01a-after.log`、`t01a-module.log`、`go-tests.log`、`check-go.json`、`check-policy.json`、`check-contracts.json` 及各步骤日志。UTC机器结果日期可能为10月28日，按Asia/Shanghai换算属于10月29日。未运行网络连接、证书认证、任务执行或持久化测试；未运行Linux CI/race；未声称协议冻结。

剩余问题：真实mTLS/证书身份与node_id绑定、协商能力交集、连接状态与sequence防重放、Task/Execution/Sync ID的实际关联、动作schema/规范参数/计划摘要和审批匹配、权限/撤权复核、持久去重/恢复核对、真实帧和日志安全均在本轮之外。握手ID/正序号、摘要编码、JSON object、空截断通知等候选假设交总控和通信/安全owner审查。本函数尚未接入任何入口，复审后也不能跳过这些上层门槛直接派发。

完成本地提交后停止，提交编号见最终交接消息；不自行合并、推送或进入下一任务。
