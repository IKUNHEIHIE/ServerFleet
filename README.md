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

AUTH-01B离线读取预检/不可变范围已通过独立审查，见 [授权预检复审](docs/development/reviews/AUTH-01B-REVIEW.md)。Codex后续为 [N03-A任务单](docs/development/CODEX-N03A-WORK-PACKAGE.md)：预检先行的离线节点读取组合；DeepSeek/Gemini维持原任务。G2未冻结，尚未接真实节点/API/执行，App仍是开发空壳；候选接收不批准认证/字段迁移或运行时profile，D07不能因离线预检通过而关闭。

TASK-01C顺序批次进度已接收，实际交付为e169afa；W05-R1关闭标识trim问题，但同key无效load恢复仍会首提交显示旧数据，继续返修且未合入。见 [本轮复审](docs/development/reviews/FOLLOWUP-02-REVIEW.md)。[后续任务](docs/development/PARALLEL-FOLLOWUP-03.md)安排DeepSeek整理首条只读闭环的可批准方案/验收包，Gemini补剩余隔离问题；Codex继续原N03-A。方案仅为待批建议，G2及D01–D07/P1-C10仍OPEN。

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
