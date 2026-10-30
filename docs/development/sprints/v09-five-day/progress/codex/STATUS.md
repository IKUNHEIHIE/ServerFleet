# V09-5D-CX / codex-03（接续codex-02）

2026-10-31用户明确授权本会话接替“完成 V09-5D-CX 开发包”（01a188b2-b181-7230-94a7-136c5356739b）。新唯一会话标识codex-03；沿用本包分支、允许目录、审查人与停止条件。接手HEAD为`ecb7cd405a0630d814580d1a6123d3e9a56c3e97`，原会话已中断；保留并核验其7项未提交文件，无重置/清理或他人目录写入。公共任务板/所有权登记仍由总控单写，本行记录本次用户授权的接替关系。

**ACTIVE，完整v0.9尚未完成。** 用户要求持续完成CX-01—08；先前b61ad15“无独立工作”的停止判断已撤回。这里是当前状态，逐增量SHA/实际验证/历史失败见[RESUMED.md](RESUMED.md)；早期HANDOFF/EVIDENCE/COMMITS为历史快照，不能覆盖这里的新实现或当作最新全量检查。

基线/main `8a7dd4707d2d7e142e137d098296788d2c80eddd`；分支feature/v09-platform-cx02；实际目录 `D:\admin\desktop\codex\myproject\ks\.worktrees\feature--v09-platform-cx02`。用户消息中的外置ks.worktrees目录不存在，实际目录与任务单/Git一致。审查codex-review-01+安全/契约/owner。2026-10-30，Asia/Shanghai。

| 子项 | 当前实现与可复验证据 | 尚未交付 / 具体依赖 |
|---|---|---|
| CX-01 | 首接口检查点2fc8dab；独立认证RFC6902候选/兼容测试；S01—S04及各模块接线说明持续补充 | 认证/事务/tasks/Proto/WSS共享差异尚无总控接收SHA，未改受保护API/Proto/gen/根锁/迁移序列，不冻结G2 |
| CX-02 | 持久初始化/口令/限速/Cookie/CSRF/Origin/epoch/Tgate；未挂载真实HTTP handler含用户/角色/grants；同Tx审计；凭据密文/暂存/服务使用永久防重放；只读受限opaque Bearer；持久审批与完整任务消费绑定。实际SQLite/HTTPS、重启撤权/回滚测试 | 正式迁移/安全决策/唯一任务实际消费尚未接收；MFA政策与实现未完成（要求MFA即拒绝启用）；凭据轮换/撤销任务与页面、scope/approval实际接线未完成 |
| CX-03 | TLS1.3+mTLS当前指纹/吊销、ControllerLinks/Agent连接循环、接入审批/签发/重装撤销持久化；本地私钥/CSR/证书与固定Root HTTPS注册客户端/handler。新增Ubuntu实际CSR→pending→SQLite重启→真实身份/同Tx审批审计回滚→issued持久材料→mTLS→显式grant吊销/重放拒绝；真实WSS/Proto/Ping组件另有原生证据 | WSS生产依赖、PKI profile/轮换/信任包/Node同Tx接线未接收；注册管理角色/当前listener profile仍测试输入，无完整安装器与Agent运行profile，独立Linux安装/arm64/1C1G未验收 |
| CX-04 | Agent副作用前claim/start持久化、去重/TTL/取消/live对账/UNKNOWN；输出落盘前强制注册过滤、持久生产序号/有界日志；实际WSS→临时文件→脱敏日志→重连/部分丢帧实验。审批外层rollback/重放负向 | DS Command缺完整Offer字段、ApprovalConsumer缺source/AI session等完整绑定、SubmitTx及日志序号/补读/ACK未接收；root/Shell/插件准入未获批，真实Linux统一链未交付 |
| CX-05 | 持久AI零资源/逐动作grant/组快照/三级模式；消息幂等/fence/UNKNOWN；未挂载POST/list有界分页及真实Cookie HTTPS SSE；BYOK文本HTTPS适配、当前持久推理闸；新增默认停用的持久模型设置/版本绑定/同Tx独立审计与重启使用；工作流冻结顺序任务/停用/UNKNOWN暂停；Eino隔离PoC | 正式外发政策/生产内容过滤器、设置公开路由与页面组合、统一任务/审批/通知接线未完成；实际外部模型/BYOK未验收，元数据/角色/过滤/模型部分仍明确double，Eino未定生产框架 |
| CX-06 | 签名源/清单/声明UI/私有工件/持久生命周期，默认disabled/实例隔离/禁用先封Host/UNKNOWN；WASM PoC。平台密文配置历史/CAS/一次性恢复源与同Tx审计、真实SQLite重启及unknown不重跑。可选Xray两模板/custom审查/管理API隔离；R37中央实例绑定UUID/REALITY材料与实际一次导出fence；R38持久用户ID/统计标识、CAS/tombstone、期望与已应用状态分离、当前中央引用检查、快照分页，实际SQLite改名同UUID/重启停用/审计回滚/事务前撤销通过；历史26.6.1真实TLS/Vision/Stats实验 | 两份已知官方外部发布物安全FAIL，不得安装/发布；WASM/Host/来源/密钥未准入。平台历史/用户存储正式Host与完整typed审计schema、唯一任务及真实应用配置/健康证据/页面未接收；双架构安装/接管/目标预检与恢复部署、实际用户停用断连/QR/typed遥测持久业务未完成；历史源delivered不是部署成功，核心未编入Xray |
| CX-07 | 更新签名/序列/平台/哈希、同FD暂存一次借用；新增真实文件备份/切换/持久阶段与回执，UNKNOWN永久锁、明确健康失败+兼容性核验才精确恢复本次备份；恢复连续Root/撤销并集/数据库外hold闸，SQLite旧快照不清隔离 | UPDATE/PKI新决策/离线解密/旧主控隔离/实际恢复事务未接收；服务/health/审计提交适配仍明确double，Linux真实更新/迁移/回退/灾备未验收。无默认放行verifier |
| CX-08 | Controller/Agent骨架及四个Linux交叉编译脚本，构建/组合/恢复接缝说明 | main尚无新模块接收SHA，未在入口组装未接收业务；正式中间件/路由组合、Agent安装与完整发行物尚未完成 |

已提交R33 `60e67e583670894232ddca0787a74e31d35ba165`（事务内身份/实际Linux注册），R32 a9f1069、R31 c680e37、R30 7d0d8d1。R34密文历史/一次性源已完成实际SQLite及Ubuntu plugins11/SQLite13顶层验证，精确证据LINUX-PLUGIN-HISTORY-EVIDENCE.json；正式Host/统一审计与真实部署未验收。全量check最近五套PASS（10:04:17Z，R33），R34模块/architecture与staticcheck PASS，security新PASS（10:18:13Z）；外部Xray FAIL另列。继续包内独立实现。

以上R33—34检查时间为历史；接替后R37提交`7f05242`，R38提交`4ef7e53`。最新实现/原生与Windows检查以RESUMED.md和[本次codex-03交接](TAKEOVER-codex-03.md)为准。R37原生auth67/SQLite15 PASS；R38原生可选包17 PASS/3已阻断二进制SKIP、SQLite15 PASS。最终全量check五组PASS17:38:25.159Z/security PASS17:38:15.568Z（上海2026-10-31）；源码逐一吻合最终原生证据，保护文件零差异。中央材料v2禁止直接降级旧auth，用户pending/unknown不冒充部署成功；AC-23仍NOT_ACCEPTED，完整包ACTIVE，未宣称所有子项完成或不存在其他独立工作。

**新增实际Linux组件证据**：本机已有Ubuntu24.04 WSL2、UID1000、amd64；12包212顶层测试运行PASS（含Agent重复）/3实际Xray二进制SKIP。真实WSS/Agent副作用与重连、SQLite旧快照外部隔离、私有文件/Sync分支均已执行。Agent/恢复旧fixture的0700准备及错误检查问题修正，未放宽生产门槛；首轮FAIL归档。逐项见LINUX-NATIVE-EVIDENCE.json和LOCAL-LINUX-VALIDATION.md。不是物理VPS/安装/arm64/1C1G或完整统一任务验收。

上述12包是R28历史完整扫描；R29—33为各变化包原生重跑，逐次独立哈希证据不覆盖旧记录。R33注册链在Linux实际完成，仍不等于所有13个现有包重新跑过或正式统一任务接线通过。

**Xray外部发布物安全FAIL**：26.6.1符号扫描55项；正式新版26.10.29独立下载/双哈希核验后扫描3项，未执行新版。两份均不得发布/安装。R21真实TLS/Vision Stats/进程重启功能实验证据保留，不当安全验收。精确pin/发现/复查命令见XRAY-ARTIFACT-SECURITY.md；生产Stats typed telemetry/持久历史仍未接收，不能通过TaskOutput/float64绕接缝。

实际SQLite组合固定DS源`bb8954988fe7b2f832edf291803529d7745e4f0a`，单独module/sum，不是共享接收SHA。R33十二项Windows PASS8.390s；Ubuntu对应十二项及Agent注册含旧21项共22项PASS，新增事务ctx绑定真实权限读者，首次审计原因码不匹配FAIL保留并修正固定映射。角色/profile/资源resolver及其他Runtime/Task/Model测试输入明确记录。R19真实WSS/Agent隔离包PASS9.323s，业务准入仍fixture。npm ci/tools:install本轮已PASS，依赖未变；根security无漏洞不覆盖可选外部Xray二进制。Linux四个骨架交叉编译R19已PASS，未作为安装验收。

总控已确认98f6d94d仅依赖边登记且已在基线；RecordTx/扩展Request/DS新SQL接缝不是共同接口。四份PRIV/WC/PKI-OPS/UPDATE完整正式原文缺失，只提交独立PROPOSED决策，未补写历史。缺项只暂停具体接线，继续可独立实现及本地负向验证。没有访问生产/VPS/云/真实凭据、远程推送、合并main或启动其他Agent。

AC-01—24与完整PoC仍未验收。Windows局部实验/交叉编译不能替代Linux amd64/arm64、1核1GB、外部供应商或真实灾备证据。最终停止必须给最新HEAD、对应检查及未完成清单；当前不声明完整v0.9交付。
