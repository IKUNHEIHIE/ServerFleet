# R37 身份生命周期与运维可用性

v0.9.1 返修当前身份续期、接入后的明确读取授权、升级诊断、通用就绪与节点诊断。既有 G1/安全 ADR、原 49 项迁移 SQL/checksum、Proto 与生成代码保持；新增节点诊断迁移只追加。产品版本发布不表示真实供应商、长期 VPS、root/双机灾备或完整 G2 已验收。

Agent 使用当前未过期 mTLS 叶证书和同一私钥 CSR 请求 `/api/v1/agent/identity/renew`，同一旧指纹绑定稳定 requestId/CSR 摘要，只产生一个后继证书。签发和持久化审计同事务；节点行政状态、当前身份、代次、Root 与公钥都不允许被输入改写。撤销旧身份会同时撤销其续期后继。签发丢失响应可重试；旧身份在后继真实握手后才进入 2 分钟退役窗口。过期不能换取新凭据。

Agent 对新证书验证节点、代次、公钥、Root、链和有效期，写私有临时文件、fsync、原子替换 issued.json；既有私钥和执行账本不变。临近到期重试最多每分钟一次，失败不截断当前连接。成功后拒绝新任务，等待活动执行归零并结束旧会话，用新身份重连；这不是未知任务的重跑通道。

批准接口新增可选 grantRead，旧客户端省略则保持不自动授予。Web 明确勾选后仅通过现有 auth.CreateEnrollmentReadGrant（复用既有授权引擎并保留有界永久关联） 给当前账户该节点的 nodes.read；不增加任务/提权权限或管理员通配符。批准已提交但授权失败时返回 ENROLLMENT_APPROVED_READ_GRANT_PENDING，可对同一批准重试；已撤销的 grant 不因重试复活。替换令牌绑定已有节点，另要求管理员具备 nodes.revoke 精确授权，禁止暂停/吊销节点自动恢复。

启动先预检既有身份和原 keyring，再应用迁移。upgrade-check 使用只读连接检查已知迁移 checksum、身份 Version1/2/初始化标记、目录权限、keyring 和已有密文认证；不生成新密钥、不写身份或迁移。实际未知历史 payload 应保留失败材料另行兼容，不猜测修复或静默清空。

节点诊断持久化最新已认证握手版本、受服务端约束的能力、证书到期、连接 ID 和每节点最多 100 条连接/断开事件；最新心跳仍来自 nodes.last_seen_at，不对每次心跳额外无限写历史。查询沿用当前 nodes.read；Recorded=false 不伪造版本或认证。到期提示不授予权限，日志固定分类，不记录秘密。

公共 /readyz 按已配置子系统检查，不以 Agent 数量/供应商未配置作为全局失败；原私有监督就绪通道保持原语义。访问日志用最终响应 request_id 关联路由、状态、字节数和耗时，不记录原始 URL 查询、IP、Cookie、Authorization、请求正文或 CA/密钥。

接口独立候选位于 api/openapi/revisions/v09-r37/identity-operations-candidate.json；部署与恢复步骤见 deployment/UPGRADE.md、deployment/REVERSE-PROXY.md。专项/精确 head CI 与发布回执在对应 PR 评论保留；不把自动检查当作真实旧库或长期运行验收。
