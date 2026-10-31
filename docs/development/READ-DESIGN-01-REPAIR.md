# READ-DESIGN-01 · 两份设计的限定返修

日期：2026-10-29（Asia/Shanghai）；总控codex-review-01。认证设计48409bac接收为PROPOSED工件，Codex本任务完成停止；HTTP ece5efd、Web 63b0feb各有2个P2，尚未合入main。A1+B1方向仍有效，不再次问同一方向，不启动实现。具体证据见reviews/READ-DESIGN-01-REVIEW.md。

## 共同规则

保持原分支/工作目录和原提交，追加返修，不同步main/rebase/reset、不清理旧工件。只修改各自原DESIGN.md、ACCEPTANCE.md、HANDOFF.md，不能写其他目录/代码/端口/OpenAPI/Proto/gen/数据库/配置/依赖/共享文件。新认证设计可从主目录已提交main或精确48409bacf4e582f8ab4f196982a5677fa3a21d18只读核对，不读其他会话未提交内容；其细节仍PROPOSED，不能假定已经批准。

先读AGENTS、README、架构索引、模块归属/依赖、TASK-BOARD、本单及复审报告，记录实际目录/分支/起始HEAD。按原任务单补具体规则和验收计划，引用须核对内容/效力，不以路径存在代替语义。所有真实HTTP/认证/存储/浏览器保持UNTESTED，不新增镜像测试/预览/实现，不发其他Agent消息。diff检查，可跑check:policy，如实保存命令/输出；交精确新HEAD、3文件、修正位置和风险后停止，不合并main/推送。两位可并行，无须等待另一位返修。

## READ-HTTP-DESIGN-01-R1 · DeepSeek

| 项目 | 范围 |
|---|---|
| 会话 / 审查 | deepseek-01 / 总控+安全/契约/节点 |
| 目录 / 分支 | `D:\admin\desktop\codex\myproject\ks\.worktrees\docs--node-read-http-design-ds01` / docs/node-read-http-design-ds01 |
| 精确起点 | ece5efd77e8c99fb9a9d1f19eb31db5bd78b3734，保留原提交追加R1 |
| 允许修改 | 原docs/contracts/decisions/node-read-http-01/{DESIGN,ACCEPTANCE,HANDOFF}.md |
| HD01-R01 · 身份来源故障缺少明确HTTP分支 | 原DESIGN §5.1只列无效凭据401，未列Authenticator身份/会话来源不可用；ACCEPTANCE A1-14将来源缺失/配置/失败混写401/503。须与“凭据本身无效/到期/撤销”分开，给可信身份来源不可用/坏状态的固定阶段、HTTP/媒体/no-store及优先级提案；不能因未得到主体就一律401或提示重新登录。明确策略/库存0次，与坏query/策略reader/后端数据故障区分。补有效凭据+身份存储故障、故障恢复、不可用同时坏query等未来用例；具体码仍是待批提案，不加mapper/端口 |
| HD01-R02 · 最小可信来源方案不足 | 原§6.1/6.2只要求PolicyReader/InventorySource当前只读，把真实适配器全部留到§9未来任务，尚不能审查A1所需来源保证。补一套推荐的最小可信来源设计：主体稳定ID与显式授权记录/节点库存的权威持有者、来源如何受控建立/更新、读取路径及快照生命周期/并发模型、初始空库与已知零授权、未配置/坏数据/失败的行为；哪些数据须等待真实接入、哪些只是安装/存储元数据，不能用可公开默认mock/假节点兜底。不要求本轮写schema/DB/代码，也不扩到Agent接入实现。给来源操作/观察点对应的具体验收 |
| 同时对齐现有提案 | 只读核对已接收认证设计的Tauth/Tpolicy/Tgate、受限Bearer求交/世代和持久来源需求。明确自己的推荐边界、需要的语义输入和剩余窗口；若不采用Tgate，具体列为待共审差异，不能称auth已接受仅T0/T1边界。修正“旧Bearer客户端保持可用”的过度概括：传输/路径保留与旧JWT凭据值兼容不同，当前opaque Token方案只是候选，须盘点/迁移后才能保证旧凭据可用。不要为消除分歧自行冻结任何D项 |

保留现有正确部分：唯一GET、onlineOnly参数/default与认证先行、授权后过滤、全量构建、不把后端InvalidCheck映射客户端400、隐藏计数、无伪造字段、Error媒体与取消/缓存边界。

## READ-WEB-DESIGN-01-R1 · Gemini

| 项目 | 范围 |
|---|---|
| 会话 / 审查 | gemini-01 / 总控+UI/契约/安全 |
| 目录 / 分支 | `D:\admin\desktop\codex\myproject\ks\.worktrees\docs--node-read-web-design-gm01` / docs/node-read-web-design-gm01 |
| 精确起点 | 63b0feb8dffad7dc88252dd7dd786b80c7e54e7c，保留原提交追加R1 |
| 允许修改 | 原docs/contracts/decisions/node-read-web-01/{DESIGN,ACCEPTANCE,HANDOFF}.md |
| WD01-R01 · 不得把403当认证失效 | 原DESIGN §6.1说401/403都清会话状态；但有效会话仍可能整体无nodes.read授权，403不是会话已失效。分清身份、资源授权和临时来源故障：按可信认证失效语义处理401；403清本次节点展示/授权视图、固定无权提示，可以更新展示世代，但不凭该码清认证会话、跳登录或宣称服务端登出；503等故障也不伪装未登录。补有效会话收到403且其他已授权能力/身份状态仍有效、身份来源故障与恢复等计划，保持错误时旧行/计数不残留 |
| WD01-R02 · 非在线且无时间的历史数值不得显示百分比 | 原§3.3/§5.2只按数字有限/0–100显示value+“无采样时间”，未看online。旧候选允许online=false携带历史CPU/内存，因此输入{id:'n',name:'n',online:false,cpuPercent:23,memoryPercent:0}会显示23%/0%却无更新时间，违背需求“离线节点显示数据缺失或更新时间”。先按online判定当前可展示性：false时不显示这些无时间百分比，使用缺失/不可用固定文案，不伪造更新时间；true的合法0/100保留并明确无采样时间、不称新鲜。补online true/false×0/100/普通值/null/缺失/非法的交叉矩阵，node行仍可展示，不要求改旧schema或猜五状态 |
| 同时澄清，不另计P2 | “保留首次重复id/部分坏行dropped”是作者提案，须明确为何适合只读列表、哪些结构/身份冲突必须整次失败、与整体无部分成功及后端拒绝坏/重复ID如何对应，不能默认为已统一。泛型抽取只能复用规则和重跑旧用例，历史W06证据不能自动证明新hook/legacy实现；核对原加载器实际用例数，不硬写26。跨标签/pageshow等与认证设计存在范围差异，保留待对齐项，不自行扩大实现范围或称已完成 |

保留现有正确部分：独立legacy类型、不伪造status/revision/sampledAt、不强转富化VM、局部搜索不当授权、首提交世代/迟到结果、A1无详情时不假造行交互、未来浏览器/HTTP明确UNTESTED。
