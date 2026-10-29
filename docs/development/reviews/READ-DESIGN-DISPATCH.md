# A1+B1方向确认与设计任务派发准备

日期：2026-10-29（Asia/Shanghai）；总控任务READ-DESIGN-DISPATCH；会话codex-review-01；独立目录integration/read-design-dispatch；起始main `0569c1a5ae01ea7715b53a979a3dbb0af50b6b3c`。先登记协调任务，再记录负责人原文和限定任务；未写作者设计或修改其旧目录。

## 直接授权及效力

负责人在本会话回复 **“按推荐 A1+B1”**，对应已评审OWNER-CHOICES的两项具体方向：先legacy兼容只读列表、后详情；浏览器服务端会话Cookie方向。已将选择包状态由PENDING_OWNER_CHOICE改为A1_B1_CONFIRMED_DESIGN_ONLY，并记录原文；替代选项保留来由但未选择。不再向三位作者重复索取同一方向确认。

该回复授权准备/派发选择包中三份具体设计、兼容差异和可执行验收计划，不代表具体认证/生命周期/HTTP错误/来源原子性已批准。D01–D07/P1-C10仍OPEN，G2未冻结；无真实登录/HTTP/App接线、公共契约/端口/依赖/DB修改或生产/VPS/云/远程操作授权。

## 三项任务及共同边界

详见READ-DESIGN-01-WORK-PACKAGES.md。每项仅新增所属目录的DESIGN.md、ACCEPTANCE.md、HANDOFF.md，设计目录互不重叠；原方案、安全、W06任务保持完成停止。三位新设计待负责人复制提示词人工派发，未启动或工具代发消息。

| 任务 / 会话 | 新分支 / 新目录 | 交付重点 |
|---|---|---|
| READ-AUTH-DESIGN-01 / codex-01 | docs/node-read-auth-design-cx01 / D:\admin\desktop\codex\myproject\ks.worktrees\docs--node-read-auth-design-cx01 | 可信身份/会话与必要Bearer兼容的具体生命周期、初始化/撤销/失效、Cookie/CSRF/Origin适用、安全原文补证；不实施凭据 |
| READ-HTTP-DESIGN-01 / deepseek-01 | docs/node-read-http-design-ds01 / D:\admin\desktop\codex\myproject\ks\.worktrees\docs--node-read-http-design-ds01 | 唯一GET列表/onlineOnly兼容、字段来源投影、按成因/阶段错误、预检与库存/撤权输出观察边界/缓存；不改OpenAPI或处理器 |
| READ-WEB-DESIGN-01 / gemini-01 | docs/node-read-web-design-gm01 / D:\admin\desktop\codex\myproject\ks\.worktrees\docs--node-read-web-design-gm01 | legacy DTO/局部VM/加载接缝与B1会话失效、显示/计数/指标/焦点验收；不写TSX/fetch或强转富化类型 |

指定唯一会话、允许3文件、审查人/资料基线/停止条件；新目录统一从本次验证并收口后的main建立，精确起始HEAD在最终提示词和创建核验中给出，作者实记HANDOFF。不读取其他会话未提交内容，跨设计语义需求标假设和待对齐，由总控组合评审；不让作者自行冻结公共接口或重复提出A1/B1选择。

## 实际检查

任务配置提交`85e87cb859ebbe205bdffa8ee62938e3412bffae`，之后只澄清官方文档只读可核验的边界及本收口/证据/状态。相比起始main，仅README、选择包、任务单/登记板、P1-C10状态与审查证据等文档变化；生产apps/internal/api/proto/gen/scripts/配置/依赖未改，未创建作者设计内容。

| 检查 | 输出 |
|---|---|
| 任务边界/目录预检 | PASS，3个允许目录互不重叠、3个新分支/目录无既有占用，原作者目录不动 |
| 文档本地链接 | PASS，41个链接实际存在；不以此替代设计内容审核 |
| npm ci / npm run tools:install | PASS，127包及4个固定Go工具，未批准额外安装脚本/放宽校验 |
| npm run check | PASS，policy/contracts/go/web/git；Go212顶层/805子测试、0失败，Web179项、0失败 |
| npm run check:security | PASS，npm audit 0漏洞，govulncheck无漏洞 |
| 原始资料/候选/生成 | PASS，69原文件/3原候选，r02/r03/Proto生成与负向检查保留；r03候选SHA256仍dcb499b108fc6eab67412e08833100f9cb13c93d843863661faab56535180cfd |

输出及计数/链接/目录预检见evidence/read-design-*。合入main后只执行已授权的新目录创建，交接前核对每目录分支/精确HEAD/干净状态和任务单存在；最终创建记录保存在主目录忽略reports/read-design-worktrees.json，并在给三位的提示词明确同一精确起始提交。无自动会话启动、消息派发、远程配置/推送或其他任务扩展。

本轮准备与检查完成后交人工派发并停止，下一步等待三位各自新提交进入独立复审；不自动开始任何一位的设计/代码工作。
