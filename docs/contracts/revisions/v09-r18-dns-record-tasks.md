# R18 DNS 记录修改统一任务

V09-CLOUD-CODE-COMPLETE-01 / chatgpt-takeover-01，集中编码 **UNTESTED**。接 R17 私有 profile、凭据借用与实际 Controller authority；没有新增依赖/迁移（仍预计 37 项），旧接口/Proto/gen/SQL/hash 保留。尚未编译、测试或访问真实供应商。

新增 POST `/api/v1/dns/record-tasks`。严格字符串对象：create 使用 operation/zoneId/recordJson/idempotencyKey，update 再含 recordId，delete 不含 recordJson、必须 recordId。recordJson 是公开 DNS 记录的严格 JSON 字符串，仅 type/name/content/ttl/proxied/可选 priority；DNS owner 自己的已有验证规则用于准入和执行，不重复发明宽松校验器。

任务 action=dns.record.apply、SourceWeb、真实 Controller target、modifying、强制人工审批。启动 profile SHA、当前 Zone/credential ID/revision、操作、当前 record 完整快照摘要和待写公开记录全部冻结入任务；record 的 4096 字节内容按有界公开编码片段存储，仍遵守通用每项 1024 字节、总 32 KiB 及 32 项限制。type/name 在计划中单列，与重组对象严格一致；worker 按精确字段数重组，不接受隐藏额外参数。

执行和每次 provider 借用/I/O 复核当前任务/审批/管理员/凭据/Zone；update/delete 还重读该 Zone 内同 record ID 的完整快照，变化拒绝。Controller 进程内固定 64 把记录分片锁串行同记录修改，防止默认修改并发提高后同时消费旧快照；不是灾备 fence/双主能力。DNS owner 继续审计实际变化，任务记录真实结果；只有显式无副作用拒绝为 FAILED，丢失响应/本地提交不明/撤权等 UNKNOWN，不自动重放。外部副作用仍需实际验证，不能把审批登记称作执行成功。

正式 DNS 页新增/修改/删除改走此任务入口，回执展示 task ID，明确人工审批与执行后刷新；检查/导入也改为 R17 Controller 任务。创建账号/更换凭据界面说明输入现有加密凭据 ID，而非把原始 Token 当 opaque ref。保留旧 API 兼容，但私有 profile 的 provider 不给直接 HTTP 旧路径秘密访问能力。

统一验收待执行：真实 provider 字节与本地记录/审计/任务终态一致，取消/撤权/过期或换版凭据/错 Zone/记录漂移/重复或超长 recordJson/伪片段/无审批/并发和重启未知结果；同账号不同 Zone 的错误 record ID 拒绝。旧 Web stub 断言如依赖“同步操作即已成功”必须按正式任务语义更新，不能删用例或造成功。发布 DNS 父计划/逆序补偿的任务接线仍独立继续，不借普通 Web 记录任务隐式放行。B03—B06/G2/外部与发布准入保持 OPEN。
