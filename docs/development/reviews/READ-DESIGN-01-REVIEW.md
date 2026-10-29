# 三份节点只读设计的独立复审

日期：2026-10-29（Asia/Shanghai）；任务READ-DESIGN-REVIEW；总控codex-review-01；起始main `4196b2e8ab9c389ae0830bf56f905d9bafa00d05`。独立integration/read-design-review先登记任务，按用户交付的3个精确提交审核。未修改作者目录/原文/提交，不读取未提交设计、不启动或工具代发Agent消息。

## 决定与文件范围

| 作者 / 任务 | 精确提交 | 结论 |
|---|---|---|
| codex-01 / READ-AUTH-DESIGN-01 | 48409bacf4e582f8ab4f196982a5677fa3a21d18 | ACCEPT_PROPOSED_DOCUMENTS，无阻止文档接收的问题；具体设计/参数/迁移仍待批，任务完成停止 |
| deepseek-01 / READ-HTTP-DESIGN-01 | ece5efd77e8c99fb9a9d1f19eb31db5bd78b3734 | CHANGES_REQUESTED，HD01-R01/R02两个P2，未合入main |
| gemini-01 / READ-WEB-DESIGN-01 | 63b0feb8dffad7dc88252dd7dd786b80c7e54e7c | CHANGES_REQUESTED，WD01-R01/R02两个P2，未合入main |

三个精确HEAD和跟踪工作区干净；每项从4196b2e只新增允许目录的DESIGN/ACCEPTANCE/HANDOFF三份Markdown。没有代码、端口、API/Proto/gen、依赖/配置/DB/已批准ADR变化。原9份文档原样复制哈希归档，主线只合入认证提案，没有把未接收HTTP/Web当组合实现。认证文档接收不等于D05/D06/D07通过或已能登录。

## 认证设计接收范围

设计明确可信身份、资源授权与来源责任链；区分凭据失效/歧义、身份来源不可用、策略主体拒绝与已知零授权；限制Bearer为服务端当前权限与签发精确上限的交集，不相信JWT标签/客户端claims。生命周期、初始化/恢复、同源代理、Cookie/CSRF及跨标签/缓存/输出窗口均标PROPOSED，真实运行34项均UNTESTED。具体差异有迁移说明：sf_session→__Host名、Bearer/JWT→opaque提案、bootstrap响应、重载CSRF交付及管理ID/秘密分离。

22个来源的短句与批准/候选/历史适用性已核。安全验证器脚本先完整阅读，确认仅文件读取与git diff后提取到总控忽略reports；分别在作者精确目录和合入组合中实跑，均PASS：3文档、38本地链接、31范围、22来源引文、34未来验收项，0569c1a到4196b2e的相关源码/候选/来源相同。人工另核验G1/AI-PRIV/BOOT效力、PRIV缺失、原认证操作与scheme、当前scope/N03-A边界，不以引文匹配替代语境审查。

外部公开依据也只读核对：[OWASP会话](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)支持服务端会话到期/生命周期防护及HttpOnly的有限保护；[OWASP CSRF](https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html)支持同步Token和来源检查等机制、SameSite不是全部防护；[MDN Set-Cookie](https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Set-Cookie#cookie_prefixes)支持__Host前缀的属性约束。它们不批准本项目TTL、Cookie名、Token格式、部署兼容或接口，不能替代未来浏览器实测。本轮不据此认证实现或选定密码KDF/MFA/完整运行时。

仍待组合/准入：完整PRIV适用条款、可信持久来源/授权世代、受限Token适配、Tgate到首字节的残余窗口、公开契约变更和Web恢复规则。它们已显式PENDING，不是假装完成的实现或新的产品选择。Codex当前设计文档任务完成，不再安排辅助代码。

## HTTP返修

独立核对原3文档**60处行引用、22文件**，路径/范围有效；onlineOnly/旧Error/来源分类等已有正确设计，以下内容缺口不能由引用有效或check:policy替代。

### HD01-R01 · P2 · 身份来源故障缺少明确错误分支

位置：DESIGN.md §5.1（第88–107行），ACCEPTANCE.md A1-14（第33行）。错误表只列无效凭据401、策略/库存来源失败，却未列Authenticator身份/会话来源不可用；A1-14又将来源缺失/未配置/失败概括为401/503，没有成因映射。一个本来有效的Cookie遇到会话存储故障，与真正到期/撤销不同；统一按“未拿到主体”返回401，会让客户端误清会话/跳登录且掩盖后端故障。

需补独立固定错误阶段、HTTP/媒体/no-store及优先级提案，明确身份源故障策略/库存0次、故障恢复、同时坏query等验证；无效凭据与不可用分开，不回显原因/秘密。具体HTTP码仍待批，本项不加mapper或端口。认证设计已经显式区分identity.unavailable可作对齐输入，不代表其全部参数已批准。

### HD01-R02 · P2 · 最小可信来源仍停留在函数约定

位置：DESIGN.md §6.1–6.2（第118–127行）、§9第185行。方案规定返回“当前不可变快照”并把真实存储适配器留给以后来源任务，没有提出A1最小可信来源如何持有、受控建立/更新和读取；仅列Reader类型/owner无法审核“当前”与只读保证。原任务单明确要求基础来源有设计，不能完全后移。

需补一套最小来源提案：稳定主体ID/显式grants/节点库存的权威持有者、初始空库及已知零授权、可信创建/更新边界、请求快照路径/生命周期/并发模型、缺失与坏数据行为、何时有真实库存且如何避免假节点。可写未来存储/安装来源的设计，不要求本轮写schema/DB/Agent接入实现；并以具体来源操作/时序给验收。结合认证的世代/Tgate建议说明推荐边界与剩余差异，不能凭接口签名或“由owner提供”宣称真实来源方案完成。

## Web返修

独立核对原3文档**23处行引用、11文件**，范围有效（作者HANDOFF的13处是其较窄扫描口径，不作为误报）；仍需核对事实/运行边界。独立legacy VM、无假revision/status、A1无详情的非交互行等方向正确，未实现任何组件。

### WD01-R01 · P2 · 把403误当会话失效

位置：DESIGN.md第227行§6.1“收到401/403时清空会话状态”。有效登录主体可因整体节点读取被拒绝得到403，身份本身不因此无效；已接收认证提案也明确策略拒绝不能统一变“请登录”。此规则会把资源拒绝变成登出/登录循环，并丢掉仍有效的其他能力身份状态。

需区分认证失效、资源拒绝与临时故障。403清当前节点行/计数/授权展示并固定提示，必要时换展示世代，不能仅凭该码清认证会话/跳登录或声称服务端已退出；503等也不伪装401。补有效会话收到403但身份仍有效及其他已授权能力可用、来源故障恢复等验收，错误时仍不保留旧节点。没有要求放宽后端授权或保留无权数据。

### WD01-R02 · P2 · 缺少online=false时的指标展示限制

位置：DESIGN.md第116、196行§3.3/§5.2；ACCEPTANCE.md WDTO-03。转换只按有限0–100生成value并统一显示百分比+“无采样时间”，没有online前置。最小输入为`{items:[{id:"n",name:"n",online:false,cpuPercent:23,memoryPercent:0}]}`：原旧候选/r03允许这种历史数字形状，按提案会在非在线行显示23%/0%，却没有任何更新时间。

现行需求第58行要求离线指标显示缺失或更新时间；r03 COMPATIBILITY第23–27行区分形状可读与UI可展示。无更新时间时应先按online判当前可展示性：false不显示这些百分比，固定缺失/不可用提示；true的0/100可按合法旧值保留并明确无采样时间、不称新鲜。不能仅依赖计划中的新后端输出null，因为旧合法响应仍在兼容范围。补布尔×数值/缺失/非法交叉矩阵，节点行仍可展示，不改schema/不猜五状态。这是设计反例，不是假称运行了尚不存在的legacy组件并FAIL。

其他须澄清但不另计P2：重复ID first-wins/部分坏行dropped与整体失败的边界、泛型抽取后旧测试须重跑而非历史证据自动有效、原loader实际测试数、标签/BFCache范围与认证提案的差异。保留为共同对齐事项，不额外扩展实现。

## 组合状态、验证与停止

认证提案合入后的技术组合HEAD **`9642ab4b671a73015ee3d2b841bbbbb8f5b75613`**，随后仅审查/证据/任务状态；未合入HTTP/Web设计，生产源码/候选与4196b2e相同。三份设计的来源/跨模块差异集中登记在node-read-01/DESIGN-INTEGRATION-STATUS.md，没有静默选定Tgate、Cookie/Token迁移或跨标签范围。

| 检查 | 实际结果 |
|---|---|
| 文件范围/作者状态 | PASS，3精确HEAD、跟踪干净、各3份新增Markdown；9原文复制哈希保留 |
| 认证原文验证 | PASS，两次实跑相同38链接/31范围/22来源/34未来验收；另做效力语境审查 |
| HTTP/Web引用核验 | 60/22、23/11范围有效；内容4个P2仍OPEN，不能写成设计通过 |
| npm ci / npm run tools:install | PASS，127包、4个固定Go工具；未放宽校验/额外安装脚本策略 |
| npm run check | PASS，policy/contracts/go/web/git；Go212顶层/805子测试、0失败；Web179项、0失败 |
| npm run check:security | PASS，npm audit 0漏洞、govulncheck无漏洞 |
| 原资料/候选/生成 | PASS，69原文件/3原候选、r02/r03/Proto负向及生成检查；r03 SHA256仍dcb499b108fc6eab67412e08833100f9cb13c93d843863661faab56535180cfd |

摘要见READ-DESIGN-01-RESULTS.json，原文/哈希、验证脚本/输出、引用核验及全量检查见evidence/read-design-review-*。设计的真实认证/HTTP/存储/浏览器/缓存仍UNTESTED；检查的是当前组合工程，不能从其179旧前端测试推导新legacy实现成功。

DeepSeek在ece5efd原分支追加READ-HTTP-DESIGN-01-R1，Gemini在63b0feb原分支追加READ-WEB-DESIGN-01-R1，各自只修原3文档并同步验收/交接；具体见READ-DESIGN-01-REPAIR.md。Codex完成停止。无需同步main、不要求新分支/工作目录、不重复问A1+B1、不自行开始实现/远程/云或其他Agent。D01–D07/P1-C10仍OPEN、G2未冻结。
