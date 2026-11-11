# R31 产品、Web 与 API 同步

任务 V09-CLOUD-RENEWAL-INSTALL-01；从 main b3d13b77 接续。新增独立候选，原 G2 与 R01—R30 契约不覆写，不宣称 G2 冻结。

| 类型 | 产品/Web | API/CLI |
|---|---|---|
| 新增 | 续签安装发布配置、进度、任务入口与最近50周期历史 | GET/POST /api/v1/certificates/public-renewals/{policyId}/delivery |
| 新增 | 停止周期人工修复采用、UNKNOWN 独立结案校验 | POST 同一路径 /recover |
| 新增 | 添加节点可信一键命令、服务账户、Root 指纹、隐藏令牌输入、等待审批与续装 | GET /api/v1/installations/catalog；controller install |
| 新增 | 离线签名覆盖 Controller/Agent/组件清单/Web | controller release package-installation |
| 修改 | 续签策略实际显示自动安装和编排状态 | automaticInstallation 从恒 false 改为 boolean；state 新增 renewal_delivery_issuance/installation/publication/blocked/completed/recovered |
| 修改 | 发布等待真实网关任务，可观察确定性子任务 | 内部证书替换后继沿同一路由发布，不再重复 DNS；禁用删除式补偿 |
| 删除 | 无 | 无 |

新增契约 api/openapi/revisions/v09-r31/delivery-candidate.yaml，三个路径、四个操作。均为当前管理员、TLS、会话；POST 还需 CSRF 与 Origin/请求标记，严格 JSON、双版本 CAS、无未知字段。任务与证明每次经过当前读取/执行门，管理员身份不等于节点执行豁免。

GET catalog 未配置返回 available=false，不虚构可用下载地址。返回仅有公开 CA、公钥、Root 指纹、HTTPS 固定地址与摘要，不含令牌/私钥。目录需主控启动时私有 0600 文件配置，架构仅 Linux amd64/arm64。引导程序摘要与信任公钥仍需独立可信核对。

delivery 默认 revision=0、disabled。POST 字段 expectedRevision、policyRevision、enabled、publicationIds（≤8）。活动周期只允许停用并保留原绑定；配置漂移必须先人工修复。周期字段保留原 issue/install/publications 与实际错误；恢复额外保留 recoveredOrderTaskId、recoveredInstallTaskId、recoveryPublicationIds、recoveryIncidentIds、recoveryNote、previousErrorCode。最小任务回执不含任务名称、参数、输出或秘密。

recover 必须 confirmed=true，note 非空且≤4096字节，incidentIds≤66。新成功证明必须同 CSR/节点/私钥/zone、当前 profile、延长有效期，不能早于原周期签发；发布须已完成且实际使用新安装。所有旧任务终止，UNKNOWN 必须当前独立复核结案。无自动重试；恢复默认 enabled=false 由 Web 显式选择。

追加两项迁移，当前45；不改任何旧SQL、哈希或生成代码。无新依赖。测试结果以最终 PR 精确 head 的 CI 与评论为准，源码完成不替代真实供应商、系统安装与用户联调。
