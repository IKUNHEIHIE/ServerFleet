# DS 子项证据映射（v0.9 五日包）

所有“已测”均为本机 Windows、真实 SQLite 文件库；身份/供应商/传输/Agent 用测试端口替代，不构成真实端到端验收。外部环境项一律 UNTESTED/BLOCKED。

| 子项 | 代码 | 迁移 | 主要测试/证据 | 状态 |
|---|---|---|---|---|
| DS-01 SQLite/迁移/事务/世代 | `internal/storage/**` | ledger `schema_migrations` | `go test ./internal/storage/`：WAL/重启/坏库/回滚/取消/panic/24 并发/漂移/未知/失败回滚/备份恢复/只读/世代 CAS | PASS（本机）；跨进程压力 UNTESTED |
| DS-02 节点/分组/服务组 | `internal/nodes/{repository,group_repository,service,http}.go`、`internal/servicegroups/**` | ds/0020, ds/0021 | `go test ./internal/nodes/ ./internal/servicegroups/`：序列幂等/陈旧、revoked、离线样本、revision、未知成员、拒绝审计、审计回滚、可见性、派发准入 | PASS（fake 身份/事件）；真实 Agent 事件 UNTESTED |
| DS-03 统一任务 | `internal/tasks/**` | ds/0030, ds/0031 | `go test ./internal/tasks/`：幂等/金丝雀/审批失效/过期/撤权/取消/unknown 不重试/TTL/批次熔断/崩溃恢复/并发单次派发/日志去敏/外部审批原子消费/SSE 撤权 | PASS（fake transport）；真实 WSS/Agent UNTESTED |
| DS-04 审计 | `internal/audit/{sql,http,validation,view}.go` | ds/0010 | `go test ./internal/audit/`：追加/查询/触发器/保留/并发/重启/拒绝读取审计/resourceId | PASS |
| DS-05 文件 | `internal/files/**` | ds/0040 | `go test ./internal/files/`：路径矩阵/双端授权/dry run/提交失败/取消/恢复/断点/校验/重试/下载拒绝/幂等 HTTP | PASS（fake 字节通道）；真实文件字节/节点断线 UNTESTED |
| DS-06 监控/通知/配额 | `internal/monitoring/**`、`internal/notifications/**`、`internal/quota/**` | ds/0050, 0051, 0052 | `go test` 三包通过；SSRF 矩阵/状态迁移/维护窗、去重/QQ→SMTP 一次备用/重试上限、缺失≠0、不自动断网；通知 HTTP 已注册 | PASS（fake 供应商）；真实 SMTP/QQ/网络探测 UNTESTED |
| DS-07 DNS/证书 | `internal/dns/**`、`internal/certificates/**` | ds/0060, 0061 | `go test ./internal/dns/ ./internal/certificates/`：credentialRef 不回显、默认切换、记录冲突、KeyRef/PEM 拒绝、分发 revision、审计回滚 | PASS（fake provider/issuer）；Cloudflare/ACME UNTESTED |
| DS-08 网关/发布 | `internal/gateway/**`、`internal/publishing/**` | ds/0070, 0080 | `go test` 两包通过；接管批准、探针/端口、准入、分期/重试/回滚；网关 HTTP 部分注册 | PASS（fake checker/task）；Nginx/真实节点/POC-05 UNTESTED |
| DS-09 HTTP/备份/POC | nodes/tasks/audit/files/servicegroups/dns/notifications HTTP；`storage.BackupTo`；`tests/integration/v09/services`、`tests/poc/v09/services` | 全部 13 | 13 迁移备份恢复+损坏拒绝；POC-07 10/30/100 代理；SSE 游标/撤权；固定错误 | PASS（本机）；1C1G/Linux BLOCKED |

## POC-07 本机数据（2026-10-30）

Windows 开发机（全量测试环境）：

| 代理数 | 写入/秒 | DB | WAL（checkpoint 后） | heap |
|---|---|---|---|---|
| 10 | 9370.9 | 196 KB | 0 | 2252 KB |
| 30 | 5680.9 | 368 KB | 0 | 1317 KB |
| 100 | 5298.8 | 980 KB | 0 | 2628 KB |

隔离 Linux VM（Debian 13 x86_64，2 vCPU/464MB RAM + 4GB swap；POC 用 `taskset -c 0 GOMAXPROCS=1`）：

| 代理数 | 写入/秒 | DB | WAL（checkpoint 后） | heap |
|---|---|---|---|---|
| 10 | 8535.3 | 196 KB | 0 | 1379 KB |
| 30 | 7615.4 | 372 KB | 0 | 1471 KB |
| 100 | 7487.6 | 1016 KB | 0 | 2486 KB |

SSE 读取 3 事件 <1ms。Linux 内存低于 1GB（464MB），单 CPU 运行；仍非标准 1C1G 主机验收。报告：`reports/_review/v09-services/poc07-report.json`（Windows 忽略目录）。

## 未完成/缺口

- DS-08 已知限制：`/gateways/{id}/checks` 未注册（缺少独立 serviceRunning 观测）；`/gateway-routes` 创建要求 `gatewayId` 扩展字段；POC-05 需要真实隔离 Nginx 节点。
- DS-06 已知限制：监控先持久状态再发通知，通知失败不会在下一轮重发；SSRF 校验不做 DNS 解析，需可信 Prober 复核解析地址；quota `RecordUsage` 是可信内部入口，禁止直接暴露给未认证适配器。
- certificates HTTP、gateway/publishing HTTP 未注册：契约语义与实现模型存在差异，需契约 owner 决策（见 `INTEGRATION.md`）。
- monitoring/quota HTTP 未注册：g2-merged 候选无相应路径。
- 已在隔离 Debian 13 VM 验证：Linux amd64 全量 Go 测试通过；Cloudflare Token 真实 TXT 创建/读取/删除并清理（`xihuani.com`）；ACME Let's Encrypt staging DNS-01 dry-run 成功并清理挑战记录；QQ Bot 取得 access_token；Nginx 1.26.3 运行/HTTP 200。
- 仍未验证：真实 mTLS Agent/WSS 端到端、Nginx 受控接管 POC-05、QQ 实际消息投递（缺 targetId）、SMTP、arm64、严格 1C1G 主机；publishing 只有注入端口。
- `go.mod/go.sum`、迁移编号、HTTP 契约仍待总控接收。
- `npm run check` 的 g2r03 契约检查 FAIL：候选 provenance 固定了 `internal/nodes/node.go` 的旧哈希，DS-02 的合法字段扩展使其变化；需总控重基线，不通过删除测试/改候选规避。
