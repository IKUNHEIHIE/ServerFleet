# 兼容方案与修订差异（待批准）

## 迁移总矩阵

| 主题 | 现有候选事实 | g2-r02 提案 | 兼容/阻塞 |
|---|---|---|---|
| 旧151操作 | 原操作对象未变；多数继承 Bearer/JWT，旧Error/XrayError/无body错误并存 | 保留方法、路径、operationId、成功媒体类型和大多数成功schema；统一跨领域认证和错误 | 是显式profile的破坏性修订，绝不可当作无损替换 |
| 新34操作 | 多数 Cookie/Bearer OR，CSRF 参数只是可选说明；4项显式security=[] | Cookie写操作用同一安全分支内 `cookieSession AND csrfToken`；Bearer独立分支 | 不增加匿名项；四项特殊流程仍要求业务证明，见下文 |
| 选择契约 | 没有协商字段 | 必带 `X-ServerFleet-Contract: g2-r02`；响应确认同名头，Vary包含它；敏感响应no-store | 省略时严格遵循原操作；未知profile副作用前400。未来弃用日期须另批准 |
| 错误 | `{code,message,requestId?}`、`{error:{code,message,details?}}`、Problem或空体 | 统一Problem媒体类型；每个显式错误状态body.status const等于HTTP状态；保留旧错误状态并补通用拒绝 | 内嵌业务对象的 `error` 字段不冒充HTTP Problem；旧客户端用原profile |
| 成功/异步 | 200/201/202/204混合；部分业务仍200 | 保留逐项成功码，不把200机械改成202；`createBatchTask`仍202 | 异步业务逐域202+统一taskId的完全迁移仍E08待审，不能说已完成 |
| 分页 | items、nextCursor、PageMeta、其他业务列表混合 | 保留旧成功envelope与参数；新接口保留items+page | 缺少hasMore/limit的旧列表不伪造；不新增泄露总数；全面分页迁移仍待决 |
| 幂等 | 部分操作缺键；原文组合key/body哈希含义易误读 | 受保护写操作要求键；身份+方法+规范化目标路径+键定位记录，再比较请求摘要 | 拟24h/原子占位/跨重启存储待验证；未知结果过TTL仍不能自动重跑 |
| 任务 | BatchTaskJob六状态；TaskSummary十二状态 | 同一TaskState；jobId/taskId相等；每节点同一ExecutionState | 原六状态客户端遇到扩展状态需升级/明确拒绝，不能虚构成功失败 |
| revision | NodeGroup最低0；ServiceGroup/Node/User/Task最低1；AI/Xray另有版本 | 不重置数值；资源强ETag；保留必要expectedRevision body | scopeRevision、配置revision、协议minor均不能替代资源并发版本 |
| SSE | Last-Event-ID存在；200和204同时列出；无明确410 | 过期410 Problem，去掉含混204，已开流用stream.gap后关闭重取快照 | 使用支持请求头的fetch SSE；原生EventSource不能设profile头，不能把Token放URL |

逐操作证据见 [OPERATIONS.md](OPERATIONS.md) / [operations.json](operations.json)。后者包含每项前后认证、旧错误完整声明、新错误状态码、成功码、传递schema变化和操作哈希；测试会从源重算并拒绝遗漏/过期矩阵。页面包裹不变不代表传递schema完全不变，特别是 BatchTaskJob/TaskSummary。

## 认证与特殊流程

1. 首先完成profile支持探测，再允许新客户端发写请求；可用携带profile头的bootstrap-status读响应检查确认头。旧服务可能忽略陌生请求头，**不能靠写完成后的响应检查保护副作用**。总控须批准能力探测/服务端切换方案，再实现路由；未知profile不能静默回落，客户端不能失败后自动换旧profile重试写。
2. 无profile的旧151操作仍仅Bearer，Cookie不能被偷偷接受；旧34项按原安全声明保留，但其原有安全缺口不因此获准实现。profile选择不改变RBAC、作用域、审批与审计。Accept只选媒体类型，不推断身份或profile；新profile不能提供Problem媒体类型时406、禁止返回200包装错误。Cookie与Bearer同时出现时400，避免凭据优先级混淆。
3. 新profile读：有效Cookie **或** 受限Bearer。写：有效Cookie **且** 与会话绑定的CSRF、Origin/Host验证，**或** 独立受限Bearer。CSRF不能作为单独OR分支；Bearer也须当前角色/资源校验。跨域、无Origin的浏览器写默认拒绝；非浏览器走受限Bearer。
4. `getBootstrapStatus` 只暴露初始化布尔信息；`bootstrapInitialAdmin` 必须一次性本机短期setup token、初始化状态原子检查和限流；`login` 校验同源、限速、错误不枚举用户名、旋转会话ID；`enrollAgentWithCSR` 不依赖浏览器Cookie，必须可信HTTPS/Root SPKI固定、单用token、CSR持有证明。`security: []` 是无通用会话认证，**不代表这些证明可以省略**。本轮不补造密码学证明协议。
5. Cookie提案：Secure、HttpOnly、SameSite=Strict、Path=/、无Domain。login/bootstrap结果要求csrfToken并设置Cookie；读取刷新CSRF的机制及会话idle/absolute TTL仍待设计，不能把未定义的/me当作已有恢复方案。注销、密码变更、用户停用/降权须立即撤销受影响会话；旧Bearer也必须可撤销，不能靠JWT标注默认可信。
6. 不新增Bearer签发API。签发主体、scope/TTL、哈希存储、轮换、撤销、审计待安全owner决定；未批准前禁用非浏览器真实写接入。注册token、插件凭据、用户Bearer、Agent mTLS身份互不通用。

## 错误、幂等与响应

依据 [RFC9457](https://www.rfc-editor.org/rfc/rfc9457.html) 的Problem媒体类型，以及 [RFC9110](https://www.rfc-editor.org/rfc/rfc9110.html) 的条件请求语义。项目额外要求 `type,title,status,detail,code,requestId`；这不是声称RFC要求所有这些字段。type用受控URI，code是稳定机器码，title/detail不得直接复制异常、旧message、SQL/环境/密钥；客户端容忍未知code并显示脱敏通用提示。旧 `Error.message` / `XrayError.error.message` 只能经过同一白名单转换，不透传details。

| 场景 | 新profile结果 |
|---|---|
| 未认证 / 已认证无权 | 401 / 403；敏感资源可一致404，先鉴权再查版本，防泄露 |
| 字段/双凭据/两个revision不一致/非法ETag | 400；已存在旧422保留逐域校验语义 |
| 同幂等键不同摘要、任务状态不允许 | 409 |
| 并发前提存在但已过期 / 必须前提缺失 | 412 / 428 |
| 限流 / 服务不可用 | 429 / 503；重试只在操作本身可安全重试时进行 |
| SSE游标不可续接 | 开流前410；开流后stream.gap明确缺口并关闭 |

幂等摘要至少含规范化body、query、If-Match/expectedRevision、计划/目标/审批/协议profile等影响行为的字段；认证Token不进入明文摘要/日志。相同主体+操作目标+Key查到不同摘要立即409，**不能把body哈希放入查找主键，导致换body漏查**。不同节点路径不能共享同一幂等记录。并发相同请求只能原子创建一次操作记录；进行中返回同一任务/明确进行中结果，崩溃后先核对而非重跑。

相同请求重放前重新鉴权、检查当前scope/审批有效性，撤权后拒绝，不能泄露旧响应。对只显示一次的Token/凭据结果，副作用仍去重，但不能再次直出秘密；返回409 `IDEMPOTENT_SECRET_UNAVAILABLE`及安全操作引用，原发放结果丢失后由受控撤销/重新签发流程处理。本profile对**所有受保护写方法**加键（也包括POST预览），是否缩小为真正修改操作须逐项批准。拟24h仅为评审参数，未实现存储，也不保证恰好执行一次。

## revision映射

强ETag提案：`"sf-rN-<64hex>"`，N保持原资源revision，摘要绑定profile、资源URI和授权过滤后的规范表示。限定单一、非弱、非通配标签；服务端必须比较**完整标签**，不能仅解析N。这样相同revision的不同资源/不同可见表示不会错误共用强标签。所有表示变化需增版且更新映射原子一致；body expectedRevision是并发版本兼容别名，不是完整表示验证器。规范化表示/摘要算法及权限变化后的映射须owner批准和真实测试，不能宣称强验证器已正确实现。

| 资源/操作 | 数值/前提 |
|---|---|
| NodeGroup更新、替换成员 | 保留原body必需expectedRevision≥0；新If-Match可选，双传必须相等。不能用header代替现有必需body |
| NodeGroup删除 | 新profile必需If-Match；原profile不静默新增前提 |
| ServiceGroup替换成员 | 保留原body必需expectedRevision≥1；If-Match可选且相等 |
| ServiceGroup配置/调度/删除 | 新profile必需父ServiceGroup的If-Match，不新增第二成员版本 |
| User/Node修改、暂停/吊销 | 必需User/Node资源If-Match；NodeActionInput.expectedRevision若传必须相等 |
| Task审批/取消/恢复、旧advance/stop | 必需同一Task的If-Match；TaskAction.expectedRevision若传必须相等；审批还绑planDigest/内容/目标/当前权限 |
| AI scopeRevision | 单独授权epoch；授权/成员移除/撤销导致失效。不是资源ETag，也不由模型自报可信 |
| Xray revision/expectedCurrentRevision/expectedRevisionByNode | 属于配置历史/各节点配置的并发域；本轮原值保留，禁止套用组或任务ETag；E02/E08待审 |
| Envelope major/minor、scope_revision | 协议兼容版本/授权快照提示；均不能在Agent上替代Controller实时授权 |

无前提428优先于业务schema缺字段的普通400；非法/双传不一致400；过期412。客户端收到412先重取资源和计划再人工确认，不能自动替换新revision执行。判断与写入需同一原子事务，这仍未实现。

## 唯一任务与状态兼容

公开唯一入口 `/api/v1/tasks`；历史 `/jobs`/`jobs.submit` 保留文献/内部能力名，不新增可写别名。`jobId`路径变量、旧BatchTaskJob.jobId、新taskId、Proto.task_id都标识同一持久任务；executionId标识每节点运行。旧advance只推进未执行批次，旧stop只停后批，不直接等价于取消运行中进程；cancel为尽力终止。

| 主任务状态 | 含义与旧六状态兼容 |
|---|---|
| awaiting_approval / queued / running / completed / failed / canceled | 旧有六值，保留拼写；completed只在可靠结果支持时成立；canceled不等于副作用回滚 |
| waiting_node / paused / canceling | 旧模型无值；保留真实状态，需升级客户端或明确409 `CLIENT_UPGRADE_REQUIRED`，禁止压成queued/running |
| partially_failed / unknown / expired | 旧模型无值；禁止压成completed/failed；unknown需要现场核对，expired未启动工作不再派发 |

新profile的BatchTaskJob增加taskId、revision、planDigest（必需）及expiresAt（可选），jobId保留且必须等于taskId，state指向同一TaskState；results中state改为ExecutionState。旧TaskSummary/TaskExecution的字段保留，枚举提取为共享schema。单一任务记录投影视图，不能另写第二份状态。旧profile读取扩展状态的409升级拒绝属于待批准兼容行为，**不在原候选上静默回填enum**。

调度允许方向：awaiting_approval→queued；queued/waiting_node→running；paused→queued仅未执行部分且重新鉴权/审批有效；running→可靠终态或paused/canceling；canceling→真实收尾结果。unknown/expired/completed/failed/canceled不可由resume自动重新执行。迟到的可靠完成证据可以审计核对修正unknown的观测结果，但不能导致新派发。取消请求不是完成事件。

每节点枚举保留 queued/waiting_node/offered/acknowledged/running/succeeded/failed/cancel_requested/canceled/timed_out/unknown/expired。Proto结果枚举只表达终态；UNSPECIFIED不能映射为成功，未知数值必须拒绝作为执行依据。TaskAck.accepted不是succeeded。输出截断是独立布尔量，不把截断变成成功。

目标提交时冻结；执行有效集合为目标快照∩当前操作者RBAC∩当前AI有效授权（若适用）∩当前服务组成员约束；新增成员不加入旧任务，移除/撤权阻止后续执行。普通并发默认5、root/修改默认1→5；首批失败只停后批，本批已开始者收尾。离线900秒TTL只限未启动工作；已启动按真实timeout/cancel/unknown处理。有副作用unknown和主控恢复隔离期间不得自动重跑。完全访问仍要求AI任意root Shell真人管理员批准。

## SSE迁移限制

游标按主体、资源和流绑定；Last-Event-ID只用于续接，每次仍鉴权。重连可容忍传输重复，客户端按事件ID去重；不能保证业务恰好一次。过期410后获取授权快照，明确展示缺口再重订阅；读快照与订阅的水位一致性需真实存储证明。事件/心跳不得输出密钥、原始环境和无权节点。原生EventSource与profile头冲突已登记，禁止以URL中放Token绕过；fetch SSE适配尚未实现。
