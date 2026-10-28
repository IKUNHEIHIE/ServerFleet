# G2 开发 Agent 工作边界

1. 原始 `../ServerFleet-API-Contract-v0.9.yaml` 与原 G1 Accepted ADR **不可在此阶段擅自覆盖**；G2 的 `*-candidate.yaml` 明确是待冻结草案。
2. 多 Agent 并行工作前，先分配文件所有权与设计负责方向：Auth/RBAC、Agent/Proto、Tasks/SSE、Audit、Plugin/Xray。各自只能在受控业务接口调用底层能力。
3. 所有改变真实节点的操作必须经 Controller 权限、任务和审批；禁止独立 SSH 管道、前端直连 Agent、插件直连 Agent 或在 root Shell 中隐藏授权绕过。
4. 每个 PR 需包含 OpenAPI/Protobuf 校验、合同测试、至少一个失败用例；`buf` 编译、WSS + mTLS、权限拒绝等证据未取得时，不把当前候选改为 FROZEN。
5. 对 G1-PoC 01～08 没有证据的结果必须标为 BLOCKED/UNTESTED，不补写虚构指标或成功截图。
6. 对旧 API 的修改须形成显式迁移兼容方案，包括 `/tasks` 状态机、Cookie CSRF、`Error`→RFC9457、修订号冲突；不能静默更改已生成客户端的行为。
7. AI/插件操作要继承用户权限、资源快照与系统强制审批。AI 的限时“完全访问”不豁免任意 root Shell 的人工批准。
