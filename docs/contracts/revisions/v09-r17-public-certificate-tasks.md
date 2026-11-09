# R17 公开证书签发与 DNS 检查统一任务

V09-CLOUD-CODE-COMPLETE-01 / chatgpt-takeover-01。集中编码候选，**UNTESTED**，不得引用旧 main CI 作为本候选验证。原三 OpenAPI、Proto/gen、已接收迁移 SQL/hash、历史 ADR 不改；追加 cx/0018_public_certificate_orders，预计正式迁移 37 项。没有新增依赖/秘密，也没有访问真实供应商。Context7 当前不可用，本增量组合既有 ACME/Cloudflare 客户端与标准库，不引入新 SDK/CLI。

## 启动与秘密

普通 Linux 用户使用 provisioned PKI；仅在明确给出私有 `-acme-deployment /absolute/config.json` 时启用。配置必须私有目录/普通用户拥有，版本 1，字段：directoryUrl、cloudflareUrl（省略分别沿用既有官方 ACME/Cloudflare 默认地址）、accountKeyFile、dnsCredentialId、dnsCredentialRevision、可选 acmeRootFile/dnsRootFile。HTTPS 固定 origin，无重定向/环境代理；自定义根证书按实际字节冻结，变化拒绝继续旧计划。默认系统根仅是 TLS/公链验证，不能据此冒称供应商验收。

`controller pki init-acme-account -directory /absolute/new-private-directory` 创建新的 P-256 PKCS8 account-key.pem，只输出公开 SPKI 摘要；不覆盖、不生成 Root/Issuer。bootstrap 将 provisioned Root/Intermediate 的 SPKI 填入禁止签名身份列表；ACME account key 不能复用任一 CA 密钥。每次签名复核私有文件与启动摘要。网站私钥只在 Agent，签发只消费 R14 实际成功的公开 CSR 回执。

Cloudflare Token 必须先由当前管理员写入已有加密凭据库（kind=cloudflare_token，usageScope 含 dns）；DNS 账号只绑定该 opaque ID，启动配置固定该 ID 与 revision。正常 HTTP/AI/plugin 没有内部执行 seal，不能取得 Token。private execution scope + 正在 running 的任务/当前审批/权限 + 同 actor/request/plan + 活跃单次 CredentialOperation + 当前版本共同限制每次借用和 I/O；不提供明文 resolver。后续清理另用 CleanupJournal 实际 claim/lease，不能借已完成任务继续执行。

加密备份白名单追加配置、account-key.pem 和可选公开根，不备份 Root/网站私钥；最大固定文件数同步有界调整。该恢复材料仍受 age 完整认证、摘要和 quarantine，不能当已完成灾备激活。

## 接口与流程

- GET `/api/v1/certificates/public-profile`：当前管理员读取已配置 profile SHA/opaque DNS credential ID/revision，无秘密。
- POST `/api/v1/dns/provider-tasks`：`{operation:test,accountId,idempotencyKey}` 或 `{operation:import-zone,accountId,zoneId,providerZoneId,idempotencyKey}`。注册真实 Controller 权威本地标准类任务 `dns.provider.inspect`；仅 Web 管理员，固定 profile，读取供应商检查/已有 Zone 后持久元数据，不远端创建/删除 Zone。首次 test 不要求已验证凭据，后续导入沿用 DNS owner 的 CredentialValid 要求。统一任务详情显示真实结果；UNKNOWN 不自动再提交。
- POST `/api/v1/certificates/public-tasks`：`{csrTaskId,zoneId,idempotencyKey}`。当前管理员必须可读实际成功 CSR 和当前 DNS Zone；固定同节点、域名范围、profile。节点目标的 `certificates.public.issue` 修改类任务强制真人审批；worker 在 Controller 执行 ACME/DNS，不让 Web/Agent 接触供应商秘密。
- GET `/api/v1/certificates/public-tasks/{taskId}`：当前任务读与管理员授权，返回持久公开 orderStatus/链字节及摘要/expiry/未决 cleanup 身份数；`installed:false` 恒明确。未执行无回执为 409，不能伪写完成。issued_cleanup_pending 或 unknown 保留失败边界。实际公开链交 R15 的同一 CSR 审批安装，再经 R16 各节点证明及实际 HTTPS 发布。

外部 create 之前同事务记录 DNS TXT 意图和审计，返回 ID 后立即持久观察；无 ID 的意图始终不能成为删除授权。签发终态同审计事务保存，已取得有效公链但清理不明仍保留公链、失败任务与未决项。进程重启把旧 running 订单持久置 UNKNOWN，保留已有意图/ID，不重放 CA/DNS。实际清理重试必须与原订单/域名/计划/profile、当前 DNS 记录三元组匹配，并持有当前未取消未过期 claim；未知 provider 删除写 UNKNOWN，无自动重试。

## 验收仍待统一执行

真实独立 Agent CSR→DNS 检查/导入→批准签发→CA 公链→批准安装→HTTPS；非管理员/撤权/旧 credential revision/错账号或 Zone/错域名/未批准/旧 profile/伪 cleanup context/过期 claim/cancel/CA 或 DNS 未知结果/重启/审计提交失败/Root 与 Issuer 复用拒绝；旧无 ACME 配置装配兼容。正式 Web 提供任务与结果入口，不把登记/公链文本当安装健康。所有检查尚未运行，B03—B06/G2/真实外部/ARM/硬 1C1G/物理双机与发布准入不关闭。

DNS 常规记录写与发布 DNS 仍须各自统一任务接线；本 profile 的私有 scope 不放行旧直接 HTTP provider 调用。续签调度、备份异地/保留、远程批次更新、实际 fence 等独立未完成项继续处理。
