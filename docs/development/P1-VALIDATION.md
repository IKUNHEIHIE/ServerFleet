# M0-P1 实际验证与交接报告

日期：2026-10-29（Asia/Shanghai）。项目：`D:\admin\desktop\codex\myproject\ks`。环境：Windows amd64；Go 1.27.3、Node 24.22.1、npm 11.19.1。本阶段只交付工程基础，**不进入下一阶段**。

## A. 工程交付

五项基础工作已建立并完成可在本机执行的验证：Go/React Monorepo、模块边界/所有权、Git Worktree 协作、真实测试/契约工具链、GitHub Actions 配置。Controller/Agent 可编译且完成本地进程 smoke；Web 类型、组件测试和构建通过。

没有实现真实注册、批量执行、AI、Xray、Nginx、DNS/证书、文件传输或用户后台；没有访问云/VPS、使用真实凭据、创建或推送远程仓库；没有启动其他 Agent。所有 smoke 进程已关闭、测试 Worktree 已移除。G2 仍候选，1C1G 和安全/运行时 PoC 未验收。

资料先检查 ZIP 成员路径/大小/链接，再解压到 docs/reference；69 项清单哈希和三份 OpenAPI 副本保持一致。原始资料单独提交 `fc4c403`；工程基础提交 `64dddd5`；最终报告/实测脚本另有本地交接提交（用 `git log -3 --oneline` 查看）。身份为明确标记的本地构建身份，不冒充项目负责人。

## B. 实际测试结果

以下命令在项目根运行；先 `. ./scripts/env.ps1`。`npm run check` 会执行分组中的实际子命令，完整展开和原始输出保存在 `reports/*.log`；机器摘要已存 `P1-CHECKS.json`。所有 PASS 仅表示表内范围。

| 检查名称 | 结果 | 实际执行命令 | 关键输出 / 剩余事项 |
|---|---|---|---|
| Go/Node 本地安装 | PASS | `go version`、`node --version`、`npm --version` | go1.27.3 windows/amd64；v24.22.1；11.19.1；官方 ZIP SHA256 一致 |
| 原资料/候选来源 | PASS | `node scripts/verify-source.mjs` | 69 原文件、3 个字节一致副本 |
| 锁文件重复安装 | PASS | `npm ci` | 重新安装 127 包，退出 0；Buf postinstall 策略提示未放开，已锁平台二进制实测可运行 |
| 全量基础检查 | PASS | `npm run check` | policy/contracts/go/web/git 五组 PASS（最终完整运行约 02:35 上海时间） |
| Controller 编译 | PASS | `go build -o reports/controller.exe ./apps/controller` | Windows 可执行文件已生成 |
| Agent 编译 | PASS | `go build -o reports/agent.exe ./apps/agent` | Windows 可执行文件已生成 |
| Go 格式 | PASS | `npm run check:go` | gofmt 枚举全部工程 Go 文件，无差异；完整参数见 go-format.log |
| Go 依赖一致性 | PASS | `go mod verify`；`go mod tidy -diff` | all modules verified；tidy 无差异 |
| Go 单元测试 | PASS | `go test -count=1 -json ./...` | 6 个顶层测试 + 4 个路由子用例通过；占位模块无业务测试不被虚报为覆盖完整 |
| Go 静态检查 | PASS | `go vet ./...`；`staticcheck ./...` | 无问题 |
| 模块循环/越界 | PASS | `go test ./tests/architecture`、双入口 build | 实际 imports 符合白名单；反例被拒绝；编译无循环 |
| 前端类型 | PASS | `npm run typecheck --workspace @serverfleet/web` | TypeScript strict 通过 |
| 前端组件/Mock | PASS | `npm test --workspace @serverfleet/web` | 3 测试通过；未 mock 网络被拒绝、离线数据为 null；JUnit 已生成 |
| 前端生产构建 | PASS | `npm run build --workspace @serverfleet/web` | Vite build 通过；JS 249.13 kB / gzip 77.99 kB，CSS 7.72 kB |
| Web 边界 | PASS | `node scripts/web-boundaries.mjs` | 无生产导入测试/Go 源码、无 Agent WebSocket；静态规则不替代运行时审查 |
| YAML/ref/ID/路径参数 | PASS | `node scripts/contracts.mjs` | Core 29/34、Merged 147/185、Xray 19/23（路径/操作） |
| OpenAPI 规范 lint | PASS | `npm exec -- redocly lint --format=summary` | 三份 OpenAPI 3.1 均有效；使用 spec+明确补充规则 |
| 原 Proto 编译 | PASS | `node scripts/diagnose-original-proto.mjs` 内原目录 `buf build` | 原候选可编译，不能写成语法失败 |
| 原 Buf 配置 lint | FAIL | 同诊断脚本内原目录 `buf lint` | 缺 modules/path=proto，package 目录相对根不符；原件不改，独立根配置已解决 |
| 原 Proto Go 生成 | FAIL | 同诊断脚本内原目录 `buf generate --template ...` | unable to determine Go import path；独立 M0-P1-r1 加 go_package 后解决 |
| 修订 Proto lint/格式/编译 | PASS | `npm exec -- buf lint`；`npm exec -- buf format --diff --exit-code`；`npm exec -- buf build -o reports/agent.binpb` | STANDARD lint/格式/编译通过 |
| 修订 Proto Go 生成 | PASS | `npm run generate`、`npm run check:contracts` | 真实生成 pb.go，重新生成逐字节一致，生成代码可编译 |
| Proto 兼容性基础 | PASS | `npm exec -- buf breaking --against tests/contracts/proto-baseline` | FILE 基线通过；另对原 descriptor 以 WIRE 规则检查通过，完整配置见诊断日志/候选说明 |
| Proto 往返/损坏帧 | PASS | `go test ./tests/contracts` | Envelope/oneof 往返一致；损坏字节被拒绝 |
| 工具负向测试 | PASS | `npm run check:policy`；`node scripts/proto-negative.mjs` | 8 个脚本测试；Buf 实际拒绝不兼容类型/重复编号，不是永远返回成功 |
| 程序启动 | PASS | `node scripts/runtime-smoke.mjs` | Agent 明确离线骨架退出；Controller /healthz=200，POST /api/v1/tasks=404；进程已停止 |
| Linux 双架构交叉编译 | PASS | Node runner 逐项设置 `GOOS=linux GOARCH=amd64/arm64 CGO_ENABLED=0`，执行 `go build -o reports/<app>-linux-<arch> ./apps/<app>` | Controller/Agent 共 4 个二进制生成；只编译，未在 Linux 实机运行 |
| Worktree 真实流程 | PASS | `node scripts/worktree-smoke.mjs` | 创建 chore/p1-worktree-smoke、拒绝带未跟踪文件清理、保留测试文件；清理自建文件后移除 Worktree 和已合并测试分支 |
| 分支/提交规则 | PASS | `node scripts/git-policy.mjs` | main 与本地 Conventional Commit 通过；PR 事件验证需真实 GitHub |
| 空白/暂存检查 | PASS | `npm run check:git` | git diff --check 与 git diff --cached --check 通过 |
| npm 依赖扫描 | PASS | `npm audit --audit-level=high` | found 0 vulnerabilities（本次数据库快照） |
| Go 漏洞扫描 | PASS | `govulncheck ./...` | No vulnerabilities found（本次可达代码） |
| CI YAML/表达式 | PASS | `actionlint -shellcheck=` | GitHub Actions 语法/语义检查通过；不依赖本机 shellcheck |
| GitHub 实际 CI/PR/分支保护 | UNTESTED | `git remote -v` 为空，未连接远程 | YAML 已生成；没有线上运行、真实审批人或分支保护配置 |
| Linux race / 真机执行 | UNTESTED | CI 已配置 `go test -race ...` | 本机为 Windows，未在 GitHub Linux runner 实际运行；本地已运行普通测试和交叉编译 |
| G2 正式冻结/真实安全闭环 | BLOCKED | 本阶段未执行 | C01～C07 语义/安全证据、缺失 ADR 核验及相应 PoC 尚未完成；仅阻止后续真实联调，不阻止 P1 骨架交付 |
| 1C1G、SQLite/WAL、mTLS、WASM/Xray/Nginx PoC | UNTESTED | 未执行，不属于 P1 实现范围 | 不宣称性能/隔离/恢复已通过 |

`reports/protobuf-negative-*.log` 和 worktree-dirty-refusal.log 中的非零退出是预期拒绝；原候选 FAIL 则是已登记、以独立修订候选处理的真实缺陷，不能从报告删除。

### 初始化期间已解决的常规问题

| 现象 | 处理与证据 |
|---|---|
| Go 不存在、系统 Node 为另一版本 | 官方 ZIP 项目级安装，核对 SHA256；未修改系统环境 |
| 初次来源索引写入缺目录 | 建立 docs/contracts 并补齐 SOURCE-INDEX；重复原资料哈希验证通过，无原文覆盖 |
| YAML 默认别名上限拒绝大量正常锚点 | 有限 10000 上限、2 MiB 文件限制和去重遍历；候选原文不改；三文件 parse/lint 通过 |
| Windows Buf 找不到 diff、Proto 初始格式差异 | 定位 Git for Windows 自带 diff；仅修订候选运行 buf format，原件保留 |
| TypeScript 7 不再提供旧 compiler API 给自定义 import 检查 | 改用保守静态 import 规则，负向夹具通过；明确它不能证明动态网络安全 |
| 差异文件的空白上下文触发 Git 空白检查 | 保存为零上下文差异证据；未改原 Proto 内容，暂存检查通过 |

## C. 目录与归属

详细职责、模型、端口、禁访、允许依赖、岗位及接口审查人见 MODULE-OWNERSHIP.md；机器白名单在 dependencies.json。主要方向：tasks→auth/nodes/audit/transport/storage；plugins→auth/tasks/audit/storage；nodes/auth→audit/storage；transport→audit/storage/gen；audit→storage；storage 不反向依赖业务。Controller 组合模块，Agent 仅依赖 transport/gen；Web 仅面向 Controller REST/SSE。

公共 API/Proto/生成配置、根锁文件、数据库公共结构和 CI 由 Codex 集成负责人协调。CODEOWNERS 尚未绑定真实 GitHub 身份；文档角色和本地检查已就绪，线上强制审查未配置。

## D. 下一批任务建议

FIRST-BATCH-WORK-PACKAGES.md 已逐项列出任务编号、Agent、分支/Worktree、可改/禁改文件、已有接口、交付、自动验收、并行条件、审查与合并人，共 6 个工作包：G2-R01（Codex 契约收口）、N01（DeepSeek 节点领域 Mock）、A01（DeepSeek 审计）、W01（Gemini Mock UI）、S01（后续授权的 SQLite 基础）、T01（Codex 顺序安全准备）。

建议首次只开 **G2-R01 + N01 + W01** 三个任务，或先 G2-R01 + W01 两个。未冻接口相关任务只能做 Mock/适配/纯领域测试。安全、通信、核心任务机制和公共接口最终由 Codex 审查；真实调度和插件运行不能先行。

## E. 给项目负责人的下一步

工程具备隔离多会话的基础条件，但当前没有自动开会话；请明确指定要启动的工作包后再开始。先让 Codex 处理原 Buf/Go 包修订评审、新旧认证/错误/任务状态的兼容方案，补齐关键安全 ADR 的来源核验。DeepSeek 和 Gemini 可以在独立目录做不依赖冻结结果的 Mock 任务。

当前仅本地 Git，无远程/PR/线上 CI；下一阶段是否连接 GitHub、真实用户名/审查团队、main 保护规则由负责人另行授权。所有代码和文档保存并创建本地提交后停止，不在后台继续开发。
