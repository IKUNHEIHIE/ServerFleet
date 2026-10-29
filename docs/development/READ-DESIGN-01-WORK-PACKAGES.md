# A1+B1 · 首条只读列表的三份具体设计任务

日期：2026-10-29（Asia/Shanghai）；总控codex-review-01。负责人在本会话明确回复“按推荐 A1+B1”，确认[OWNER-CHOICES](../contracts/decisions/node-read-01/OWNER-CHOICES.md)所限定的下一轮设计方向：**先兼容只读列表、后详情；浏览器服务端会话Cookie，非浏览器Bearer兼容须评审**。三项任务现已登记，待负责人将提示词发给原三个作者会话；没有自动启动或工具代发消息。

## 共同授权、交付与验证

- 任务是具体设计/兼容差异/可执行验收计划，**不是开始实现**。A1/B1方向已确认，不再把这两个问题重新交给负责人；详细设计仍PROPOSED。D01–D07/P1-C10保持OPEN，G2未冻结，r03/r02仍是候选工件。
- 先核对各自目录、分支、实际起始HEAD/干净状态，读AGENTS、README、架构索引、MODULE-OWNERSHIP、DEPENDENCY-RULES、TASK-BOARD、本单、OWNER-CHOICES、node-read-01方案/验收、node-read-security-01来源/16缺口、r03的DECISIONS/COMPATIBILITY/MATRIX、W06-R1-REVIEW。代码/资料审查基线为0569c1a5ae01ea7715b53a979a3dbb0af50b6b3c；新目录从纳入本单的本轮最终main创建，**精确起始HEAD随交接提供，作者在HANDOFF实记**。
- 每位只新增其表中3份Markdown，目录不重叠。引用原始需求/已批准约束/候选/本地实现/自己的建议必须区分状态；引用具体路径及章节/行，核对实际内容，不能只检查路径存在。库/框架/SDK/API/CLI的具体用法按项目Context7规则；本地业务事实/代码审查无需联网补成“已批准”。
- 不修改既有文件、其他设计目录、ports、API/Proto/gen、依赖/锁、配置/CI/scripts、数据库公共结构、原始资料/ZIP/已批准ADR或批准状态；公共接口需求以设计差异表交总控，不直接更新契约。不得访问凭据/生产/VPS/云、私人外部归档或远程仓库，不合并main/推送/同步分支/重写历史，不启动或代发其他Agent消息。
- 现有只读能力仍只是离线：auth接收可信subject字符串和当前只读策略，N03-A先预检再库存、同次scope；没有登录/会话、可信来源存储或HTTP实现。不能把已知策略主体当已认证用户、source.key当会话/授权证明，也不能把重新读一次当输出原子性。
- 三位可以并行做设计，不等待/读取其他人的未提交文件。以现有已提交端口/候选/来源为依据，给出跨设计的**输入/输出语义、提供方、消费方、错误/失效条件、假设和待对齐项**；待总控组合评审统一，作者不得将其提案写成另一位已承诺的接口或新增产品选择。
- 交付DESIGN.md、ACCEPTANCE.md、HANDOFF.md。DESIGN写推荐的具体规则/流程/兼容差异和明确未定项；不能只有原则或抄旧缺口。ACCEPTANCE逐项写输入/时序、可观察结果和未来验证层次；本轮未执行的HTTP/认证/浏览器/存储保持UNTESTED，既有PASS标明历史来源。HANDOFF写任务/会话、实际目录/分支/起始与最终提交、3文件、核验命令/输出、候选依赖、未定/阻塞与风险。
- 检查引用原文/批准适用性、三设计的共同边界、git diff --check；可跑check:policy并如实保存输出。纯文档不新增镜像测试，不因不运行全量业务测试而伪造PASS。提交遵循docs(scope): 描述，仅本地提交后交精确HEAD并停止。
- 工具环境可只读使用主目录`D:\admin\desktop\codex\myproject\ks\scripts\env.ps1`提供固定Go/Node；不要把主目录node_modules/缓存当自己的可写目录。不加依赖；如需要本目录工具安装按现有流程，临时日志仅忽略reports。

## READ-AUTH-DESIGN-01 · Codex

| 项目 | 授权 |
|---|---|
| 唯一会话 / 审查人 | codex-01 / 总控+安全与契约审查 |
| 分支 | docs/node-read-auth-design-cx01 |
| Worktree | `D:\admin\desktop\codex\myproject\ks.worktrees\docs--node-read-auth-design-cx01` |
| 允许新增 | docs/contracts/decisions/node-read-auth-01/{DESIGN,ACCEPTANCE,HANDOFF}.md |
| 核心决定 | B1下可信身份/凭据生命周期的具体设计，重点D05与安全A01–A04/A10–A12/A15 |

DESIGN至少交代：

1. 请求→凭据验证→可信主体→当前nodes.read策略→N03-A的责任链；浏览器Cookie与非浏览器Bearer的支持范围、缺失/无效/过期/撤销/双凭据的处理建议。区分AuthN、资源授权、可信来源，不让客户端subject/角色或AI资源claims成为服务器身份。
2. 服务端会话与必要Bearer兼容的初始化/签发/验证/轮换/退出/撤销、停用/降权、跨请求/连接的生效边界、来源当前性/所有权与未来存储职责；给出可审查的生命周期状态表和参数建议。项目的离线未启动15分钟TTL不是登录会话TTL，不能直接套用；候选JWT标签也不是已批准Token选型。
3. Cookie属性、来源/站点/部署假设、CSRF/Origin适用范围与流程、浏览器是否可读任何凭据、日志/追踪脱敏、跨主体/会话隔离。B1不是只写一个HttpOnly关键词，不从只读GET推导未来写操作豁免。具体值均标提案/来源，不声称实施。
4. 初始化/恢复管理员与凭据的边界、最小可信配置入口及失败关闭条件；不能为方便引入匿名管理员/默认放行、生产mock、硬编码Token或暴露CA/恢复私钥。新认证操作需求只写操作/责任及安全语义差异，不新增路由文件/OpenAPI修订。
5. 对现有G1/有效安全ADR与缺失PRIV等原文逐项说明适用性。当前包只有AI/PRIV解释附录，不能冒充完整PRIV；WC/PKI-OPS/UPDATE按本设计是否实际涉及分别判断，不机械把四份都列为本任务前置。找不到所需条款时具体登记“未收录/待核验、影响哪条设计/谁补证”，仍完成可做的提案，不能捏造或搜索项目外私人归档。
6. 给DeepSeek/Web的可信主体结果/错误阶段/会话失效通知等语义输入表；不擅自修改ports或先定HTTP状态媒体。安全与兼容争议列推荐及依据，由总控组合评审。

ACCEPTANCE覆盖有效→过期/退出/撤销/停用/降权、双凭据、伪造subject/资源声明、空库存也认证、零节点授权与未认证的区别、当前策略/旧scope误用、Cookie/CSRF/Origin和敏感日志、会话主体切换。均写明确预期与模拟时序；真实运行UNTESTED。

停止：提交3份设计文档，不写auth/HTTP/存储或签发凭据，不接真实登录；收到设计复审意见再返修。

## READ-HTTP-DESIGN-01 · DeepSeek

| 项目 | 授权 |
|---|---|
| 唯一会话 / 审查人 | deepseek-01 / 总控+安全/契约/节点审查 |
| 分支 | docs/node-read-http-design-ds01 |
| Worktree | `D:\admin\desktop\codex\myproject\ks\.worktrees\docs--node-read-http-design-ds01` |
| 允许新增 | docs/contracts/decisions/node-read-http-01/{DESIGN,ACCEPTANCE,HANDOFF}.md |
| 核心决定 | A1的操作级兼容/来源/错误与撤权观察边界，重点D01/D06/D07及安全A05–A15 |

DESIGN至少交代：

1. 保留唯一GET /api/v1/nodes、listNodesForTransfer、items、必填id/name/online及onlineOnly:boolean默认false；给出原候选→A1拟形状/错误/凭据兼容差异表，哪些旧可选字段首版不输出、依据/代价及旧客户端验收。Cookie加入旧Bearer-only列表仍是待审查的契约变更，不因B1方向确认写成schema已改。
2. 明确认证/策略预检→N03-A库存读取与同次授权→可见集合的onlineOnly过滤→字段投影→序列化/响应提交的顺序，以及query解析阶段和错误先后。给出onlineOnly省略/true/false/坏值的具体解析提案、空集合/隐藏在线节点及策略失败处理；不因筛成空跳过预检。无新增集合路由/profile、分页/total或GET副作用。
3. 为id/name/online及拟保留region/provider等字段写来源、缺失/矛盾/非法时行为、字段级敏感性和可见性；A1不输出status/revision/platform/agentVersion/lastSeenAt/sampledAt，不伪造这些字段。online必填仍需可信来源/冲突处理提案，不能因不输出status就跳过状态来源问题；不把online=false扩成某个五状态权威结论。无共同采样证据的指标不作为新鲜读数，选择省略/保留可空旧值及越界处理须明确兼容依据。
4. 错误按发生阶段/成因分类：凭据、整体策略拒绝/不可用、客户端query、后端坏/重复ID、库存未知错误、部分数据+错误/混合错误、取消/期限、序列化/提交前后。给出HTTP状态/机器码/媒体/固定安全文字/no-store的具体建议及理由，不把auth.ErrInvalidCheck一律400或ErrDenied一律403；不把后端坏数据归咎客户端，不暴露敏感ID/原错误链。维持旧Error与Problem差异的兼容设计，未来详情防枚举只作接口边界，不实现或扩A1范围。
5. 可信策略/库存来源提供方、快照所有权与生命周期/并发约束、每请求新scope、撤权/读取/输出的**明确观察与生效边界**。现有顺序来源调用没有共同事务/输出原子性，分别说明可保证、不能保证的窗口和后续存储依赖，不能靠一次重读或私有字段宣称消除竞态。A1所需基础来源必须有设计，不能用公开默认mock替代真实闭环。
6. 服务端/代理/浏览器成功与错误缓存的建议和跨主体清理责任，旧客户端兼容矩阵；与auth/Web设计需对齐的语义需求/假设列表。所有具体HTTP/存储方案仍待审，不能修改OpenAPI/数据库或批准D项。

ACCEPTANCE覆盖onlineOnly全矩阵、空库存未知主体/已知零授权、预检失败库存0次、同次策略一次读取/新请求撤权、后端坏ID与坏query分开、隐藏计数/字段、部分来源错误/混合错误、取消/超时各阶段、no-store和撤权输出窗口。明确每例应如何观察及HTTP/源调用/存储层证据，未来运行UNTESTED。

停止：只提交3份Markdown，不实现Controller处理器/DTO映射/来源/数据库，不改既有方案或候选，待总控组合审查。

## READ-WEB-DESIGN-01 · Gemini

| 项目 | 授权 |
|---|---|
| 唯一会话 / 审查人 | gemini-01 / 总控+UI/契约/安全审查 |
| 分支 | docs/node-read-web-design-gm01 |
| Worktree | `D:\admin\desktop\codex\myproject\ks\.worktrees\docs--node-read-web-design-gm01` |
| 允许新增 | docs/contracts/decisions/node-read-web-01/{DESIGN,ACCEPTANCE,HANDOFF}.md |
| 核心决定 | A1+B1下legacy列表ViewModel/加载与会话失效的具体设计；保留W05/W06有效隔离约束 |

DESIGN至少交代：

1. legacy NodeSummary DTO→局部列表行类型的逐字段转换/验证表，合法、缺失、非法、未知字段/扩展名冲突的建议。保留id/name/online和onlineOnly语义，首版无详情/富化字段/新鲜指标；不得把online=false猜成offline/suspended/revoked，不补假revision或now/sampledAt，不把可选公开string与局部null混淆。
2. 当前NodeListItemView/NodeSource/NodeLoader/Workspace依赖status/revision等富化字段，写清**哪些逻辑可复用、哪些需要新的legacy类型/加载/展示接缝、预计涉及哪些文件和最小改动**，给出推荐结构与兼容取舍。不能靠强转、假字段、给生产引入fixture、放宽既有富化VM约束或一句“直接复用NodeLoader”解决类型不匹配；本任务只写设计，不写TSX/代码文件。
3. 页面组成/列和文案：只读列表、在线布尔的准确表达、loading/错误/重试、空可见列表与本地无匹配、服务端onlineOnly与本地搜索各自作用、已加载数量的口径、无权节点不进计数、指标无时间时如何标缺失。详情延后但不永久移除，R1-B迁移点明确。
4. 浏览器B1会话请求与auth失效的来源key/世代责任：key是可信调用方维护的隔离标识而非认证；退出/过期/撤销/降权、A→B、有效→无效→恢复、同key不可用恢复时首提交清除旧行/计数/详情，旧成功/失败与unmount/retry不覆盖。不要在客户端保存Token原文或信任自报主体；尚缺的后端失效反馈/来源边界明确写假设，不声称Web能独自证明撤权。
5. 错误按可信服务端语义分类/固定提示、取消/重试规则（仅只读）、成功/错误缓存与跨会话清理、Cookie/CSRF适用待对齐需求。不能臆造HTTP状态/错误body已统一；不追加真实fetch/App/登录或额外契约。
6. legacy方案下键盘Tab/Shift+Tab/Enter/Space、焦点可见和重试/来源失效去向、420/1280布局/横滚/长名称的未来验收计划。W06是富化fixture组件的历史证据，不能自动转移为新legacy组件/真实会话的PASS；原body焦点回退也不作为未批准的产品新保证。

ACCEPTANCE给出DTO边界、未知/缺失字段、onlineOnly/计数、缺时间指标、请求/来源/身份失效顺序、首提交观察、迟到成功/失败、独立卸载、键盘/窄屏与固定安全错误等具体场景。逐项区分历史W05/W06证据、未来组件测试与真实HTTP/浏览器/跨主体UNTESTED。

停止：只交3份设计Markdown；不改生产组件/测试/样式/配置/依赖、不开始legacy VM或新加载框架、不启动预览/真实网络。待总控评审后另授实现范围。
