# V09-R6-DS · Wave 6 接线与 awskr 实机交接（deepseek-06）

- 本文件是作者 `deepseek-06` 交给总控 `codex-review-02` 的接线/实机步骤，不是已冻结契约。
- 作者分支 `feature/v09-r6-services-ds06`，最终代码 SHA 见 `STATUS.md`；未推送、未合并 main。
- 总控独占 `apps/controller`、Proto/gen、根依赖/锁、迁移编号、CI 与 awskr 执行。作者只提交具体差异。

## 1. 控制器/平台接线清单（按优先级）

### 1.1 文件通道（需 CX 先落 Proto + transport opener）

1. 按 `SHARED-CHANGES.md` 第 1 节新增 `FileOpen/FileOpenAck/FileChunk/FileAck/FileComplete/FileCompleteAck/FileCancel/FileError` 与 Envelope oneof 40–47；固定工具重新生成 `gen/go`。
2. `internal/transport` 增加文件会话 opener（建议签名，需安全审查）：
   ```go
   type FileSession struct {
       Send   func(context.Context, *agentv1.Envelope) error
       Cancel func() // removes the transfer_id route and unblocks readers
   }
   // Implemented by the controller composition around ControllerLinks.Send and
   // the established frame Event callback. One session per transfer_id, bounded.
   type FileSessionOpener interface {
       Open(ctx context.Context, nodeID, transferID, executionID string) (FileSession, error)
   }
   ```
   - 用已 mTLS 认证的 `LinkObservation.Identity.NodeID` 绑定节点，不能信任帧里的 node_id。
   - 在 `ControllerLinkConfig.Event` 收到 `File*` 帧时按 `transfer_id` 路由到会话；未匹配的帧拒绝并审计；不得把文件数据交给普通任务日志。
   - `Open` 前必须确认：该节点已接受 action `files.transfer` 的 `TaskOffer`、execution_id 匹配、能力 `files.transfer.v1` 已授予。
3. `apps/controller/server` 构造：
   ```go
   opener := ... // transport-backed
   nodeTransports := files.NodeFileTransportsFunc(func(ctx context.Context, nodeID string) (files.NodeTransport, error) {
       session := ... // opener adapter implementing files.AgentFileChannel
       transport, err := files.NewChannelTransport(files.ChannelTransportConfig{
           Channel: session, MaxChunkBytes: 16 << 10, MaxFileBytes: 2 << 30,
           MaxAttempts: 3, FrameTimeout: 30 * time.Second,
       })
       return files.NodeTransport{Upload: transport, Download: transport}, err
   })
   coordinator, err := files.NewTransferCoordinator(files.CoordinatorOptions{
       Repo: filesRepo, Transports: nodeTransports, Authorizer: authorizer,
       Audit: auditRecorder, SpoolDir: filepath.Join(stagingDir, "spool"), MaxFileBytes: 2 << 30,
   })
   filesService, err := files.NewService(files.Options{/* 既有字段 */ Coordinator: coordinator})
   ```
4. 任务派发特例：action `files.transfer` 不再向两台 Agent 各发一个 shell `TaskOffer`。调度器在 `Dispatch` 或组合层拿到 `task_id`/`subject`/`request`/`execution_id` 后调用：
   ```go
   transfer, err := filesService.ExecuteTransferTask(ctx, taskID, files.TransferExecution{
       SubjectID: subject, RequestID: requestID, ExecutionID: executionID,
   })
   ```
   逐项结果已由 `ApplyTaskUpdate` 写回传输记录；任务状态/日志仍由统一任务管线负责。
5. `tasks.Command` 目前没有 `ExecutionID`（见 `SHARED-CHANGES` 第 2 节申请）。接收前协调器用 `task_<taskID>_<transferID>` 绑定 Agent 去重；接收后应传真实 CX 执行 ID。

### 1.2 Cloudflare

- `dns.NewCloudflareProvider(dns.CloudflareConfig{BaseURL: "", Client: nil, Secrets: resolver, Timeout: 20*time.Second})`，`BaseURL` 空值即 `https://api.cloudflare.com/client/v4`。
- `resolver` 由 CX 秘密存储适配 `dns.SecretResolver`；只返回当次 Bearer 令牌，不得写日志/审计/缓存到账号记录。
- 在 `apps/controller/server` 用配置开关注入 `cfg.DNSProvider`；未配置保持 `nil`（失败关闭）。不得用 fake 充当功能完成。

### 1.3 SMTP / QQ

- 注入 `notifications.NewSMTPMailer` / `notifications.NewQQBotSender` 到 `notifications.Options{SMTPSender, QQSender}`；`notifications.SecretResolver` 由 CX 秘密存储适配。
- SMTP secret 支持 JSON `{username,password}`、`username:password`、裸密码；QQ secret 为 AppSecret。
- 真实发送仍需每渠道账号配置 + `SetSecretRef`，并由 `TestChannel` 先验证凭据。

### 1.4 ACME

- 生成/保存密钥仍归 CX 秘密存储；实现 `certificates.ACMESigner`：
  - `PublicKey(ctx, keyRef)` 返回 *ecdsa.PublicKey（P-256）；
  - `SignDigest(ctx, keyRef, digest)` 返回 raw r||s 64 字节。
- 实现 `certificates.ACMEDNS01Publisher`：使用 dns 服务/Cloudflare 适配器写入 `_acme-challenge.<domain>` TXT，返回 cleanup 删除记录；建议把记录 ID 持久化以便清理。
- `certificates.NewACMEClient(...).IssueDNS01(...)` 返回链 PEM；证书落盘/写入秘密存储由总控组合，客户端不碰私钥。Directory 默认 Let's Encrypt；验收前先用 staging。
- 该客户端与内部 PKI `certificates.Issuer` 分域，不替换内部 CA。

### 1.5 Nginx 站点与发布

- 渲染：`gateway.RenderNginxSite(gateway.NginxSiteRequest{...})`；`ExistingSite` 必须同时给 `TakeoverApproved` 与 `ApprovalRef`；`HTTPListen/HTTPSListen` 可配置（生产默认 80/443，隔离测试用 127.0.0.1:18080/18443）。
- 应用（两步，仍是一个统一任务）：
  1. 用文件通道把 `site.Config` 推到 `site.ArtifactPath`（`PushFileWithPolicy(..., ConflictOverwrite, reader, len, site.Digest)`）；节点目录必须在 Agent 允许的 staging 根内。
  2. `gateway.BuildNginxApplyTask(site, gateway.NginxTaskInput{...})` 得到 `TaskRequest`，经既有 `TaskSubmitter` 提交；Agent 动作必须：
     - 校验 `artifact_path` 在 staging 根内、文件 SHA-256 等于 `config_sha256`；
     - 只写 `site_path`；写前备份旧文件；`nginx -t` 通过后才 reload；
     - 失败恢复旧文件并报告失败步；root/reload 按现有审批规则，不由本包绕过；
     - remove 仅在磁盘摘要等于 `config_sha256` 时删除。
- 发布：`publishing.LocalBridge` 的三个函数由总控注入真实 publisher（DNS 记录、证书、网关站点任务）；未注入保持 `ErrPublisherUnavailable`。`publishing.RollbackAll` 已实现逆序补偿；HTTP `POST .../rollback-all` 为候选路由，是否纳入 OpenAPI 由总控决定。
- 生产 publisher 仍需总控决定 plan 的 gatewayIds/zoneId/certificateId 持久化（当前 Plan 不存这些字段，作者未擅自扩 schema）。

## 2. awskr 隔离实机步骤（非交互，仅总控执行）

前提：只使用 `awskr` 普通用户私有目录；不碰已有 `serverfleet-controller`/`serverfleet-agent`、nginx 站点、MySQL、防火墙、cron；不占 80/443/8443/18443/9443；所有测试进程可回退并清理。

### 2.1 Linux 代码/资源基线（无需外部秘密）

在本机准备固定 SHA 的测试包并上传（总控已有流程），在 awskr：

```bash
set -euo pipefail
QA="$HOME/.local/share/serverfleet-qa/wave6-ds-$(date +%Y%m%d)-<shortsha>"
mkdir -p "$QA"
cd "$QA/project"   # 固定 SHA 的 worktree 副本
export PATH="$PWD/.tools/go/bin:$PATH"
/usr/bin/time -v taskset -c 0 go test ./internal/files ./internal/dns ./internal/notifications ./internal/gateway ./internal/publishing ./internal/certificates -count=1 2>&1 | tee "$QA/pkg-tests.log"
free -m | tee "$QA/free.txt"; df -h "$HOME" | tee "$QA/df.txt"
```

预期：所有包 `ok`，0 FAIL；日志没有真实秘密。记录 `Maximum resident set size`、`Elapsed`、`free/df`。**4GB awskr 不代表 1C1G 验收**。

### 2.2 真实文件通道（需 CX Proto/opener 已接线）

1. 在 `$QA` 下构建 controller/agent；使用 `$QA/data/fleet.db`、`$QA/data/staging`、127.0.0.1 高位端口（如 19443）。不要复用系统服务 DB/端口。
2. 用一次性管理员初始化并用现有入网流程登记两个节点：
   - 节点 A：awskr 本机 Agent（私有目录、`-state-dir "$QA/agent-a"`）；
   - 节点 B：用户 Windows 的 WSL Agent（按 CONTROL-RESUME 允许），或第二台授权测试机；**不能把 awskr 上两个进程记成两台服务器互传**。
3. 在节点 A 的私有目录创建测试文件（约 1–4MB 随机字节；`sha256sum` 记录），在节点 B 创建私有目标目录；不要用系统目录。
4. 通过统一任务/文件 API 创建传输（先确认目标节点在线、源读/目标写分别授权）：
   ```bash
   curl -sS -X POST "$CTRL/api/v1/nodes/$NODE_A/files/uploads" ... # 或
   curl -sS -X POST "$CTRL/api/v1/transfers" -H "Content-Type: application/json" -d '{
     "sourceNodeId":"'$NODE_A'","destinationNodeId":"'$NODE_B'",
     "sourcePaths":["'$QA_SRC'/blob.bin"],"destinationDir":"'$QA_DST'",
     "conflictPolicy":"overwrite"
   }'
   ```
   （实际字段/鉴权头以当前 Controller DTO 为准；总控接线时同步 GM/Web。）
5. 预期：
   - 任务终态 completed/partial_failure 与逐项结果一致；目标文件存在、大小/SHA-256 与源一致；
   - 审计有 `files.transfer_execute`、源读/目标写授权、任务 ID；日志无文件内容/凭据；
   - 源路径越界、目标 symlink、无目标写权限分别失败并留审计。
6. 故障/恢复：
   - 传输中 kill 节点 B Agent（只 kill `$QA` 进程组），重启后重新执行；预期从已确认偏移续传/重连，最终 SHA 一致；无法续传时明确失败，不伪造成功。
   - 传输中取消：预期任务取消、已复制项保留、未开始项 cancelled，不重跑未知副作用。
   - 磁盘不足：在 B 的私有目录用临时小文件系统/受限目录模拟；预期 `insufficient_space`，不留下成功文件。若无法安全构造，记录 UNTESTED，不得声称已测。
7. 清理：
   ```bash
   pgrep -af "$QA" | awk '{print $1}' | xargs -r kill
   rm -rf "$QA/data" "$QA/agent-a" "$QA/agent-b" # 仅本次 QA 私有目录
   ss -ltnp | grep -E ':(19443|18080|18443|19000)' || true
   ```
   不执行 `systemctl reload/restart nginx`、不改 `/etc/nginx`、不触碰系统 serverfleet。

### 2.3 隔离 Nginx 语法/代理 smoke（不碰系统 nginx）

在项目副本的忽略目录 `reports/_review/wave6-ds/nginxplan/main.go` 写一个临时程序（仅测试用，不提交）：

```go
package main

import (
    "fmt"
    "os"
    "serverfleet.local/serverfleet/internal/gateway"
)

func main() {
    site, err := gateway.RenderNginxSite(gateway.NginxSiteRequest{
        GatewayID: "gw-qa", Domain: "qa.internal", Upstream: "127.0.0.1:19000",
        HTTPListen: "127.0.0.1:18080", SiteDir: os.Args[1], ArtifactDir: os.Args[2],
    })
    if err != nil { panic(err) }
    if err := os.WriteFile(os.Args[1]+"/serverfleet-site.conf", site.Config, 0600); err != nil { panic(err) }
    fmt.Printf("digest=%s path=%s artifact=%s\n", site.Digest, site.Path, site.ArtifactPath)
}
```

```bash
set -euo pipefail
QA="$HOME/.local/share/serverfleet-qa/wave6-nginx-$(date +%Y%m%d)"
mkdir -p "$QA/conf" "$QA/artifacts" "$QA/logs" "$QA/html"
go run ./reports/_review/wave6-ds/nginxplan "$QA/conf" "$QA/artifacts" | tee "$QA/plan.txt"
cat > "$QA/nginx.conf" <<EOF
worker_processes 1;
pid $QA/logs/nginx.pid;
error_log $QA/logs/error.log;
events { worker_connections 64; }
http { access_log $QA/logs/access.log; include $QA/conf/serverfleet-site.conf; }
EOF
# 上游与站点（只用 127.0.0.1 高位端口）
printf 'wave6-nginx-ok\n' > "$QA/html/index.html"
(cd "$QA/html" && python3 -m http.server 19000 --bind 127.0.0.1 >/dev/null 2>&1 & echo $! > "$QA/upstream.pid")
nginx -t -p "$QA/" -c "$QA/nginx.conf"
nginx -p "$QA/" -c "$QA/nginx.conf" -g 'daemon off;' >"$QA/logs/stdout.log" 2>&1 & echo $! > "$QA/nginx.pid.shell"
sleep 1
curl -sS -o "$QA/curl.out" -w '%{http_code}\n' -H 'Host: qa.internal' http://127.0.0.1:18080/
cat "$QA/curl.out"   # 预期 wave6-nginx-ok，HTTP 200
# 端口冲突：再次启动同一隔离配置应失败（EADDRINUSE）
if nginx -t -p "$QA/" -c "$QA/nginx.conf" >/dev/null 2>&1; then
  timeout 3 nginx -p "$QA/" -c "$QA/nginx.conf" -g 'daemon off;' 2>&1 | grep -qi 'address already in use' && echo 'EADDRINUSE observed'
fi
kill "$(cat "$QA/nginx.pid.shell")" "$(cat "$QA/upstream.pid")" 2>/dev/null || true
rm -rf "$QA"
```

预期：`nginx -t` syntax ok；HTTP 200 返回上游内容；再次启动观察到端口冲突；系统 nginx 与 80/443 完全未改。`HTTPS` 路径需要测试证书时，用临时自签证书和 `127.0.0.1:18443`，不写系统证书目录。

### 2.4 外部供应商/ACME

- 本地 TLS fake 已覆盖 Cloudflare/SMTP/QQ/ACME 协议；在 awskr 复跑 `go test` 即可，不需要外网账号。
- 真实 Cloudflare/ACME/QQ/SMTP 必须由负责人逐项授权并提供测试账号/Zone/目标；测试使用 `sf-it-*`/staging，记录创建→读回→删除→remaining=0；未授权时记录 **BLOCKED**，不得报成功。
- 真实 ACME 先用 staging Directory；真实证书不得写入系统信任库或现有 nginx 目录。

### 2.5 采样与证据

- 每个测试保存：命令、退出码、stdout/stderr、`free -m`、`df -h`、`ss -ltnp`、测试文件 SHA-256、任务 ID/审计查询结果。
- 资源采样用 `/usr/bin/time -v` + `taskset -c 0`；1C1G/arm64 仍需单独环境，不得由 4GB x86 awskr 推断。
- 证据放入忽略目录 `reports/_review/wave6-ds/`，由总控引用；作者不写 awskr。

## 3. 停止条件

本包在以下任一情况停止并交回总控：真实 Agent Proto/opener 尚未接收（文件通道实机）、外部供应商无授权、迁移/Plan 目标字段需契约决策、或总控要求先合并当前增量。其余独立实现已完成。
