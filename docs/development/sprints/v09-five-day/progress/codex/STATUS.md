# V09-5D-CX / codex-02

**ACTIVE，完整v0.9尚未完成。** 用户要求持续完成CX-01—08；先前b61ad15“无独立工作”的停止判断已撤回。这里是当前状态，逐增量SHA/实际验证/历史失败见[RESUMED.md](RESUMED.md)；早期HANDOFF/EVIDENCE/COMMITS为历史快照，不能覆盖这里的新实现或当作最新全量检查。

基线/main `8a7dd4707d2d7e142e137d098296788d2c80eddd`；分支feature/v09-platform-cx02；实际目录 `D:\admin\desktop\codex\myproject\ks\.worktrees\feature--v09-platform-cx02`。用户消息中的外置ks.worktrees目录不存在，实际目录与任务单/Git一致。审查codex-review-01+安全/契约/owner。2026-10-30，Asia/Shanghai。

| 子项 | 当前实现与可复验证据 | 尚未交付 / 具体依赖 |
|---|---|---|
| CX-01 | 首接口检查点2fc8dab；独立认证RFC6902候选/兼容测试；S01—S04及各模块接线说明持续补充 | 认证/事务/tasks/Proto/WSS共享差异尚无总控接收SHA，未改受保护API/Proto/gen/根锁/迁移序列，不冻结G2 |
| CX-02 | 持久初始化/口令/限速/Cookie/CSRF/Origin/epoch/Tgate；未挂载真实HTTP handler含用户/角色/grants；同Tx审计；凭据密文/暂存/服务使用永久防重放；只读受限opaque Bearer；持久审批与完整任务消费绑定。实际SQLite/HTTPS、重启撤权/回滚测试 | 正式迁移/安全决策/唯一任务实际消费尚未接收；MFA政策与实现未完成（要求MFA即拒绝启用）；凭据轮换/撤销任务与页面、scope/approval实际接线未完成 |
| CX-03 | TLS1.3+mTLS当前指纹/吊销、ControllerLinks/Agent连接循环、接入审批/签发/重装撤销持久化；新增本地私钥/CSR/证书持久材料。真实WSS/Proto/Ping/重启/撤销组件已在Ubuntu WSL amd64原生运行 | WSS生产依赖、PKI profile/轮换/信任包/Node同Tx接线未接收；无完整安装器与Agent运行profile，独立Linux安装/arm64/1C1G未验收 |
| CX-04 | Agent副作用前claim/start持久化、去重/TTL/取消/live对账/UNKNOWN；输出落盘前强制注册过滤、持久生产序号/有界日志；实际WSS→临时文件→脱敏日志→重连/部分丢帧实验。审批外层rollback/重放负向 | DS Command缺完整Offer字段、ApprovalConsumer缺source/AI session等完整绑定、SubmitTx及日志序号/补读/ACK未接收；root/Shell/插件准入未获批，真实Linux统一链未交付 |
| CX-05 | 持久AI零资源/逐动作grant/组快照/三级模式；消息幂等/fence/UNKNOWN；未挂载POST/list有界分页及真实Cookie HTTPS SSE；新增BYOK文本HTTPS适配、凭据借用与当前持久推理闸，本机TLS+合成Key实测；工作流冻结顺序任务/停用/UNKNOWN暂停；Eino隔离PoC | 模型设置持久管理/正式外发政策/生产内容过滤器、公开路由与页面组合、统一任务/审批/通知接线未完成；实际外部模型/BYOK未验收，配置/过滤/模型部分仍明确double，Eino未定生产框架 |
| CX-06 | 签名源/清单/声明UI/私有工件/持久生命周期，默认disabled/实例隔离/禁用先封Host/UNKNOWN；WASM PoC。可选Xray两模板/custom最小审查/管理API隔离；历史26.6.1真实TLS/Vision与精确Stats读取/非复位/重启计数段验证 | 两份官方外部发布物安全FAIL，不得安装/发布；WASM/Host/来源/密钥未准入。Xray Linux双架构安装/接管/配置历史/用户导出/typed遥测与持久采样业务未完成；最小审查不代替目标-test，核心未编入Xray |
| CX-07 | 更新签名/序列/平台/哈希、同FD暂存一次借用；新增真实文件备份/切换/持久阶段与回执，UNKNOWN永久锁、明确健康失败+兼容性核验才精确恢复本次备份；恢复连续Root/撤销并集/数据库外hold闸，SQLite旧快照不清隔离 | UPDATE/PKI新决策/离线解密/旧主控隔离/实际恢复事务未接收；服务/health/审计提交适配仍明确double，Linux真实更新/迁移/回退/灾备未验收。无默认放行verifier |
| CX-08 | Controller/Agent骨架及四个Linux交叉编译脚本，构建/组合/恢复接缝说明 | main尚无新模块接收SHA，未在入口组装未接收业务；正式中间件/路由组合、Agent安装与完整发行物尚未完成 |

最新检查点 `ffc43cc293dc92bc696aaf1fa7df5b7cf40ab19b`（R28 Linux原生验证），R27身份材料d61e7a0；此前R26 bc97a05、R23更新c3d6eae、R24消息HTTP d61e563、R25 BYOK 6552197。全量check五套PASS（09:04:10Z）/security PASS（09:02:48Z），两份Linux隔离模块含测试govulncheck PASS；外部Xray FAIL另列。精确命令/结果/阻塞见RESUMED，继续包内下一项。

**新增实际Linux组件证据**：本机已有Ubuntu24.04 WSL2、UID1000、amd64；12包212顶层测试运行PASS（含Agent重复）/3实际Xray二进制SKIP。真实WSS/Agent副作用与重连、SQLite旧快照外部隔离、私有文件/Sync分支均已执行。Agent/恢复旧fixture的0700准备及错误检查问题修正，未放宽生产门槛；首轮FAIL归档。逐项见LINUX-NATIVE-EVIDENCE.json和LOCAL-LINUX-VALIDATION.md。不是物理VPS/安装/arm64/1C1G或完整统一任务验收。

**Xray外部发布物安全FAIL**：26.6.1符号扫描55项；正式新版26.10.29独立下载/双哈希核验后扫描3项，未执行新版。两份均不得发布/安装。R21真实TLS/Vision Stats/进程重启功能实验证据保留，不当安全验收。精确pin/发现/复查命令见XRAY-ARTIFACT-SECURITY.md；生产Stats typed telemetry/持久历史仍未接收，不能通过TaskOutput/float64绕接缝。

实际SQLite组合固定DS源`bb8954988fe7b2f832edf291803529d7745e4f0a`，单独module/sum，不是共享接收SHA。十二项最新PASS8.850s（R26），含旧库恢复仍隔离、AI消息HTTP/SSE及持久推理闸、重启撤权、域/审计回滚、缺接口拒绝。模拟Gate/Runtime/Task/Model明确记录。R19真实WSS/Agent隔离包PASS9.323s，业务准入仍fixture。npm ci/tools:install本轮已PASS，依赖未变；根security无漏洞不覆盖可选外部Xray二进制。Linux四个骨架交叉编译R19已PASS，未作为安装验收。

总控已确认98f6d94d仅依赖边登记且已在基线；RecordTx/扩展Request/DS新SQL接缝不是共同接口。四份PRIV/WC/PKI-OPS/UPDATE完整正式原文缺失，只提交独立PROPOSED决策，未补写历史。缺项只暂停具体接线，继续可独立实现及本地负向验证。没有访问生产/VPS/云/真实凭据、远程推送、合并main或启动其他Agent。

AC-01—24与完整PoC仍未验收。Windows局部实验/交叉编译不能替代Linux amd64/arm64、1核1GB、外部供应商或真实灾备证据。最终停止必须给最新HEAD、对应检查及未完成清单；当前不声明完整v0.9交付。
