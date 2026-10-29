# AUTH-01B · 离线读取预检与不可变范围

2026-10-29；会话 `codex-01`；审查人 `codex-review-01`。分支 `feature/auth-read-preflight-cx01`，实际工作目录 `D:\admin\desktop\codex\myproject\ks.worktrees\feature--auth-read-preflight-cx01`。开始核对 HEAD 为 `1f8f8cbcb9655484d391a287865b38eee9c6885b`，工作区干净。

依据根 AGENTS、README、架构索引、模块归属/依赖规则、TASK-BOARD、CODEX-AUTH-01B-WORK-PACKAGE 和 G2-R02A-REVIEW。仅新增本说明、`node_read_scope.go`、`node_read_scope_test.go`，不修改现有 auth 文件、端口或其他模块，不增加依赖。所有测试数据均为合成输入。

## 接缝与可信边界

`PrepareNodeReadScope(ctx, subjectID, PolicyReader)` 不要求已有库存或目标节点。它校验主体格式，通过显式读取器取得**一次**当前可信策略，复用既有私有 `validateNodePolicy` 校验整份策略，确认主体存在，再复制该主体的精确 `nodes.read` 节点集合。没有构造假节点ID，也不调用 `EvaluateNodePolicy` 去绕过原空目标保护。

subjectID 必须由未来可信认证适配绑定，不能把客户端自报主体、角色或节点授权当成证据。PolicyReader 必须来自可信后端并返回当前快照；代码不能仅凭 Go 类型证明来源可信，也不能发现读取器自己缓存了过期策略。读取器返回的所有 map 在准备结束前须保持只读，不能与准备并发写入。

准备成功返回 `*NodeReadScope`；唯一公共方法是 `CheckNode(ctx, nodeID)`。它只检查一个精确节点，无主体/动作覆盖参数，无通配扩展、管理员例外、组解析或 AI/root/插件授权。返回对象没有 Authorize 方法，不实现或替换 Authorizer，没有接入生产组合根或 nodes。

范围内只有私有、复制后的集合，没有源策略、读取器、主体声明、context 或集合访问器。不返回 map/slice，不给外部修改内部成员的路径。正常 Go 值复制共享的仅是私有只读集合；覆盖某个值为零值不改变另一个值。捕获完成后原 KnownNodes、ReadGrants、嵌套授权 map 被修改/替换都不会改变捕获范围；并发 CheckNode 只读私有集合。

## 分类与取消

| 场景 | 实际结果 |
|---|---|
| 主体已知、有精确授权，策略整体有效 | 返回有效范围；CheckNode 只允许捕获集合中的精确节点 |
| 已知主体无授权，包括库存为空、授权 map 为 nil 或空 | 有效零资源范围；任何合法实际节点检查均 ErrDenied，绝不解释为全部节点 |
| 主体格式合法但未知，包括空策略/空库存 | ErrDenied，返回 nil 范围；仍已读取并校验当轮策略 |
| 空/非法主体、nil context | ErrInvalidCheck，不读策略；主体/目标不 trim、不作身份归一化 |
| nil 读取器/策略、读取失败，即使同时给出可用策略 | ErrPolicyUnavailable，nil 范围，无旧允许兜底 |
| 坏策略、非法ID、悬空授权、其他主体的无关坏规则 | ErrPolicyUnavailable；校验整份策略，不静默跳过坏配置 |
| nil 或零值范围 | CheckNode 返回 ErrPolicyUnavailable（固定字段 read_scope），不是有效零授权预检 |
| 有效范围上的空/非法/通配节点ID | ErrInvalidCheck；合法但未知/未授权节点为 ErrDenied |
| 调用前、读取后、校验/复制或返回前观察到取消/期限 | 标准 context 错误；准备返回 nil 范围，检查不能返回允许/拒绝覆盖取消 |
| 读取器返回包装的 Canceled/DeadlineExceeded | 规范化为标准 context 错误，保留 errors.Is 分类，去掉包装文字 |

分类复用原 `PolicyError` / `ErrInvalidCheck` / `ErrDenied` / `ErrPolicyUnavailable`；`errors.Is/As` 的原语义不变。错误只含固定分类/字段，不含主体、节点、策略内容或读取器异常，原异常不留在 unwrap 链。ctx 的取消/期限优先于读取结果和授权结果。

准备入口、读取后、策略遍历、授权复制以及最终返回均检查 ctx；CheckNode 入口和最终返回同样检查。读取器必须配合 ctx；本实现不启动后台 goroutine 强杀不合作的读取器。最终检查后才发生的取消无法被同步返回值预知，检查成功也不承诺后续工作不会被取消。

## 请求生命周期与未接线部分

每个新请求都必须重新调用 PrepareNodeReadScope。有效、未预先取消且读取器存在的准备只调用读取器一次；格式错误或预先取消可以在读取前失败。未知主体也不能以“无库存”为由跳过预检。CheckNode 不再读取策略，不持有读取器，不是实时撤权接口。

新准备会观察可信读取器提供的当前撤权；旧范围仍准确表达捕获时的集合，捕获之后的撤权不会自动改变它。**不得跨请求复用旧范围为当前权限**，也不得在新准备失败时退回旧范围。此 API 没有认证会话、请求ID或可信请求边界，无法自动识别调用方是否跨请求复用；这是未来可信读取服务必须落实的生命周期约束。本轮没有伪造 scopeRevision、时效票据或全局授权缓存。

没有修改 NodePolicyAuthorizer 的每次实时读取行为、EvaluateNodePolicy 的空目标拒绝、Visible 或 r03 限制探针。未来读取服务仍须安排：空库存也先做预检、授权可见集合后再过滤/计数、输出前如何处理撤权，以及库存/策略版本和序列化的原子性。本范围仅把一次策略读取的结果固定下来，不提供这些存储/输出保证，**D07 仍 OPEN，G2 未冻结**。

旧 NODE-POLICY.md 是 AUTH-01A 当时的说明，不改写其历史；当前 nodes 的错误传播以已接收 N02-R1 和 G2-R02A-REVIEW 为准。本轮没有读取/修改 DeepSeek/Gemini 的任务目录或未提交文件，没有访问节点、云、HTTP 或数据库。

## 实际验证与交接

固定 Windows amd64 环境：Go1.27.3、Node24.22.1、npm11.19.1；使用主目录 scripts/env.ps1 提供本机固定运行时，本工作目录的工具、缓存和 reports 都为忽略文件。原依赖版本不变。

| 命令 | 实际结果 |
|---|---|
| npm ci / npm run tools:install | PASS；127包、固定4个Go工具。保留原 Buf postinstall 许可提醒，没有修改许可配置；实际 Buf 检查通过 |
| go test -count=1 -cover ./internal/auth/... | PASS；总语句覆盖率 **98.6%**；原授权测试保留 |
| go test -count=1 '-coverprofile=reports/auth01b-cover.out' ./internal/auth/...；go tool cover '-func=reports/auth01b-cover.out' | PASS；PrepareNodeReadScope、CheckNode 均 **100.0%**；覆盖报告保存在 reports |
| npm run check:go | PASS：格式、mod verify/tidy、两个入口构建、全包测试、vet/staticcheck；全包176个顶层/648个子测试，0失败；auth 28个顶层/233个子测试，其中新增14个顶层/48个命名子测试 |
| npm run check:policy | PASS：69原始文件/3原候选字节完整性、8项工具测试、Go/Web依赖边界、分支规则和actionlint |
| npm run check:contracts | PASS：旧OpenAPI/Proto/生成一致性/兼容及r02检查；已接线的r03来源矩阵/候选真实正反检查仍通过，14项JS、4个Go顶层/5个子测试；未改变原限制探针 |
| npm run check:security | PASS：npm `found 0 vulnerabilities`，govulncheck `No vulnerabilities found` |
| git diff --check / git diff --cached --check | PASS；只新增本目录3个文件，现有跟踪文件无改动 |

首次覆盖率命令把 `-coverprofile=reports/auth01b-cover.out` 未加引号传给 PowerShell，原工具输出 `no required module provides package .out`、命令退出1，同时包本身 PASS、当时覆盖95.7%。修正参数引用并补复制/最终返回取消测试后，任务单原命令和带报告命令均通过；没有加依赖、删测试或修改检查规则来掩盖该失败。

直接调用新实现的负向场景包括：无库存未知主体拒绝；零授权不能读任何节点；读取器曾成功后失败不回退；源授权/已知节点/外层 map 后续变化不改变旧范围，新准备观察移除；空/通配/跨主体目标拒绝；读取最后返回允许时实际取消/到期仍不能拿到范围；敏感源错误不回显且不留在错误链。复制/返回取消用包裹真实可取消 context 的观察点测试，按成功调用实测检查次数覆盖全部观察点，不硬编码实现次数，也不靠并发时间碰运气。

实际日志/摘要：reports/go-tests.log、check-go.json、check-policy.json、check-contracts.json、check-security.json、auth01b-results.json、auth01b-cover.out、g2r03-results.json 及各工具日志。提交说明记录最终本地 commit，交总控独立复审。

未验证：真实认证/会话/身份来源、策略存储的当前性与快照原子性、跨请求生命周期强制、实际撤权与输出的原子绑定、HTTP/库存/计数/缓存、Linux CI/race 和生产性能。本轮并发测试只证明普通只读并发场景运行通过，不冒充 race 检测。没有实现登录、凭据、节点/API接线、任务/审批/执行或持久化。完成本地提交后停止，不合并main、不推送、不启动其他 Agent。
