# Server Fleet · M0-P1 工程基础

Go 模块化单体 Controller、独立 Go Agent、React + TypeScript + Vite Monorepo。已包含可构建骨架、模块端口、测试/契约工具链，以及离线授权/节点/审计/通信规则和隔离Web组件；尚未形成真实管理API、节点执行或生产发布能力。

**状态：G0 产品已确认；G1 方向已批准；G2 仍为候选，未冻结。P1 完成后暂停，后续开发需负责人明确分配新任务。**

首批 G2-R01/N01/W01 的独立审查与下一批分工见 [首批审查报告](docs/development/reviews/BATCH-01-REVIEW.md) 和 [第二批任务单](docs/development/SECOND-BATCH-WORK-PACKAGES.md)。以任务登记板为准；原始 P1 报告是当时的验证快照。

第二批交付新增离线 Protobuf 语义校验、节点领域规则及已返修的节点列表组件；均未接入真实节点/执行链路，App 仍是开发空壳。最新复审见 [第二批复审](docs/development/reviews/BATCH-02-REVIEW.md)，后续独立任务见 [第三批任务单](docs/development/THIRD-BATCH-WORK-PACKAGES.md)。

第三批已接收默认拒绝的节点读取策略、审计事件校验/受限视图及列表搜索筛选，均为未接线的离线基础。最新结论见 [第三批审查](docs/development/reviews/BATCH-03-REVIEW.md)，下一步见 [第四批任务单](docs/development/FOURTH-BATCH-WORK-PACKAGES.md)：有界帧解码、节点授权错误接线、只读节点详情。G2仍未冻结，不自动启动Agent。

第四批接收有界离线帧解码、只读节点详情，当时节点授权错误接线因1个P2退回。历史记录见 [第四批审查](docs/development/reviews/BATCH-04-REVIEW.md)和[第五批任务单](docs/development/FIFTH-BATCH-WORK-PACKAGES.md)。

第五批接收离线连接对账/序列及节点授权返修，N02-R01关闭，当时W04焦点改动因1个P2退回。历史见 [第五批审查](docs/development/reviews/BATCH-05-REVIEW.md)和[第六批任务单](docs/development/SIXTH-BATCH-WORK-PACKAGES.md)。

第六批先行接收目标快照/未启动15分钟等待规则及焦点返修，W04-R01关闭，历史见 [部分验收](docs/development/reviews/BATCH-06-PARTIAL-REVIEW.md)。随后G2-R02A通过独立复审，仅接收节点只读候选/兼容矩阵/检查工件；r03已接入根契约检查，来源注释已修正，D01–D07及P1-C10仍OPEN。最新见 [契约复审](docs/development/reviews/G2-R02A-REVIEW.md)。

随后TASK-01B默认分批/当前批纯判定通过审查；W05加载容器因恢复来源首帧旧数据、标识trim合并两个P2退回，尚未进入main。结论与证据见 [本轮审查](docs/development/reviews/FOLLOWUP-01-REVIEW.md)；DeepSeek补顺序进度纯值、Gemini原分支返修，见 [两位后续任务](docs/development/PARALLEL-FOLLOWUP-02.md)。

AUTH-01B离线读取预检/不可变范围已通过独立审查，见 [授权预检复审](docs/development/reviews/AUTH-01B-REVIEW.md)，随后进入 [N03-A任务](docs/development/CODEX-N03A-WORK-PACKAGE.md)。G2未冻结，尚未接真实节点/API/执行，App仍是开发空壳；候选接收不批准认证/字段迁移或运行时profile，D07不能因离线预检通过而关闭。

TASK-01C顺序批次进度已接收，实际交付为e169afa；W05-R1关闭标识trim问题，但同key无效load恢复仍会首提交显示旧数据，继续返修且未合入。见 [本轮复审](docs/development/reviews/FOLLOWUP-02-REVIEW.md)。[后续任务](docs/development/PARALLEL-FOLLOWUP-03.md)安排DeepSeek整理首条只读闭环的可批准方案/验收包，Gemini补剩余隔离问题；Codex继续原N03-A。方案仅为待批建议，G2及D01–D07/P1-C10仍OPEN。

N03-A预检先行的离线库存读取已接收，见 [读取用例复审](docs/development/reviews/N03-A-REVIEW.md)。它保证新入口先预检、只用同次范围过滤、错误无部分列表及输出复制隔离，仍未接真实身份/HTTP或存储原子性。Codex下一项为 [READ-SEC-01安全来源/证据任务](docs/development/CODEX-READ-SEC01-WORK-PACKAGE.md)，配合DeepSeek方案；Gemini仍修W05-R2，G2/D07未关闭。

W05-R2复审接收原加载容器及两次返修，两个隔离问题均关闭；READ-PLAN-01因来源事实混淆和遗漏onlineOnly旧参数退回，尚未形成批准包。见 [最新复审](docs/development/reviews/FOLLOWUP-03-REVIEW.md)与[后续任务](docs/development/PARALLEL-FOLLOWUP-04.md)：DeepSeek修正方案，Gemini做隔离真实浏览器验收，Codex继续安全证据核验。当前仍无真实API/App来源接线，G2/D01–D07/P1-C10未批准。

READ-SEC-01安全来源/证据包已接收，见 [安全证据复审](docs/development/reviews/READ-SEC-01-REVIEW.md)和[验收缺口](docs/contracts/decisions/node-read-security-01/SECURITY-ACCEPTANCE.md)。Codex本任务完成停止；待DeepSeek方案返修、Gemini浏览器结果复审后，统一形成可供负责人决定的具体方案包。证据文档接收不批准真实认证/HTTP或G2冻结。

READ-PLAN-01-R1方案文档已接收，原来源/onlineOnly问题关闭，公开字段省略与局部null已澄清；方案仍PROPOSED。W06的19项作者浏览器记录及6张截图已核验，但同ID保持选中再激活、关闭按钮、迟到失败/卸载等指定覆盖未齐，报告未接收。见[最新复审](docs/development/reviews/FOLLOWUP-04-REVIEW.md)及[W06-R1补验](docs/development/PARALLEL-FOLLOWUP-05.md)。DeepSeek/Codex完成停止，Gemini原目录补验；尚无新实现/负责人批准或G2冻结。

W06-R1现已接收，W06-R01/R02关闭：作者同ID/关闭键盘与迟到结果证据经核对，总控独立补齐真实同key旧拒绝/两次卸载3项浏览器验证；原25项作者断言及其C24前提分别记录。见[浏览器复审](docs/development/reviews/W06-R1-REVIEW.md)。三位当前任务均完成停止，下一步见[负责人选择包](docs/contracts/decisions/node-read-01/OWNER-CHOICES.md)：首版范围与凭据设计方向待选，尚未派发新设计/实现；G2及D01–D07/P1-C10仍OPEN。

负责人随后明确确认 **A1+B1：先兼容只读列表、后详情，浏览器服务端会话Cookie设计方向**。已登记[三位设计任务](docs/development/READ-DESIGN-01-WORK-PACKAGES.md)，分别为身份/会话、操作级HTTP兼容/撤权边界、legacy Web列表接缝；只新增各自设计/验收/交接Markdown，待人工派发，不接真实登录/API/App或改公共契约。方向选择已生效，完整D01–D07仍待具体设计评审，G2未冻结。

三份具体设计首次复审接收认证提案48409bac；HTTP ece5efd、Web 63b0feb各因2个P2退回，见[复审](docs/development/reviews/READ-DESIGN-01-REVIEW.md)及[限定返修](docs/development/READ-DESIGN-01-REPAIR.md)。Codex完成停止，两位原分支只修各自3文档；[组合清单](docs/contracts/decisions/node-read-01/DESIGN-INTEGRATION-STATUS.md)保留Cookie/Token迁移、可信来源/Tgate、Web失效/指标等待对齐项。设计文档接收不批准具体安全参数、契约或真实实现，A1+B1不重问，D01–D07/P1-C10仍OPEN。

HTTP返修4fbdcf5与Web返修39b2b7c现已接收为PROPOSED，原四个P2关闭；总控澄清有状态凭据的权威查询顺序及Web展示世代/终止错误门控。三位当前设计任务完成停止，详见[返修复审](docs/development/reviews/READ-DESIGN-R1-REVIEW.md)。三份设计齐备仍不代表认证/HTTP/App已实现；下一步建议的[W07旧列表展示组件](docs/development/W07-LEGACY-VIEW-PROPOSAL.md)已列清范围、等待新实施批准，未创建作者目录或派发任务。D01–D07/P1-C10仍OPEN，G2未冻结。

2026-10-30负责人要求五天完成**完整v0.9**，现切换为[三位复合实施包](docs/development/sprints/v09-five-day/PLAN.md)：Codex平台安全/Agent、DeepSeek数据业务、Gemini完整管理台与浏览器验收，各包连续多子项，检查点增量集成；W07并入GM，不再单项等待。新本地实施授权不代表已有实现、G2冻结、生产/VPS/云或远程准入；24项真实验收均须证据，五日全量目标风险高，未完成项如实保留。最新授权和唯一会话/目录以任务板及本计划为准。

2026-10-31 **Wave 1 三线复合实施已通过独立审查与组合验证并合入 `main`**（`V09-5D-CX` `aef07e5`、`V09-5D-DS` `07f4154`、`V09-5D-GM` `86e0cf5`，总控集成验证 `5d103a0`：Go 546 顶层 + 1,116 子测试 PASS、Web 282 单测 PASS、安全扫描 0 漏洞），详见[Wave 1 审查报告](docs/development/reviews/V09-WAVE1-REVIEW.md)。同日完成 **Wave 2 主控组合接线与 Web/API 端到端集成**（`V09-WAVE2-WIRING` `c840800`：`apps/controller` 组合 19 项 SQLite WAL 迁移与跨模块 HTTP/RBAC/审计事务、Web S01 CSRF 恢复与 Bootstrap 登录衔接、`tests/integration/v09` HTTPS 端到端测试，Go 547 顶层 + 1,116 子测试 PASS、Web 283 单测 PASS、0 漏洞），详见[Wave 2 集成报告](docs/development/reviews/V09-WAVE2-REVIEW.md)，并登记派发 [Wave 3 任务单](docs/development/sprints/v09-five-day/WAVE3.md)。G2 及 D01–D07/P1-C10 仍 OPEN。

## 快速验证

环境固定为 Go `1.27.3`、Node `24.22.1`，npm 11，详见 [工具说明](docs/development/TOOLCHAIN.md)。

```sh
npm ci
npm run tools:install
npm run check
npm run check:security
```

Windows 当前项目已提供 `.tools` 下的本地 Go/Node；新会话先运行：

```powershell
. ./scripts/env.ps1
npm ci
npm run tools:install
npm run check
```

启动本地空壳：`go run ./apps/controller`（仅 `127.0.0.1:8080/healthz`）；`go run ./apps/agent`（显示骨架状态后退出）；`npm run dev --workspace @serverfleet/web`。不连接 VPS，不使用生产密钥。

## 目录

| 位置 | 用途 |
|---|---|
| `apps/controller`、`apps/agent` | 独立程序入口；主控只组合模块 |
| `apps/web` | 静态 React 管理界面，当前仅空状态 |
| `internal/{auth,nodes,tasks,audit,transport,plugins,storage}` | 模块边界与临时内部端口 |
| `api/openapi` | 三份字节不变的候选契约工作入口 |
| `proto`、`gen/go` | 独立 Proto 修订候选及真实生成 Go 代码 |
| `plugins` | 可选业务包，核心禁止导入；未实现 Xray |
| `tests`、`scripts` | 测试夹具、依赖边界和契约/协作校验 |
| `docs/reference` | 安全解压的原始资料，69 项清单哈希受保护 |
| `docs/development`、`docs/contracts` | 协作、工具、契约差异、验证与交接 |

## 开发前必读

1. [AGENTS.md](AGENTS.md)：所有新会话的规则。
2. [当前资料索引](docs/architecture/README.md)：需求、有效 ADR 与候选的优先级。
3. [模块归属](docs/development/MODULE-OWNERSHIP.md) 与 [依赖规则](docs/development/DEPENDENCY-RULES.md)。
4. [多 Agent 流程](docs/development/MULTI-AGENT-WORKFLOW.md)、[Worktree 操作](docs/development/WORKTREE-GUIDE.md)。
5. [候选契约差异](docs/contracts/P1-CANDIDATE-REVIEW.md)、[首批任务建议](docs/development/FIRST-BATCH-WORK-PACKAGES.md)。
6. [P1 验证报告](docs/development/P1-VALIDATION.md)。

Git 目前仅在本地；没有远程仓库，GitHub Actions 是否通过必须以真实运行记录为准。
