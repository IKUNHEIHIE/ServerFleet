# 交接说明（gemini-06 · Wave 6）

## 运行

```sh
# 单测 / 类型 / 构建（38 文件 / 332 项）
npm run check:web

# 桩契约回归（16 步；负向/退化，未改动）
node tests/browser/v09/run-acceptance.mjs

# 真实 Controller 组合回归（70 步；负向/退化，未改动）
node tests/browser/v09/run-live-controller-acceptance.mjs

# 正常成功用例（18 步；需 WSL 默认发行版；12 PASS / 6 BLOCKED 为当前基线预期）
node tests/browser/v09/run-positive-success-acceptance.mjs [--skip-build] [--out <目录>] [--distro Ubuntu]
```

正向套件的环境要求与原理（脚本头注释同样写明）：

- 需要 WSL2 发行版（默认 `wsl -l -q` 第一个；`--distro` 可指定）。套件交叉编译
  `GOOS=linux GOARCH=amd64 CGO_ENABLED=0` 的 controller+agent，在 WSL 原生文件系统
  运行（SQLite WAL 语义正确、Agent `runtime.GOOS=linux` 才能通过 controller 的
  handshake 结构校验——Windows 本机 Agent 会被 `hello.Platform != "linux"` 拒绝）。
- 本地 PKI 按 `apps/controller/localpki` 的加载格式预置（EC P-256/PKCS8/CA:TRUE），
  并以该 CA 签发 SAN 覆盖 `DNS:localhost` 的服务端证书；Agent 以 `-root-cert` 钉扎。
  **控制器必须显式传 `-agent-pki-dir <work>/pki`**：默认值是 `-db` 旁边的 `db/pki`，
  会让控制器自签另一把 CA，与 Agent 钉扎的 SPKI 不一致，enroll 一律
  403 `ENROLLMENT_REJECTED`（本轮调试实测踩过并已固定在脚本里）。
- 长驻进程全部前台 exec 运行：wsl.exe 客户端在测试期间保持会话存活；WSL 会在会话
  结束时终止其进程组——这是可靠的清理机制（`nohup` 后台化反而会被会话结束立即
  杀死，本轮实测）。`wsl.exe -- sh -c <串>` 的命令串会被 WSL 默认 shell 二次解析，
  因此所有 WSL 操作走脚本文件 + 直接 argv。
- 退出码：仅存在 FAIL 才为 1；BLOCKED 是如实登记的缺口，不算失败。

## 当前基线的预期结果

- `run-positive-success-acceptance.mjs`：12 PASS / 6 BLOCKED / 0 FAIL。
  BLOCKED 均引用服务端精确响应（B01 500 internal_error、B02 503
  `MESSAGE_SOURCE_UNAVAILABLE`、B03 501 `PLUGIN_CATALOG_SOURCE_UNAVAILABLE`、
  B04 无服务组/网关/DNS 数据集、B05 503 `UPDATE_SOURCE_UNAVAILABLE`、
  B06 404 `BACKUP_NOT_FOUND`）。
- 总控接线（CX/DS Wave 6 契约 + apps/controller 组合入口）后重跑同一脚本：
  B01–B06 自动按"真实成功"断言，无需改脚本（正向尝试分支已双向写好）。

## 未做 / 边界

- 未修改 Go 代码、OpenAPI/Proto、`package.json`/锁文件、CI、`apps/web/public`。
- 未操作 awskr、未创建云资源、未使用真实外部秘密；全部测试数据为合成值。
- 密钥管理状态入口（CX Wave 6 #1）、Agent 常驻连接字段、任务日志/输出查询端点：
  契约未交付，前端未伪造，见 `SHARED-CHANGES.md` 的精确请求。
- 双架构/1C1G/物理跨机/外部供应商：各自收证范围，见 `AC-EVIDENCE-WAVE6.md` 五列矩阵。
