# R19 发布 DNS 父计划、统一审批与逆序补偿

V09-CLOUD-CODE-COMPLETE-01 / chatgpt-takeover-01。集中代码候选，**UNTESTED**：没有编译、运行测试、访问真实供应商或取得新的 CI 认证。沿用 R17 私有 profile 与凭据 scope；追加 `ds/0082_publishing_dns_tasks`（预计正式迁移 38 项），不改历史 SQL/hash、原始资料、旧 OpenAPI/Proto/gen、依赖锁或 CI。

## 实际执行边界

在真实网关生产组合且启用私有 ACME/DNS profile 时替换 DNS stage。由实际发布计划冻结当前服务组 revision、Zone、域名、所选网关实际 IP（A/AAAA，去重排序最多 8）、复用记录完整本地快照、profile 和原始创建身份；规范 SHA 是不可变 batch ID。非 IP 网关地址拒绝，不暗中用 DNS 或环境代理获得新目标。

发布通过实际 Controller 权威的 `publishing.dns.apply` 修改类任务，必须 Web 当前管理员与真人审批。每条记录的公开内容也列入冻结任务参数；worker 要同时匹配 owner batch 的实际 task/head-plan、当前统一执行、当前父计划/资源/服务组 revision、域名所属 Zone、固定 credential revision、当前实际网关地址。普通 HTTP/AI/plugin 无此执行 scope；借用操作仍有单次凭据事务和每次 I/O 复权。

每条 create/delete 先同审计提交意图，返回实际 provider ID 和本地持久字节快照后再记录结果。复用记录只读核验，不能写成当前发布新建。任务成功与 owner applied/rolled_back 必须同时成立，不能从 Task ID、排队或单个接口返回推断成功。明确无副作用的供应商拒绝记 FAILED；凭据借用执行后提交不明同样归 UNKNOWN，不能包装成普通 provider 错误重试。未知意图、丢失提交或进程重启均不自动重放；重启同审计把 running/rolling_back 留为 UNKNOWN，并保留已观察 ID。一个发布只有一个不可变 batch/执行与补偿任务，不随重复“继续发布”新增副作用。

## 等待与真实逆序

DNS 子任务等待批准或执行时，run 返回 202 和持久 running stage 的 typed dnsBatchId/taskId 回执。下次明确“继续发布”读取同一子任务；只对该受控 deferred DNS stage 允许继续，其他未知 running stage 沿用拒绝。旧同步返回和旧 provider 组合保留；没有 ACME/profile 时不会自动启动新外部路径。

回滚使用独立 `publishing.dns.rollback` 人工审批任务。必须原 apply 已终态，且其他已产生副作用的后续阶段均真实 rolled_back；按记录逆序只删除该 batch 确实创建且本地快照仍完全一致的 ID。reused/pending/deleted 不删除；无 ID、未知创建/删除、记录漂移均保留未决，不用“名称一致”猜测删除。rollback-all 先网关、再证书、最后 DNS，任何未知/失败/等待均不把上游阶段伪标已回滚。当前执行补偿者从发布服务私有上下文传递，不能用 HTTP 自称原作者。

## 供应商操作前读取的边界

Context7 当前没有可调用的专用工具，本次只读核对 Cloudflare 官方 [GET 单记录](https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/get/) 与 [DELETE 单记录](https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/delete/) 文档。既有固定 Cloudflare 客户端新增按同 Zone/ID 读取 type/name/content/proxied/MX priority，复用及删除前与本地关键身份和值比对。字段缺失/错误或 provider 漂移拒绝删除；该读取也必须当前 task/claim 与单次秘密借用。未新增 SDK/依赖，未调用真实账号。

这不是供应商原子 compare-and-swap：不编造 If-Match/事务承诺，不能据此声称消除其他外部操作者在 GET/DELETE 间的竞争。需要当前私有 profile 的受控写入与操作员对外部修改的实际协调；本地分片锁和 batch 持久意图不等于多主 fence。实际外部并发/物理双机证明仍 OPEN。旧测试 provider 端口保留；正式 Cloudflare 使用新增复核路径，不能通过伪造 GET 结果让测试绿灯。

## 正式界面与验收

新增只读 GET `/api/v1/publications/{publicationId}/dns-tasks`，须当前管理员、发布及子任务读授权；仅公开目标、task/plan 摘要与逐条观察。正式发布详情展示同一 batch、执行/补偿任务、pending/created/reused/deleted/unknown，并提供统一任务审批入口和逆序补偿按钮。202 不是成功，TTL 不代表全网即时撤回。

统一验收仍待：真实 provider 正/负/未知及实际字节、审批撤权/过期/credential revision/Zone/网关地址与组 revision 漂移、复用保留/重复继续、创建中/删除中重启、跨任务伪造/跳过后续阶段补偿拒绝、记录外部修改和缺失 GET 字段、审计提交失败，以及真实网关/证书/HTTPS/逆序完整组合。G2、B03—B06、完整 v0.9 与发布准入不因写入此代码关闭。
