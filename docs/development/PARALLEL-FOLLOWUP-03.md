# TASK-01C / W05-R1审查后的后续安排

日期：2026-10-29；总控codex-review-01。本轮核对DeepSeek所报a652696是TASK-01C起点，实际原分支干净HEAD为e169afa9e1b8e1277b8b63119c3d7acee6c2851c，按实际新增4文件交付审查接收。Gemini返修54df33e关闭W05-R02，W05-R01因同key无效load恢复仍曝光旧数据而保留OPEN，继续W05-R2。Codex继续N03-A原任务/目录/8b2d8aa基线，本单不干预其在编内容。

## 共同边界

开始记录任务ID、唯一会话、实际目录/分支/起始HEAD；审查人均为总控，交付后停止。先读AGENTS、README、架构索引、模块归属/依赖规则、TASK-BOARD、本单和reviews/FOLLOWUP-02-REVIEW.md。

不改根/模块AGENTS、ports、依赖/锁、API/Proto/gen、共享脚本/配置、CI/依赖规则、数据库公共结构、原始资料或已批准ADR；不加依赖，不使用真实凭据/节点/Controller/Agent/数据库/云，不推送/合并main、不启动其他Agent。原提交、分支、报告保留，不reset/rebase、自动stash或同步main掩盖差异。G2未冻结，D01–D07及P1-C10未关闭。

## READ-PLAN-01 · 首条只读闭环的可批准方案与验收包

| 项目 | 分配 |
|---|---|
| 会话 / 审查 | DeepSeek deepseek-01 / 总控产品、契约与安全审查 |
| 分支 / Worktree | docs/node-read-plan-ds01 / `D:\admin\desktop\codex\myproject\ks\.worktrees\docs--node-read-plan-ds01` |
| 基线 | 本轮审查交接后的main；旧TASK-01C目录停止保留 |
| 允许新增 | 仅docs/contracts/decisions/node-read-01/内PROPOSAL.md、ACCEPTANCE.md、HANDOFF.md；不修改既有文档/代码/候选，不自行生成新API修订 |
| 目标 | 将r03已有D01–D07待决清单转成负责人可实际批准的首版实施方案，帮助下一步进入真正只读闭环；不再只重复现有矩阵或新增无关纯工具。提交明确推荐、依据、代价、依赖与未验证项，批准仍由负责人后续给出 |
| 必读来源 | 已接收r03 README/MATRIX/COMPATIBILITY/DECISIONS及候选、P1-C10、有效需求/已批准G1安全ADR、当前auth/nodes/Web公开入口与局部类型；AUTH-01B复审、N03-A任务单（只有分配，不能当作实现已接收）。不得读取Codex/Gemini未提交内容 |
| PROPOSAL | 给出首版纳入的列表/详情/指标范围及明确推荐路线，逐项对应D01兼容、D02状态权威、D03版本与精度、D04采样形状/时效、D05身份/凭据、D06错误与防枚举、D07空集合/共同策略/撤权输出/缓存。区分已批准约束、现有事实、本次建议、缺失依据和仍需批准；引用具体文件/章节或行，不能把r02 profile或候选提案当成已批准规则 |
| 必须揭示 | 当前Node缺revision/platform/agentVersion，Web局部类型与旧列表DTO不同；不能靠填1、当前时间、false猜具体状态或客户端subjectID凑闭环。若最小路线仍需类型/策略/来源变化，写出具体改动范围和阻塞，不能声称现有字段已足够。缺失安全ADR须列待补证据，不捏造其要求 |
| 负责人决策 | 最多整理3个真正需要负责人选择的问题，给出推荐选项、可比较的替代及对范围/兼容/安全的具体影响；其余技术建议由方案负责论证。不得要求负责人批准抽象的“全部安全”或把开发者应解决的实现细节全部退给负责人；无回复不算批准 |
| ACCEPTANCE | 制定可执行验收矩阵，包含可信主体绑定、空库存未知主体、策略失败无成功空/部分列表、同次共同范围与新请求撤权、隐藏节点不进计数、字段/采样不伪造、旧客户端兼容、来源切换首提交隐藏、成功/错误缓存边界。标注当前实际证据、缺失测试和未来负责模块，不能把静态/离线PASS写成真实HTTP/浏览器已验收 |
| 实施拆分 | 按依赖列出从当前main到首条只读闭环的最小后续任务及完成证据，标明哪些可并行、哪些须先批准/等N03-A验收；不分配/启动新Agent、不占用其他作者目录，也不自启实现 |
| 禁止扩围 | 不实现认证/HTTP/数据库/客户端/执行、不冻结契约、不更新D01–D07状态，不改P1-C10或原r03候选/决定文件，不引入新Runtime、插件、Token协议或未经依据的安全承诺 |
| 验证与交付 | 检查本地引用、事实/提案分层、7个待决项与验收覆盖完整性、diff --check；可运行check:policy，不为纯文档镜像测试或声称跑过完整运行验收。交付精确新HEAD、文件/检查与实际输出、最多3个待选问题和证据缺口 |

这是明确的文档方案任务，能提出待批建议，但不因此授权真实接线。方案交总控审核后停止，后续批准和实现另行分配。

## W05-R2 · 同key加载来源失效也必须切换世代

| 项目 | 分配 |
|---|---|
| 会话 / 审查 | Gemini gemini-01 / 总控UI与数据隔离审查 |
| 原分支 / Worktree | feature/web-node-loader-gm01 / `D:\admin\desktop\codex\myproject\ks\.worktrees\feature--web-node-loader-gm01` |
| 精确起点 | 54df33ec4f0bc34e0bce5734d0a769bb0b543baa；保留0ebe812和54df33e，追加返修，不合并main或改写历史 |
| 允许写入 | 原features/nodes加载容器/测试/说明最小修复，忽略reports/_review临时验证；不改既有展示组件/测试、App/配置/契约/依赖 |
| W05-R02 | 本轮关闭：精确key不再trim合并，带首尾空白key安全失败；保持此修复及原两个独立用例PASS |
| 必修W05-R01剩余 | 现有identity只比较key。A已ready并打开详情→同key但load非函数（当前代码主动识别此无效来源）→恢复有效A，key和epoch未变，首个提交再次显示旧8节点/计数/详情；之后effect才切loading。来源可用性变化必须使旧世代失效，恢复首提交不得复活旧snapshot；不能只用被动effect清理 |
| 回归要求 | 保留原177项回归；加入同key的ready→load undefined/null/非函数→恢复有效来源的布局阶段首提交断言；分别验证节点、计数和旧详情均无；覆盖失效发生在pending时abort及迟到成功/失败、StrictMode、null/padded key原修复、真实pending重试交错、卸载、稳定有效同key重渲染不重复请求 |
| 简化要求 | 不引入通用请求框架/缓存/事件总线，不以每次对象引用改变都刷新替代稳定身份规则。状态只承担必要的来源世代/请求隔离；来源key仍非认证凭证，不接真实fetch/App |
| 验证 | npm run check:web、npm run check:policy；重跑reports/_review/followup02/vitest.review.config.mts的5个总控用例，原4PASS/1FAIL保留，新输出用R2新文件；全部5PASS后交总控复审。必须加载固定环境，不删失败用例或降低断言 |

总控配置及两个测试文件以文本归档在reviews/evidence/followup02-loader-probe*。其中旧followup01测试源码不变，新增用例直接观察布局阶段真实组件DOM。当前W05及两次提交仍未进入main；本轮关闭一个问题不代表组件已接收。

## 交接与停止

报告实际路径/分支、起始/最终HEAD、文件、命令/真实输出、负向场景、兼容边界和剩余风险；FAIL/BLOCKED/UNTESTED如实保留。只创建本地提交，交总控后停止，不合并main、不推送、不启动其他Agent或擅自开始下一功能。
