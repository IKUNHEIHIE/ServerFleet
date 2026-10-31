# READ-PLAN-01 / W05-R2审查后的后续任务

日期：2026-10-29；总控codex-review-01。W05-R2通过复审，W05-R01关闭，接收原W05及两次返修。READ-PLAN-01因两处方案内容问题退回；只是待批方案，未批准R1-A/R1-B、Cookie、字段迁移或D01–D07。Codex继续READ-SEC-01原任务/目录，不读取或改变其在编内容。

## 共同边界

先读AGENTS、README、架构索引、模块归属/依赖规则、TASK-BOARD、本单及reviews/FOLLOWUP-03-REVIEW.md。记录任务/唯一会话、目录/分支/起始HEAD，审查人均为总控，交付后停止。

不修改共享AGENTS、ports、依赖/锁、API/Proto/gen、CI/scripts/依赖规则、数据库公共结构、原始资料或已批准ADR；不加依赖，不接生产/VPS/云/真实凭据/节点/Controller或Agent，不推送/合并main、不启动/代发其他Agent消息。旧提交/分支/工件保留，不reset/rebase/自动stash或同步main掩盖差异。G2、D01–D07/P1-C10仍OPEN。

## READ-PLAN-01-R1 · 修正来源事实与旧查询兼容

| 项目 | 分配 |
|---|---|
| 会话 / 审查 | DeepSeek deepseek-01 / 总控产品、契约与安全审查 |
| 原分支 / Worktree | docs/node-read-plan-ds01 / `D:\admin\desktop\codex\myproject\ks\.worktrees\docs--node-read-plan-ds01` |
| 精确起点 | d0709fc1fe0a5518000bdb7b81f8c1f8f82334e1；保留原提交，追加返修，不合并main或改写历史 |
| 允许写入 | 原docs/contracts/decisions/node-read-01/的3份文档；不改其他文件/代码/候选或批准状态 |
| 必修RP01-R01 | PROPOSAL第12行把revision/status/platform/agentVersion/共同采样一起概括成Node缺字段，但Node已有Status、Metrics.ObservedAt；第30行又承认存在，造成前置任务矛盾。明确真正缺revision/platform/agentVersion；Status/ObservedAt已有，权威来源、状态优先级、共同采样语义和公开形状仍待确认。公开详情platform/agentVersion可省略、局部VM可null，不能把拥有非空可选元数据来源与revision/status的真正前置条件混为一谈。D02提案不能写成已批准事实；同步Q3/依赖/交接措辞 |
| 必修RP01-R02 | 推荐方案承诺legacy兼容，却3份文档均未提旧列表onlineOnly布尔查询（默认false）。原Merged和r03均保留该参数。补进R1-A/D01/验收及Controller任务：保留参数/默认语义，先认证/策略预检和授权可见集合，再执行onlineOnly过滤，不因过滤成空跳过预检/吞策略故障；true/false/省略、隐藏在线节点不进结果/计数、过滤后空集、坏query的批准解析规则有验收。不得自行决定公开bool解析或HTTP码未批准细节 |
| 决策可读性 | Q1与Q3部分重复，且Q3标题“现在还是R1-B再补”的替代却是“永久legacy-only”。合并重复阶段选择或使标题/选项对齐；最多3个真正独立的问题，不能让一次选择隐含永久删去既定产品能力。方向建议仍待负责人批准 |
| 当前状态核对 | 原98a1f6b引文可保留为历史基线，但返修交接不能继续称N03-A/W05未接收。从主目录已提交的N03-A/FOLLOWUP03复审记录核对新事实，标明引用提交/阶段；不读取作者未提交文件，不要求同步分支，不把新离线接收当真实HTTP完成 |
| 验证 | 核对原文内容与批准状态、引用路径/行及语义、D01–D07覆盖、旧参数/验收与决策选项一致；diff --check，可跑check:policy。路径/行号存在不是内容正确证据；不新增镜像测试或声称跑过真实HTTP |

本轮没有否定“先列表、后详情”和Cookie方向作为待批建议；它们尚未批准，也没有进入main。修正后交总控复审，再形成可供负责人最终选择的包，不自行启用R1-A、legacy VM或认证实现。

## W06 · 加载容器的真实浏览器与键盘验收

| 项目 | 分配 |
|---|---|
| 会话 / 审查 | Gemini gemini-01 / 总控UI与安全审查 |
| 新分支 / Worktree | chore/web-node-browser-qa-gm01 / `D:\admin\desktop\codex\myproject\ks\.worktrees\chore--web-node-browser-qa-gm01` |
| 基线 | 本次W05-R2复审交接后的main；旧W05目录停止保留，含原FAIL/PASS及预览工件 |
| 允许写入 | 仅新增apps/web/src/features/nodes/BROWSER-ACCEPTANCE.md作为报告；临时预览/脚本/截图/trace写忽略reports/_preview或reports/_review。既有生产组件/测试、App、样式/配置、候选、依赖不改 |
| 目标 | 当前W05独立组件用例和179项常规测试已通过，但总控本轮未做真实浏览器。用真实浏览器验证已接收NodeLoader组合后的键盘、状态恢复和来源切换，提供可复核记录；不是接真实数据源或首条真实HTTP闭环 |
| 隔离预览 | 只在临时目录组合真实NodeLoader/现有组件与合成fixture、显式固定时钟/可控Promise；fixture不得导入生产工程。仅localhost临时预览，独立临时浏览器资料，无真实账号/网络来源。记录启动命令/端口/版本、验证代码HEAD和页面入口，结束后停本任务启动的进程，不影响其他服务 |
| 浏览器用例 | 实际Tab/Shift+Tab遍历搜索/筛选/行/详情/重试；Enter/Space激活同ID立即聚焦详情，Escape/关闭返回；loading/错误/重试、A→B、A→null→A、同key无效load恢复首提交无旧节点/计数/详情，迟到成功/错误不覆盖；重试/来源变化后的焦点去向与既有约定一致，无后台抢焦点；保留固定时钟下0/100、缺失/陈旧显示 |
| 视觉与证据 | 420和1280宽度检查可读性、表格横向滚动、长名称和键盘焦点可见性。保存截图和结构化步骤/实际activeElement/状态记录；若用脚本发真实Tab，说明工具/按键机制。不能把element.focus/jsdom断言冒充原生Tab/真实浏览器；首提交约束可用布局观察记录，不能只给最终截图 |
| 报告 | BROWSER-ACCEPTANCE.md注明每例PASS/FAIL/BLOCKED/UNTESTED、对应截图/trace的实际绝对路径、工具/浏览器版本、局限及剩余风险。没有真实登录/HTTP/跨主体攻击/生产性能，不因本次验收声称这些能力已完成 |
| 发现问题 | 保留真实失败、最小复现与截图交总控；本项只验收，不顺带改组件、重写加载框架或开始legacy VM。需要修复时由总控另行定范围，不删除测试或美化结果 |
| 检查/停止 | npm run check:web、npm run check:policy及实际浏览器记录；不新增低价值镜像测试。只提交本报告，交付新HEAD/文件/实际输出后停止，不合并main、不推送、不启动其他Agent。库/CLI/浏览器工具用法按Context7/适用技能读取当前说明 |

W06没有生产fetch、Token/Cookie、真实接口或G2冻结授权。DeepSeek方案仍在返修，Codex继续安全来源核验，两者不依赖W06的未提交材料；没有自动启动或消息派发。
