# V09-CLOUD-WB-01 · HANDOFF

## 本检查点实际执行

| 命令 | 结果 |
|---|---|
| `npm ci` | 127 packages，退出 0（`@bufbuild/buf` postinstall 未批准，与既有说明一致） |
| `npm run tools:install` | 未执行（本检查点不需要 Go 工具链二进制；`go build/vet` 直接用 Go 1.27.3） |
| `go build ./apps/agent ./apps/controller ./tests/browser/v09/fixturehost` | 退出 0 |
| `go vet ./tests/browser/v09/fixturehost` | 退出 0 |
| `gofmt -l tests/browser/v09/fixturehost/` | 无输出 |
| `npm run build --workspace @serverfleet/web` | 退出 0（dist 由浏览器共用，构建期间无并发访问） |
| `npm run check:web` | 退出 0；Test Files 39 passed / Tests 348 passed |
| `SF_BROWSER=/usr/bin/google-chrome node tests/browser/v09/run-product-update-acceptance.mjs` | 退出 0；**20 PASS / 0 FAIL** |

## 专项步骤（P00—P19）

P00 构建 → P01 本地 CA/服务端证书 → P02 阶段 A 宿主 → P03 setup token 摘要 →
P04 浏览器 bootstrap + 异人管理员 → P05 真实 Agent 令牌/pending → P06 审批/issued →
P07 节点与更新授权 → P08 真实发布构建入槽 + 合成 ed25519 签名 inbox →
P09 阶段 B 装配 `UpdateProduct` 并启动受监督真实 Agent（1.0.0）→
P10 浏览器进入更新中心 → P11 检查更新 → P12 冻结计划 → P13 确认 + 风险勾选 + 202 →
P14 异人管理员审批 → P15 版本 1.0.0→1.0.1 且 PID 改变 → P16 槽位字节 = 发布构建 SHA →
P17 作业 completed 且 taskId 与审批任务一致 → P18 篡改工件在计划阶段被拒且产品不变 →
P19 重启后恢复 1.0.1、作业终态持久。

## 关键实现点（供复验）

1. **程序级注入，无 HTTP 入口**：新增 `tests/browser/v09/fixturehost/product_update.go`，
   仅从 `-product-update-config` 指定的私有 0600 JSON 读取可信配置；文件必须是普通文件、
   非符号链接、非组/其他可读，路径必须绝对，`controllerOrigin` 必须 https。
   `SchemaFingerprint` 由宿主先组合一次正式 `server.New`（完成正式迁移）后从真实库读取，
   配置文件无法伪造。
2. **生命周期闭合**：`fixturehost` 在 `ListenAndServeTLS` 返回后调用 `composed.Close()`。
   缺少这一步时第二次启动会被产品的正确行为拒绝（`updates: installation reserved`）。
   这是宿主缺陷修复，不是绕过产品校验。
3. **Web 侧修复**：`更新中心` 原先把「历史作业里还没有 backupId」当作 apply 前置阻断，
   导致第一次真实升级永远无法发起。现改为如实提示「本次升级需要备份」，备份仍由服务端在
   `backing_up` 阶段创建并回写 `backupId`；确认文本与风险勾选仍为强制前置。
   对应单测已同步为「提示存在但不再永久禁用」，仍断言未确认时按钮禁用且不发请求。
4. **浏览器写语义**：所有写请求在真实页面上下文发起（Cookie/Origin/Sec-Fetch-*/CSRF 由
   浏览器提供，并带 `Idempotency-Key`），不使用绕过这些边界的裸客户端。

## 依赖与剩余风险

- **无新增后端依赖**：直接消费已合入 main 的 `server.Config.UpdateProduct`、
  `updates.ProductDeploymentOptions`、`tests/integration/v09/platform/update_product_r3_test.go`
  所描述的装配方式，未等待 CX/DS 新接口。
- **平台差异**：既有正向套件依赖 Windows `wsl.exe`，本任务新增原生 Linux 专项脚本
  `tests/browser/v09/run-product-update-acceptance.mjs`，未修改原套件，未把两条通道混算。
- **未覆盖**：Controller 组件升级、在线发行源、分批/金丝雀；这些仍属未完成，不得由本专项推断。
- **环境事实**：`proxy.golang.org` 不可达，需 `GOPROXY=https://goproxy.cn,direct`；
  `github.com` 直连偶发超时，`git` 操作需重试。此项已记入 STATUS，不影响代码结论。
- **进程与目录**：所有实测使用普通用户 `ubuntu`(uid=1000) 的私有 0700 目录；只终止本任务
  自己创建的进程（宿主、自有 Agent、无头浏览器）；未触碰任何既有服务。

## 复现方式

```sh
export SF_BROWSER=/usr/bin/google-chrome
node tests/browser/v09/run-product-update-acceptance.mjs            # 完整（含构建）
node tests/browser/v09/run-product-update-acceptance.mjs --skip-build
```

运行态目录 `~/sf-wb01-product-update`（0700，保留用于复验；脚本每次运行前清空运行态，
不保留旧 DB/身份）。

---

## CP-3：B06 灾难恢复前置条件探测

| 命令 | 结果 |
|---|---|
| `node tests/browser/v09/run-recovery-prerequisite-probe.mjs` | 退出 0；**11 PASS / 3 BLOCKED / 0 FAIL** |

### 设计取舍（为什么不像 B05 那样注入执行器）

B05 能被真实验证，是因为 `server.Config.UpdateProduct` 是**产品已合入的程序注入点**，
注入的配置只描述「可信工件来自哪里」，执行仍由产品自己的 `internal/updates` 完成。

B06 不同：`server.Config.RecoveryExecute` 是**执行器本身**。若我在宿主里写一个恢复适配器，
被测对象就变成我写的代码，而不是 AgentMore 的产品实现——这是自证。所以本专项
**按生产同构启动（不注入 `RecoveryExecute`）**，只记录产品的真实返回，对「实际恢复」不下结论。

### 步骤（R00—R13）

R00 身份/工具/目录权限（非 root，0700）→ R01 构建宿主 → R02 私有 PKI 与 0600 合成 secret →
R03 生产同构启动（无 `RecoveryExecute`，含 Agent PKI 链路）→ R04 Web 同源宿主 →
R05 浏览器完成首次 bootstrap → R06 `VACUUM INTO` 生成真实备份并核对 `schema_migrations` 账本 →
R07 备份校验真实通过 → R08 预览 `eligible=false`（2/4 项产品侧关闭）→
R09 创建作业 `501` → R10 作业读回 `404` → R11 未知备份 `404` 且不泄露路径 →
R12 未认证 `403` 且不返回检查项 → R13 页面实况与能力一致（不出现无依据的「恢复成功」）。

### 结论（不得外推）

- 可验证的部分是通的：备份校验 `200 verified=true`，两条负向鉴权/存在性判定正确。
- 不可验证的部分是产品侧关闭：预览恒不合格、执行器未装配、作业状态不可读回。
  因此「浏览器端到端恢复」在本基线**不可能跑通**，不因页面存在而算完成。
- 第二 Controller 健康、独立 DB/PKI 只算隔离基础，不当灾备成功。

### 环境事实（本轮新增）

- `node -e` 内联脚本的 ESM/参数行为不稳，探测脚本改为落地 `.mjs` 临时文件执行
  （`node file.mjs args`），避免 `argv` 与模块类型歧义。
- 宿主的 `-origin` 必须是实际 Web 端口；origin 里出现 `undefined` 会被 auth 的
  Origin 校验直接拒绝，表现为 `compose server: auth: invalid check`（非配置缺失）。
- 无头浏览器进程不经过 `track()`，必须在 `cleanup()` 中显式终止，否则事件循环不退出，
  脚本会被外层 `timeout` 杀掉（退出 124）。

```sh
node tests/browser/v09/run-recovery-prerequisite-probe.mjs            # 完整（含构建）
node tests/browser/v09/run-recovery-prerequisite-probe.mjs --skip-build --keep
```

运行态目录 `~/sf-wb01-recovery-probe`（0700）。

---

## 返修轮 R1（覆盖上面 CP-2 的 20 PASS 计数）

首轮独立审查（锚定 `d0dd938`）提出四项。以下为处置与复核方式，正文细节见 `STATUS.md`。

### 代码改动

| 文件 | 改动 |
|---|---|
| `tests/browser/v09/run-recovery-prerequisite-probe.mjs` | R08—R10 改由 `probeBlocked()` 唯一入口记账：只有 status + problem content-type/type/status/code + 预览检查项集合全部精确匹配才 BLOCKED，其余 FAIL；新增 `--require-complete`；修掉 `extra.status` 覆盖判定字段的 bug |
| `tests/browser/v09/run-product-update-acceptance.mjs` | P18 增加 `expectUpdateProblem()` 严格断言（503 + problem 结构）与 1.0.3 合格对照；删除「计划通过就继续 apply」兜底；新增 P18B 真实回滚；P19 增加失败终态持久与作业数不增的断言 |
| `tests/browser/v09/run-probe-classification-selftest.mjs` | 新增：把 R08—R10 片段抽到 vm 里喂响应，固化 WB-R1 的回归（含审查给的「三端点全 500」口径）；R2 扩到 17 组，覆盖必需成功项失败/缺失、重复、未知失败/通过项、结构非法 |

### 返修轮 R2（WB-R2-01）

`previewMismatches()` 不再只看两个已知失败项，而是要求：项名集合恰好四项、结构合法、
`backup_integrity` 与 `recovery_key_reference` 均通过、失败集合恰好两项且 reason 逐字相符。
任一偏差（新的恢复密钥失败、缺项、重复、未知项、reason 漂移）都返回非空 mismatches → FAIL。
最小复核：

```sh
node tests/browser/v09/run-probe-classification-selftest.mjs   # 17/17
```

### 复核命令

```sh
node tests/browser/v09/run-probe-classification-selftest.mjs           # 17/17，退出 0（R2 后）
node tests/browser/v09/run-product-update-acceptance.mjs               # 21 PASS / 0 FAIL
node tests/browser/v09/run-recovery-prerequisite-probe.mjs             # 11/3/0，退出 0（INCOMPLETE）
node tests/browser/v09/run-recovery-prerequisite-probe.mjs --require-complete   # 同结果，退出 1
```

### P18B 的构造要点（复验时最容易搞错的地方）

- 候选版本必须让**旧槽位字节 ≠ 候选字节**，否则「回滚成功」与「新版本继续运行」在字节层面无法区分。
  因此 1.0.3 的工件使用 **1.0.0 的真实字节**（声明 1.0.3）：签名、SHA-256、平台、序号全部合法，
  失败点被强制推到安装后的运行版本校验。
- 不用 `1.0.1` 做对照计划：它与已安装版本同序号，`FrozenInstallPlan.valid()` 要求
  `Attempted.Sequence > Previous.Sequence`，会被策略挡掉，据此判断「planner 可用」是错的。
- 回滚完成的判据是账本（`receipt.State=rolled_back` + `Current=1.0.1` + `switched`/`rollback-switching`
  阶段标记）与槽位字节、进程镜像、健康文件；作业 `failed` 只是必要条件。
- 作业终态判断用白名单（completed/partial_failure/failed/canceled/expired/unknown），
  不要用中间态黑名单——原先漏了 `running`，会把执行中的作业当终态。

### 本轮环境修复（非产品改动）

容器重建后系统 Go 为 1.21.4、Node 为 18/22，低于 `go.mod`（1.27.3）与 `scripts/tool-versions.json`（24.22.1）。
已补齐：`.tools/go` → 已下载的 `go1.27.3` toolchain（`GOTOOLCHAIN=local`），Node `v24.22.1` 置于 `/opt/node24`。
`.tools/` 在 `.gitignore` 内，不进入提交。`check:go` 所需的 `SF_R*_BINARY` 用本轮真实构建提供，
不提供时对应测试会以「explicit prebuilt actual Agent binary is required」失败——这是基线环境依赖。

