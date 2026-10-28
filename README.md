# Server Fleet · M0-P1 工程基础

Go 模块化单体 Controller、独立 Go Agent、React + TypeScript + Vite Monorepo。当前交付只有可构建骨架、模块端口、测试/契约工具链和协作规范；没有真实节点管理或生产发布能力。

**状态：G0 产品已确认；G1 方向已批准；G2 仍为候选，未冻结。P1 完成后暂停，后续开发需负责人明确分配新任务。**

首批 G2-R01/N01/W01 的独立审查与下一批分工见 [首批审查报告](docs/development/reviews/BATCH-01-REVIEW.md) 和 [第二批任务单](docs/development/SECOND-BATCH-WORK-PACKAGES.md)。以任务登记板为准；原始 P1 报告是当时的验证快照。

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
