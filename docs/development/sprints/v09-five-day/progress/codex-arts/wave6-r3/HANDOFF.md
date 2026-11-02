# Codex Arts · Wave6-R3 交接与本轮检查点

## HEAD 与检查点

| 检查点 | 提交 | 内容 |
|---|---|---|
| CP-1 | `92eb5ecdddc81566ed365e9c1806fbcfe11c389d` | AI 预算正式界面（owner 窗口/限额/已用/剩余、管理员 CAS、409、只读、缺策略拒绝）+ 4 个组件测试 |
| CP-2 | 本文件所在提交 | B01 真实文件页面闭环：fixturehost 注入 `server.Config.FileExecution`、私有 spool、受控 nodeID→逻辑根注册表；两个真实常驻 Agent 各自私有 `-file-root`；页面创建 transfer → 统一任务 → 双方执行 → 目标 SHA/任务/执行终态；新增预算浏览器步骤 P20 |

## CP-2 运行方式（严格正向，含 B01）

```powershell
# 在仓库根目录，使用仓库固定 Node/Go（本机只读共享 .tools）
$env:PATH="<repo>/.tools/node-v24.22.1-win-x64;<repo>/.tools/go/bin;$env:PATH"
node tests/browser/v09/run-positive-success-acceptance.mjs `
  --ai-host --attempt-file-flow --require-complete --distro Ubuntu `
  --out reports/positive-w6r3-b01
```

结果（2026-11-02，WSL Ubuntu，linux/amd64 交叉构建）：

- **23 PASS / 4 BLOCKED / 0 FAIL（27 步）**，`--require-complete` 下退出码 1（4 个已知 BLOCKED 仍存在，未伪造通过）。
- B01：`transfer xfer_03ddc8228e524be13497bb0a completed via unified task tsk_1ec4849dca25d975bcf51b47; both endpoints succeeded; destination sha256 matches source`。
- P20：`owner budget window browser-fixture-window r2: requests used 1/12, outputTokens 8192; counters preserved by server`。
- 证据：`reports/positive-b01.log`、`reports/positive-w6r3-b01/`（截图，Windows 本地忽略目录）。

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

- B01 负向：取消、撤权、已写后丢回执/UNKNOWN（按后端已支持能力逐一验证，缺能力时给最小失败用例）。
- 回归 70 步真实 Controller 与 16 步契约桩分别重跑并记录。
- 其他可独立 Web 项（错误/空/权限/部分结果）与 B04/B05/B06 宿主结构。