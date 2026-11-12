# 升级与身份恢复（v0.9.1）

## 升级前

先停止所有旧 Controller 写入进程，保留一份一致的数据库备份和原 `credential-keys/`、PKI、配置及节点执行账本。运行中的 SQLite 不能只复制 `.db` 而遗漏 WAL；使用已有加密备份流程，或停止所有写入并完成 checkpoint 后复制整套状态。不要删除原密钥、重建初始化标记或把升级失败的数据库当空库启动。

使用新包中的程序执行只读预检：

```sh
./controller upgrade-check -db /srv/serverfleet/fleet.db -credential-key-dir /srv/serverfleet/credential-keys
```

返回 `readyForMigration:true` 表示迁移账本已知且 checksum 匹配、当前身份 payload 与原密钥可读取。命令不应用迁移、不生成密钥、不访问节点或供应商。它不等于 TLS/PKI、业务配置及真实旧库的全量升级验收。

当前兼容已有身份状态 Version 1/2，缺失的受支持可选字段沿用现有兼容处理；未知版本、损坏字段、初始化标记矛盾继续拒绝。v0.9.0 的 49 项迁移保持原 SQL/checksum，v0.9.1 追加节点诊断迁移。没有拿到实际历史快照的版本，不承诺任意跨版本跳跃均可升级。

| 错误码 | 处理 |
|---|---|
| `MIGRATION_LEDGER_INCOMPATIBLE` | 核对源/目标版本及迁移历史；不要手改 checksum |
| `IDENTITY_VERSION_UNSUPPORTED` / `IDENTITY_PAYLOAD_JSON_INVALID` | 身份版本不受支持或JSON损坏，保留原副本核对源版本 |
| `IDENTITY_USER_RECORD_INVALID` / `IDENTITY_SESSION_RECORD_INVALID` / `IDENTITY_GRANT_RECORD_INVALID` | 已定位到对应记录类型的字段约束失败；不输出具体用户/会话秘密，保留副本安排兼容处理 |
| `IDENTITY_INITIALIZATION_MARKER_MISMATCH` / `IDENTITY_CLOCK_ROLLBACK` | 核对初始化标记或系统时间，不能绕过保护 |
| `IDENTITY_PAYLOAD_OR_INITIALIZATION_MARKER_INVALID` | 保留失败副本，核对源版本和身份数据；不要清空用户/会话绕过 |
| `CREDENTIAL_KEY_DIRECTORY_MISSING` / `CREDENTIAL_KEYRING_MISSING` | 找回与该 DB 配套的原密钥目录/文件；只复制 DB 不够 |
| `CREDENTIAL_KEY_DIRECTORY_PERMISSIONS` / `CREDENTIAL_KEYRING_PERMISSIONS` | 目录须当前运行用户拥有且 0700，文件须 0600；拒绝符号链接或错误所有者 |
| `CREDENTIAL_KEYRING_FORMAT_OR_VERSION_INVALID` | 核对原文件和版本，不生成替代密钥 |
| `CREDENTIAL_KEY_BINDING_OR_CIPHERTEXT_INVALID` | DB 与 keyring 不配套或加密数据异常；恢复原配套备份 |

预检通过后先升级 Controller，再升级 Agent。检查 `/readyz`、登录、精确节点读取授权与 Agent 新连接。自动更新仍必须遵守签名、审批和健康回滚要求；本次 unsigned 下载包不能直接替代受信任更新目录。已经写入新迁移后，不直接用旧二进制打开新库；回滚须使用升级前一致的整套状态，并遵守原恢复权威/隔离流程，不能同时启动两个写入者。

## Agent 身份自动续期

v0.9.1 的常驻 `agent -serve` 在身份证书剩余约 4 小时时主动续签。签发寿命仍为 12 小时；Root pin、节点 ID、信任代次和本地私钥不变。续签请求经直接 mTLS，拒绝过期、撤销、暂停或代次不匹配身份，不借用网站 ACME 续签。

重复请求返回同一证书，网络失败保留旧身份；本地新证书验证并以 0600 原子写入后，停止接收新任务、等待运行任务结束，再连接新身份。Controller 观察到新证书的真实握手后，旧证书保留 2 分钟重叠再退役；丢失响应不会提前吊销旧证书。身份切换不自动重放 UNKNOWN 或其他执行。

接近到期的节点诊断会提示检查续期，Agent 日志记录固定 `IDENTITY_RENEWAL_UNAVAILABLE` 类别及到期时间。必须在旧证书失效前完成升级；已过期的身份不能靠重启获得新的权限。

## 已过期身份的显式恢复

1. 管理员独立核验机器和原节点 ID，并具有该节点的 `nodes.revoke` 精确授权。
2. 在“添加节点”填入“恢复已有节点 ID”，生成仅使用一次、最长 10 分钟的替换令牌。普通首次安装留空此项。
3. 停止原 Agent，确认运行任务结果；把原身份目录中的 `enrollment.json`、`identity-key.pem`、`issued.json` 移入另一个 0700 私有归档目录。保留 `executions/` 及其他执行账本和原权限，不删除数据库或清空 UNKNOWN。
4. 使用原 Root pin、同一节点 state-dir 和新令牌重新执行入网，管理员批准。同一替换令牌/请求只取得一次身份；批准会吊销该节点的旧证书。节点资产与既有精确资源授权继续按原节点 ID 保留。
5. Agent 收到 issued 后运行 `-serve`。被行政暂停/吊销的节点不得用此流程自动解除限制，须先按其管理策略处置。

令牌和 enrollment 是不同实体：前者的 ID 标识一次性凭证，后者的 ID 是 Agent 持久 CSR 请求 ID。审批使用 enrollment ID，不能把 token ID 当作审批 ID。

安全写入令牌文件的示例（不要启用 shell 的 `set -x`）：

```sh
umask 077
install -d -m 700 /srv/serverfleet/enrollment
read -rsp 'Enrollment token: ' SF_ENROLL_TOKEN
printf '%s' "$SF_ENROLL_TOKEN" > /srv/serverfleet/enrollment/token.txt
unset SF_ENROLL_TOKEN
chmod 600 /srv/serverfleet/enrollment/token.txt
```

该目录和文件必须由运行 Agent 的同一用户拥有。随后把绝对路径传给 `-enroll-token-file`，避免把秘密放进命令参数、日志或聊天。安装器已有等待审批能力，不需要手工轮询多个 Cookie/CSRF 请求。

## 会话与故障定位

Web 会话闲置 30 分钟或登录后达到 8 小时会失效。保温不能突破绝对期限；自动化必须处理 401 和重新认证，不能假定登录永久有效。普通节点/任务 API 的受限 Bearer 适配仍未装配，不因本次补丁自动变为可用。

403 优先检查响应的固定诊断码/`X-SF-Diagnostic-Code`：`HOST_MISMATCH`、`ORIGIN_MISMATCH`、`ORIGIN_REQUIRED`、`FETCH_SITE_REJECTED`。服务端结构化访问日志可用 `request_id` 关联状态码与路由，不记录 Cookie、令牌、正文或查询串。
