# 受限Bearer迁移候选（未挂载主入口）

实现为auth自有持久opaque token；不采用旧JWT示例。原token256位，仅签发响应一次交付，存储SHA256摘要和管理ID，metadata/list/audit不含原文。丢失响应需要撤销重发，不假装可恢复原文。

精确端口为`IssueRestrictedToken(ctx, sessionSecret, csrf, RestrictedTokenInput)`、ListRestrictedTokens、RevokeRestrictedToken；输入name/purpose=script-read/actions/nodeIds/ttlSeconds，期限60–3600秒、动作只nodes.read/tasks.read、1–200个显式节点。普通用户可签自己的当前授权子集；管理员不自动得节点；token不签发给AI/插件，不带初始化/用户/审批/执行/Host/解密权限。

候选HTTP由`IdentityHTTPConfig.EnableRestrictedTokens=true`显式注册：GET /api/v1/auth/tokens→200 {items:metadata[]}；POST同路径→201 {metadata,token}（严格JSON、当前Cookie+CSRF、只交付一次）；DELETE /api/v1/auth/tokens/{tokenId}→204。缺/坏Cookie401、越权403、坏输入400、源故障503，no-store。它们是独立新契约候选，不改字节保护的原始OpenAPI，也不声称已有生成客户端。

消费端先`RestrictedBearerCredential(request)`：直接TLS、GET、唯一标准Authorization、无Cookie/Origin/Fetch/CSRF浏览器头，拒绝空/多/混合/非opaque；仅获准读路由显式选此分支。然后`AuthorizeRestrictedToken(ctx,secret,action,exactNodeIds)`在同一auth状态中查摘要/期限/撤销/user epoch，求token动作和节点上限∩当前角色∩grant∩当前组成员；返回不同于Cookie Identity的BearerIdentity。输出先缓冲，再BearerCommitGate重复求交且核对观察世代。不得仅提取SubjectID调Authorizer来丢掉token上限，也不能以请求nodeIds扩大token。批量列表只能以token当前有效节点集准备库存请求，零授权仍空集；如需该专用prepare适配应与DS/GM一起审查，当前未接线。

口令变更/降权/停用导致epoch失效；单token撤销不依赖浏览器登出。认证观察保存ClockFloor，观察过期后时钟回拨失败关闭。state/schema仍为auth候选，正式迁移注册/安全政策/接口接收由总控决定。没有默认启用Bearer兼容，也没有将旧JWT与新Cookie双读为等价身份。
