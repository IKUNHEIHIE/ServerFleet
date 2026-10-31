# DS 进度 · deepseek-02

分支 `feature/v09-services-ds02`；基线 `8a7dd470`。每个检查点记录实际命令与结果；未验证项明确标 UNTESTED/BLOCKED。

## 检查点 C1 · DS-01 存储/迁移接缝

- SHA：`629a640d0e446f1dd791aa9fda73ca229823740d`（`feat(storage): add SQLite WAL, immediate transactions and migration seam`）
- 依赖：`modernc.org/sqlite v1.61.0`（纯 Go、CGO=0，无需 gcc）；直接依赖 1 个 + 9 个 indirect。go.mod/go.sum 属总控单写文件，精确差异见 `SHARED-CHANGES.md`，需总控接收后再同步主线。
- 实现：`internal/storage/{storage,tx,migrate,generation,backup,errors}.go`、`internal/storage/testkit/db.go`。只有 storage 导入 `database/sql`；领域包使用 `storage.Queryer/Tx/Row/Result`。
- 语义：
  - `Open` 强制 WAL、busy_timeout、foreign_keys=1、synchronous=NORMAL、启动 `PRAGMA quick_check`；只读模式禁止写。
  - `InTx` 进程内单写者排队（ctx 可取消）、`BEGIN IMMEDIATE`；fn 返回错误/ctx 取消/panic 一律回滚；回滚后不可能报告成功。
  - 迁移按命名空间版本全序应用，checksum 漂移=拒绝，库中存在未注册版本=拒绝（防降级）；每个迁移单事务。
  - `Generations` 提供持久单调计数与 CAS（expected 0 仅首次创建），给 CX 作凭据/会话世代接缝。
  - `BackupTo`=VACUUM INTO+只读校验；`RestoreFile` 拒绝覆盖现有文件、先校验再原子改名。
- 实际命令/输出：
  - `./.tools/go/bin/go test ./internal/storage/...` → `ok ... 3.182s`
  - `./.tools/go/bin/go test ./...` → 全部 ok（含 architecture/contracts）
  - `go mod tidy` 已运行，go.mod 直接 require modernc.org/sqlite。
- 测试覆盖（真实文件库，非 :memory:）：重启保持+迁移跳过、坏库拒绝、错误回滚、取消回滚、panic 回滚、24 并发写者全部成功、checksum 漂移拒绝、未知版本拒绝、备份/二次备份拒绝/恢复、只读拒绝写、generation 并发 CAS 仅 1 成功、迁移版本校验。
- 未覆盖/风险：坏库仅覆盖“非 SQLite 文件”与启动 quick_check；页级损坏需真实损坏样本（UNTESTED）。1C1G 与 100 节点吞吐留给 POC-07（DS-09）。跨进程并发写只依赖 busy_timeout，未做多进程压力（UNTESTED）。
- 阻塞：无。`go.mod/go.sum` 需总控接收；迁移版本命名（`ds/....`）需总控批准，未批准前仅本包使用。

## 检查点 C2 · DS-02 节点/分组/服务组

- SHA：`b858e0d`。迁移 `ds/0020_nodes`、`ds/0021_servicegroups`。
- 实现：节点库存（JSON tags、原始指标+采样来源）、普通分组 revision CAS、服务组独立资源/成员/暂停调度/派发预览；所有写经注入 Authorizer 并在同事务写审计，纯拒绝落审计；审计失败回滚；读先 `PrepareNodeReadScope` 再读库存/过滤/计数，空库不绕过。
- 测试：`go test ./internal/nodes/ ./internal/servicegroups/` → ok。覆盖重启保持、seq 幂等/陈旧拒绝、revoked 终态、离线不补零、分组 revision 冲突、未知成员无部分成功、拒绝审计、审计失败回滚、可见性过滤、派发准入（online+healthy+!paused+gateway）、删除守卫。
- 未覆盖：真实 CX Agent 事件来源（用受控 `ApplyAgentUpdate` 入口，运输层未接）；服务组 gatewayIds/healthyNodeIds 富化由 composition 提供。

## 检查点 C3 · DS-04 审计持久化

- SHA：`b7fc5a2`。迁移 `ds/0010_audit`。
- 实现：append-only 表 + UPDATE 触发器阻止篡改；`AppendTx` 与业务同事务；TaskID/NodeIDs/Source/ReasonCode 结构化字段；保留期 `PurgeBefore` 需 ≥1h 陈旧、先写保留事件；分页检索按 subject/action/result/request/task/source。
- 测试：`go test ./internal/audit/` → ok。覆盖重启、过滤/分页、回滚、触发器、20 并发写、保留期、非法事件零写入。

## 检查点 C4 · DS-03 统一任务调度

- SHA：`619eb24`；CX 适配 `e6e2aa8`。迁移 `ds/0030_tasks`，新增 `ds/0031_tasks_cx_fields`（0030 未改写）。
- 实现：持久提交/目标快照/幂等键、审批 revision+计划摘要绑定、1→5 金丝雀/默认 5 分批、离线 15m TTL、批次熔断、取消、SSE 游标、去敏日志、崩溃恢复（在途→unknown 不重跑）、每次派发复检授权/审批；仅经注入 `Dispatcher` 端口。
- CX 适配：`CanonicalParams`、`Source/PluginID`、`ScopeRevision`、外部 `ApprovalConsumer`（同一 `storage.Tx` 原子消费+审计）、`ReadGate` 逐次读取/每轮 SSE 复检。
- 测试：`go test ./internal/tasks/` → ok。覆盖幂等、金丝雀、审批失效/过期、撤权、取消、unknown 不重试、TTL、批次阻塞、崩溃恢复、并发 RunOnce 单次派发、重复结果、日志去敏、外部审批回滚。
- 未覆盖：AI scopeRevision 的执行期复检需 CX 授权范围端口；真实 transport/Agent 端到端未接。

## 检查点 C5 · DS-05 文件

- SHA：`628d280`。迁移 `ds/0040_files`。
- 实现：POSIX 远端路径校验（穿越/重复分隔符/控制字符/反斜杠/尾斜杠/符号链接拒绝）、源读+目标写+transfer 分别授权、dry run 零写入、幂等键、任务失败/未提交恢复、完成态与项目结果一致性降级、断点上传（精确 offset、分块/总量限制、SHA-256、传输失败可重试）、下载/上传仅经注入 transport 端口。
- 测试：`go test ./internal/files/` → ok。覆盖路径矩阵、双端拒绝审计、dry run、提交失败、取消、恢复、校验不符、重试、下载拒绝、HTTP 幂等/上传会话。

## 检查点 C6 · DS-07 DNS/证书

- SHA：`276d9d0`。迁移 `ds/0060_dns`、`ds/0061_certificates`。
- 实现：Cloudflare 账号仅保存 secret-store `credential_ref`+状态，Provider 端口注入；Zone/记录 CRUD/冲突/单默认原子切换；内部 PKI 与 ACME 分表，私钥仅 `KeyRef`、PEM 输入拒绝；分发记录绑定服务组 revision 并经统一任务提交；续签/轮换/吊销走 Issuer 端口；私钥不返回/不入审计/不入 JSON。
- 测试：`go test ./internal/dns/ ./internal/certificates/` → ok；架构边界通过。覆盖重启、拒绝审计、审计失败回滚、空结果 fail-closed、revision 冲突、密钥材料扫描。

## 检查点 C7 · DS-09 模块 HTTP/备份/POC

- SHA：`f132735`；`e6e2aa8` 增加任务读闸门。无新增迁移。
- 实现：nodes/tasks/audit/files/servicegroups 的 stdlib HTTP 注册，可信 `Principal` 注入、固定错误映射、严格 JSON（拒未知字段）、分页/游标、任务 SSE（Last-Event-ID + 每轮复检）、审计只读 API（无修改/保留路由）；跨五域快照备份/恢复/损坏拒绝；POC-07 10/30/100 代理吞吐采集。
- 实测（Windows dev，非 1C1G）：10 代理 9370.9 writes/s；30 代理 5680.9；100 代理 5298.8；DB 980KB、WAL checkpoint 后 0KB、heap ~2.6MB、SSE 3 事件 0ms。报告写入忽略目录 `reports/_review/v09-services/poc07-report.json`。`go test ./tests/integration/v09/services/ ./tests/poc/v09/services/` → ok。
- 未覆盖：1C1G/Linux 验收（BLOCKED）；DNS/证书/监控/通知/配额/网关/发布的 HTTP 目前只有服务层端口（DS-06/DS-08 完成后补或列缺口）；Controller 入口接线属 CX。

## 当前未完成/阻塞

- DS-06 监控/通知/配额、DS-08 网关/发布：子代理实现中。
- 所有真实外部环境（Linux 双架构、Cloudflare/ACME、QQ/SMTP、Nginx）UNTESTED/BLOCKED；不得以本地假端口计为验收。
- `go.mod/go.sum`、迁移命名、HTTP 契约仍未获总控接收；按候选处理。

## 检查点 C8 · DS-06 监控/通知/配额

- SHA：`247862d`（通知域）+ `a3eacba`（监控/配额）。迁移 `ds/0050_monitoring`、`ds/0051_notifications`、`ds/0052_quota`。
- 实现：HTTP/TCP/TLS 监测、SSRF 目标策略与逃逸形式拒绝、维护窗、上下线迁移告警、注入 Prober（nil fail-closed）；SMTP/QQ 仅存 secretRef、去重窗口、QQ→SMTP 备用一次、显式重试上限/退避、未知结果不重试；流量周期/权威修正、缺失≠0、估算门控、提醒去重、超额仅告警不断网。通知 HTTP 注册（读取需注入 gate，响应不含 secretRef）。
- 测试：`go test ./internal/monitoring/ ./internal/notifications/ ./internal/quota/` → ok；staticcheck 通过。
- 未覆盖/风险：监控先持久状态后发通知，告警投递失败不会下轮自动重发；SSRF 不做 DNS 解析，需真实 Prober 复核；真实 SMTP/QQ/网络目标 UNTESTED。

## 检查点 C9 · DS-08 网关/发布

- SHA：`ab4510a`。迁移 `ds/0070_gateway`、`ds/0080_publishing`。
- 实现：网关与节点绑定、准入由实时 online+ready+installed+checked+enabled 派生；接管现有站点必须显式批准；provision 只提交一个统一任务并回写分步结果；路由域名唯一、upstream/serviceGroup 二选一、启用需 Checker 通过、记录服务组 revision 快照；发布固定 DNS→证书→网关顺序、逐阶段持久、失败停止、重试跳过已成功阶段、显式补偿且不宣称 DNS 缓存即时回滚。
- HTTP：网关列表/详情/启用/provision/provision 查询/路由 CRUD；`/checks` 与 publishing HTTP 未注册（避免推断/缺契约）。
- 测试：`go test ./internal/gateway/ ./internal/publishing/` → ok；staticcheck 通过。真实 Nginx、节点、POC-05 未执行。

## 最终验证（2026-10-30）

- `npm run check`：**非全绿**。policy/go/web/git 通过（policy 在最终 docs HEAD 复跑 PASS；Go 全包测试+vet+staticcheck；Web 测试+build）。contracts 仅 `g2r03-explicit-contracts` FAIL：其 provenance 固化哈希期望 `internal/nodes/node.go`=c61c768a…，而 DS-02 合法新增 CountryCode/CreatedAt/UpdatedAt/eventSeq 后为 b9f42e1c…；`visibility.go`、`auth/node_policy.go` 未变。未改候选/测试，需总控重基线该候选来源哈希。其余结构/lint/roundtrip/negative/g2r02 通过。
- `npm run check:security`：PASS（npm audit 0，govulncheck 无漏洞）。
- `node scripts/check.mjs policy`：PASS（最终 HEAD 提交标题合法；架构边界通过）。
- `go test ./...`：PASS（含 architecture、contracts、integration、POC）。
- `go mod tidy -diff` / `go mod verify`：PASS。
- 跨 13 个迁移的备份/恢复/损坏拒绝：`go test ./tests/integration/v09/services/` PASS。
- POC-07 本机：10/30/100 代理 9370.9/5680.9/5298.8 writes/s，DB ≤980KB，checkpoint 后 WAL 0，SSE 3 事件 <1ms；环境 Windows，不代表 1C1G/Linux 验收。
- 代码最终 HEAD：`9f33728`（网关/发布读拒绝审计补充）；本进度文档随后形成最终 docs HEAD。最终检查时工作区 clean。

## 未完成清单（不得当作完整 v0.9 已验收）

1. 真实 Linux Debian/Ubuntu amd64/arm64、1 核 1GB 压测、mTLS Agent/WSS 端到端：BLOCKED（无授权隔离环境）。
2. Cloudflare 多账号、ACME/内部 CA、QQ/SMTP、Nginx 真实供应商/节点、POC-05：UNTESTED/BLOCKED（仅注入端口与 fake 测试）。
3. certificates HTTP 未注册：候选“节点本地 CSR/信任包”与 KeyRef 模型冲突，需契约决策。
4. publishing HTTP 未注册：g2-merged 候选无对应路径；服务层端口已备。
5. gateway `/checks` 未注册：无独立 serviceRunning 观测，不用推断值冒充。
6. monitoring/quota HTTP 未注册：候选无路径；服务层已备。
7. CX 仍需实现 tasks.ReadGate、audit ReadGate、notifications ReadGate、ApprovalConsumer、Dispatcher、NodeLookup、GroupResolver/ClassResolver 等组合端口；Controller 入口未接线。
8. `go.mod/go.sum`、迁移编号、HTTP/SSE 契约仍未获总控接收/冻结。
9. 监控告警失败不自动重发；SSRF 需 Prober 复核 DNS 解析地址；quota 可信采集入口不得直接暴露。

## 真实环境验证（隔离 VM，用户授权）

- 环境：Debian GNU/Linux 13 (trixie) x86_64；2 vCPU；464MB RAM（低于 1C1G）+ 4GB swap；20GB 盘；root 仅用于隔离测试；出口香港机房（非代理）。
- Linux amd64：`go test -count=1 ./...` EXIT=0（含 architecture/contracts/integration/POC）。
- POC-07（`taskset -c 0` + `GOMAXPROCS=1`）：10/30/100 代理 8535.3/7615.4/7487.6 writes/s；DB ≤1016KB；checkpoint 后 WAL 0；heap ≤2.5MB；SSE 3 事件 <1ms。
- Cloudflare：Token verify active；`xihuani.com` Zone 可读；用 `tests/poc/v09/services/cloudflare` 真实创建 TXT `sf-it-*`、读回、删除，清理核验 remaining=0。
- ACME：`certbot --dns-cloudflare --dry-run`（Let's Encrypt staging，DNS-01，`sf-stage.xihuani.com`）成功；挑战 TXT 清理 remaining=0。
- QQ：`getAppAccessToken` HTTP 200，取得 access_token（expires_in=7200）；未提供 targetId，未发送消息。
- Nginx：1.26.3 安装并 active，`http://127.0.0.1/` 200；端口 80 已被 nginx 监听，第二个绑定得到 `EADDRINUSE`；POC-05 受控接管仍缺 Agent/Controller 接线，未验收。
- arm64：`GOOS=linux GOARCH=arm64` 交叉构建 controller/agent 成功，ELF machine=183（AArch64）；未在 arm64 真机运行。
- 真实监控：本地回环 HTTP 探针 up→down→up，持久状态与两条迁移告警正确；Windows/Linux 均 PASS（`tests/poc/v09/services/monitoring`）。
- 凭据仅存于本机/VM 临时 0600 文件，未入仓库/Git/日志；测试后应轮换 VM 密码、Cloudflare Token、QQ AppSecret。

## 检查点 C10 · V09-5D-DS Wave 3（业务域 HTTP 补齐与 SQLite WAL 端到端）

- 分支 `feature/v09-services-ds02`；起始基线 `98f39e04e73ee3150b9f6146d92102c59e5358c3`（Wave 1/2 已合入 main）。
- 代码检查点（按域）：
  - `abcfb30` `feat(monitoring): add availability monitor and traffic risk http handlers`
  - `3ce4073` `feat(quota): add node traffic quota http handlers and risk listing`
  - `d9f5e1e` `feat(certificates): add internal distribution http handlers and listing`
  - `c6715f7` `feat(publishing): add preview and publication http handlers`
  - `83a86de` `feat(gateway): add checks and dispatch http operations`
  - `0dcd9f1` `feat(nodes): add lifecycle identity capabilities and diagnostics http`
  - `bca1a70` `feat(tasks): add resume http operation and revision in job dto`
  - `a7e0852` `test(services): cover wave3 http endpoints with sqlite wal`
- 实现范围：
  - `internal/monitoring/http.go`：`GET/POST /api/v1/monitoring/availability-monitors`、`PATCH .../{monitorId}`（`state`/`paused` 切换）、`GET /api/v1/monitoring/traffic-risks`，`AuditActions()`；同时注册候选契约/前端的无前缀别名。
  - `internal/quota/http.go`：`GET/PUT /api/v1/nodes/{nodeId}/traffic-quota`，`AuditActions()`；`quota.Service.ListTrafficRisks`/`LatestUsage` 供 traffic-risks 与配额视图使用。
  - `internal/certificates/http.go`：`GET/POST /api/v1/certificates/internal/distributions`、`POST .../{distributionId}/revoke`，`AuditActions()`；`ListDetails` 读模型；响应/审计不含 keyRef 或私钥材料。
  - `internal/publishing/http.go`：`POST /api/v1/publishing/preview`、`GET/POST /api/v1/publishing/publications`、`GET .../{publicationId}` 及无前缀别名；服务层新增 `Preview`/`ListDetails`。
  - `internal/gateway/http.go`：`GET /api/v1/gateways/{gatewayId}/checks`（只返回聚合标志，`recorded:false` 表示无记录）、`PATCH .../dispatch`（`{state}` 与 `{enabled, expectedRevision}`）。
  - `internal/nodes/http.go`：`POST .../suspend|revoke`、`GET .../identity|capabilities|diagnostics`；suspended 不被 agent 心跳覆盖，revoke 终态；identity/capabilities 明确 `recorded:false`，不伪造。
  - `internal/tasks/http.go`：`POST /api/v1/tasks/{jobId}/resume`（仅 queued 未开始任务，重新鉴权+审批有效后延长期限并 revision+1）；GET/创建响应补充 `revision`/`planDigest`。
- 实际验证（代码 HEAD `a7e0852`，固定 Go 1.27.3 / Node 24.22.1）：
  - `go test ./internal/... ./tests/...` → 全部 PASS；新增 `tests/integration/v09/services/wave3_http_test.go` 在真实文件 SQLite WAL 上覆盖 7 个域、越权（401/403）、并发配额写、证书 issuer 失败回滚、审计落盘。
  - `npm run check` → `reports/check-all.json`：`policy` PASS、`contracts` PASS、`go` PASS、`web` PASS、`git` PASS（34 测试文件/283 项 web 单测 PASS，Vite build PASS）。
  - `npm run check:security` → `npm audit` 0 漏洞；`govulncheck ./...` 无漏洞。
- 边界与剩余风险：
  - 未修改 `apps/controller/**`、`apps/web/**`、公共配置/锁/契约；未新增迁移（保持 Wave 2 集成测试固定的 19 项迁移）。总控接线清单见 [HANDOFF-WAVE3.md](HANDOFF-WAVE3.md)。
  - Wave 2 未构造 monitoring/quota/certificates/publishing 服务；总控需按交接文档构造并注入端口，否则相关路由不可达。
  - certificates 的 Issuer/TaskSubmitter、publishing 的三个 publisher、monitoring 真实 Prober 与真实 notifications 适配均无生产实现；未接线前保持失败关闭，真实网络/CA/DNS/Nginx/Agent 验收 UNTESTED。
  - `tasks.resume` 只处理 queued 未开始任务（后端无 paused 状态）；`GET /api/v1/nodes/{nodeId}` 详情不在 Wave 3 DS 清单内，仍未提供。
  - Node 无持久 revision，suspend/revoke 拒绝非 0 `expectedRevision`；revoke 不负责关闭活动 WSS 或吊销节点证书（属 transport/CX）。

### 最终校验（文档检查点 `fe6f77c`）

- `npm run check`（2026-10-31T09:58:45Z）→ `policy` PASS、`contracts` PASS、`go` PASS、`web` PASS、`git` PASS；`reports/go-tests.log` 中 `tests/integration/v09/services` 为 PASS，新增 `TestWave3ServiceHTTPEndToEnd` 已执行。
- `npm run check:security` → `npm audit` 0 漏洞，`govulncheck ./...` 无漏洞。
- 本地提交未推送、未合并；收件与后续接线由总控 `agy-review-01` 按 [HANDOFF-WAVE3.md](HANDOFF-WAVE3.md) 处理。

## 检查点 C11 · V09-5D-DS Wave 4（deepseek-04：探测历史/按需探测、上传字节闭环、发布执行与补偿）

- 分支 `feature/v09-services-ds02`；起始基线 `0f9a75e416a82a9589a29d4d9cf8a52bf0266066`（Wave 1/2/3 已合入 main）。
- 会话：`deepseek-04`；代码提交：`bfe3b93`（monitoring）、`238ef81`（files）、`fcc7390`（publishing）、`22fe183`（集成测试）；交接 [HANDOFF-WAVE4.md](HANDOFF-WAVE4.md)、证据 [AC-EVIDENCE-WAVE4.md](AC-EVIDENCE-WAVE4.md)。
- **DS-W4-01 监控探测历史与按需探测**：
  - `GET /api/v1/monitoring/availability-monitors/{monitorId}/history`（+ 无前缀别名）：查询真实持久表 `monitor_checks`（`ds/0050_monitoring`），`limit` 1–500 缺省 50，按 `checked_at DESC, id DESC`；校验 `monitoring.read` 与 monitor 存在性，不再返回 501。
  - `POST .../{monitorId}/probe`：`monitoring.write` + SSRF/Allowlist 校验后调用注入 `Prober`，持久化样本/状态/连续失败/迁移告警，审计 `monitoring.check`；未注入 Prober 返回 `503 prober_unavailable`。
- **DS-W4-02 文件分阶段上传字节闭环**：
  - 新增 `PUT /api/v1/nodes/{nodeId}/files/uploads/{uploadId}/content`：原始字节按 `?offset=`/`X-Upload-Offset` 续写 `stagingDir`，受声明大小与 `MaxUploadBytes` 双重限制，超限 `413` 并截回、进度不变；同名 upload 的内容写入/完成/中止进程内串行化。
  - `POST .../complete` 端到端校验暂存文件真实字节数与 SHA-256：一致→推送注入 transport→`completed`+审计；不一致→删除暂存、`failed`、`422 checksum_mismatch`+审计；未上传字节→`400`；transport 失败保留 `open` 可重试。
- **DS-W4-03 发布适配与 HTTP 集成**：
  - `internal/publishing/bridge.go` 新增可组合 `LocalBridge`/`NewLocalBridge`（DNS/证书/网关三端口由组合层函数注入，nil 失败关闭，绝不假成功）。
  - 新增 `POST /api/v1/publishing/publications/{id}/run` 与 `.../rollback`（+ `/api/v1/publications/...` 别名），响应携带真实持久阶段状态；失败返回对应状态码 + 稳定 `error.code`（不泄露 provider 原文）；新增 `RunDetail`/`RollbackDetail` 避免执行后额外要求读权限，拒绝时不回显计划数据。
  - `tests/integration/v09/services/wave4_http_test.go`：真实文件 SQLite WAL + HTTP 覆盖监控探测/历史、文件字节上传/SHA 完成/负向、发布三阶段执行/失败重试/逐阶段回滚/审计；分块续传由 `TestHTTPUploadContentAndCompleteChecksum` 覆盖。Wave 3 集成环境扩展文件服务与可观测 publisher。
- 验证（本机，固定 Go 1.27.3 / Node 24.22.1）：
  - `go test ./...` → 全部 PASS（含 architecture/contracts/integration/POC）；`go test ./internal/monitoring ./internal/files ./internal/publishing ./tests/integration/v09/services -count=1` → PASS。
  - `TestWave4ServiceHTTPEndToEnd` PASS（4 子测试：monitoring probe history / file upload bytes and checksum / publication execution and rollback / audit）。
- 边界与剩余风险：
  - 未改 `apps/controller/**`、`apps/web/**`、公共契约/锁/根文件；**未新增迁移**（保持 19 项基线）。monitor 的 `sourceNodeId` 未持久化，history/probe 无法按关联节点过滤可见性（需契约/迁移决策）。
  - 文件 `UploadTransport`、监控 `Prober`、发布 publisher 仍由组合层注入；`server.go` 当前未注入 Uploads、Prober/publisher 为 fail-closed，故真实进程下 content 可暂存但 complete/run 诚实返回 503/502。
  - 真实 Agent 字节通道、真实 HTTP/TCP/TLS 公网目标、Cloudflare/ACME/Nginx、QQ/SMTP 投递仍未实测：UNTESTED/BLOCKED，详见 AC-EVIDENCE-WAVE4。

## 检查点 C12 · V09-5D-DS Wave 5（deepseek-05：真实 NetProber 与本地暂存传输）

- 分支 `feature/v09-services-ds02`；起始基线 `3adf891f3155b6452f06f25df53fd20f553742ae`（Wave 4 已合入 main）。
- 会话：`deepseek-05`；代码提交：`d475c8a`（monitoring）、`f2f7b9e`（files）、`506c977`（集成测试）、`25939d8`（失败归类防御）；交接 [HANDOFF-WAVE5.md](HANDOFF-WAVE5.md)、证据 [AC-EVIDENCE-WAVE4.md](AC-EVIDENCE-WAVE4.md) 第 4 节。
- **DS-W5-01 真实标准库 NetProber**（`internal/monitoring/prober.go`）：
  - `NewNetProber(NetProberConfig{AllowedHosts, Resolver, RootCAs})` 实现 `monitoring.Prober`；HTTP/HTTPS 走显式禁用代理的 `http.Transport`，TCP 完成握手，TLS 完成经证书链校验的握手（最低 TLS 1.2）。
  - 防 DNS Rebinding：自定义 `DialContext` 对每个底层连接先解析、对解析出的每个 IP 重新执行 SSRF 地址校验，再按字面 IP 连接；任一地址被拒绝即失败关闭。元数据地址/主机名始终拒绝；回环/私网等仅由显式白名单放行（字面 IP 需 IP/CIDR 条目，主机名需精确主机名条目）；重定向每一跳复用同一校验。
  - 脱敏：返回 `ProbeResult`（Healthy/Latency/StatusCode/Reason）；失败为 `*ProbeError`，仅含稳定码（`invalid_target`/`target_forbidden`/`dns_failure`/`dial_failed`/`tls_failed`/`request_failed`/`timeout`）与固定摘要，不读取/回显响应体与响应头。HTTP 2xx/3xx 为 up，其余为 down（`http_status_<code>`）。
  - 单测：真实 httptest HTTP/TCP/TLS 正反向、状态码不健康、超时、调用方取消、DNS 失败、metadata/localhost/userinfo/非法目标拒绝、伪造解析器返回回环与公私混合地址被拦截、回环仅在显式主机名/IP 白名单下放行。
- **DS-W5-02 受控本地目录 LocalStagingTransport**（`internal/files/local_transport.go`）：
  - `NewLocalStagingTransport(rootDir)` 同时实现 `files.UploadTransport`/`DownloadTransport`；布局 `<rootDir>/<nodeID>/<remotePath>`。
  - `PushFile` 校验 nodeID/远端路径/SHA-256 格式，逐段创建目录（`Lstat` 拒绝符号链接）、临时文件 + `Sync` + 原子 `Rename`（0600/0700），核对写入字节数与摘要，失败清理且目标不变；`PullFile` 逐段拒绝符号链接、仅普通文件流式读回并支持 ctx 取消；Windows 保留名与 `:` 路径段拒绝。
  - 单测：推/拉/覆盖往返；nodeID 与路径逃逸矩阵；字节数/摘要失败无残留；符号链接目录与目标文件拒绝且外部文件不被改写；缺失/目录/符号链接 pull 拒绝；取消；坏 root；Windows 保留名。
- **DS-W5-02 端到端集成**（`tests/integration/v09/services/wave4_http_test.go`，扩展 `wave3_http_test.go` 的 `newWaveEnvWith` 注入点）：
  - 真实 `NewNetProber` + 本地 `httptest.Server`（`AllowedHosts=127.0.0.1`）：HTTP 创建 monitor → `POST /probe` 返回 200/up/latency/statusCode → `GET /history` 读回持久样本，响应不含目标响应体；`/missing` 目标返回 404 → down + `http_status_404` 且不回显响应体。
  - 真实 `NewLocalStagingTransport`（`t.TempDir()`）：HTTP 创建上传 → `PUT .../content` → `POST .../complete` 返回 `completed` 与真实 SHA-256 → 磁盘文件字节一致 → `files.Service.Download` 读回一致；未落盘文件下载失败。
- 验证：
  - `go test ./internal/monitoring/... ./internal/files/... ./tests/integration/v09/services/... -count=1` → PASS（`ok monitoring 0.613s`、`ok files 0.402s`、`ok services 0.669s`）。
  - `TestWave5RealProberAndLocalStagingTransport` → PASS（2 子测试）。
  - `npm run check` → PASS（policy/contracts/go/web/git；`TestWave5RealProberAndLocalStagingTransport` 在 `reports/go-tests.log` 中执行）；`npm run check:security` → PASS（npm audit 0，govulncheck 无漏洞）。
- 边界与剩余风险：
  - 未改 `apps/controller/**`（接线由总控按 HANDOFF-WAVE5 执行）、`apps/web/**`、公共契约/锁/根文件；**未新增迁移**（19 项基线）。
  - 本分支真实网络目标仅 `127.0.0.1` 的 httptest；公网目标/IPv6/证书异常主机、真实 DNS TTL 切换未实测：UNTESTED。
  - HTTP 健康口径固定 2xx/3xx；本地 staging 无配额/清理；生产 Agent 上行/下行通道仍属 CX，未接线时 downloads 保持失败关闭。
