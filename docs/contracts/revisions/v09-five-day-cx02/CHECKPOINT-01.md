# CX-01：可单独接收的接缝候选

2026-10-30；codex-02；基线 8a7dd470。此提交只包含候选和精确共享变更请求，不冻结 G2，不改 API/Proto/gen/锁/公共 ports。

## 操作级约定

| 现有操作/接缝 | 实施建议与所需变更 | 失败例 |
|---|---|---|
| GET /api/v1/auth/bootstrap-status | 仅 initialized 布尔值；no-store；不返回安装秘密 | 仓储故障 503，不能解释为未初始化 |
| POST /api/v1/auth/bootstrap | 本机独立交付 32 字节 setup 秘密、10 分钟单用；原子消费+创建管理员+永久 initialized；完成后普通登录 | 并发第二次拒绝；回滚不得消费秘密或留下半用户；空库不重开 setup |
| POST /api/v1/auth/login | 固定 HTTPS Origin；账户和来源分别限速；密码 KDF；新建不透明服务端会话；Cookie 只放秘密，响应含非秘密身份/到期/CSRF | 未知/停用/密码错同类 401；来源不可用 503；429 不运行 KDF |
| POST /api/v1/auth/logout | 当前身份+Origin+CSRF；先提交撤销再清 Cookie | 双凭据/过期/CSRF错拒绝；写失败不得 204 |
| GET /api/v1/auth/me；GET/DELETE /auth/sessions[/{sessionId}] | 每请求查询当前主体/会话 epoch；sessionId 是独立非秘密管理 ID；禁止跨主体撤销 | 跨主体 403；来源故障 503；secret 不进入 JSON |
| GET /api/v1/nodes（listNodesForTransfer） | 保持路径/items/onlineOnly；Cookie 支持待契约接收；认证→现有一次 Prepare→库存→缓冲→世代闸门 | 未认证库存 0 次；策略未知主体 403；已知零授权 items=[]；来源故障无部分结果 |
| POST /api/v1/tasks | 继续唯一 tasks 入口；服务端冻结节点/规范参数/摘要/来源/权限世代；提交和每节点派发重新鉴权 | 旧审批、扩展目标、撤权、参数变更拒绝；未启动 15 分钟 TTL；未知副作用不重跑 |
| GET /api/v1/tasks/{taskId}/events | 现有 SSE 游标语义；订阅及每次输出查当前授权/期限；持久事件后发布 | 游标缺口明确错误；撤权终止，不发送缓存队列；故障不伪造成功 |
| Agent TaskOffer/TaskCancel→TaskAck/Started/Output/Finished | 保持现有 Proto 字段号；Controller 在授权/审批/审计/持久任务后发送；Agent 二次能力/参数检查+先持久 executionId 再启动 | 重复相同 ID 不重跑；同 ID 不同摘要拒绝；未可靠完成 UNKNOWN；ACK 不等于成功 |
| AgentHello/心跳/重连 | mTLS 证书查权威 nodeId/serial/撤销状态，消息仅对账；transport 回调交 apps 组合再调 DS | 错 CA、吊销、证书与消息身份不符拒绝；恢复只核对不调度 |

错误机器码/媒体按 HTTP 提案分阶段协调：缺失/无效/歧义 401，已认证权限/来源/CSRF 403，后端不可用 503，输入 400；不根据底层错误文本映射，不暴露原因细节。no-store 适用于成功及失败。以上仍为候选，不能覆盖既有契约。

## 仓储及可信边界

Auth 自有记录：initialized、setup 摘要/期限、用户/密码 KDF/credentialEpoch、会话秘密摘要/管理 ID/主体/CSRF/期限/撤销、授权世代、登录失败窗口。DS 只提供事务机制，不读取/解释 auth 私表。事务必须串行化写入、错误/取消回滚、回调只调用一次，提交成功才交付秘密；重启保持初始化/撤销/限速。随机源/时钟/存储失败关闭。

实现先在 auth 模块内，测试仓储仅在 *_test.go。没有持久仓储适配时不把内存仓储接入 Controller。Tgate 检查凭据和策略世代，不能声称消除最终检查后至网络发送的窗口；需 owner 明确接收边界。

首个具体差异见 progress/codex/SHARED-CHANGES.md。缺正式 PRIV/WC/PKI-OPS/UPDATE 适用原文、WSS 依赖、存储/任务扩展与运行时 PoC；不阻塞 auth 自有组件、签名验证、隔离负向实现。外部端口的消费等待总控接收，常规模块内部代码继续。
