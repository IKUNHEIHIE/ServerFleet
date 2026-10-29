# READ-AUTH-DESIGN-01 · 节点只读身份与会话设计

2026-10-29（Asia/Shanghai）；作者codex-01；审查：总控codex-review-01及安全/契约负责人。起始HEAD：`4196b2e8ab9c389ae0830bf56f905d9bafa00d05`；代码/资料对照基线：`0569c1a5ae01ea7715b53a979a3dbb0af50b6b3c`。两者在本任务涉及的internal/apps/api/proto/原始资料/r03中无差异。

**A1+B1方向已确认；本文具体规则均为PROPOSED，未授权实现。** 首版保留既有只读列表，详情延后；浏览器使用服务端会话Cookie，必要的非浏览器Bearer兼容须评审。不再询问相同方向，不修改候选、端口或批准状态。D01–D07/P1-C10仍OPEN，G2未冻结。对应验收见[ACCEPTANCE](ACCEPTANCE.md)，真实命令见[HANDOFF](HANDOFF.md)。

## 1. 来源与效力

R编号是本设计的来源索引。引文为定位短句，必须与其章节一起解读；“候选”“离线事实”“外部建议”均不表示项目已批准。

| ID | 文件/章节及实核行范围 | 原文定位短句 | 效力与用途 |
|---|---|---|---|
| R01 | [负责人选择包](../node-read-01/OWNER-CHOICES.md#L3-L32) | `A1_B1_CONFIRMED_DESIGN_ONLY` | 已确认方向，仅授权本轮设计；优先于旧方案/复审中“尚未选择”的历史叙述。 |
| R02 | [共同规则/本任务](../../../development/READ-DESIGN-01-WORK-PACKAGES.md#L5-L38) | `详细设计仍PROPOSED` | 本任务权限及共同边界，不能代替其他作者承诺接口。 |
| R03 | [功能需求§2.2.3](../../../reference/ServerFleet-ALL-v0.9-G2/01-当前开发基线/ServerFleet-Functional-Requirements-v0.9.md#L85-L94) | `不得泄露其他租户或无权限节点` | 产品约束：服务端可见范围，授权后才计数/筛选。 |
| R04 | [功能需求§10.2–10.3](../../../reference/ServerFleet-ALL-v0.9-G2/01-当前开发基线/ServerFleet-Functional-Requirements-v0.9.md#L373-L398) | `客户端传入资源 ID 不构成授权凭证` | AI/工具适用边界及真实后端负向验收，不能把AI授权claims当普通登录身份。 |
| R05 | [G1正式决定](../../../reference/ServerFleet-ALL-v0.9-G2/01-当前开发基线/architecture/G1-DECISIONS-Accepted.md#L3-L26) | `BOOT、PRIV、TASK、WC、PKI-OPS、UPDATE` | 已批准：Controller统一边界、SQLite WAL方向及不冲突旧条款继承；未批准本文Cookie值/TTL。 |
| R06 | [来源与替代索引](../../../reference/ServerFleet-ALL-v0.9-G2/01-当前开发基线/architecture/SOURCE-AND-SUPERSESSION-INDEX.md#L3-L14) | `不得虚构为已内置本包` | 缺失原文及替代次序；未收录不表示从未批准/失效。 |
| R07 | [AI/PRIV解释附录](../../../reference/ServerFleet-ALL-v0.9-G2/01-当前开发基线/architecture/ADR-AI-PRIV-001-Approval-Interpretation-Accepted.md#L3-L26) | `不替代既有权限模型` | Accepted解释：权限交集、AI硬边界、敏感内容不直出；不是完整PRIV原文。 |
| R08 | [BOOT§12](../../../reference/ServerFleet-ALL-v0.9-G2/01-当前开发基线/architecture/history/05-ADR-BOOT-001-Agent一键安装与安全注册-已批准.md#L289-L300) | `BOOT-D3：首次注册授权` | Agent信任/注册约束，不能作为Web会话、setup或API Token参数的批准依据。 |
| R09 | [G2核心§2](../../../reference/ServerFleet-ALL-v0.9-G2/01-当前开发基线/g2/G2-CORE-CONTRACT-RULES.md#L12-L18) | `不接受客户端自报角色` | 候选：本机setup、会话、CSRF、受限Bearer、失效建议；JWT尚未选型。 |
| R10 | [Merged列表](../../../../api/openapi/g2-merged-candidate.yaml#L86-L113) | `operationId: listNodesForTransfer` | 候选兼容事实：已有列表/onlineOnly/items；[根认证](../../../../api/openapi/g2-merged-candidate.yaml#L15-L16)为Bearer。 |
| R11 | [Merged认证scheme](../../../../api/openapi/g2-merged-candidate.yaml#L6205-L6213) | `name: sf_session` | 候选Cookie名称与JWT标签事实，不是已部署凭据格式。 |
| R12 | [Merged初始化/登录](../../../../api/openapi/g2-merged-candidate.yaml#L4852-L4999) | `operationId: bootstrapInitialAdmin` | 已有候选操作，security:[]只表示没有现有会话要求，不表示setup可无证明执行。 |
| R13 | [Merged退出/身份/会话撤销](../../../../api/openapi/g2-merged-candidate.yaml#L5000-L5139) | `operationId: revokeOwnSession` | 已有候选生命周期操作，不另造并行路由；Bearer退出含义尚须澄清。 |
| R14 | [Merged登录/初始化/会话结构](../../../../api/openapi/g2-merged-candidate.yaml#L9754-L9830) | `仅针对 Cookie 会话的同源 CSRF 防护` | LoginResult已有csrfToken；Session.sessionId须区分公开管理标识与认证秘密。 |
| R15 | [真实读取范围](../../../../internal/auth/node_read_scope.go#L8-L102) | `a client claim is not sufficient` | 离线事实：一次读策略、已知零授权有效、未知拒绝；无凭据认证/跨请求失效。 |
| R16 | [真实节点读取用例](../../../../internal/nodes/read_usecase.go#L20-L95) | `scope, err := auth.PrepareNodeReadScope` | 离线事实：先prepare后库存、同次scope、复制、失败无部分列表；顺序读不原子。 |
| R17 | [策略定义/来源约定](../../../../internal/auth/node_policy.go#L11-L22) | `This type does not authenticate SubjectID` | 不能把ReadGrants中的主体当认证用户；[reader约定](../../../../internal/auth/node_policy.go#L67-L69)的当前性仍需实现。 |
| R18 | [AUTH-01B复审](../../../development/reviews/AUTH-01B-REVIEW.md#L7-L19) | `ACCEPT OFFLINE RULES` | 历史离线接收；旧scope有意保持快照，不是可重放授权票据。 |
| R19 | [N03-A复审](../../../development/reviews/N03-A-REVIEW.md#L7-L21) | `不能直接推出HTTP400` | 历史离线接收；坏库存ID产生内部InvalidCheck，不能归咎客户端。 |
| R20 | [r03待决](../../revisions/g2-r03/DECISIONS.md#L3-L14) | `D01–D07仍OPEN / 未批准` | D05凭据、D06错误、D07输出/缓存仍待批；D08仅技术协调。 |
| R21 | [安全16缺口](../node-read-security-01/SECURITY-ACCEPTANCE.md#L7-L44) | `类型不能强制请求生命周期` | 已接收证据包的A01–A04/A10–A12/A15为本任务重点；不是已实现。 |
| R22 | [W06-R1复审](../../../development/reviews/W06-R1-REVIEW.md#L5-L39) | `真实认证/HTTP/cache/跨主体安全未完成` | 最新历史组件/浏览器证据更新了旧安全包的W05语境；无真实身份保证。 |

外部公开依据于2026-10-29只读核对：E01 [OWASP Session Management](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html#session-id-life-cycle)的ID生命周期、服务端存储、到期/退出章节；E02 [OWASP CSRF Prevention](https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html#synchronizer-token-pattern)的同步Token、Origin、SameSite及登录CSRF章节；E03 [MDN Set-Cookie](https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Set-Cookie#cookie_prefixes)的host-only/前缀属性。Context7先resolve到官方`/owasp/cheatsheetseries`再分别查询会话和CSRF，随后直接核对上述页面。它们支持防护机制，本文数值、支持范围和部署取舍是本项目建议，不能升格为批准或浏览器实测。

## 2. 推荐责任链与凭据选择

推荐流程：**请求入口约束 → 凭据数量/类型判定 → 当前凭据与主体状态验证 → 可信身份结果 → 当前nodes.read策略 → N03-A一次prepare/库存/同次范围 → 授权后筛选投影 → 提交前当前性闸门 → 输出**。query值和HTTP错误先后由HTTP设计组合确认；至少在身份/来源安全失败时不得调用库存，也不得返回成功空列表。

AuthN由auth凭据验证边界证明“谁发出请求”，资源授权由当前可信策略证明“这个主体此刻能读哪些节点”，可信来源由Controller组合根/存储提供并维护所有权。客户端的subjectId、X-User/X-Role、roles、NodePolicy、AI scopeRevision、节点ID和Web source.key都不能替代其中任一环节。上游代理不提供可直接信任的用户身份头；外部同名头须剥离，认证只在Controller验证。已登录不自动拥有nodes.read，更不授予AI、root、任务或插件权限（R03–R09/R17）。

| 请求输入（在验证秘密之前判定） | 推荐内部结果与后续行为 |
|---|---|
| 无认证凭据 | `credential.missing`，无可信主体；策略/库存均0次。空库存同样适用。 |
| 单个受支持会话Cookie | 查服务端当前会话、主体、epoch、时间；成功才生成身份。Cookie可被任意客户端发送，不能凭UA认定是真浏览器。 |
| 单个Authorization: Bearer | 仅接受服务端签发且当前有效的受限API Token，限本设计的只读操作；检查主体、用途、限制、到期/撤销。不把任意JWT/示例串当合法Token。 |
| Cookie与Authorization同时出现 | 即使同主体或其中一个为空/过期/已撤销，也`credential.ambiguous`拒绝；不择优、不回退、不合并权限。普通无关Cookie不算身份凭据。 |
| 重复Authorization、逗号合并凭据、重复会话Cookie、旧/新会话名共存 | `credential.ambiguous`拒绝；不由解析顺序选一个。格式坏/不支持的认证scheme为`credential.invalid`。 |
| 只有旧sf_session | 本提案不自动接受旧名；`credential.unsupported`，迁移须统一批准。它与Bearer或新名同送也拒绝。 |
| 过期/撤销/未知秘密/用户停用/epoch不匹配 | 对外同类“登录状态不可用”，不暴露用户名/撤销原因/秘密；内部保留固定阶段分类。来源故障须另记`identity.unavailable`，不能当无凭据或回退旧成功。 |

上表为语义分类，不是新增Go类型、机器码目录、HTTP状态或错误媒体。尤其凭据成功但策略未知主体是`policy.subject_denied`，已知零节点授权是有效空范围；二者均不同于未认证。策略拒绝/不可用不得被统一改成“请登录”以掩盖故障；内部ErrDenied/InvalidCheck不能单独决定公开HTTP码（R19/R20）。

## 3. 服务端身份、来源所有权与读取约束

建议未来auth拥有用户主体、凭据验证器、会话/API Token记录、用户credentialEpoch与撤销规则；storage只负责连接、事务/迁移，公共结构另审。Controller只注入经过审查的来源、时钟和部署配置；nodes不解析Cookie/密码、不签发凭据。SQLite WAL仅为G1方向，现有存储没有这些实现。

会话记录建议包含：非秘密的随机管理ID、认证秘密摘要、主体稳定ID、credentialEpoch、创建/最近有效请求时间、绝对/空闲到期、状态、CSRF绑定、内部世代。API Token记录另含用途/允许操作及有限精确节点集合。秘密由密码学安全随机源生成，服务端对会话/API/setup认证秘密只存SHA-256摘要及必要元数据，校验避免秘密相关的提前比较；同步CSRF随机值不是登录秘密，须在服务端受保护保存以支持同源补取，不能从摘要反推原值。密码另交经审查的密码验证器/KDF处理，不能用Token摘要方案存密码。随机源、读取/写入/事务或可信时钟不可用时不签发、不放行。记录格式与算法实现均须另审，不新增数据库表。

主体ID由身份目录分配并与策略精确绑定，不由username/displayName、节点名、IP或客户端header推导。账户改名保留同一稳定ID，删除后重建同名账户须分配新ID，旧授权/凭据不能继承。身份目录存在但策略缺主体仍拒绝；目录不可用时不能靠ReadGrants有同名字符串完成登录。

每请求验证凭据的同时，从同一权威身份来源读主体当前启用状态/epoch；首版不做正向认证缓存，不凭客户端过期时间、JWT自载角色或连接建立时结果免查。来源只能从已提交的权威状态生成只读快照，不允许API调用者注入reader或mock。来源map在prepare/复制结束前不得并发写；返回复制隔离不能证明来源读取期间并发安全。多进程/备份回滚的当前性还需存储证据。

Bearer限额是服务端记录，不是客户端claims：有效集合＝用户当前nodes.read集合∩该Token签发时的精确节点上限，允许操作只限既有列表读取；无wildcard、不隐式授予后续新增节点、不允许AI代持/扩大。建议可信策略提供方一次读当前策略并构造受限不可变快照交N03-A，源策略完整性必须先验证，不能通过截掉坏条目掩盖坏策略。现有端口不携带Token限制，如何注入这一请求级求交适配由总控另批；在此适配未实现/验收前，不启用Bearer兼容。撤掉后重新授予某节点可在未超过Token原上限时恢复读取；需要禁止恢复时应撤销Token。

每次调用真正的ReadVisibleInventory，由其调用PrepareNodeReadScope一次；Controller不能先prepare再让N03-A重复prepare，也不能存scope供下次用。认证记录读取/提交闸门不是第二次资源策略准备。策略失败库存0次；库存故障或部分数据+错误、中途坏/重复ID都整体失败、丢弃数据。成功的复制输出才交HTTP/Web做onlineOnly、投影、搜索/计数；不读旧Visible作空集合捷径（R15–R19）。

## 4. 建议参数与生命周期

以下全部PROPOSED，均非旧ADR默认值；负责人无需再次选择B1。数值是首版可审查建议，风险是频繁后台轮询可能保持空闲活性，因此仍强制绝对到期。离线未启动任务15分钟、AI完全访问15/30/60分钟和BOOT注册10分钟各有专用语境，均不是会话期限依据（R05/R07/R08）。

| 参数 | 推荐值/语义 | 取舍及验收点 |
|---|---|---|
| 会话/API秘密、setup秘密、CSRF随机量 | 各独立32随机字节；不复用；会话/API秘密用固定无填充base64url编码 | 256位是本提案；E01支持高熵不透明ID，不是JWT。公开管理ID独立于认证秘密，不能登录。 |
| 浏览器服务端期限 | 空闲30分钟、绝对8小时；到达任一边界即失效，客户端不能延长绝对期限 | 仅成功验证并通过来源防护的请求更新空闲时间，更新不得复活撤销记录；后台轮询也可能更新，不能宣称用户在场。 |
| Cookie持久性 | 会话Cookie，不设Expires/Max-Age；服务端仍强制上述到期 | 关闭浏览器不保证Cookie被删除，恢复浏览器仍须服务器验证。退出清除使用Max-Age=0。 |
| API Token | 默认1小时；可下调，延长需显式管理批准且首版上限24小时；无自动刷新/refresh token | 轮换需经授权重新签发，不能凭旧Token自动续命；期满重签属于兼容成本。 |
| setup能力 | 本机显式生成后10分钟、单用；初始化标记独立持久化 | 独立推荐值，虽与BOOT相同也不继承其批准；不能因数据库为空自动生成/公开。 |
| 请求预算 | 建议最长10秒，传播ctx；不是撤销实时性SLA | 不合作来源/阻塞写入仍需另验收，超时不产生成功/部分列表。 |
| 登录猜测限制 | 建议账户标识+来源分别限速，5次失败/5分钟后延迟或限流；无永久锁定 | 固定失败文案；受信代理解析来源地址；分布式/拒绝服务权衡及MFA/KDF方案仍待安全审查。 |

| 状态/事件 | 服务端转移与必须保持的不变量 | 其他请求/客户端结果 |
|---|---|---|
| 未初始化 → setup待用 | 仅本机受控安装入口生成短期秘密；摘要保存，任何远程bootstrap-status不返回秘密 | 不开放节点读取、匿名管理员或生产mock；状态探测只返回必要布尔量。 |
| setup提交 → 已初始化 | 验证setup、登录Origin防护及输入；原子消费单用能力、创建首位管理员/凭据与持久初始化标记 | 并发仅一成功；不完整事务不能留下可抢占管理员。候选LoginResult自动建会话建议改为完成后普通登录，列入差异。 |
| 登录验证 → 活跃会话 | 候选username/password交可信验证器；配置要求第二因素时必须验证，不能因字段可选而绕过；完成后新建主体绑定会话/CSRF | 不接受调用者指定session secret/subject/角色；销毁预认证状态，防会话固定；只在Cookie传认证秘密。 |
| 活跃 → 再认证/轮换 | 密码重新验证或安全事件需新秘密/CSRF及管理世代，旧秘密原子失效；普通列表GET不轮换或重新Set-Cookie | 首版不设旧秘密宽限；并发旧请求可能失败并须重新发起只读请求。迟到旧列表不能覆盖新Cookie。 |
| 活跃 → 空闲/绝对到期 | 验证时按服务端时间拒绝，清理异步不影响判定；`now >= expiresAt`失效 | 无自动续期或旧结果兜底，UI失效清除并走重新登录；回拨/时间异常不能延长已到期会话。 |
| 活跃 → 退出/单会话撤销 | 所有权检查及Cookie CSRF通过后，先权威提交撤销，再确认退出/清Cookie；管理ID不能撤销他人会话 | 之后验证旧秘密的所有连接均拒绝；只删Cookie不等于退出。存储失败不返回撤销成功，UI先本地清除并固定提示待确认。 |
| 用户停用/密码修改或恢复/角色降权 | 原子提交主体状态或递增credentialEpoch，使其所有会话/API Token失效；不靠逐条缓存清理完成撤销 | 新连接/旧keep-alive均重新验证。增权也推荐重新认证后生效；重启/恢复不能使旧epoch回退。 |
| 单节点资源撤权 | 更新当前授权及独立授权世代，不把它等同密码/会话撤销 | 新请求仍可认证，但新prepare不能读被撤节点；已知零授权有效空集合。活动请求窗口见§7。 |
| API Token签发/轮换/撤销 | 首版推荐经单独授权的Controller本机交互入口，由可信验证器确认当前有资格的真实用户；签发主体取服务端、限额不得超出当前权限；仅向该操作者的受保护非浏览器交互单次交付 | 普通浏览器/管理API不获得Bearer秘密；轮换撤旧并建新原子处理，无旧Token宽限；交付失败不能重放取回秘密，只能撤销后重签。 |
| 初始化后凭据丢失/恢复 | 进入本机受控恢复流程，验证有权操作人、重置验证器并撤销旧epoch；恢复期间管理读取失败关闭 | 禁止远程“空库重置”、通用后门、把CA/恢复解密私钥放入普通API。流程细节见§8缺项，不借Agent注册令牌恢复用户。 |

首位管理员的管理角色不等于nodes.read全量授权：初始化不由空库/角色名合成wildcard。推荐必须有显式可信读取授权记录，已知零集合允许普通用户读出空列表；管理员初始授权应如何形成，由完整PRIV适用条款和总体来源设计对齐，不能在本轮推断为“管理员默认全节点”。本机Bearer签发也不是仅凭操作系统用户名/本机网络即可授权；它需验证Controller主体及当前签发资格，不进入shell参数/历史/通用日志。若以后需要远程HTTP签发/一次秘密交付，必须先核验PRIV及R07适用边界再独立设计，本提案不提供该例外。

§2的凭据选择矩阵适用于受保护读取/管理操作；登录是独立的同源凭据验证操作，拒绝Bearer/重复会话Cookie，却允许浏览器自动携带一个已有Cookie。它不与新密码合并授权；同源完成A→B登录时，原子撤销本浏览器原会话并创建B的新秘密/CSRF/世代，旧Cookie已无效也不能被重激活。若状态存储不能完成该替换，则不签发B会话；其他设备会话是否保留按单会话/全用户撤销事件区分。Web在提交账户切换前就清除A的行/计数，不能等B结果到达才隔离；页面重载可安全补取CSRF不意味着重新签发会话Cookie。

## 5. Cookie、部署及CSRF/Origin

推荐浏览器与Controller API处于**一个明确配置的外部HTTPS origin**，可由受信反向代理转发；不支持跨站嵌入、跨源凭据CORS或共享父域Cookie作为首版假设。外部origin需精确scheme/host/port匹配，禁止通配、后缀匹配和`null`。Host/目标origin依据固定配置校验；只对配置中的代理连接读取其覆盖后的Forwarded类信息，直接入站的伪造头无效。缺配置、未知代理、TLS真实性无法确认均失败关闭；开发环境不得静默降级生产属性。

推荐Cookie名`__Host-sf_session`，属性`Secure; HttpOnly; SameSite=Lax; Path=/`，省略Domain。`__Host-`约束及host-only行为依据E03；Lax允许外部链接进入UI，API来源规则仍独立执行。HttpOnly只保护JS读取Cookie，不阻止XSS借同源请求操作；SameSite不隔离同站不同源，也不代替CSRF（E01/E02）。不选择新`__Host-Http-`前缀，以免在本设计增加额外浏览器支持假设。

认证Cookie不进JSON、URL、localStorage/sessionStorage、Service Worker缓存或Web模型；浏览器JS不可读SID，也不获得Bearer。密码/第二因素由登录表单短暂输入并通过受保护请求提交，不能声称JS从来接触不到用户输入；提交后清除、不持久化。CSRF随机量可经同源no-store响应交JS，仅存内存；它不是身份凭据，单独持有不能登录/获得节点。会话轮换/切换即更换CSRF，比较绑定当前会话，拒绝旧会话Token。

| 场景 | 推荐来源防护流程与失败行为 |
|---|---|
| Cookie读取GET（列表、身份等敏感JSON） | 不要求CSRF Token；绝无业务写副作用。Origin存在须精确同源且非null；缺Origin时，精确同源Referer可建立来源。Sec-Fetch-Site若为cross-site/same-site或与来源冲突即拒绝；若标准来源头均缺，仅接受same-origin Fetch Metadata；两者都不能证明同源则拒绝。顶层导航直接到API不作例外，可导航UI再同源请求。该兼容限制须浏览器实测。 |
| Cookie POST/PUT/PATCH/DELETE（含退出、撤销、未来写操作） | 先凭据数量/有效性检查，再要求Origin同源；Origin缺失用Referer精确origin，二者均无/null拒绝，即使带CSRF也拒绝；同时验证服务端会话绑定同步CSRF Token，缺/错/跨会话拒绝。Fetch Metadata仅补充，不能替代Token或来源。 |
| 登录/初始化（尚无会话） | 要求受保护同源JSON POST、固定Content-Type及自定义请求标记、精确Origin/Referer；拒绝simple form/text请求、跨源/null或两来源头均缺。结合禁用跨源CORS防login CSRF；不假定有已认证CSRF Token，不把攻击者会话固定给受害者。setup另有单用本机能力；这不是身份校验的替代。 |
| 页面刷新后需CSRF | 已验证会话的同源身份初始化读取可补发当前绑定CSRF；候选me响应Identity尚无此字段，需契约owner评审扩展或受保护交付方式。未批准前不能用会话SID代替或新增裸路由。 |
| Bearer-only受控调用 | 不自动带Cookie，不依赖浏览器ambient认证；不要求Cookie CSRF Token。若含Origin则仍校验固定同源，跨源浏览器不在支持范围；无来源头的非浏览器由有效Bearer认证。伪造Origin/Fetch Metadata不能创建身份。无Cookie不是绕过双凭据检查的客户端承诺。 |

自定义CSRF头沿用候选语义[CSRF参数](../../../../api/openapi/g2-merged-candidate.yaml#L6335-L6341)，但HTTP最终状态/媒体未选定；来源失败或CSRF失败都不得返回节点/部分数据。以上严格同源规则比“SameSite=Lax”本身更强，不能宣传为所有旧浏览器/非浏览器Cookie客户端无损兼容，也不从GET免Token推导未来写操作免CSRF。

## 6. 给HTTP/Web与契约owner的语义输入及差异

以下是**本作者提议的需求**，不是DeepSeek/Gemini已承诺的接口；均需总控组合审查，不改ports/DTO/OpenAPI。可信主体结果仅在Controller/auth内部流转，Web接收的identity仅供显示，不能回传当认证。

| 语义输入/输出 | 提供方 → 消费方 | 失效/错误与待对齐项 |
|---|---|---|
| 已认证请求身份 | auth → Controller组合：稳定subjectID、认证方式、非秘密凭据管理ID/内部世代、服务端到期、验证时点 | 不携带原始秘密或客户端角色；只对当前请求有效，不作为可序列化授权票据；新请求重新验证。结构尚不存在。 |
| 当前有效读取策略/观察凭据 | auth策略来源+存储 → N03-A/提交闸门 | 完整验证的只读策略及其配套授权世代；Token时服务端求交。世代不是Node.revision/AI scopeRevision；当前NodePolicy无此元数据，需另批局部适配。 |
| 固定错误阶段 | 入口/凭据/身份来源/策略/库存/取消/提交 → HTTP映射 | missing/invalid/ambiguous与unavailable分开；不读错误原文判断HTTP；后台坏ID属于库存/内部校验故障，不能InvalidCheck→客户端400。具体码和Error/Problem由HTTP设计建议并共审。 |
| 登录态失效/授权失效 | HTTP安全语义 → Web可信应用适配 | 区分重新登录、暂不可用、权限已变；首个观察提交清除旧行/详情/计数并中止/忽略旧请求；错误不能当成功空集合。通知不是实时推送保证。 |
| Web隔离世代 | 可信应用来源适配 → legacy列表加载接缝 | session变更/主体切换/已知降权/退出/失效时换世代；source.key不含秘密，不作服务器身份，重用同key的恢复也须遵守已接收隔离规则。 |
| 来源/缓存当前性 | Controller/代理/Web → 安全验收 | 身份/读取成功及失败、含CSRF的响应建议no-store；禁止服务端正向权限缓存、代理缓存、SW/离线缓存、持久化节点VM。内存只供当前会话展示，见§7残余窗口。 |

| 与现有候选的具体差异 | 推荐与原因 | 总控需协调的范围/兼容风险 |
|---|---|---|
| 列表Bearer-only；详情Cookie OR Bearer（R10、[详情](../../../../api/openapi/g2-merged-candidate.yaml#L5578-L5617)） | A1只给现有列表增加经审Cookie OR受限Bearer语义，保持路径/operationId/items/onlineOnly | 仅方向已确认，security声明未改；不能启用所有旧写接口Cookie。详情仍延后。 |
| cookieSession名sf_session（R11） | 推荐__Host-sf_session以限制作用域，不双读旧名 | 名称是显式契约差异；如存在仓库外旧Cookie客户端须迁移，不能声称已兼容。 |
| bearerFormat:JWT（R11） | 推荐不透明有状态受限API Token，仍使用Bearer传递 | 仅传输方式兼容；不是JWT凭据值兼容，须盘点既有签发方/客户端及独立修订标签。没有证据就不接受外部JWT。 |
| bootstrap返回LoginResult（R12/R14） | 建议初始化提交后再普通登录，setup不直接变会话 | 响应语义有差异，交契约owner；不通过伪造LoginResult绕过，不另加路由。 |
| login返回identity/sessionExpiresAt/csrfToken（R14） | 保留语义，认证秘密只经Cookie；重载后补取CSRF需受保护交付 | me扩展/交付形式待审；不把Identity.roles当权限缓存。Session.sessionId须非秘密管理ID。 |
| logout/revokeOwnSession同时允许Bearer（R13） | Cookie退出撤当前会话；API Token若支持自撤则只撤自身，不扩大到用户所有会话 | Bearer管理范围与新Token签发/轮换操作尚无完整候选；不静默复用sessionId当Token秘密，不默认授予管理权限。 |
| 原Error/Problem、r03 no-store及错误码不同 | 传阶段语义、推荐全链no-store，保留兼容媒体直到共审 | [r03兼容§授权/缓存](../../revisions/g2-r03/COMPATIBILITY.md#L37-L63)及D06/D07仍待决；本设计不先定HTTP媒体/状态。 |

## 7. 撤销观察边界、连接与缓存

建议用同一权威来源的提交顺序定义边界，不靠跨机器墙钟比较：Tauth为当前凭据/主体读取，Tpolicy为本次策略捕获，Tgate为响应写首部/正文前的最后当前性闸门，Tcommit为首字节提交，Tr为撤销事务提交。

1. Tr先于Tauth：旧凭据/降权epoch必须失败，策略和库存0次；换TCP连接、keep-alive、并发HTTP请求都不能复用连接级身份。资源撤权先于Tpolicy时新范围不得含已撤节点。
2. 建议Tgate检查凭据状态/epoch、期限及Tpolicy配套授权世代是否仍匹配；不再调用Prepare、不重读完整策略、不自动重试库存。若观察到变化/不可用/取消，丢弃已缓冲列表，按阶段失败；响应全量就绪前不流式发送items。此闸门、权威世代和缓冲限制尚未实现，不能从现有源码声称有保证。
3. Tr在Tgate后、Tcommit/发送期间：没有共同锁/事务与输出提交协调，就仍存在输出旧快照的窗口；即使再查一次也会有最后检查之后的窗口。已发字节不能撤回；超时不是无泄露证明。D07需与HTTP/存储明确接受或消除何种窗口后才能接线，不承诺“立即撤销所有在途响应”。
4. 节点读取是一次性响应；A1不增加SSE/WSS。未来长连接不能沿用本次认证到无限期，必须另定到期/撤销传播和重认证；Agent mTLS完全独立。
5. 退出在本页立即清内存、递增世代、取消旧读取，并向其他同源标签发不含秘密的失效提示；跨标签消息只是清理提示，不能使服务端相信新身份或授权。其他标签建议在重新可见/聚焦后先清旧内容并重新验身份/读取；迟到结果按世代丢弃。关闭页、网络断开、休眠或远程撤权未被客户端观察时，已显示数据无法远程收回；不把下次HTTP拒绝当成所有屏幕实时清除。
6. no-store不删除既有DOM/内存、不消除BFCache/浏览器恢复或恶意缓存风险。Web需在pageshow（含恢复）、主体/会话切换、已知授权变化及来源不可用时先清旧节点和计数，再重新验证；真实浏览器/代理/Service Worker行为均须验收。不以Vary、节点revision或私有map代替隔离证据。

## 8. 初始化/恢复的安全来源适用性与未定项

最小可信配置入口建议为Controller本机受控配置/安装控制台：固定HTTPS origin、受信代理边界、经过审查的凭据/身份与策略/库存来源、服务端时钟/随机源、状态存储及审计脱敏出口。配置不是客户端可上传的NodePolicy/subject，环境中一个静态“管理员Token”也不能替代用户会话/撤销。未初始化/缺任一所需来源时管理读取失败关闭；进程health不代表可管理节点。

setup秘密只能给有本机控制权的安装操作者，通过独立受保护交互一次性提供，不放进默认配置、URL/命令行参数/日志或Web状态探测；本轮不生成任何秘密。初始化后即使用户表丢失也不能自动重新开放setup，必须进入受控恢复。重启/恢复须保留撤销记录与单用标记的单调性；如不能证明备份未回滚旧凭据，恢复后先令全部旧会话/API Token失效，并保持管理入口关闭直到授权恢复完成。恢复流程不提供普通HTTP私钥上传/下载。

| 来源 | 本设计适用性判断 | 缺项/影响/补证责任 |
|---|---|---|
| G1-D1/D4/D5与有效DEPLOY方向（R05/R06） | 直接约束模块/统一后端鉴权、继承旧安全条款；反向代理与HTTPS只是部署设计假设 | 总控+部署owner核对最终入口/代理真实性；不从网关方向推出Cookie配置已批准或TLS已部署。 |
| 完整ADR-PRIV-001 | **直接相关、未收录/待核验**；影响用户身份/凭据、初始管理员权限、恢复和撤销的最终批准 | 负责人/安全owner从授权原归档提供版本/批准/替代关系与适用条款；R07不能补齐原文。在补证与具体安全复审前不接真实认证；本文提案仍可评审。 |
| AI/PRIV解释附录及AI原约束（R04/R07） | 普通nodes.read不授予AI资源目录或执行；无AI/root流程，不额外引入真人任务审批 | 明文秘密禁入日志/API/模型的适用安全边界有效；首版建议本机受控交付Bearer，不提供HTTP秘密查询/签发回显。受控交付本身仍须与PRIV补证共审；不以Cookie传输SID的必要性推导普通API可查询/导出Token。 |
| ADR-BOOT-001（R08） | Agent首次信任/安装/证书轮换仍有效，本设计不执行Agent注册 | 本机管理员setup与Agent enrollment完全分域；BOOT参数不能自动批准本文参数，无需为A1先实现Agent PKI。 |
| ADR-WC-001 | 原文未收录；本设计需要部署HTTPS但不管理站点证书私钥或签发 | TLS部署/密钥持有边界由部署/安全owner核验相关条款；若未来接网站证书能力再补其完整验收，不将整套WC作为会话设计的无条件前置。 |
| ADR-PKI-OPS-001 | 原文未收录；本轮不动私有CA/信任恢复 | 如管理员恢复依赖PKI/恢复凭据则须由安全owner补证后另授权；正常用户会话不要求先实施整套PKI。 |
| ADR-UPDATE-001 | 原文未收录；本轮无更新/自动迁移执行 | 部署/升级/回滚若影响会话状态、epoch或单用标记，由部署+存储owner补适用条款与不复活旧凭据证据；不机械阻塞无更新能力的离线设计。 |
| G2核心与Merged/r02/r03（R09–R14/R20） | 接口/迁移证据，仍是候选 | 总控协调本设计§6差异、HTTP错误与Bearer发行路径；不采用r02运行时profile、不改候选。 |

具体待齐：PRIV适用原文；登录验证器/KDF与第二因素启用策略/限速审查；受控Bearer签发交付及节点限额适配；会话名/初始化返回/CSRF重载交付的契约迁移；权威身份与策略配套世代/持久撤销；Tgate到输出窗口及真实跨标签/缓存验收。各项为D05/D06/D07原有缺口的具体化，不重新索取A1+B1方向选择。未读其他作者未提交内容，不宣称其设计已同意这些需求。

日志/追踪建议只记随机请求关联ID、固定阶段/结果、受控主体/非秘密管理ID及版本观察点；身份标识按审计权限最小披露。入口到代理/APM/错误链统一排除Authorization、Cookie/Set-Cookie、密码/第二因素、setup/API/CSRF秘密和节点敏感详情；调试模式不例外。固定公开错误不回显数据库路径、源异常或坏ID；登录未知用户/密码错/停用不暴露账号存在性，验证器建议对未知用户采用相当成本的失败路径并统一限速外观，耗时差异仍需负向验证。现有PolicyError固定字段不等于全链脱敏已实现（[当前错误](../../../../internal/auth/policy_errors.go#L5-L45)）。
