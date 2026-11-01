# 完整v0.9完成声明 · 独立核验

日期：2026-11-01（Asia/Shanghai）；任务 V09-INDEPENDENT-COMPLETION-AUDIT；审查 codex-review-01；代码基线 **2da203cac40ccc2bfc4c7bb644978005cd99dab1**。本报告只在独立审查分支保存，不修业务、不改原main、不覆盖接任者/作者工作目录。

## 结论

**不能确认完整v0.9已完成，不能据现有证据验收为生产可交付版本。可以确认本地Wave 1—5有大量真实实现、主控/数据库组合和自动化验证；复跑的测试结果与接任者报告一致。**

负责人之前明确选择完整v0.9，不是仅完成本地阶段任务。接任对话0814494e-ab0a-4ec0-b1be-89fc17c58623的本地只读记录（回复步骤1012、1010）将完成范围限定为“当前本地全栈工程”及Wave 1—5，并列出后续外部动作。该阶段完成与完整产品交付不能等同；此外，下述问题中有实际代码/运行接线缺口，并非只缺真实供应商凭据或一次上线验收。

`ks`与对话中导出的`ks-clean`在审查时均为上述同一HEAD且跟踪工作区干净；打包副本没有提供另一份更完整实现。Codex对话工具不支持该外部会话，实际通过用户指定UUID对应本地SQLite只读提取少量相关答复；未修改会话、未向其他Agent发消息，未保存整份私人对话。

## 独立复跑

隔离目录`.worktrees/integration--v09-independent-completion-audit-cx01`，其审查登记提交5ec3500仅比精确代码基线增加一行任务登记，业务/契约/配置/测试源码与原main一致。固定Go1.27.3、Node24.22.1、npm依赖安装成功；测试使用合成身份/新临时数据库/回环HTTPS，不连接VPS/云或使用真实外部秘密。

| 实际执行 | 结果 |
|---|---|
| npm run check中的policy/contracts/web/git | PASS；69原始文件/3字节精确候选、边界/契约/生成验证通过；Web37文件/329项PASS |
| 首次npm run check的go组 | FAIL，go-mod-verify访问proxy.golang.org连接超时；此轮未进入Go测试，不能称首次全套成功 |
| npm run check:go重试 | PASS；从接任者已存在的本地模块下载缓存读取固定版本，未关闭校验；build/test/vet/staticcheck通过，636顶层+1134子测试PASS，0FAIL |
| Go跳过项 | 9项：2项Windows符号链接限制、2项Linux权限验证、1项Linux CLI闭环、Cloudflare真实服务、3项真实Xray；SKIP不算PASS。本轮未重跑原报告的WSL闭环 |
| npm run check:security | PASS，npm audit 0漏洞，govulncheck无已知漏洞；覆盖根依赖，不覆盖外部Xray发行物或所有业务安全缺陷 |
| 真实Go Controller浏览器验收 | 独立复跑70/70 PASS；生产Web构建+实际Go Controller+SQLite+HTTPS。脚本同时验证成功与正确拒绝/不可用，见下节 |

首次新建审查worktree尚在checkout时提前登记，遇到index.lock；等待创建会话完成后正常重试，无删除锁/重置/覆盖。Go初次网络失败、重试输出及9项SKIP均保留在忽略reports。浏览器使用独立profile，输出位于`reports/completion-audit-live/`，进程由脚本关闭；`--keep`保留本次合成临时数据供复验。

## 阻止“完整完成”的具体证据

| 项 | 当前可复核事实 | 完整需求差距 |
|---|---|---|
| 默认凭据密钥 | `apps/controller/server/server.go:504-509`给CredentialService固定`local-default`及写在代码中的32字节密钥；`internal/auth/credential_cipher.go:35-39`实际据此构造AES-GCM；借用操作门又统一拒绝 | 生产秘密的密钥来源/安全持有/轮换和真实供应商借用尚未接通；不能携该默认配置上线保存真实凭据 |
| Agent与任务执行 | `apps/agent/runtime/runtime.go`只暴露受控单次`-connect-once`；`apps/agent/connect.go:214-253`唯一动作tasks.command只接受空JSON并执行固定self-check；`apps/agent/executor.go:105-116`明确未准入root/Shell | 已证明连接/任务回执机制，尚无常驻节点管理、通用批准命令/修改类执行、相应实际系统隔离与双架构安装验收 |
| 跨节点文件 | `internal/files/local_transport.go:19-21`说明LocalStagingTransport仅本地/离线组合；默认写主控暂存目录。新浏览器第65步仅证明该目录内字节/哈希持久化 | 尚无经真实Agent在两台服务器之间完成文件传输的交付证据，不能把本机按nodeID分目录等同远端文件通道 |
| AI模型调用 | `apps/controller/server/ai.go:33-40`的Profile与Generate回调固定返回ErrModelUnavailable，setupAI不消费可用模型适配配置 | 会话/授权/审批/消息机制已做，实际AI推理和BYOK主线接线未完成 |
| Xray/插件 | `internal/plugins/xray_http.go:34-50,239-267`将安装、校验、部署、用户修改、客户端导出等绑定不可用处理器，启用后也返回501；主控LifecycleManager传入nil runtime工厂 | 可选Xray完整安装/配置/用户/流量等并未通过真实运行链路；现有模板/格式/Host组件不能代替主线功能。XRAY-ARTIFACT-SECURITY.md仍记录两份候选发布物扫描FAIL，本轮没有新准入工件 |
| 更新/恢复 | `internal/updates/http.go:207-220`的apply无条件返回501 UPDATE_INSTALLER_UNAVAILABLE；`apps/controller/server/updates_recovery.go:32-54`未注入更新Source/Planner或Recovery.Execute，恢复创建被阻止 | 已有验证/计划/文件安装组件与页面；真实更新安装、进程监管/恢复执行、完整隔离灾备尚未交付 |
| 发布/网关/证书/DNS/通知 | 主控消费可选cfg.DNSProvider/GatewayChecker/CertificateIssuer/PublishingBridge等，默认缺少实际对应适配；发布LocalBridge空回调失败关闭 | 主线真实供应商/网关安装/证书签发/通知送达和发布/补偿尚未整体完成，不应仅列为“不影响完成的外部动作” |
| 完整验收/准入 | `ACCEPTANCE.json`24项仍NOT_ACCEPTED/evidence空；Wave5审查末尾保留G2/D01—D07/P1-C10和运行时选型OPEN，双架构实机/1C1G及多项PoC缺实际交付证据 | 注册表可能落后于局部实现，不能据此认定全部没做；但仍缺逐项完整验收证据和明确发布准入 |

以上失败关闭/未准入控制本身有安全价值，本报告不要求放宽它们。需要补齐实现、适配与安全验证后再开放；接口可响应、页面能显示错误不等于对应功能可用。

## 70/70的实际含义

本轮日志可复核：第48步断言插件目录刷新501，第49步断言Xray未安装门控，第50/51步断言更新源/规划器503，第54/55步断言恢复预演失败及禁止提交，第66/67步断言发布/回滚502。它们是有效的负向测试PASS，**不是插件安装、系统更新、灾备或服务发布成功的证据**。测试脚本自己说明未注入的适配器需诚实失败，不能将这70步统称为全量产品功能成功。

## 完成口径建议

当前可标为“本地集成候选版本，Wave 1—5阶段验收通过”。正式“完整v0.9完成”应以原24项产品验收矩阵逐项闭环为准：补齐上述主线代码与受控Agent能力、生产配置/密钥与外部适配，取得双架构实机/供应商/插件/升级恢复及必要PoC证据，并收口契约与发布准入。不能只把测试数或打包导出视为完成率，也不应给出没有权重依据的百分比。

机器摘要与原始输出索引见本目录`V09-INDEPENDENT-COMPLETION-AUDIT.json`；本审查没有改动原main或另派实现任务。
