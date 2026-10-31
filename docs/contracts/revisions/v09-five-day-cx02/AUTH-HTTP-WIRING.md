# R02 候选接线差异（待总控/安全/DS/GM接收）

不改根受保护OpenAPI。S01 auth-session.patch.json仍为独立迁移候选；本轮未挂主入口。新增auth自有实现的精确文件为internal/auth/{identity_sql,management,idempotency,session_views,http}.go，ports.go不变；已有Authorizer由IdentityService实现，但SubjectID仍必须来自真实认证边界，不能自认证。

## 组合输入

1. `NewSQLIdentityStore(IdentitySQLBackend)`：Read为单条auth表查询的Scan(payload,ever_initialized)，InTx为DS单次BEGIN IMMEDIATE回调，Exec返回RowsAffected；实际映射在tests/poc/v09/platform/identity_sql_test.go.txt。业务DDL由IdentitySchemaStatements返回，交总控注册不可变迁移编号，不由构造器自动建表。缺表/缺行/损坏/初始化标记倒退均503。
2. `NewIdentityServiceWithResources(store, ResourceResolver, []PermissionDefinition)`：资源解析来自受审node/node_group/service_group来源，每次返回当前精确节点集合；新grant冻结当前成员，使用时求交，禁止wildcard。服务只拥有auth数据，不读nodes私表。nil解析器创建/使用相关grant失败关闭。权限注册由服务器程序配置，客户端不能声明只读/管理员。
3. `NewIdentityHTTP(service, IdentityHTTPConfig{Origin})`返回独立handler；直接HTTPS，不能仅凭X-Forwarded-Proto推断HTTPS。主控应在独立审查后挂该handler；DS authenticator可适配`AuthenticateRequest`返回的SubjectID，不能读X-User/X-Role。当前入口仍health，不消费未接收模块。

4. R03新增`IdentitySQLStatements.AppendAudit(ctx, []IdentityAuditEvent)`：闭包必须绑定相同DS Tx，逐事件SQLRecorder.AppendTx后返回，不能开启嵌套writer事务。结果固定映射success/denied/failure，Source为web/system，RequestID来自服务器上下文，NodeIDs为授权冻结目标。auth出箱清除与auth状态写入、审计插入都在同一事务；任一失败全部回滚。映射实测代码在identity_audit_test.go.txt。nil不丢出箱，仅保留待消费状态；生产是否允许deferred模式须审查，不伪称审计域已收到。

## 已实现操作和兼容语义

- GET bootstrap-status：initialized、bootstrapAvailable仅在setup存在且未过期时true；绝不返回setup秘密。
- POST bootstrap：要求Idempotency-Key、setupToken/username/password；只原子初始化一次；同键原请求在原setup期限内可确认204，无Cookie。初始化后普通登录。
- POST login：有界严格JSON、单Cookie可替换旧会话，Bearer/旧Cookie/混合/重复均拒绝；秘密只Set-Cookie，JSON为identity/sessionExpiresAt/csrfToken。
- GET me：保留userId/displayName/roles/permissions/isBootstrapAdmin，加csrfToken仅按S01新schema；内部Identity/epoch不直接JSON输出。新晋管理员不标为bootstrap管理员。
- POST logout、GET/DELETE sessions：当前认证/Origin/CSRF/所有权；先提交撤销再清Cookie，不因清Cookie就声称撤销成功。
- GET/POST users、GET/PATCH users/{userId}：管理员管理、本人详情可读；PATCH强If-Match；角色/停用变化递增epoch并阻止停用最后管理员。创建要求本地密码；password在旧草案中虽可选，本地认证不能无密码建可登录用户，此差异待接收。
- GET roles/permissions：返回服务器内置能力目录，无任意角色表达式/通配符或客户端风险分类。
- GET/POST resource-grants、DELETE resource-grants/{grantId}：普通用户只能看自己的grant；管理员创建/撤销；节点/组/服务组精确范围，过期/当前成员求交；receipt不重建已撤grant。

列表按授权后的可见集合分页，limit 1–200，游标只在同一可见集合查找，不作为凭证；no-store。认证源故障503、凭据401、权限/Origin/CSRF403、输入400、CAS412、缺If-Match428、幂等改参409、登录限速429，全部Problem固定类别、不含源错误文本。读取缓冲后CommitGate复检身份/epoch/auth策略世代，最终检查后的网络窗口及外部成员世代仍不能声称已消除。

请求receipt上限5000、1小时，bootstrap确认限原setup期限；用户/会话/审计出箱有界，整体auth状态16MiB。当前aggregate持久化是真实实现，不是生产性能结论；需1C1G压测后决定是否规范化表/索引。MFA要求或非空第二因素码在验证器未配置时失败关闭，未交付MFA启用流程。实际使用/审查状态与未完成清单见progress/codex/RESUMED.md。
