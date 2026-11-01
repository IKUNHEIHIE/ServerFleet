# V09-R6-DS · Wave 6 状态（deepseek-06）

- 任务：`V09-R6-DS`；会话唯一 ID `deepseek-06`；分支 `feature/v09-r6-services-ds06`；起点 `73d5dd46fb29b3cf4a445b43b48186d5938fe781`；审查 `codex-review-02`。
- 工作目录：`.worktrees/feature--v09-r6-services-ds06`；允许目录内修改；未改 `apps/**`、`auth/transport`、Web、Proto/gen、根锁/依赖、迁移公共编号或原始资料。
- 本包按子项提交多个可运行增量；真实双机/外部供应商/awskr 仍由总控执行，未据此宣称完整验收。

## 提交

| SHA | 内容 |
|---|---|
| `721f166` | `feat(files): add bounded agent file channel and transfer coordinator`（含 `SHARED-CHANGES` 第 1 节 Proto 申请） |
| `eda29cd` | `feat(dns): add real cloudflare provider adapter with secret references` |
| `cde8fca` | `feat(notifications): add real smtp and qq bot senders with secret references` |
| `98f0450` | `feat(publishing): add nginx site plans and reverse-order compensation`（提交标题重写后 SHA） |
| `a764306` | `test(files,notifications): remove unused fault-injection fields` |
| `da0eff5` | `feat(certificates): add acme dns-01 client with remote signer` |
| `814f1db` | `fix(certificates): use modern ec public key encoding` |
| `d70321f` | `feat(gateway): add configurable nginx listen addresses`（隔离测试 127.0.0.1 高位端口） |
| `80d6918` | `docs(deepseek): record wave6 ds evidence and awskr handoff` |
| `7699b4e` | `feat(dns): add acme challenge publisher and provider consistency guards` |
| `eeabb99` | `feat(files): report transfer terminal results to unified tasks` |
| `9d25dc1` | `fix(gateway): bound nginx task parameters` |

以上均在作者分支本地；未推送、未合并 main。

## 已实现与证据

### DS-W6-01 真实 Agent 文件通道（控制器侧）

- 代码：`internal/files/{channel,channel_transport,coordinator}.go`、`service.go`（`ExecuteTransferTask` 接缝）、测试 `channel_transport_test.go`、`coordinator_test.go`、`fake_agent_test.go`。
- 能力：版本化有界帧；上传/下载；每片 ACK；断线续传（源可 seek）；整文件 SHA-256 双端校验；冲突 skip/overwrite/rename；取消；尺寸/片大小/路径/节点/身份限制；错误码映射；执行期重新授权（源 `files.read`、目标 `files.write`、两端 `files.transfer`）；拒绝审计；从任务 ID 加载传输记录，不接受调用方传路径；逐项部分结果；源下载→私有 spool→目标上传；`files.transfer_execute` 审计；取消不重跑未知副作用。
- 命令/结果：
  - `./.tools/go/bin/go test ./internal/files/ -count=1` → PASS（含新增通道与协调器用例）。
  - `./.tools/go/bin/go test ./... -count=1` → 全包 PASS（2026-11-01，含 architecture/contracts/integration/POC）。
- 覆盖：真实 70,003 字节往返、上传/下载断线恢复、摘要不符拒绝、目标空间/符号链接/路径/未授权错误码、冲突策略、非 seekable 续传拒绝、逐项部分失败、执行期撤权拒绝+审计、取消、终态不重跑、spool 清理、任务结果回写、Coordinator 缺端口失败关闭。
- 任务一致性：`files.TaskReporter` 把两个目标终态写回统一任务（completed→succeeded、canceled→canceled、partial/failed→unknown，避免自动重试可能已产生副作用的传输）；测试断言 completed/partial 两种终态。
- 未覆盖：真实 WSS 双机字节链路、awskr/WSL 跨机、真实磁盘满、跨文件系统 rename、符号链接竞态、并发大文件。详见 HANDOFF。

### DS-W6-02 Cloudflare 真实适配

- 代码：`internal/dns/cloudflare.go`；测试 `cloudflare_test.go`、`cloudflare_service_test.go`；`dns.SecretResolver` 端口。
- 能力：v4 API verify/zones 分页/dns_records CRUD；Bearer 令牌仅按请求解析；拒绝重定向；TLS≥1.2；无环境代理；1MiB 响应上限；401/403/9109 凭据状态；错误不含 provider 文本或令牌。
- 命令/结果：`go test ./internal/dns/ -count=1` → PASS；本地 TLS fake Cloudflare 全流程（verify→TestAccount→CreateZone→CreateRecord→ListRecords）PASS。
- 一致性补偿：CreateRecord/UpdateRecord 本地失败时尝试 provider 回滚/删除；DeleteRecord 把 provider 404 视为已不存在并清理本地行；`ErrProviderNotFound` 可区分。
- 未覆盖：真实 Cloudflare 账号/Zone（无授权秘密），UNTESTED。数据库失败后的补偿路径由代码覆盖但无注入式单测，UNTESTED。

### DS-W6-03 SMTP / QQ 真实发送

- 代码：`internal/notifications/{secrets,smtp,qq}.go`；测试 `smtp_test.go`、`qq_test.go`、`provider_service_test.go`。
- 能力：SMTP 隐式 TLS/STARTTLS、TLS 后 AUTH PLAIN、RFC2047/UTF-8 base64、超时、认证失败凭据无效、DATA 后断线 unknown；QQ token+private/group 消息、2000B 限制、401/403/429/5xx/超时分类、拒绝重定向；服务层只在 provider accepted 时记成功。
- 命令/结果：`go test ./internal/notifications/ -count=1` → PASS；本地 TLS SMTP 服务器 + SQLite `TestChannel` → `provider_accepted`/`smtp_250`/凭据 configured/审计 PASS。
- 未覆盖：真实 SMTP 邮箱、真实 QQ App/目标、TLS 异常主机、IPv6、退信 DSN，UNTESTED。

### DS-W6-04 Nginx 受控站点与发布反序补偿

- 代码：`internal/gateway/nginx.go`；`internal/publishing/rollback_all.go`、`http.go` 新候选路由；测试 `nginx_test.go`、`rollback_all_test.go`、`http_test.go`。
- 能力：确定性 Nginx conf（HTTP/HTTPS、直连/服务组 upstream、ACME challenge、HSTS、代理头）；站点目录/证书/密钥路径注入拒绝；既有站点接管双门；配置文件不进任务参数，改由 artifact 路径+摘要+site 路径的 apply/remove 计划；`RollbackAll` 逆序 gateway→certificate→dns、首个补偿失败即停、逐阶段持久化、续跑、不重写摘要/不宣称 DNS 即时回滚；HTTP `rollback-all`（候选路由）。
- 命令/结果：`go test ./internal/gateway/ ./internal/publishing/ -count=1` → PASS；`TestHTTPRollbackAllReversesExecutedStages` PASS。
- 未覆盖：真实 Nginx 执行、awskr 隔离 nginx、真实 reload/端口冲突、POC-05；详见 HANDOFF。

### DS-W6-05 ACME DNS-01 客户端

- 代码：`internal/certificates/acme.go`；测试 `acme_test.go`。
- 能力：stdlib RFC 8555（directory/newNonce/newAccount/newOrder/authz/dns-01/finalize/order/certificate），ES256 JWS、nonce 一次性、kid/JWK、badNonce 单次重试、bounded polling、https/重定向/响应上限、私钥只经 `ACMESigner`、DNS 值只经 `ACMEDNS01Publisher` 并清理。
- 命令/结果：`go test ./internal/certificates/ -count=1` → PASS；fake ACME 服务器独立验证 JWS/挑战 TXT/CSR 签发/链/序列号/有效期；badNonce、错误 TXT、finalize 失败、processing 轮询、signer 失败、重定向拒绝 PASS。
- 生产 DNS-01 publisher：`dns.NewACMEChallengePublisher` 经 dns 服务创建/清理 `_acme-challenge` TXT，测试创建/清理/重复清理/非法输入/provider 失败/provider 404 幂等 PASS。
- 未覆盖：真实 Let's Encrypt/ZeroSSL/Cloudflare DNS 账号、证书写回秘密存储，UNTESTED/BLOCKED。

## 门禁

- `npm run check:go`（build/test/vet/staticcheck）→ PASS（最终代码 `814f1db`）。
- `npm run check:policy` → PASS；`npm run check:security` → npm audit 0、govulncheck 无已知漏洞。
- `npm run check` 完整运行（2026-11-01T06:12:23Z，代码 `eeabb99`）：`policy/contracts/go/web/git` 均 PASS（web 37 文件/329 项）；Go 全包含 architecture/contracts/integration/POC PASS。
- `npm run check:security`（`eeabb99`）：npm audit 0，govulncheck 无已知漏洞。
- `9d25dc1` 仅收紧 Nginx 任务参数边界（gateway），随后 `npm run check:go` PASS；web/contracts 未变。最终 HEAD 以 `git log -1` 为准。
- `go test -race`：**UNTESTED**，本机 CGO_ENABLED=0 且无 gcc；不能声称 race 通过。
- 迁移：**未新增**；AMCE/Nginx/文件通道均不依赖新表。`publishing.rollback-all` HTTP 路由为候选，未改 OpenAPI/Proto/gen。

## 剩余阻塞 / 未完成

1. 真实 Agent WSS+mTLS 文件会话需 CX 按 SHARED-CHANGES 第 1 节新增 Proto/生成代码与 transport opener；本包只有控制器侧实现与 fake agent 边界证据。`tasks.Command.ExecutionID` 仍未接收，协调器使用稳定替代 ID。
2. Controller 组合入口未接线（总控单写）：files Coordinator、Cloudflare provider、SMTP/QQ senders、ACME signer/challenge、Nginx artifact push+apply task、publishing 生产桥。缺适配时必须保持失败关闭，不得记功能完成。
3. awskr/WSL 实机、真实 Cloudflare/ACME/QQ/SMTP、真实 Nginx、POC-05、1C1G、双架构均 UNTESTED/BLOCKED；无授权外部秘密。
4. `publishing.RollbackAll` 仅按阶段摘要补偿；真实供应商补偿需要总控注入的生产 publisher 才能验收。
5. 设计缺口：published plan 尚未持久化 gatewayIds/zoneId/certificateId；本次不做无依据的 schema 扩展，发布真实目标选择仍由总控契约决定。
6. Race detector 不可用；并发行为仅由现有串行/并发单测与 SQLite 事务覆盖，不宣称 race 验收。
