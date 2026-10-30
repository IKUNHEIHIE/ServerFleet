# V09-5D-CX / codex-02

**ACTIVE，完整v0.9尚未完成。** 用户要求持续完成CX-01—08；先前b61ad15“无独立工作”的停止判断已撤回。这里是当前状态，逐增量SHA/实际验证/历史失败见[RESUMED.md](RESUMED.md)；早期HANDOFF/EVIDENCE/COMMITS为历史快照，不能覆盖这里的新实现或当作最新全量检查。

基线/main `8a7dd4707d2d7e142e137d098296788d2c80eddd`；分支feature/v09-platform-cx02；实际目录 `D:\admin\desktop\codex\myproject\ks\.worktrees\feature--v09-platform-cx02`。用户消息中的外置ks.worktrees目录不存在，实际目录与任务单/Git一致。审查codex-review-01+安全/契约/owner。2026-10-30，Asia/Shanghai。

| 子项 | 当前实现与可复验证据 | 尚未交付 / 具体依赖 |
|---|---|---|
| CX-01 | 首接口检查点2fc8dab；独立认证RFC6902候选/兼容测试；S01—S04及各模块接线说明持续补充 | 认证/事务/tasks/Proto/WSS共享差异尚无总控接收SHA，未改受保护API/Proto/gen/根锁/迁移序列，不冻结G2 |
| CX-02 | 持久初始化/口令/限速/Cookie/CSRF/Origin/epoch/Tgate；未挂载真实HTTP handler含用户/角色/grants；同Tx审计；凭据密文/暂存/服务使用永久防重放；只读受限opaque Bearer；持久审批与完整任务消费绑定。实际SQLite/HTTPS、重启撤权/回滚测试 | 正式迁移/安全决策/唯一任务实际消费尚未接收；MFA政策与实现未完成（要求MFA即拒绝启用）；凭据轮换/撤销任务与页面、scope/approval实际接线未完成 |
| CX-03 | TLS1.3+mTLS当前指纹/吊销；ControllerLinks及Agent连接循环；一次性token/CSR审批/签发/重装撤销持久化。真实WSS主动重连/Proto/真实Ping-Pong、证书TLS与SQLite重启证据 | WSS生产依赖、PKI profile/轮换/信任包/Node同Tx接线未接收；无完整安装器与实际Agent运行profile，CX Linux实机未验收 |
| CX-04 | Agent副作用前claim/start持久化、去重/TTL/取消/live对账/UNKNOWN；输出落盘前强制注册过滤、持久生产序号/有界日志；实际WSS→临时文件→脱敏日志→重连/部分丢帧实验。审批外层rollback/重放负向 | DS Command缺完整Offer字段、ApprovalConsumer缺source/AI session等完整绑定、SubmitTx及日志序号/补读/ACK未接收；root/Shell/插件准入未获批，真实Linux统一链未交付 |
| CX-05 | 持久AI零资源/逐动作grant/组快照/三级模式；消息幂等/生成前持久fence/UNKNOWN不重试；真实SQLite+Cookie HTTPS SSE/游标重连/撤销关闭；工作流可信触发/冻结顺序任务/停用/UNKNOWN暂停；Eino隔离PoC | 实际BYOK/model/生产内容过滤器、公开消息POST/list适配/页面、统一任务/审批与通知接线未完成；推理/过滤器正向仍为明确double，Eino未定生产框架 |
| CX-06 | 签名源/清单/声明UI/私有工件/持久生命周期；默认disabled、严格升级consent、独立实例ID/晚到隔离/禁用先封Host/未知不卸载。WASM PoC。可选Xray两模板与自定义JSON最小安全审查，在固定26.6.1实际-test/启动/代理不能访问私有API；流量段纯对账 | WASM/Host准入与正式来源/密钥未接收；Xray Linux双架构安装/接管/发布/配置历史/用户导出/实际Stats采集/持久业务未完成。最小审查不代替目标-test；核心未编入Xray |
| CX-07 | 更新签名/序列/平台/哈希、私有暂存同FD复验及一次借用；恢复连续Root/撤销并集/在途UNKNOWN计划；新增数据库外永久hold/当前verifier闸。真实SQLite旧快照仍不能清除外部隔离 | UPDATE/PKI新决策/离线解密材料/旧主控隔离/实际恢复事务未接收；安装/health/失败回退与完整灾备尚未交付。无默认放行verifier |
| CX-08 | Controller/Agent骨架及四个Linux交叉编译脚本，构建/组合/恢复接缝说明 | main尚无新模块接收SHA，未在入口组装未接收业务；正式中间件/路由组合、Agent安装与完整发行物尚未完成 |

最新实现检查点 `f179f7eb57692ae6ccdef058ca7ae1f569eb00e6`（R21 Stats），R20为bfb8ab3。R17—R19全量check五套PASS（07:23:09Z）/security PASS（07:23:18Z），四个Linux骨架交叉编译与隔离依赖扫描PASS；R20—21模块/架构/staticcheck PASS，未用旧全量检查覆盖后续代码。每次增量只提交自己目录，精确命令/结果/阻塞见RESUMED，继续包内下一项。

**Xray外部发布物安全FAIL**：26.6.1符号扫描55项；正式新版26.10.29独立下载/双哈希核验后扫描3项，未执行新版。两份均不得发布/安装。R21真实TLS/Vision Stats/进程重启功能实验证据保留，不当安全验收。精确pin/发现/复查命令见XRAY-ARTIFACT-SECURITY.md；生产Stats typed telemetry/持久历史仍未接收，不能通过TaskOutput/float64绕接缝。

实际SQLite组合固定DS源`bb8954988fe7b2f832edf291803529d7745e4f0a`，单独module/sum，不是共享接收SHA。十二项PASS（最新8.869s），含旧库恢复仍隔离、AI消息/真实HTTPS SSE、重启撤权、域/审计回滚、缺接口拒绝。模拟Gate/Runtime/Task/Model明确记录。R19真实WSS/Agent隔离包PASS 9.323s，业务准入仍fixture。npm ci/tools:install本轮已PASS；最新全量检查覆盖R19，后续增量另验。根security无漏洞不覆盖可选外部Xray二进制。

总控已确认98f6d94d仅依赖边登记且已在基线；RecordTx/扩展Request/DS新SQL接缝不是共同接口。四份PRIV/WC/PKI-OPS/UPDATE完整正式原文缺失，只提交独立PROPOSED决策，未补写历史。缺项只暂停具体接线，继续可独立实现及本地负向验证。没有访问生产/VPS/云/真实凭据、远程推送、合并main或启动其他Agent。

AC-01—24与完整PoC仍未验收。Windows局部实验/交叉编译不能替代Linux amd64/arm64、1核1GB、外部供应商或真实灾备证据。最终停止必须给最新HEAD、对应检查及未完成清单；当前不声明完整v0.9交付。
