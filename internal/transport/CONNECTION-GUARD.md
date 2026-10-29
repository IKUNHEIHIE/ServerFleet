# T01-C · 离线单连接绑定与入站序列守卫候选

日期：2026-10-29（Asia/Shanghai）；会话 `codex-01`；独立审查人 `codex-review-01`。仅离线合成输入，G2未冻结。

## 任务与改动

实际工作目录：`D:\admin\desktop\codex\myproject\ks.worktrees\feature--connection-guard-cx01`；分支 `feature/connection-guard-cx01`；起始HEAD `189112ef23444f3d0d28474fa46c2cb866aded0d`。启动时目录/分支/基线一致且干净。已读根AGENTS、第五批共同约束/T01-C及必读资料、第四批审查。使用最小局部实现，不启动其他Agent。

仅新增 `connection_guard.go`、`connection_guard_test.go`、本说明。既有T01-A/B实现与测试、ports.go、Proto/gen、公共契约、依赖/锁文件、其他模块不改，不增加依赖，不接Sender/Controller/Agent或网络。

## 入口与可信输入

- `NewConnectionGuard(ConnectionBinding)`：显式绑定NodeID、ConnectionID及单一入站Direction，初始高水位0。绑定按值保存；没有从帧自动建绑定、改绑、重置或恢复水位的方法。
- `Receive(raw, ValidationContext, ValidationLimits, FrameDecodeLimits)`：先复用DecodeEnvelopeFrame的原始大小/结构/解码/语义检查，再核对绑定和序列，全部通过才提交水位并返回消息。
- `Close()`：幂等终止该内存连接实例，保留原水位；新帧不能使它重开。nil/零值守卫始终未绑定，不能接收。
- `HighWatermark()`：锁保护的只读诊断值；nil/未绑定返回0，不是身份或绑定证明。

绑定必须来自未来经验证的TLS身份、节点记录和连接生命周期组合根。本轮仅接受可信调用方的显式合成值，类型和非空ID不证明mTLS。不要从Envelope自报ID生成绑定，不要每收到一帧就新建守卫。新连接必须由可信调用方显式创建新实例并分配新ConnectionID，旧ID帧不能进入新绑定。本层没有全局连接表，不能自行证明调用方没有重复使用某个ConnectionID。

绑定ID规则为本轮局部保守约定：非空、合法UTF-8、无Unicode空白/控制字符；不trim、不猜UUID、不做模式或别名匹配。Direction只接受已有AgentToController或ControllerToAgent。缺失/无效绑定返回nil守卫和固定错误。请求中的ID按完整字符串精确比较，不会修改可信绑定。

期望版本、握手阶段、时钟及全部T01-A/B限额仍由可信调用方每次显式传入；Direction还必须与创建时固定的入站方向一致。不会根据payload切换方向、自动推进握手、协商能力或把有效拒绝ControllerHello变为连接批准。

## 原子性与序列候选

每个实例仅有一份私有状态：不可变绑定、高水位、终止标志和mutex。Receive从状态检查、整次有界解码/语义校验到绑定/序列比较及提交，全部在同一锁内；Close和诊断读取也使用同一锁。没有“先读水位再另行写入”的窗口，没有外部业务回调或I/O。

严格递增允许跳号，不要求连续；相等或降低的sequence拒绝。0被既有语义校验拒绝。只比较 `sequence > highWater`，不计算“上一序号+1”，因此最大uint64可以接收，之后不会回绕到0/1，需由可信生命周期管理决定后续连接处理。

所有原始、语义、绑定、序列失败均返回nil消息且水位不变；修正后可重用此前未消费的序号。水位的提交代表传输层接纳，后续业务失败不会由本接口回滚；它不与任务执行或权限事务绑定。相同MessageID也不构成新的去重表，执行去重仍需后续独立设计。

Close在线性化顺序上是终止操作：已经持锁的Receive可先提交，调用方可能因调度稍后才得到该结果；Close返回后**新调用**的Receive全部拒绝。关闭不撤销先前已经返回/提交的消息，不宣称能中断业务副作用。Close会等待当前有界CPU校验退出，不提供墙钟超时或网络中断机制。

外部复制ConnectionGuard句柄只共享私有状态指针，不复制mutex/水位；任意别名关闭后所有别名都终止。构造时的绑定变量和返回的Envelope可在调用结束后修改，不影响内部绑定/水位。调用期间原始slice、语义策略map及活跃句柄不得被调用方并发写入或重新赋值；本层不保护调用方自己的共享内存。

## 错误与未验证边界

守卫错误是 `*ConnectionError{Code}`，可errors.As识别，Code固定为CONNECTION_INVALID_BINDING、CONNECTION_UNBOUND、CONNECTION_CLOSED、CONNECTION_DIRECTION_MISMATCH、CONNECTION_NODE_MISMATCH、CONNECTION_ID_MISMATCH、CONNECTION_SEQUENCE_REJECTED。没有帧/ID/绑定值回显。原解码和语义错误仍保留 `*FrameError` / `*ValidationError`，不吞错误或返回半成品。

本轮来源为现行G2通信候选的node_id仅对账、重放拒绝方向及已接收T01-A/B；严格递增/跳号、关闭生命周期和绑定ID约定仍为局部候选，未锁定重连恢复协议。

只保护此内存实例的一个入站方向；真实证书验证/吊销、跨实例或跨重启防重放、全局ConnectionID唯一性、sequence网络乱序重排、execution_id持久去重、任务关联/授权/审批、恢复隔离、限流/背压及执行均未实现。更高序号仍可承载相同业务内容，必须由下游任务规则控制。成功不形成资源权限或派发许可。原始帧不重叠部分编码拼接等T01-B限制仍有效。

## 实际验证与交接

工具为Go1.27.3、Node24.22.1/npm11.19.1及项目固定依赖；按Context7查阅Go1.27.3同步/锁语义。运行时只读使用主目录已有工具，依赖/固定Go工具安装在本任务忽略目录；不改锁文件或全局依赖脚本权限。

| 命令/检查 | 实际结果 |
|---|---|
| `npm ci` / `npm run tools:install` | PASS；127个npm包、固定4个Go工具 |
| `go test -count=1 -cover ./internal/transport/...` | PASS；98.2%。全包JSON复核transport共40个顶层项、220命名子项（含既有fuzz种子）；新增12个连接守卫顶层测试 |
| `go test -count=20 ./internal/transport -run '^TestConnectionConcurrent'` | PASS；同序号32并发、不同递增序号竞争、32接收与Close竞争各重复20轮 |
| `npm run check:go` | PASS；gofmt、module verify/tidy差异、双入口构建、全包测试、vet/staticcheck |
| `npm run check:policy` | PASS；69原始文件/3原候选完整性、8工具测试、Go/Web边界、分支/actionlint |
| `npm run check:contracts` | PASS；原三份OpenAPI、Buf lint/format/build/兼容与生成一致性、协议负向变异、r02显式26项Node样例及Go往返/WIRE检查 |
| `npm run check:security` | PASS；npm found 0 vulnerabilities；govulncheck No vulnerabilities found |

覆盖：正常递增/跳号、相等/降低/0/最大uint64、错误node/connection、相反绑定方向/语义方向/阶段、缺绑定/关闭、错误版本/ID/枚举/限额、畸形/重复字段/解码失败均不消费水位；纠正后同序号可用；32个相同序号恰好1成功；关闭/句柄别名/显式新连接隔离；13种已知消息真实编码经Receive通过，并保留未知扩展。测试调用实际守卫及其DecodeEnvelopeFrame链路，没有另写测试判定器。

原始缺口探针：错误node_id的合法帧经旧有界解码+语义校验通过，要求连接对账的TestConnectionWrongNodeRejected实际FAIL（exit1）。原入口的职责仅为结构与单帧语义；同名最终测试改为调用新守卫，收到CONNECTION_NODE_MISMATCH，水位0，改正后序号1可用。

实现测试文件时曾残留两个闭合括号，初次构建失败；清理后又发现未使用的proto导入，移除后模块与后续检查通过。保留拒绝覆盖，没有关闭检查或删除失败用例。日志分别为 `reports/t01c-before.log`、`t01c-initial-test-syntax.log`、`t01c-initial-unused-import.log`；最终为`t01c-module.log`、`t01c-concurrency-console.log`、`t01c-{go,policy,contracts,security}-console.log`、`check-*.json`及`go-tests.log`。

UNTESTED：真实WSS/mTLS/身份/吊销、持久状态/任务/权限/执行、网络负载/重连、Linux CI/race及1C1G性能。仅本地并发用例通过，不声称已通过数据竞争检测器或穷举所有交错。最终提交编号见交接消息；创建本地提交后立即停止，交总控复审，不合并main、不推送、不启动其他Agent。
