# DS Wave 4 接线交接（deepseek-04）

- 任务：`V09-5D-DS` Wave 4（`DS-W4-01`—`DS-W4-03`）
- 会话：`deepseek-04`；分支 `feature/v09-services-ds02`；起始基线 `0f9a75e416a82a9589a29d4d9cf8a52bf0266066`
- 代码提交：`bfe3b93`（monitoring）、`238ef81`（files）、`fcc7390`（publishing）、`22fe183`（集成测试）
- 未修改 `apps/controller/**`、`apps/agent/**`、`apps/web/**`、公共契约/锁/根文件；未新增迁移（保持 19 项迁移基线，`tests/integration/v09/controller_wave2_test.go` 不受影响）。

## 1. 主控挂载：无需改签名

三个 `RegisterHTTP` 的签名均未变化，`apps/controller/server/server.go` 现有调用会**自动**获得新路由：

| 域 | 现有注册调用 | 新增路由 |
|---|---|---|
| monitoring | `monitoring.RegisterHTTP(mux, service, riskSource, authenticator)` | `GET .../availability-monitors/{monitorId}/history`、`POST .../availability-monitors/{monitorId}/probe` |
| files | `files.RegisterHTTP(mux, service, authenticator)` | `PUT /api/v1/nodes/{nodeId}/files/uploads/{uploadId}/content` |
| publishing | `publishing.RegisterHTTP(mux, service, authenticator)` | `POST .../publications/{publicationId}/run`、`POST .../publications/{publicationId}/rollback` |

monitoring/publishing 同时注册了候选契约/前端使用的无前缀别名（`/api/v1/availability-monitors/...`、`/api/v1/publications/...`）。

当前 `server.go` 组合层的行为（不接线也不会假成功）：

- `files.Options.Uploads` 未注入：字节 PUT 正常落 `stagingDir`，但 `complete` 返回 `503 unavailable`（`ErrTransportAbsent`），不会把本地暂存伪装成已送达节点。
- `monitoring.Prober` 未注入：history 正常；probe 返回 `503 prober_unavailable`。
- publishing 使用 `failClosedPublisher` 等三端口：run/rollback 返回 `502 publisher_unavailable`，阶段状态会如实持久为 `failed`/`partial_failure`。

## 2. 需要总控/后续会话补的端口（不作为本包成功条件）

1. **文件上传 transport**（CX）：实现 `files.UploadTransport`
   `PushFile(ctx context.Context, nodeID, destinationPath string, content io.Reader, size int64, sha256Hex string) error`，
   注入 `files.NewService(files.Options{... Uploads: adapter ...})`。该端口必须走受控 Agent 通道，不得新增 SSH/第二队列。
2. **监控 Prober**（生产）：实现 `monitoring.Prober`（HTTP/TCP/TLS），注入 `cfg.MonitoringProber`。probe 前由服务执行 `TargetPolicy`（SSRF/Allowlist）校验；Prober 仍应自行复核 DNS 解析后的地址。
3. **发布三阶段 provider**：使用新增的 `publishing.NewLocalBridge` 组合，替换 `server.go` 里的三个 failClosed 类型：

```go
bridge := publishing.NewLocalBridge(publishing.LocalBridgeConfig{
    DNSPublish:          dnsPublisherFunc,   // 由 dns 域/适配器提供
    DNSRollback:         dnsRollbackFunc,
    CertificatePublish:  certPublisherFunc,  // 由 certificates 域/适配器提供
    CertificateRollback: certRollbackFunc,
    GatewayPublish:      gatewayPublisherFunc,
    GatewayRollback:     gatewayRollbackFunc,
})
publishing.NewService(publishing.Options{
    /* ... */ DNS: bridge.DNS, Certificates: bridge.Certificates, Gateway: bridge.Gateway,
})
```

精确签名（`internal/publishing/bridge.go`）：

```go
type LocalBridgeConfig struct {
    DNSPublish          func(context.Context, DNSPublishRequest) (StageResult, error)
    DNSRollback         func(context.Context, string, StageResult) error
    CertificatePublish  func(context.Context, CertificateRequest) (StageResult, error)
    CertificateRollback func(context.Context, string, StageResult) error
    GatewayPublish      func(context.Context, GatewayRequest) (StageResult, error)
    GatewayRollback     func(context.Context, string, StageResult) error
}

type LocalBridge struct {
    DNS          DNSPublisher
    Certificates CertificatePublisher
    Gateway      GatewayPublisher
}

func NewLocalBridge(config LocalBridgeConfig) LocalBridge
```

`LocalBridge` 返回的三个端口永不为 nil；未配置的函数对应阶段失败关闭（`ErrPublisherUnavailable`），绝不视为成功。

## 3. 新增服务层导出（本包内）

```go
// internal/files/service.go、upload.go
func (s *Service) UploadLimit() int64
func (s *Service) WriteUploadContent(ctx context.Context, subjectID, uploadID string, offset int64, content io.Reader) (Upload, error)

// internal/publishing/service.go
func (s *Service) RunDetail(ctx context.Context, subjectID, requestID, planID string) (PlanDetail, error)
func (s *Service) RollbackDetail(ctx context.Context, subjectID, requestID, planID string, kind StageKind) (PlanDetail, error)
```

`RunDetail`/`RollbackDetail` 在执行后直接读取同一计划的阶段行，避免为了回显阶段结果额外要求 `publishing.read`（执行成功后不会因缺读权限返回 403）。授权失败时不返回计划数据。

## 4. 端点契约

### 4.1 监控历史（DS-W4-01）

`GET /api/v1/monitoring/availability-monitors/{monitorId}/history?limit=N`（别名 `/api/v1/availability-monitors/{monitorId}/history`）

- 权限：`monitoring.read`（服务层先校验，再按 monitor 存在性读取）。
- `limit`：1–500，缺省 50（`DefaultCheckLimit`）；非整数/越界 → `400 invalid_request`。
- 200：

```json
{"monitorId":"mon_...","limit":50,"items":[
  {"id":12,"monitorId":"mon_...","status":"up|down|unknown",
   "latencyMs":25,"statusCode":200,"error":"","checkedAt":"2026-10-31T12:00:00Z"}
]}
```

- 结果来自真实持久表 `monitor_checks`（迁移 `ds/0050_monitoring`；需求文档中的 `availability_results` 对应此表，未改名以免改动已应用迁移校验和），按 `checked_at DESC, id DESC` 排序。未知 monitor → `404`；未认证 `401`；拒绝 `403`。

### 4.2 按需探测（DS-W4-01）

`POST /api/v1/monitoring/availability-monitors/{monitorId}/probe`（同名无前缀别名）

- 权限：`monitoring.write`；审计动作 `monitoring.check`（拒绝也落审计）。
- 服务先执行 `TargetPolicy.Validate`（SSRF/Allowlist），再调用注入 Prober；结果写入 `monitor_checks` 并更新状态/连续失败计数/告警迁移。
- 200：`{"monitorId","status","state","latencyMs","statusCode","error","checkedAt","consecutiveFailures"}`。
- `503 prober_unavailable`（未注入 Prober）、`404`（monitor 不存在）、`400`（目标策略拒绝）、`403`。

### 4.3 上传字节写入（DS-W4-02）

`PUT /api/v1/nodes/{nodeId}/files/uploads/{uploadId}/content`

- Body：原始字节（不是 JSON）。起点由 `?offset=N` 或 `X-Upload-Offset: N` 指定，缺省 0，必须等于已记录 `bytesReceived`（续传）。
- 权限：`files.write` + 关联节点；同时校验 upload 属于当前主体与路径节点。
- 大小：单次受 `MaxUploadBytes`（默认 512 MiB）与声明的 `sizeBytes` 双重限制；超限 → `413 upload_too_large`，暂存文件截回写入前偏移，进度不变。
- 200：`{"uploadId","status":"awaiting_data","bytesReceived":N,"expectedSize":M}`。
- `409 conflict`（offset 不匹配/已关闭/主体不符）、`404`（upload 不存在或节点不匹配）、`400`（offset 非法/空 body）、`401/403`。
- 同一 upload 的内容写入、`complete`、`abort` 由进程内锁串行化，避免完成校验与写入竞争。

### 4.4 完成校验（DS-W4-02）

`POST /api/v1/nodes/{nodeId}/files/uploads/{uploadId}/complete`

- 校验暂存文件真实字节数 = 声明大小；计算 SHA-256 与声明摘要比对。
- 一致：通过注入 `UploadTransport` 推送节点 → 状态 `completed`，审计 `files.upload_complete` success。
- 摘要不一致：删除暂存文件，状态 `failed`，审计 failure，`422 checksum_mismatch`。
- 未上传字节（声明大小 > 已收字节）：`400 invalid_request`，不推进状态。
- 未注入 transport：`503 unavailable`；transport 失败保留 `open` 便于安全重试，审计 failure。
- 200 响应沿用 `{"uploadId","status","files":[{name,path,sizeBytes,version}]}`，`version` 为实际 SHA-256。

### 4.5 发布执行与补偿（DS-W4-03）

`POST /api/v1/publishing/publications/{publicationId}/run`（别名 `/api/v1/publications/{publicationId}/run`）

- 权限：`publishing.write`；固定 DNS → 证书 → 网关顺序，逐阶段持久化；成功阶段不重跑；失败停在 `partial_failure`。
- 200：`{"id","status":"queued|running|succeeded|partially_completed|failed","createdAt","stages":[{"name","status","message"}]}`。
- 阶段失败：`502` + 同一 operation 结构 + `error:{code:"stage_failed"|"publisher_unavailable"|"invalid_stage_result",message}`；此时 `stages` 为真实持久状态（已成功/失败阶段），可据此重试。
- `409`：`plan_completed`/`plan_canceled`/`revision_conflict`/`stage_already_running`；`404`：计划不存在；`403` 拒绝时不回显计划数据。
- Provider 原始错误不进入响应/审计/日志（仅稳定错误码与脱敏摘要）。

`POST /api/v1/publishing/publications/{publicationId}/rollback`（别名同上）

- Body：`{"stage":"dns|certificate|gateway"}`；仅对已执行（succeeded/failed）阶段补偿。
- 200：同一 operation 结构；全部已执行阶段回滚完成 → 计划 `canceled`，部分完成 → `partial_failure`，阶段状态 `rolled_back`。
- `400 stage_not_rollable`（未执行）、`400 invalid_request`（缺 stage/非法 stage）、`502 rollback_failed`（同 operation + error）。
- 不把 DNS 缓存/上游 TTL 表示为即时回滚；阶段摘要不改写。

## 5. 权限与审计

- 未新增权限键：沿用 `monitoring.read/write`、`files.read/write/transfer`、`publishing.read/write/plan_create`。
- run/rollback 响应不再要求额外 `publishing.read`（`RunDetail`/`RollbackDetail`）。
- 新审计动作：无新增动作名；probe 使用既有 `monitoring.check`，上传完成使用 `files.upload_complete`，发布使用既有 `publishing.stage/run/rollback`。

## 6. 语义与已知边界

1. **monitor 无持久节点关联**：候选 `Monitor08.sourceNodeId` 未落库（现有 `monitors` 表无该列；新增列需迁移，会改变 Wave 2 集成测试固定的 19 项迁移基线）。因此 history/probe 强制 `monitoring.read`/`monitoring.write`，但无法按关联节点过滤可见性；HTTP 层继续忽略 `sourceNodeId`（与 Wave 3 行为一致）。若要实现按节点可见性，需契约/迁移决策后另开任务。
2. **上传是 Controller 暂存闭环**：本地字节/SHA 校验闭环已真实执行；"已送达节点"仍取决于 `UploadTransport` 接线，未接线时失败关闭。
3. **发布 provider 未生产实现**：`LocalBridge` 只提供组合接缝；未接真实 DNS/证书/网关适配器时 run 保持失败关闭，不得宣称 AC-15 全通过。
4. **审计拒绝路径**：读类 history 拒绝沿用服务层既有策略（无 requestId 不写拒绝审计）；probe/run/rollback 拒绝均落审计。
5. 未新增 `GET /uploads/{uploadId}` 状态端点；当前进度由 content 响应回传。

## 7. 本分支验证

- `go test ./...` → 全部 PASS（含 architecture、contracts、integration、POC）。
- `go test ./internal/monitoring ./internal/files ./internal/publishing ./tests/integration/v09/services -count=1` → PASS。
- `TestWave4ServiceHTTPEndToEnd`（真实文件 SQLite WAL + HTTP）：监控 probe/history、文件分块上传/SHA 完成、发布三阶段执行/失败重试/回滚、审计落盘。
- 外部环境（真实 Agent 文件通道、真实 HTTP/TCP/TLS 目标、Cloudflare/ACME/Nginx、QQ/SMTP）仍 UNTESTED/BLOCKED，见 `AC-EVIDENCE-WAVE4.md`。
