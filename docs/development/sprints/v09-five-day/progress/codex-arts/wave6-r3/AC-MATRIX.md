# Codex Arts · AC-01—24 证据映射（GM-04，2026-11-02）

`ACCEPTANCE.json` 由总控维护，本文件不修改它，只给总控可直接合并的本轮证据与如实状态。
列含义：**页面/接口**=真实构建页面的正常与负向步骤；**外部/实机**=真实外部账号、物理双机、ARM 等；**准入**=是否足以支持发布准入。

| AC | 源码/模块 | 页面/接口证据（正常/负向） | 外部/实机 | 准入 | 状态 |
|---|---|---|---|---|---|
| AC-01 节点接入/身份/生命周期 | `apps/web/src/features/nodes`、CX nodes/auth | P01–P08、P15–P17：bootstrap、一次性令牌、pending→approve→issued、WSS+mTLS 在线/离线、审计；真实 Linux Agent | 同 WSL 主机双 Agent，**不是物理双机** | 不足 | PASS（限定） |
| AC-02 普通分组/自动分类 | `features/groups`、DS nodes | 既有真实 Controller 70 步覆盖；本会话未新增 | 无 | 不足 | 本会话未复核 |
| AC-03 文件互传 | `features/files`、`tests/browser/v09/fixturehost`、CX files | B01 成功（SHA 一致、双方执行 succeeded）；B01n×5：取消、撤权（403）、丢回执（重启不重放）、多文件缺源、遍历/overwrite 拒绝 | 同 WSL 主机双 Agent；无物理双机 | 不足（缺续传/symlink/混合 partial） | PASS（限定） |
| AC-04 服务组 | `features/service-groups`、DS servicegroups | B04 用真实服务组创建；本会话未单独做页面负向 | 无 | 不足 | 部分 |
| AC-05 批量任务 | `features/tasks`、CX tasks | P09/P10 正常；P13 Agent 拒绝；P14 撤权 403；B01n 取消不冒充回滚 | 无 | 不足（批次/审批失效未在本会话新增） | PASS（限定） |
| AC-06 Nginx 网关 | `features/gateways`、DS/CX gateway | B04 网关创建 409 `takeover_approval_required`；未执行真实 Nginx | 无真实 Nginx 本会话 | 不足 | BLOCKED |
| AC-07 DNS | `features/dns`、DS dns | 既有 70 步页面链路；本会话未新增 | 无真实供应商账号 | 不足 | UNTESTED（外部） |
| AC-08 证书/内部 TLS | `features/certificates`、CX/DS | 既有页面与负向；本会话 B02 用本地 TLS 上游 | 无真实 ACME/CA | 不足 | UNTESTED（外部） |
| AC-09 监控告警/QQ/SMTP | `features/settings/notifications`、DS | 既有页面/服务测试；本会话未新增 | 无真实 SMTP/QQ | 不足 | UNTESTED（外部） |
| AC-10 AI 会话资源授权 | `features/ai`、CX ai | 既有 70 步 + B02（真实 TLS 上游合成） | 无真实供应商账号 | 不足 | PASS（合成） |
| AC-11 AI 三级审批 | `features/ai`、CX auth | 既有 70 步 + 本会话预算面板权限/409 组件测试 | 无 | 不足 | PASS（限定） |
| AC-12 插件平台 | `features/plugins`、CX plugins | B03 目录 501，插件中心明确不可安装；无工件执行 | 无 | 不足 | BLOCKED |
| AC-13 更新中心 | `features/updates`、CX updates | B05 503 `UPDATE_SOURCE_UNAVAILABLE`；页面专门说明；组件回归 | 无正式更新源 | 不足 | BLOCKED |
| AC-14 工作流/用户权限/审计 | `features/workflows`、`users`、`audit` | 真实 Controller 70 步（57–68）覆盖；本会话未改 | 无 | 不足 | PASS（既有） |
| AC-15 服务发布 | `features/publishing`、DS publishing | B04 409；未执行真实发布/补偿 | 无 | 不足 | BLOCKED |
| AC-16 服务可用性监控 | `features/monitoring`、DS | 真实 Controller 70 步 63/64（NetProber 本地） | 无外部目标 | 不足 | PASS（本地） |
| AC-17 安全批量执行 | `features/tasks`、CX tasks | P13 拒绝、B01n 取消/撤权；冻结目标/批次由既有 70 步 | 无 | 不足 | PASS（限定） |
| AC-18 节点恢复/主控灾备 | `features/backups`、CX recovery | B06 预演缺身份/fence；B06n 第二隔离 Controller 实测（初始化/登录/nodes 401） | 无真实新权威/旧主控 fence、无 PKI-OPS 原文 | 不足 | BLOCKED |
| AC-19 安全凭据 | `features/settings`、CX auth | B02 合成凭据、P19 仅引用/修订号；无明文回显 | 无真实外部凭据 | 不足 | PASS（合成） |
| AC-20 流量配额/到期 | `features/monitoring`、DS quota | 既有页面；本会话未新增 | 无 | 不足 | 本会话未复核 |
| AC-21 Xray 安装 | `features/plugins/xray`、CX | B03 目录 501；未运行工件 | 无 arm64/物理机 | 不足 | BLOCKED |
| AC-22 Xray 配置 | `features/plugins/xray`、CX | 同上；声明式 UI 保留，未执行 | 无 | 不足 | BLOCKED |
| AC-23 Xray 用户/受控导出 | `features/plugins/xray`、CX | 同上 | 无 | 不足 | BLOCKED |
| AC-24 Xray 流量 | `features/plugins/xray`、CX/DS | 同上 | 无真实实例 | 不足 | BLOCKED |

## 判定口径

- 只有真实页面/统一 HTTP + 真实任务/Agent/进程终态才算 PASS；202、queued、check、preview 不算成功。
- 明确预期拒绝只在对应负向步骤通过；500/401/403/格式错/超时按 FAIL。
- 同 WSL 主机双 Agent 只作为双端协议证据，不得当作物理双机证据。
- 本表不改变 `ACCEPTANCE.json` 的 `NOT_ACCEPTED` 总状态；未复核项不据此声称通过。