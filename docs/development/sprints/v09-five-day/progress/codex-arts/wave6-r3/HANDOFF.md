# Codex Arts · Wave6-R3 交接与本轮检查点

## HEAD 与检查点

| 检查点 | 提交 | 内容 |
|---|---|---|
| CP-1 | `92eb5ecdddc81566ed365e9c1806fbcfe11c389d` | AI 预算正式界面（owner 窗口/限额/已用/剩余、管理员 CAS、409、只读、缺策略拒绝）+ 4 个组件测试 |
| CP-2 | `ba60e55469662fece4858a6756c6b6d8338b9d94` | B01 真实文件页面闭环：fixturehost 注入 `server.Config.FileExecution`、私有 spool、受控 nodeID→逻辑根注册表；两个真实常驻 Agent 各自私有 `-file-root`；页面创建 transfer → 统一任务 → 双方执行 → 目标 SHA/任务/执行终态；新增预算浏览器步骤 P20 |
| CP-3 | `2c3a1b0c237ea8b0c5299b4d1526d166138ebb15` | B01 取消负向：32MiB 传输在任务 running 后由页面取消；终态 cancelled、无 copied 项、父任务非 completed；显式授权 `tasks.cancel` |
| CP-4 | `35e0c7f`（负向/宿主）/ `d2d2f14`（Web） | B01 撤权/丢回执负向；B06n 第二隔离 Controller 宿主（独立 DB/PKI/端口）；更新中心与插件目录的阻塞错误说明 |

## 已发现、待后端修复（最小失败用例）

- **queued 阶段取消可能不收敛**：页面创建传输后立即 `POST /api/v1/transfers/{id}/cancel`，实测 20 秒内 transfer 仍停在 `cancelling`，任务未进入终态；此时取消器尚未持有运行上下文。当前负向测试改为**等待 task=`running` 后再取消**（已 PASS），没有把 queued 取消算作通过。
- 复现要点：创建 transfer → 不等待 running → 立即 cancel → 轮询 `GET /transfers/{id}` 20s。归属 CX 的 tasks/files 取消状态机；本会话不修改后端。
- 其余文件项：`overwrite`/`rename` 当前被 worker 明确拒绝（B01n 已断言 400/409/422），不能静默退化为 skip。

## CP-2 运行方式（严格正向，含 B01）

```powershell
# 在仓库根目录，使用仓库固定 Node/Go（本机只读共享 .tools）
$env:PATH="<repo>/.tools/node-v24.22.1-win-x64;<repo>/.tools/go/bin;$env:PATH"
node tests/browser/v09/run-positive-success-acceptance.mjs `
  --ai-host --attempt-file-flow --require-complete --distro Ubuntu `
  --out reports/positive-w6r3-b01
```

结果（2026-11-02，WSL Ubuntu，linux/amd64 交叉构建）：

- **29 PASS / 4 BLOCKED / 0 FAIL（33 步）**，`--require-complete` 下退出码 1（4 个已知 BLOCKED 仍存在，未伪造通过）。
- B01：`transfer ... completed via unified task ...; both endpoints succeeded; destination sha256 matches source`。
- B01n 取消：`cancel ... -> cancelled; no copied item; parent task is not completed`。
- B01n 撤权：`revocation ... -> failed; live read was 403 while revoked; no copied item; task failed is not completed; grants restored`。
- B01n 丢回执：`destination agent interrupted after write start; transfer ... -> failed; no copied item; task failed is not completed; agent restarted but the transfer stayed failed (no replay)`。
- B01n 多文件缺失：`multi-file transfer ... -> failed; missing source item is failed; task failed is not completed`。
- B01n 路径/策略：`traversal source=400, destination=400; overwrite=400; no task accepted`。
- B06n：`second controller :<port> healthy; separate DB/PKI (initialized=false, nodes/login 401); first controller keeps its identity; no recovery evidence implied`。
- P20：`owner budget window browser-fixture-window r2: requests used 1/12, outputTokens 8192; counters preserved by server`。
- 回归：真实 Controller 浏览器 **70/70 PASS**；契约桩 **16/16 PASS**；Web **347/347**（各自独立报告）。
- 证据：`reports/positive-b01.log`、`reports/live-regression.log`、`reports/stub-regression.log`、`reports/web-tests.log` 与对应截图目录（Windows 本地忽略目录）。

## 根检查（2026-11-02）

- `npm ci` PASS（127 packages，固定 Node v24.22.1）。
- 最终提交后重跑 `npm run check`：policy / contracts / go / web / git **全部 PASS**（Web 347/347）。首次运行时 `go mod tidy -diff` 仅因本机 `GOPROXY=off` 无法解析依赖自身的测试模块而失败，属于环境原因，无代码/依赖改动；使用可访问代理后同一检查通过。
- `npm run check:security` **PASS**：npm audit 0 漏洞；govulncheck 无已知漏洞。
- 原始输出：`reports/check-all.json`、`reports/check-security.json`、`reports/full-check.log`。

## UI 边界态审计（2026-11-02）

- 对 `apps/web/src/features/**` 逐一核对：使用 `useResource` 的页面全部经由 `AsyncPanel` 显示错误/重试；列表面板均有明确空态（`EmptyState`/`isEmpty`）。
- 本轮补齐两处“已知阻塞码”的专门说明：更新中心 503 `UPDATE_SOURCE_UNAVAILABLE`/计划器不可用、插件目录 501 `PLUGIN_CATALOG_SOURCE_UNAVAILABLE`；均有回归测试。
- 网关创建的 409 `takeover_approval_required` 已由服务端消息经 `describeApiError` 原样呈现，未新增静默成功路径；无权限/空态沿用既有 `AsyncPanel`/403 处理。
- 未发现需要新增“假数据”或前端自造计数的缺口；不存在未加错误/空态的列表页残留。

## B01 实现要点（安全边界）

- fixturehost 只在显式提供 `-file-spool` 与 `-file-roots` 时开启文件执行；两者必须同时给出且要求 `-agent-pki-dir`，否则启动失败。
- `-file-roots` 是测试程序写入的私有 0600 JSON（`{"nodeID":"/abs/root"}`），读取时逐次校验文件形态并委托正式 `files.NewRootPathMapper`；**fixturehost 不提供任何修改该文件的 HTTP 端点**。
- 两个 Agent 均以 `-serve` 常驻，各自 `-file-root` 只指向自己的私有目录（源 `fixtures/src`、目标 `fixtures/dst`，0700）。
- 传输仍走统一任务、当前授权、WSS+mTLS 与文件帧 Gate；页面只调用 Controller REST。
- 因测试宿主启用文件执行，Controller 协商协议 minor=1；所有 Agent 连接必须显式声明文件能力（`-file-root`），否则握手被真实拒绝（本轮首次运行即此失败，保留为失败证据）。

## 已知阻塞（未改变）

- B03 Xray：目录源 501，缺可准入工件/Runtime。
- B04 发布：网关创建返回 `takeover_approval_required`，缺完整 typed worker/终态补偿组合。
- B05 更新：`UPDATE_SOURCE_UNAVAILABLE`，缺正式更新源/安装装配。
- B06 灾备：预览缺目标身份/旧主控 fence，缺第二隔离主控真实执行。

## 下一步

- 剩余硬依赖（非本会话可单独关闭）：B03 可准入工件/Runtime；B04 typed 发布 worker/工件生命周期/终态补偿；B05 正式更新源/解析器/安装装配；B06 真实新权威/旧主控 fence 与正式 PKI-OPS 原文。第二隔离 Controller 的宿主隔离已在本轮实测。
- 可继续的独立项：B04/B05 的测试宿主注入骨架（需要 CX/DS 先给出注入接口签名，避免猜测生产契约）；遍历/symlink、续传与多文件部分结果的宿主级负向。
- 本增量未改生产 Go 或公共契约；新增 Web 提示只消费既有错误码。