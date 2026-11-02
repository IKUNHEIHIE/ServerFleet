# Codex Arts · Wave6-R3 进度（V09-R6-GM-04）

- 任务 ID：`V09-R6-GM-04`；唯一会话：`codex-arts-01`；审查/集成：`codex-review-02`。
- 分支：`feature/v09-r6-console-ca01`；工作目录：`D:/admin/desktop/codex/myproject/ks/.worktrees/feature--v09-r6-console-ca01`。
- 交接起点 SHA：`fafbe932e2aef1b071366b0cd4e6e96318e65924`（与 `reports/v09-gm04-codex-arts-handoff.json` 的 finalHead 一致）。
- 开工时工作区：干净（`git status --short` 无输出）。

## 环境差异与处置（如实登记）

- 新工作目录未预置 `.tools`、`.cache`、`node_modules`、`reports`（交接说明称总控已准备，与实际不符）。
- 处置：仅创建被 `.gitignore` 忽略的本地开发链接 `.tools -> D:/admin/desktop/codex/myproject/ks/.tools`（只读使用共享固定 Go/Node，不写入链接目标），并执行 `npm ci`（PASS，127 packages）。未修改任何跟踪文件或依赖版本。
- Node：`v24.22.1`（仓库 `.tools` 固定发行版）；Go：`go1.27.3 windows/amd64`（共享只读）。
- 根检查：`npm run check` 的 policy/contracts/web/git PASS；Go 首次仅因本机 `GOPROXY=off` 缺依赖测试模块而 tidy 失败（环境原因），用可访问代理后 `check:go` PASS；`check:security` PASS（npm 0 漏洞、govulncheck 无已知漏洞）。见 `reports/check-all.json`、`check-go.json`、`check-security.log`。

## 可运行增量

| # | 提交 | 范围 | 验证 |
|---|---|---|---|
| CP-1 | `92eb5ecdddc81566ed365e9c1806fbcfe11c389d` | AI 预算正式界面：owner 窗口/限额/已用/剩余，管理员 CAS、409、只读、缺策略拒绝 | `tsc --noEmit` PASS；`check:web` PASS（Web 345/345）；新增 `ai-budget.test.tsx` 4/4 |
| CP-2 | `ba60e55469662fece4858a6756c6b6d8338b9d94` | B01 真实文件页面闭环：fixturehost 注入文件 worker/私有 spool/受控根注册表；双常驻 Agent 私有 file-root；B01 终态断言；预算 P20 浏览器步骤 | 严格正向 `--ai-host --attempt-file-flow --require-complete`：23 PASS / 4 BLOCKED / 0 FAIL；B01、P20 PASS；Go 构建/vet PASS |
| CP-3 | `2c3a1b0c237ea8b0c5299b4d1526d166138ebb15` | B01 取消负向：页面创建 32MiB 传输 → 任务 running → 页面取消；终态 cancelled、无 copied 项、父任务非 completed；授权显式增加 `tasks.cancel` | 严格正向：24 PASS / 4 BLOCKED / 0 FAIL（28 步），B01/B01n/P20 PASS；真实 Controller 回归 70/70；契约桩 16/16 |
| CP-4 | 本提交（见 `git log -1`） | B01 撤权与丢回执负向；B06n 第二隔离 Controller 宿主；更新中心/插件目录的阻塞错误说明 | **27 PASS / 4 BLOCKED / 0 FAIL（31 步）**；B01n×3、B06n、P20 PASS；桩 16/16、真实 70/70；Web 347/347 |

## 未完成/阻塞（滚动更新）

- B01 撤权/丢回执：已用真实页面 + 真实进程覆盖（见 B01n）；遍历/symlink、续传、多文件部分结果仍属后端/后续项。
- B03 Xray：缺可准入工件/Runtime（501 目录源）；页面已显式说明且无可执行安装路径。
- B04 发布：网关创建 `takeover_approval_required`，缺 typed worker/工件/终态补偿端口；宿主骨架可继续搭，但真实执行等 DS/CX。
- B05 更新：`UPDATE_SOURCE_UNAVAILABLE` / 计划器端口缺失；页面已显式说明 503 不是成功。
- B06 灾备：第二隔离 Controller 宿主已实测（独立 DB/PKI、身份隔离）；真实新权威/旧主控 fence 仍需 CX 与正式 PKI-OPS 原文。