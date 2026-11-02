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
