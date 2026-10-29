# T01-B · 有界原始帧解码候选

日期：2026-10-29（Asia/Shanghai）；唯一会话 `codex-01`；独立审查人：总控 `codex-review-01`。**离线实现，未接网络、身份认证或执行；G2未冻结。**

## 任务与文件范围

工作目录 `D:\admin\desktop\codex\myproject\ks.worktrees\feature--frame-decoder-cx01`；分支 `feature/frame-decoder-cx01`；起始HEAD `ff66dea5c5402f7ba13907230759f842e4dea44f`。启动时目录/分支/基线一致、工作区干净。已读工作目录AGENTS及主目录第四批任务单共同约束/T01-B、架构/归属/依赖/任务板与第三批审查。

仅新增 `frame_decoder.go`、`frame_decoder_test.go`、本说明。现有T01-A实现/测试/说明和ports.go保持原样；不修改Proto、生成物、公共契约、依赖/锁文件、共享工具或其他模块，不增加依赖/启动其他Agent。新增入口复用已有ValidateEnvelope，没有更改其候选规则。

## 调用与限制

```go
message, err := transport.DecodeEnvelopeFrame(raw, validationContext, validationLimits, frameDecodeLimits)
```

上下文、时间、语义限制沿用T01-A。新增 `FrameDecodeLimits` 三项均须调用方明确给正数，不使用零值选择库默认：

| 参数 | 含义 |
|---|---|
| MaxFrameBytes | `len(raw)`的实际字节上限；在复制/扫描/分配解码对象/调用protobuf之前检查；等于上限允许 |
| MaxFields | 整帧扫描的总tag预算，包括Envelope、所有已知message子字段、未知group的内部字段和end-group；不按每层重置 |
| MaxDepth | 嵌套层数，Envelope计1，每个已知message或group再加1；同时传给UnmarshalOptions.RecursionLimit |

实现另有公开安全上界 `MaxSupportedFrameDepth=128`，调用方深度只能选择1～128。这是防止依赖解码路径遭遇过深递归的实现能力上限，**不是默认值、推荐配置或已冻结协议阈值**。MaxFrameBytes/MaxFields仍由可信调用方设定；没有隐藏1MiB/64KiB/心跳等生产参数。

调用期间原始slice和语义允许列表须只读。入口不修改输入，成功返回的新对象不引用测试中原始帧的后续改写；失败始终返回nil消息，不能使用protobuf解码中途产生的半成品。nil/空原始slice，包括typed-nil []byte，按畸形空帧拒绝；旧ValidateEnvelope对typed-nil wrapper/payload的保护保持不变。

## 两阶段结构处理与语义接线

1. 校验帧限制有效后立即比较实际输入长度；超界返回FRAME_TOO_LARGE，甚至不尝试解析畸形tag。旧 `proto.Size(decoded)` 只限制重编码大小，不能替代这一步。
2. 用protowire的有界标量/tag/长度消费函数做结构预检。显式验证字段号1～2^29−1、wire type、截断/溢出/长度与group配对。按固定生成descriptor识别message子字段；已知字段的错误wire type拒绝。
3. 扫描使用显式栈，没有自身递归，也不调用会递归跳group的ConsumeField/ConsumeFieldValue。栈只持有原slice视图，长度块不按声称的长度分配或复制。入栈前检查深度，处理每个tag前消耗全局工作预算。已知message的内容和未知group都受预算约束。
4. 仅预检通过后构造Envelope，用 `proto.UnmarshalOptions{RecursionLimit: MaxDepth, DiscardUnknown:false, NoLazyDecoding:true}` 解码。预检已给未知group设置外部深度界限，避免依赖其库内默认group递归上限；已知message解码也显式限制递归。
5. 最后直接调用已有ValidateEnvelope；版本/方向/阶段、ID、动作/枚举/JSON/摘要形状、offer期限等仍由原函数判定，失败保留其类型并丢弃消息。不创建连接/握手状态机，不更新序号/任务记录。

结构预算限制的是解析输入/字段与嵌套规模，不是操作系统硬内存quota或每次调用的墙钟超时。输入bytes已经由调用方持有，本函数不能阻止上游先收取巨型网络帧；未来接收层仍须在读入/解压阶段实施相同或更严格上限。本轮没有网络、压缩、速率或背压实现。

## 重复字段与扩展兼容

本轮局部保守规则来自第四批任务单，**不是Protobuf标准或wire冻结修改**：

- 仅Envelope顶层的已知单值元数据1～7出现第二次即拒绝，即使值相同。
- 顶层oneof20～32合计最多出现一次；重复同种和异种均拒绝，不采用last-one-wins或merge。两条完整Envelope拼接因此会被重复元数据/oneof检查拒绝。
- 不把该规则下推成“所有字段只准一次”。payload原有repeated能力/指标/核对执行仍合法；payload中的普通singular重复继续采用库语义，未擅自改变原候选。
- 合法未知varint/fixed32/fixed64/bytes/group扩展保留；重复未知扩展也保留。未知bytes为不透明数据，不把内容猜成Protobuf再递归，避免拒绝任意二进制扩展。未知group须格式正确且在深度/字段预算内，其内部1或20等字段号不是Envelope字段。
- 未知payload-only可结构解码，但仍被原ValidateEnvelope作为缺已知payload拒绝；未知扩展不能作为身份/授权或新动作依据。

当前固定schema没有packed numeric repeated字段。已知字段的严格wire-type检查若将来遇到新增packed/新消息字段，需要通信owner随契约升级评审；本次不改Proto，也不声称支持尚未批准的新schema。

**不可识别的编码歧义**：Protobuf本身没有独立消息边界。互不重叠的两个部分编码（如元数据部分+单一payload部分）连接后与正常Envelope相同，入口不能知道它们的来源。测试明确保留此合法组成方式；仍依赖上游“一帧一个Envelope”约定。非最短但库接受的varint没有被全面禁止，本入口也不是canonical wire编码器；实际长度预算始终按原始bytes计，不能靠重编码缩小绕过。

## 错误与安全边界

帧错误是可errors.As识别的 `*FrameError`，只含固定Code：FRAME_INVALID_LIMITS、FRAME_TOO_LARGE、FRAME_MALFORMED、FRAME_FIELD_LIMIT、FRAME_DEPTH_LIMIT、FRAME_DUPLICATE_METADATA、FRAME_DUPLICATE_PAYLOAD、FRAME_DECODE_FAILED。不包装原始protobuf错误、不回显帧/参数/标识值；语义错误仍是原 `*ValidationError`。所有错误均搭配nil消息。

成功只表示原始结构、配置限额和既有候选语义检查通过，不证明mTLS/证书身份、能力协商真实性、序号单调、防重放、Task/Execution关联、计划绑定、当前授权/审批、持久去重或执行安全。不能将新入口的成功映射成派发许可。上述门槛仍需后续独立任务与真实安全PoC。

## 实际验证

固定工具：Go1.27.3、Node24.22.1/npm11.19.1、protobuf-go/protoc-gen-go1.36.12。按项目规则先通过Context7查询protowire/UnmarshalOptions当前文档，并只读核对锁定版本的本地实现。没有更新依赖。运行时只读使用主目录现有工具，依赖和固定Go工具安装在本Worktree忽略目录。

原缺口实测（只调用原proto.Unmarshal+ValidateEnvelope的探针）：在有效Heartbeat帧后附加 `protocol_major=1`，两步均通过；要求拒绝的TestRawDuplicateMetadataRejected实际FAIL（exit1），输出 `duplicate Envelope metadata accepted by decode + semantic validation`。随后同名测试改为直接调用新增原始帧入口，收到FRAME_DUPLICATE_METADATA并通过；未添加默认放行生产mock或删除拒绝覆盖。

| 命令/检查 | 实际结果 |
|---|---|
| `npm ci` / `npm run tools:install` | PASS；127个npm包、固定4个Go工具安装完成，锁文件和全局脚本权限未改 |
| `go test -count=1 -cover ./internal/transport/...` | PASS，98.0%；原T01-A测试保留。全包JSON复核transport合计28个顶层项、191个命名子项（含fuzz种子） |
| 13种有效消息、实际原始长度恰好/超界、重编码大小更小的非最短wire、未知扩展超大wire | PASS，直接调用入口；不是在测试里另写防护算法 |
| 重复1～7、13种同oneof、异oneof、完整Envelope拼接；畸形tag/长度/UTF-8/截断/group；无半成品输出 | PASS；字段/已知message与group深度恰好/超界、4096层group拒绝；合法未知扩展/repeated字段和不透明bytes兼容 |
| `go test ./internal/transport -run '^$' -fuzz '^FuzzDecodeEnvelopeFrame$' -fuzztime=10s -parallel=2` | PASS；15种子，95,771次执行，无panic/输入修改或失败消息泄露；不是穷举安全证明 |
| `npm run check:go` | PASS；格式/module verify/tidy差异、双入口构建、全包测试、vet/staticcheck |
| `npm run check:policy` | PASS；69原始文件及3原候选字节校验、8工具测试、Go/Web边界、分支/actionlint |
| `npm run check:contracts` | PASS；原三份OpenAPI、Buf lint/format/build/FILE兼容、生成一致性、协议负向变异，以及r02显式26项Node测试/Go往返/原协议WIRE检查通过 |
| `npm run check:security` | PASS；npm found 0 vulnerabilities；govulncheck No vulnerabilities found |

输出位于本Worktree忽略的 `reports/t01b-before.log`、`t01b-module.log`、`t01b-fuzz.log`、`t01b-check-*-console.log`、`check-{go,policy,contracts,security}.json`、`go-tests.log`及各工具日志。日期按Asia/Shanghai解释。文档和提交范围/空白检查在最终提交前执行；最终commit见交接消息，避免提交自引用。

UNTESTED：网络帧读入/压缩、真实WSS+mTLS/吊销、重放/速率/背压、并发接收组合根、真实任务/权限/审批/持久化、Linux CI/race及1C1G性能。算法预算和10秒fuzz不等于生产安全验收；上游消息边界与所有权限门禁仍未接线。完成本地提交后立即停止，交总控复审；不合并main、不推送、不开始其他任务。
