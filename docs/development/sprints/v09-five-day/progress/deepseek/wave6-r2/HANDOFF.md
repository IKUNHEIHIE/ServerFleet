# V09-R6-DS-03 · 交接（deepseek-06）

- 分支 `feature/v09-r6-services-ds06`；基线 `c9013f4`；代码 `cde4a21`；未推送/未合并/未操作 awskr。
- 共享 patch/DDL/OpenAPI 登记见 [SHARED-CHANGES.md](SHARED-CHANGES.md)，状态/阻塞见 [STATUS.md](STATUS.md)。

## 复现命令

```bash
go test ./internal/publishing/ -count=1
go test ./internal/certificates/ -run TestCleanup -count=1
go test ./apps/agent/ -run TestGatewayCapability -count=1
go test ./tests/integration/v09/services/ -run TestCXFileTransport -count=1
./node_modules/.bin/redocly lint api/openapi/revisions/v09-r7/services-candidate.yaml
go test ./... -count=1
npm run check
npm run check:security
```

本机真实 Nginx（portable 1.27.5 在 ASCII 用户目录；不使用系统 nginx/80/443）：

```bash
NGINX_BIN=C:/temp/nginx-portable/nginx.exe \
  go test ./apps/agent/ -run TestGatewayCapabilityRealNginxIsolated -count=1 -v
# PASS: nginx -t/reload、loopback proxy 200、idempotent apply、invalid restore、remove
```

当前结果：以上全部 PASS；`npm run check` policy/contracts/go/web/git PASS，`npm run check:security` 0 漏洞；`go test -race` UNTESTED（CGO=0、无 gcc）。

## 总控接线顺序

1. 接收 `cde4a21`，按 SHARED-CHANGES 登记 `certificates.CleanupMigrations()`（ds/0062）、`v09-r7-services` OpenAPI 根配置；不改历史迁移/原候选。
2. Agent：按第 1 节把 `newGatewayCapability(...).Actions()` 加入 `controlledCommandActions`，增加绝对路径 CLI 配置与 `gateway.nginx.v1` 能力；Controller `agentAllowedActions` 加两个 action schema；修改类继续走审批，不能让 standard 绕过。
3. Controller：按第 2 节注入 `NewProductionPublishingBridge`、`gatewayTaskHost`、`GatewayArtifactUploader`、`CleanupJournal`；实现 `ProviderCredentialLookup` 并扩展 credential gate module/scope（第 3 节）。缺 artifacts/lookup 时保持失败关闭。
4. ACME：在 `IssueDNS01` 失败且 `DNS01Records` 非空时写 cleanup journal；清理失败不能变成证书成功；`RetryDue` 的系统主体授权与 HTTP 候选路径由总控接，清理路由可直接调用 `certificates.RegisterCleanupHTTP`。
5. 文件：CX 本轮独占；DS 只用公共 ports 黑盒，不在本分支改 `internal/files`。当前 unsupported policies/续传/rename 的具体失败用例见黑盒测试和 SHARED-CHANGES 第 6 节。

## 实机/外部仍缺

- 真实 DNS Zone/证书/网关/Nginx Agent：需外部授权账号、Controller 发布桥与 Agent 注册；未跑标 BLOCKED。
- 真实 Cloudflare/SMTP/QQ/ACME：无授权秘密，保持本地 TLS fake 证据，UNTESTED。
- awskr/WSL 双机文件、1C1G、arm64、物理双机：未授权/未验收。
- 传输后撤权、任务取消、清理取消/重试的真实生产链路需在 Controller 注入后由总控验收；不以本地模块 PASS 代替。
