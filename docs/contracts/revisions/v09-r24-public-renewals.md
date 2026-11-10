# R24 公开证书续签策略与审批任务

2026-11-10；V09-CLOUD-CODE-COMPLETE-01 / chatgpt-resume-20261110。从远端最新 `ca309ea` 在独立 `feature/cloud-public-renewals-20261110` 继续；保留原分支后续13项提交、R20—R23和全部历史验收，单会话编码。代码、正式 Web、产品和独立 API 候选同步；全部 **UNTESTED**。只格式整理及源码复核，没有编译/测试/CI/真实供应商证明。追加 `cx/0020_public_certificate_renewals`，预计正式迁移 40 项；旧 SQL/hash、原始资料、旧候选与 Proto/gen 不变。

## 产品与交互

证书管理的公开证书页新增“公开证书续签”。管理员用成功签发任务和同一公开链的成功节点安装任务登记策略，配置到期前 1—60 天。列表只投影当前 owner、当前可读原任务的条目，展示节点/域名、到期/调度时间、版本、续签任务及真实状态。没有默认启用的策略，一节点/密钥槽只允许一条，部署上限 1000 条。

到期每分钟检查只创建统一 `certificates.public.issue` 修改类任务，明确 SourceSystem，仍需真人审批和当前节点授权。该任务复用公开 CSR，不获取或复制节点网站私钥。任务已完成只代表签发，不代表安装、Nginx 重载或 HTTPS 生效；用户从原签发面板读取公开链，使用既有链安装入口完成独立人工审批安装，再“接续已安装的新证书”。必须恰为该策略上一周期创建的任务，节点/域名/密钥槽/CSR/Zone 一致、到期时间延长，才能清空当前任务引用进入下一周期。网关证书引用更新继续走发布，不对既有网站静默改链。

策略调整使用明确 expectedRevision；重复旧请求返回 409。已有周期任务时不得通过调整、停用/重新启用、改天数或时钟周期生成新任务；失败、取消、过期和 UNKNOWN 必须人工核对，不自动重试外部副作用。停用保留当前任务引用，每次供应商 I/O 再检查策略 enabled/revision/owner/base/profile。已发出的供应商请求可能已生效，停用不是回滚，原任务和清理观察继续保留。

## 安全、持久性与恢复

原签发必须 completed/target succeeded、owner 公链 issued 且清理为空；独立安装任务也须成功，receipt 的 plan/node/CSR/domain/key/chainSHA/到期与原签发逐项一致。当前管理员、原任务读授权、DNS Zone/credential revision 和运行门持续核验。system 仅允许当前策略绑定的六字段计划，核对其 owner、版本、上一订单、CSR、Zone、profile、单节点和确定性幂等键；普通 Web 的三字段路径保持，AI/plugin/其他 system 动作不借此获得执行 scope。审计记录保留真实来源。

每周期幂等键只依赖策略 ID、版本和上一订单，不取当前日/重试次数。提交与 owner 绑定间断线，恢复时只查询或提交同一幂等键得到原任务；调整策略前也先查询该缺口，防止漏绑定后绕过 UNKNOWN。owner 更新/任务绑定与审计分别在同一事务，审计失败不能放出无记录新 owner 状态。恢复 reconciliation 强制停用所有恢复策略并递增版本，原在途任务仍由既有 UNKNOWN 对账处理，离线恢复不能重新激活旧任务权限。

## 接口与兼容

新增 GET/POST `/api/v1/certificates/public-renewals`；POST 为严格七字符串对象，4 KiB 上限，TLS/CSRF/当前管理员及 owner 限制。配置/接续不调用外部供应商；GET 不接受 query/body。独立 R24 OpenAPI 登记，不改 R17 的普通 Web 签发请求；内部 system 计划新增 renewalId/renewalRevision/previousOrderTaskId 三项，权限边界仍在 Controller 内部。

待统一验收：正确窗口/时区、重复调度/缺口重启、审批过期/撤权、停用与在途 I/O、配置/凭据/Zone 漂移、跨 owner/node/chain/CSR 伪造、失败/UNKNOWN 不重跑、审计回滚、恢复后策略失效、安装/接续及真实发布切换。已新增 owner/槽位不可变、CAS、任务绑定、事务回滚及恢复失效专项源码，尚未运行。完整自动安装/发布编排、未知结果人工恢复流程及真实外部验收仍 OPEN；此增量不关闭 B03—B06/G2/发布准入。
