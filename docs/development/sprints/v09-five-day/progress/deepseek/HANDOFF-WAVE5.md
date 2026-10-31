# DS Wave 5 接线交接（deepseek-05）

- 任务：`V09-5D-DS` Wave 5（`DS-W5-01`—`DS-W5-03`）
- 会话：`deepseek-05`；分支 `feature/v09-services-ds02`；起始基线 `3adf891f3155b6452f06f25df53fd20f553742ae`
- 代码提交：`d475c8a`（monitoring NetProber）、`f2f7b9e`（files LocalStagingTransport）、`506c977`（真实实现 HTTP 集成测试）、`25939d8`（探测失败归类防御）
- 未修改 `apps/controller/**`、`apps/agent/**`、`apps/web/**`、公共契约/Proto/生成代码/根锁文件；未新增迁移（保持 19 项基线）。`RegisterHTTP` 签名未变，无新路由。

## 1. 新增实现与构造签名

### 1.1 `monitoring.NewNetProber`（DS-W5-01）

`internal/monitoring/prober.go`：

```go
type HostResolver interface {
    LookupIPAddr(context.Context, string) ([]net.IPAddr, error) // *net.Resolver satisfies this
}

type NetProberConfig struct {
    AllowedHosts []string        // 与 monitoring.Options.AllowedHosts 同一份显式白名单
    Resolver     HostResolver    // 可选；nil 使用 net.DefaultResolver
    RootCAs      *x509.CertPool  // 可选；nil 使用系统信任库（内部 CA 监控时才需要）
}

func NewNetProber(cfg NetProberConfig) (*NetProber, error)
```

`*NetProber` 实现 `monitoring.Prober`（`HTTP`/`TCP`/`TLS`）：

- **真实探测**：HTTP/HTTPS 使用 `http.Transport`（显式 `Proxy: nil`，禁用连接复用）；TCP 完成三次握手；TLS 完成证书链校验的握手（最低 TLS 1.2，`ServerName` 为原始主机名）。
- **健康口径**：HTTP 仅 `2xx`/`3xx` 记为 up；`4xx`/`5xx` 记为 down，`Reason` 固定为 `http_status_<code>`，不读取响应体、不保留响应头。
- **超时**：方法内用 `context.WithTimeout(ctx, spec.Timeout)`，同时设置 `TLSHandshakeTimeout`/`ResponseHeaderTimeout`；调用方取消原样返回 `context.Canceled`，探测自身超时归类为 `timeout`。
- **防 DNS Rebinding 二次 SSRF**：每个底层 TCP 连接都由 `DialContext` 先解析主机（HTTP 重定向的每一跳同样经过），在连接前对**解析出的每个 IP** 重新执行地址校验：
  - 云元数据 IP（`169.254.169.254`、`169.254.170.2`、`100.100.100.200`、`fd00:ec2::254`）与元数据主机名**始终拒绝**，即使列在白名单；
  - 回环/私网/链路本地/未指定/组播/CGNAT 地址仅在显式白名单下放行：字面 IP 目标要求精确 IP 或 CIDR 条目；主机名目标要求**精确主机名**条目（不能用 IP/CIDR 条目放行主机名解析出的私网地址）；
  - `localhost`/`*.localhost`、无点单标签名、`.internal`/`.local`/`.home.arpa` 始终拒绝；
  - 解析结果中只要有一个地址被拒绝，整个探测失败关闭，不会挑选另一个“看起来合法”的地址连接。
- **脱敏**：失败返回 `*ProbeError{Code, Summary, Cause}`，`Error()` 只含稳定错误码与固定摘要；不拼接响应体、响应头、URL userinfo 或原始上游文本。错误码：

```go
ProbeCodeInvalidTarget   = "invalid_target"
ProbeCodeTargetForbidden = "target_forbidden"
ProbeCodeDNSFailure      = "dns_failure"
ProbeCodeDialFailed      = "dial_failed"
ProbeCodeTLSFailed       = "tls_failed"
ProbeCodeRequestFailed   = "request_failed"
ProbeCodeTimeout         = "timeout"
```

> WAVE5 任务单中的 `Status`/`LatencyMs`/`StatusCode`/`ErrorCode`/`ErrorSummary` 对应既有 `ProbeResult.Healthy`/`Latency`/`StatusCode` 与 `ProbeError.Code`/`Summary`；HTTP DTO 仍按 Wave 4 契约输出 `status`、`latencyMs`、`statusCode`、`error`，未改字段。

### 1.2 `files.NewLocalStagingTransport`（DS-W5-02）

`internal/files/local_transport.go`：

```go
func NewLocalStagingTransport(rootDir string) (*LocalStagingTransport, error)
```

`*LocalStagingTransport` 同时实现 `files.UploadTransport` 与 `files.DownloadTransport`。

- 布局：`<rootDir>/<nodeID>/<remotePath>`（远端 POSIX 路径逐段映射为本地目录）。
- 构造：空/首尾空白 root 拒绝；创建 `0700` 目录后 `EvalSymlinks` 规范化并核对必须是目录。
- `PushFile`：`nodeID` 经 token 校验（拒绝 `.`、`..`、`/`、`\`、空格、`:`），`remotePath` 经 `ValidateRemotePath`；逐段创建目录，已存在组件用 `Lstat` 拒绝符号链接（不使用会跟随链接的 `MkdirAll`）；同目录 `CreateTemp` 写入后 `Sync` + 原子 `Rename`，文件 `0600`；写入时同时计算 SHA-256，**字节数必须等于 `size`、摘要必须等于 `sha256Hex`（小写 64 hex）**，失败删除临时文件且目标不变。
- `PullFile`：逐段 `Lstat` 拒绝符号链接，只允许普通文件，流式写入 sink；支持 `ctx` 取消。
- 附加拒绝：Windows 保留设备名（`CON`/`NUL`/`COM1`…，仅 Windows 生效）与含 `:` 的路径段（ADS/卷名）。
- 错误：`ErrInvalidPath`、`ErrInvalidTransfer`、`ErrChecksumMismatch`、`ErrSymlinkForbidden`、`context.Canceled/DeadlineExceeded`。

## 2. 总控 `apps/controller/server/server.go` 默认回退接线（精确参数）

当前组合层（基线 `3adf891`）在 `stagingDir` 解析后构造 files 服务（约 `server.go:614`），随后构造 monitoring 服务（约 `server.go:668`）：

- `files.Options.Uploads = cfg.UploadTransport`、`Downloads` 未注入；
- `monitoring.Options.Prober = cfg.MonitoringProber`、`AllowedHosts = cfg.MonitoringAllowedHosts`。

Wave 5 建议由总控在审查时改为以下**默认回退**（无需新增 HTTP 路由或服务层 API；`files`/`monitoring` 包已 import）：

### 2.1 files：本地暂存目录回退

```go
uploadTransport := cfg.UploadTransport
var downloadTransport files.DownloadTransport
if uploadTransport == nil {
    localFiles, err := files.NewLocalStagingTransport(filepath.Join(filepath.Dir(cfg.DBPath), "node-files"))
    if err != nil {
        return cleanup(err)
    }
    uploadTransport = localFiles
    downloadTransport = localFiles
}

filesService, err := files.NewService(files.Options{
    DB:         db,
    Repo:       filesRepo,
    Audit:      auditRecorder,
    Authorizer: authorizer,
    Submitter:  taskBridge,
    Canceller:  taskBridge,
    Uploads:    uploadTransport,
    Downloads:  downloadTransport,
    StagingDir: stagingDir,
    Now:        now,
})
```

精确构造参数：

- `rootDir = filepath.Join(filepath.Dir(cfg.DBPath), "node-files")`；即与 `staging.db` 同级的私有目录，**不要复用 `stagingDir`**（后者存的是 Controller 上传分块，混放会让节点文件树与暂存文件互相污染）。
- 仅有 `cfg.UploadTransport == nil` 时才启用回退；已注入真实 Agent 通道时不得用本地目录顶替上行，下行保持未注入（`Download` 返回 `503`/`ErrTransportAbsent`）。
- 若总控希望显式控制，可另行增加 `Config.NodeFileRoot string` 与 `Config.DownloadTransport files.DownloadTransport`；默认值与回退条件同上。新增字段属 CX 目录变更，本分支未改 `apps/controller`。

### 2.2 monitoring：真实探测器回退

```go
prober := cfg.MonitoringProber
if prober == nil {
    netProber, err := monitoring.NewNetProber(monitoring.NetProberConfig{
        AllowedHosts: cfg.MonitoringAllowedHosts,
    })
    if err != nil {
        return cleanup(err)
    }
    prober = netProber
}

monitoringService, err := monitoring.NewService(monitoring.Options{
    DB:           db,
    Repo:         monitoringRepo,
    Audit:        auditRecorder,
    Authorizer:   authorizer,
    Prober:       prober,
    AllowedHosts: cfg.MonitoringAllowedHosts,
    Now:          now,
})
```

精确构造参数：

- `NetProberConfig.AllowedHosts = cfg.MonitoringAllowedHosts`：与 `monitoring.Options.AllowedHosts` **传同一份切片**，保证“服务层目标策略”和“解析后二次校验”使用同一白名单；空切片表示不允许任何回环/私网例外。
- `Resolver`、`RootCAs` 保持 `nil`（系统 DNS 与系统信任库）；需要监控内部 CA 目标时再显式传入 `*x509.CertPool`。
- `NewNetProber` 仅在白名单条目非法时返回错误；此时应启动失败，不得静默丢弃 prober（否则重新落入 `503 prober_unavailable`）。

## 3. 接线后的行为变化（审查需知）

1. `POST /api/v1/monitoring/availability-monitors/{id}/probe` 与 `GET .../history`：probe 现在会对目标发起真实 HTTP/TCP/TLS 探测并持久化样本；非 2xx/3xx 记为 down，`error` 为 `http_status_<code>`。**Controller 主机到目标的网络可达性**成为探测结果的一部分。
2. `PUT .../files/uploads/{uploadId}/content` → `POST .../complete`：完成校验通过后，字节真实落到 `<dir(DBPath)>/node-files/<nodeID><destPath>` 并返回 `completed`；不再返回 `503 unavailable`。`files.Service.Download`（当前无 HTTP 路由）从同一目录读回。
3. 该本地目录是 **Wave 5 本地闭环桥接**，不是 Agent 文件通道：不具备节点端权限/磁盘配额/断点续传，也不得对外宣称为生产文件链路。生产 `UploadTransport` 仍由 CX 的受控 Agent 通道实现并在 `cfg.UploadTransport` 注入。
4. 安全边界：本地目录仅由服务进程写；`nodeID`/路径段校验、符号链接拒绝、原子写入与摘要核对均在 transport 内完成；`Download` 失败仍被服务层折叠为固定 `ErrTransportFailure`，不回显本地路径。

## 4. 本分支验证

- `go test ./internal/monitoring/... ./internal/files/... ./tests/integration/v09/services/... -count=1` → 全部 PASS。
- `TestWave5RealProberAndLocalStagingTransport`（真实文件 SQLite WAL + HTTP）：本地 `httptest.Server` 真实 HTTP 探测 → `/probe` 200/up/latency/statusCode 落库 → `/history` 读回；`404` 目标 → down + `http_status_404` 且不回显响应体；HTTP 创建上传 → 字节 PUT → complete → 磁盘文件与 SHA-256 一致 → `Download` 读回一致；未落盘文件下载失败。
- `npm run check` / `npm run check:security`：结果见 `STATUS.md` 检查点 C12 与 `AC-EVIDENCE-WAVE4.md` 第 4 节。

## 5. 剩余风险与未覆盖

1. 本分支只对 `127.0.0.1` 的 `httptest` 目标做了真实网络探测；真实公网 HTTP/TCP/TLS、IPv6-only、证书链异常主机的行为仍 UNTESTED（逻辑由单测覆盖）。
2. DNS Rebinding 用注入的 `HostResolver`（`net.IPAddr` 伪造答案）验证解析后拦截；没有在真实 DNS 服务器上制造 TTL 切换场景。
3. HTTP 健康口径固定为 2xx/3xx；如需“任意响应即 up”或指定期望状态码，需要契约决策。
4. 本地 staging 目录没有容量上限/清理策略；生产节点通道仍需 CX 实现。
5. `UploadTransport` 已注入而 `DownloadTransport` 未注入时，下载继续失败关闭（`ErrTransportAbsent`），本分支未改变该语义。
