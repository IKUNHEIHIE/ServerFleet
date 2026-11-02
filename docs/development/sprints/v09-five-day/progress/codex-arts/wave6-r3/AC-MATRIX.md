# Codex Arts · AC 证据映射（GM-04，2026-11-02）

`ACCEPTANCE.json` 由总控维护，本文件只提供本轮可直接合并的证据指针与如实状态；不修改 AC 文件本身。

| AC | 本轮证据 | 状态与边界 |
|---|---|---|
| AC-01 节点接入/身份/生命周期 | P01–P08、P15–P17：真实 Controller + 真实 Linux Agent 的 bootstrap、一次性令牌、pending→approve→issued、WSS+mTLS 在线/离线、审计；Node 页显示服务端真值 | 真实页面正常链路 PASS；过期/重复凭据、坏 CA、身份撤销由既有 70 步与后端用例覆盖，本增量未新增外部实机项 |
| AC-03 文件互传 | **B01**：页面创建 transfer → 统一任务 → 两个真实常驻 Agent（各自私有 `-file-root`）→ 目标字节 SHA-256 与源一致 → transfer/task 双方执行终态；**B01n**：取消（cancelled）、撤权（failed，撤权期间读取 403）、目标 Agent 写入中断/丢回执（failed，重启后仍不 completed、不重放）；失败不冒充成功 | 正常、取消、撤权、丢回执均 PASS（真实页面 + 真实进程）；遍历/symlink、续传、多文件部分结果仍属后端/后续项 |
| AC-05 批量任务 | P09/P10（标准任务真实执行）、P13（peer-approved host.patch 被 Agent 拒绝、页面显示失败）、B01n（取消不冒充回滚）、P14（撤权后 403 不清会话） | 真实页面正常/负向 PASS；批次/金丝雀/审批失效由既有 70 步覆盖，本增量未改 |
| AC-10/11 AI 授权与审批 | B02 真实 TLS 上游生成 + 真实 AIBudget；**P20** owner 预算窗口/限额/已用/剩余与管理员 CAS，服务端计数跨重配保留；缺策略拒绝在组件测试中断言 | 页面与 API PASS；AI 任意 root shell 的真人管理员审批仍按原有证据，本增量未放宽 |
| AC-13 更新中心 | B05 调用真实 `/system/updates/check` 得到 503 `UPDATE_SOURCE_UNAVAILABLE`；页面新增专门说明（检查/安装都不算成功）并有回归测试 | **BLOCKED**：缺正式更新源/签名安装装配；无外部实机证据 |
| AC-15 服务发布 | B04 真实服务组 + 网关创建得到 409 `takeover_approval_required`；页面/接口不显示发布成功 | **BLOCKED**：缺完整 typed 发布 worker/工件事务/终态补偿 |
| AC-18 节点恢复/主控灾备 | B06 真实备份校验 + 恢复预演仍缺目标身份/fence；**B06n** 启动第二隔离 Controller（独立 DB/PKI/端口），其 `initialized=false`，第一主控凭据登录第二主控 401、第二主控 nodes 401，第一主控身份保持 | **BLOCKED**：第二主控隔离宿主已实测；真实新权威/旧主控 fence、撤销不复活仍缺 CX/PKI-OPS |
| AC-21—24 Xray | B03 目录刷新 501 `PLUGIN_CATALOG_SOURCE_UNAVAILABLE`；插件中心新增专门说明且无可执行安装路径 | **BLOCKED**：缺可准入安全工件与 Runtime；未运行任何未审查工件 |

## 判定口径

- 只有真实页面/统一 HTTP + 真实任务/Agent/进程终态才算 PASS；202、queued、check、preview 不算成功。
- 明确预期拒绝只在对应负向步骤通过；500/401/403/格式错/超时按 FAIL。
- 本文件不改变 `ACCEPTANCE.json` 的 `NOT_ACCEPTED` 总状态，只提供本轮增量证据。