# Codex Arts · Wave6-R3 交接与本轮检查点

## HEAD 与检查点

| 检查点 | 提交 | 内容 |
|---|---|---|
| CP-1 | `92eb5ecdddc81566ed365e9c1806fbcfe11c389d` | AI 预算正式界面（owner 窗口/限额/已用/剩余、管理员 CAS、409、只读、缺策略拒绝）+ 4 个组件测试 |
| CP-2 | `ba60e55469662fece4858a6756c6b6d8338b9d94` | B01 真实文件页面闭环：fixturehost 注入 `server.Config.FileExecution`、私有 spool、受控 nodeID→逻辑根注册表；两个真实常驻 Agent 各自私有 `-file-root`；页面创建 transfer → 统一任务 → 双方执行 → 目标 SHA/任务/执行终态；新增预算浏览器步骤 P20 |
| CP-3 | 本文件所在提交 | B01 取消负向：32MiB 传输在任务 running 后由页面取消；终态 cancelled、无 copied 项、父任务非 completed；显式授权 `tasks.cancel` |

## CP-2 运行方式（严格正向，含 B01）

```powershell
# 在仓库根目录，使用仓库固定 Node/Go（本机只读共享 .tools）
$env:PATH="<repo>/.tools/node-v24.22.1-win-x64;<repo>/.tools/go/bin;$env:PATH"
node tests/browser/v09/run-positive-success-acceptance.mjs `
  --ai-host --attempt-file-flow --require-complete --distro Ubuntu `
  --out reports/positive-w6r3-b01
```

结果（2026-11-02，WSL Ubuntu，linux/amd64 交叉构建）：

- **24 PASS / 4 BLOCKED / 0 FAIL（28 步）**，`--require-complete` 下退出码 1（4 个已知 BLOCKED 仍存在，未伪造通过）。
- B01：`transfer xfer_31e0b0bfb133d6d213b5be10 completed via unified task tsk_3a193f8777c85631ca7b1bac; both endpoints succeeded; destination sha256 matches source`。
- B01n：`cancel xfer_c3cd19d34bcea25d80062993 -> cancelled; no copied item; parent task is not completed (cancel is not a rollback claim)`。
- P20：`owner budget window browser-fixture-window r2: requests used 1/12, outputTokens 8192; counters preserved by server`。
- 回归：真实 Controller 浏览器 **70/70 PASS**；契约桩 **16/16 PASS**（各自独立报告，未相加宣传）。
- 证据：`reports/positive-b01.log`、`reports/live-regression.log`、`reports/stub-regression.log` 与对应截图目录（Windows 本地忽略目录）。

## 根检查（2026-11-02）

- `npm ci` PASS（127 packages，固定 Node v24.22.1）。
- `npm run check`：policy / contracts / web / git **PASS**；Go 首次因本机 `GOPROXY=off` 无法解析依赖自身的测试模块，`go mod tidy -diff` 失败（环境原因，无代码/依赖改动）。用可访问的 `goproxy.cn` 补齐固定版本模块后 `npm run check:go` **PASS**（含测试、vet、staticcheck）。
- `npm run check:security` **PASS**：npm audit 0 漏洞；govulncheck 无已知漏洞。
- 原始输出：`reports/check-all.json`、`reports/check-go.json`、`reports/check-security.log`、`reports/full-check.log`。

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

- B01 剩余负向：撤权与“已写后丢回执/UNKNOWN”。后端联合测试已有真实进程证据；页面宿主尚未提供确定性故障注入/执行状态观测口，需精确端口后再补，不能用单线程或结果不明当成功。
- B04/B05/B06 宿主结构：当前页面已按服务端真实语义显示拒绝/不可用；扩完整流程需 CX/DS 提供发布 typed worker 与终态补偿、正式更新源与安装装配、第二隔离主控与真实 fence/新权威端口。缺口已在 `STATUS.md` 与 `<codex-review-02>` 可见的 BLOCKED 明细中列出。
- 其他可独立 Web 项（错误/空/权限/部分结果）按能力逐个补；本增量未改生产后端或公共契约。