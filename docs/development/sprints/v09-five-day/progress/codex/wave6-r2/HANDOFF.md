# V09-R6-CX-03 · codex-06 交接

审查人 **codex-review-02**。分支 `feature/v09-r6-platform-cx06`，工作目录 `.worktrees/feature--v09-r6-platform-cx06`；同步基线 `c9013f4231c5f0a586eb0af26386f6e265996c0c`。固定、干净的被测代码为 **22bbe91799ab11fbf0a0035296055a110eae4c40**；最终交接提交只增加本目录证据/文档。包内可独立实现与本机验证已收口，按授权停止；不宣称完整 v0.9、G2 冻结或生产发布完成。

未合并 main、推送、SSH/操作 awskr、操作云资源、使用真实外部秘密、修改其他作者目录、启动其他 Agent 或代发消息。中央入口、Proto/gen、历史 DDL、根配置/锁均未直接修改。R1 历史补丁不再应用。

## 可运行增量

| SHA | 主要文件与结果 |
|---|---|
| c9f6855a73e742438cff8248965f7ebdc1e070a4 | internal/files/execution_bindings、root_mapper；internal/tasks/worker；新增 Controller file_worker；transport 生命周期：HTTP→统一调度→两个真实常驻 Agent→多文件实际字节/SHA/父任务终态。持久接受先于 Open；UNKNOWN 不重放。 |
| 6f10bc5fb5f19aa24d7c512304c91a2946c65ab7 | internal/ai/budget_http、budget_view、budget_sql；新增 ai_budget_http 与 r7 候选：owner 读取、当前管理员 CAS、策略/审计同事务、保留使用计数和 operation fences。 |
| 199d678591d71fe30beec547090634af389b540d | files 取消/并发终态保护、绑定校验、当前授权逐帧复核；实际 HTTP 取消及资源撤权后无目标写，已不确定的写保持 UNKNOWN。 |
| 804993887de9b9f413a27310e9b14cc0b70cf889 | updates/task_worker、install_sql、实际 Linux 进程/SQLite 测试；recovery/runtime_guard 与 tasks/transport 接线端口：冻结计划/人工审批、真实健康与兼容回滚、原子审计、恢复启动前及运行时隔离门。 |
| 41a5f5827a24b560914051176cbe829a19b4a986 | 固定 clean HEAD 的完整组合 runner、私有 Git index 补丁应用检查。 |
| 544c048d15fce906c88629ddff60476748f8589b | 实际 Agent 有界重连夹具等待与进程退出诊断；生产重试政策未改。 |
| a0e22c49a328a488a0e0907d5ad9271f683d31af | 活跃 HTTP 响应写/flush 复核，保留 WSS hijack 及独立帧门；公开显式组合端口使未接中央补丁的分支也通过静态分析。 |
| 22bbe91799ab11fbf0a0035296055a110eae4c40 | updates/http、task_jobs：实际 awaiting_approval 等统一任务状态可返回；真实 owner 作业列表/单项、当前读权限与明确窗口；中央适配及更新 OpenAPI 仅交 patch。 |

完整 51 个被测变更文件、提交、工具版本、日志/二进制/补丁 SHA256、根 SKIP 名单见 [EVIDENCE.json](EVIDENCE.json)。已接收的持久凭据密钥/借用、常驻 Agent、AI 持久预算/typed 审计继续复用，本轮未另造密钥或任务中心。

## 固定代码验证

固定 Go 1.27.3、Node 24.22.1、npm 11.19.1；本机现有 WSL Ubuntu，UID 1000。全套验证在干净 `22bbe917` 上完成，后续只写交接文档。

| 命令/范围 | 实际结果 |
|---|---|
| `. ./scripts/env.ps1` 后 `npm ci` | PASS，127 packages；Buf postinstall allowScripts 警告保留，正式 Buf lint/build/generate/兼容检查实际通过，未改根依赖。 |
| `npm run tools:install` | 公共代理首试网络失败、GOPROXY=off 查询失败；最终将 GOPROXY 指向本 worktree 已有 `.cache/gopath/pkg/mod/cache/download` 的 file URL，原安装器/四个精确版本 PASS，未关闭校验或改 pins。 |
| `npm run check` | policy/contracts/go/web/git 全 PASS；包含固定生成/兼容、模块边界、build/test/vet/staticcheck、Web 类型/测试/构建及工作区检查。 |
| 根 Go JSON | 39 package、763 顶层 +1188 子测试 PASS，0 FAIL；20 SKIP 逐项保留，不能算成功。 |
| Web | 38 文件、340 测试 PASS；既有 Vite >500kB 警告保留，无前端修改。 |
| `npm run check:security` | PASS；npm 0 漏洞、govulncheck `No vulnerabilities found`，不代表 Xray 发行物准入。 |
| `node tests/poc/v09/platform/run-wave6-r2-combined.mjs` | 完整中央候选 overlay、6 顶层 PASS /0 SKIP。正式 Controller、本地 TLS、两个实际常驻 Agent；文件完整测试约 77 秒。 |
| `run-wave6-r1-linux.mjs` 依次 agent/transport/ai/budget/controller/updates/recovery/restore | 分别 42/58/34/1/1/17/9/1 顶层 PASS；Agent、updates 各 1 个私有子进程 helper SKIP，实际由其父测试另起进程执行。此 runner 的 controller 目标使用已接收正式组合，无 R1 overlay。 |
| 另交叉编译 auth/files/tasks 的测试二进制，UID1000 WSL `-test.v -test.timeout=180s` | 73/60/70 顶层 PASS；files 仅跳过 1 个 Windows 路径规则，Windows 根测试覆盖该规则。实际凭据重启/轮换/并发/私有权限/撤销回归通过。 |
| Linux 完整候选 `go vet -overlay=reports/wave6-r2/platform-overlay.json ./...`，Linux `govulncheck ./...` | PASS；扫描包含 Windows 构建不含的 Linux 进程/文件代码。 |
| `GOOS=linux GOARCH=amd64/arm64 CGO_ENABLED=0 go build -mod=readonly` Controller/Agent | 四个二进制编译 PASS，Controller 带完整中央候选 overlay；ARM 运行 UNTESTED。 |
| 更新 HTTP 专项、更新/恢复模块及架构检查 | PASS；当前 Cookie/CSRF、真实 SQLite、等待人工审批、同计划同任务、跨 owner 404/空列表、撤权后两读路由 403、审批前零安装调用。目标为明确 OFFLINE inventory 夹具。 |
| 两份新候选 Redocly lint；五份补丁私有 index 应用并 `diff --cached --check` | PASS，无中央源码/实际 index 改写。 |

Linux 十一组共 **366 顶层 PASS、3 SKIP**；与 Windows/组合测试有重叠，不相加宣传覆盖。原始输出位于本 worktree 忽略的 `reports/wave6-r2`、`reports/wave6-r1-linux` 和根 reports。EVIDENCE 保存摘要与哈希；两 helper SKIP 及 Windows 专用规则有明确解释，不隐藏跳过。

完整组合具体覆盖：三个文件项 99,000/48,000/0 字节、六个唯一子执行、目标字节和 SHA、两节点父 task/transfer 终态；已有文件 skip，overwrite 创建前拒绝；实际源帧后 HTTP cancel/资源 DELETE 撤权；checksum 不匹配前置失败；SQLite 强制接受记录失败后无 Open/写且 UNKNOWN；实际目标已发布后额外当前 scope gate 拒绝回执，重开数据库/Controller、原 Agent 实际重连后不增加子执行、不重写文件。额外 scope gate 夹具没有冒称真实权限撤销，后者有独立实际 HTTP 用例。

更新实际 Linux 用例使用真实签名测试二进制、真实 owned PID/镜像/nonce/版本、实际 SQLite 备份/hash/user_version、统一任务审批和 SQL 审计。已知版本健康失败安全回滚后新 PID 健康；实际 user_version=2 不兼容时不重启旧二进制，留下 UNKNOWN/永久 reservation。角色、online、安装来源/bootstrap 端口仍为明确夹具；不等于正式产品更新准入。恢复测试使用真实还原 SQLite/Controller，各入口与活跃流的隔离门已验证；旧主控 fence/新离线权威 verifier 仍是 double。

## 中央接收顺序

1. `controller-files.patch`：cx/0008_file_execution_bindings、唯一 worker/桥接接线；明确私有 Agent root 映射和 spool、Enrollment authority 必须提供，不可叠加放行 AgentFileGate。
2. `controller-ai-budget.patch`：owner 预算路由。`redocly-ai-budget.patch`：独立根 lint 条目。
3. `controller-platform.patch`：在 files+AI 后应用；候选 cx/0009_update_journal 需总控分配/登记，更新 action/admin/current task/Job 适配与恢复 RuntimeGate。包含 server.go、enrollment.go、updates_recovery.go；作者未直接改这些共享源码。
4. `updates-openapi.patch`：在 AI lint patch 后应用，新增 r7 更新候选/根条目。历史 g2 `jobId/state/toVersion` 与已采用 HTTP 的 `id/taskId/status/targetVersion` 不兼容，明确候选差异，不能宣称旧契约兼容或覆盖历史契约。

重现：先 `node tests/poc/v09/platform/prepare-wave6-r2-platform-overlays.mjs`，再 `prepare-wave6-r2-update-contract.mjs`，最后 `check-wave6-r2-patches.mjs`。完整运行用 `run-wave6-r2-combined.mjs`，要求固定干净 HEAD；所有生成仅写 reports/作者审查补丁。迁移升级 21→23、旧 checksum 保持及二次应用均通过。Proto/生成代码没有改动。

配置和安全前提详见 [SHARED-CHANGES.md](SHARED-CHANGES.md)。默认 CLI 没有自动启用文件/更新 worker。更新 resolver 只能读已有不可变 owner 计划，SQL journal 要求独立真实 installed provenance，不能从 HTTP 确认词/备份推定。HTTP job 是实际 task 状态，未编造安装 phase/健康；列表只看最近 200 个统一任务并标 truncated，旧项按精确 ID 查询。

## 保留的失败与剩余项

首轮 bootstrap 夹具、UNKNOWN 父任务不能收口、撤权后仍期待可读、更新夹具 migration namespace、实际 Agent backoff 的 10 秒等待、静态分析、工具代理、中央 projector 初始化顺序及 PowerShell 点号参数问题均保留原始失败和修复经过。Web 模型测试曾在 `ai-pages.test.tsx:148` 异步加载后立即查控件失败；未改 GM 代码/断言，独立 10 项及最终全量 340 项复跑通过。不能抹去这次间歇失败。15 份经过/失败日志索引见 EVIDENCE。

| 剩余项 | 状态、独立工作已停的具体原因 |
|---|---|
| 中央接线/DDL/API 接收 | BLOCKED：具体补丁与可运行 overlay 均已交付；登记编号、共享入口/契约消费者接入及 main 合并归总控。不能自行改共享源码消除依赖。 |
| 文件策略范围 | UNIMPLEMENTED_REJECTED：当前仅 skip，最多16项、每项64MiB、显式私有根；overwrite/rename/createParents/nonzero resume 明确拒绝。已持久 UNKNOWN 只对账，不自动重跑。 |
| 真正 ENOSPC/inode 耗尽 | UNTESTED：已有实际 EFBIG 部分写失败与 Fstatfs 预查，当前未有私有容量受限文件系统夹具；未填满共享磁盘，也不把 EFBIG 冒称 ENOSPC。 |
| 更新产品准入 | BLOCKED：正式 UPDATE/PRIV/WC/PKI-OPS、签名/信任密钥治理、产品版本/storage readiness/rollback 契约与实际宿主管理仍需批准/总控。OwnedSupervisor 只管自己启动的子进程，不接管旧服务、外部 PID 或 awskr。 |
| 恢复权威与 fence | BLOCKED：新权威/实际新钥、旧 Controller 持续可靠 fence、真实撤销/对账证明仍缺；隔离门/显式释放机制已交付，但不提供真实权威证明，不批准生产恢复释放。 |
| 可选 Xray | BLOCKED：固定26.10.29公共工件此次在获取阶段失败，未作本轮新扫描/执行；历史候选安全 FAIL 保持，未另选未批准 Runtime、未安装。 |
| 实机/外部/产品验收 | UNTESTED：物理双机、ARM运行、1C1G、真实外部模型/账号、root准入、当前代码浏览器全验收及 awskr 操作不在本轮已执行证据内。AI 8192 是保守输出上限预留，不是实测 token/金额；缺策略仍拒绝，owner限制不放宽。 |

本轮任务要求的独立文件主链、AI预算HTTP、更新统一任务/审计与恢复入口防护均已交代码/正常和负向证据；剩余项涉及共享所有权、正式权威/工件或未具备环境，不能以默认放行替代。交接后停止，由 codex-review-02 审查接收并安排剩余实机/产品工作。
