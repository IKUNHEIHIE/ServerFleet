# Xray中央实例绑定材料（PROPOSED）

R37 / CX-02、06。`internal/auth/xray_materials.go`扩展中央CredentialService持有的加密组件、认证聚合及每密钥GCM预算；没有第二个插件秘密库，没有新增公开credential kind、路由、数据库迁移编号或核心对可选Xray的import。

## 调用和当前授权

`NewXrayMaterialService(credentials, gate)`要求真实IdentityTransactionStore、中央cipher、明确gate及`XrayMaterialPermissions()`权限目录。所有操作携带完整`XrayMaterialOperation`：真实主体、操作ID、动作/来源、节点/实例/入站/用户/purpose、服务端计划摘要、材料引用/预期修订、收件主体、操作期限及可选材料期限。引用由`XrayMaterialReference(scope)`域分离计算；输入字符串和已知subject不能自认证。

gate收到完整operation及阶段，必须验证当前来源身份/CSRF或受控插件任务、确认/适用审批、同一节点实例及当前用户/配置/插件修订、准确计划摘要及期限。写入时使用真实txCtx内只读权威来源，不能重新打开第二个写事务或回退原请求快照；模块内另要求当前显式节点grant，管理员不默认拥有节点。允许来源仅真人web与受控plugin，AI不能借此导出秘密。缺来源/权限/typed审计则关闭。

`Generate`只支持`xray-user`（node+instance+inbound+user）和`xray-reality`（node+instance）；无秘密输入/输出。先同Tx提交永久reserved、typed审计和共享GCM计数，再生成随机v4 UUID或X25519私钥并封存；scope/引用/owner/创建及到期时间/公共参数/修订全部参与AAD。完成丢失保留reserved；再次生成拒绝，不默默更换用户身份。完全相同的ready生成请求只重放元数据，不再生成或加密。使用同scope的新操作不会覆盖原身份。

`Metadata`仅返回引用、scope、状态、修订、创建/到期时间，不返回UUID/私钥/REALITY公共参数；`Revoke`以预期修订CAS关闭中央引用。撤销不声称已断开已有Xray会话或已部署新配置，这些必须经唯一任务链明确实施。

`WithMaterial(ctx, op, consume)`在同Tx审计/永久operationId占用提交后才调用consumer；完成后再查当前材料/修订/权限/来源，并同Tx提交delivered或unknown。consumer错误/panic/取消/撤权/轮换/审计或提交失败都不算成功，reserved/unknown/delivered均不能再次借用。web reveal只允许同一真人收件主体的用户UUID；REALITY服务端私钥仅供受控plugin use，不交浏览器。借用返回后缓冲尽力clear；可信consumer必须有界且不得保留秘密，不能保证Go运行时全部拷贝擦除。

`WithPublicParameters`只提供已封存REALITY客户端验证值，须当前reveal/use权限；解密并核对派生公钥及AAD，拒绝单改数据库public字段。它不提供服务端私钥，也不替代受控客户端导出的最终交付闸。

## 精确事务及兼容候选

`IdentitySQLStatements`新增可选字段：

```go
AppendXrayMaterialAudit func(context.Context, []XrayMaterialAuditEvent) error
```

typed事件保留完整scope、主体、operationId、action/source、reference、planDigest、revision、result和时间。SQLIdentityStore每次从当前Tx实际sink派生私有marker，覆盖父ctx旧marker；材料写操作强制该sink存在。sink必须在同一Tx完整持久化，任一事件/审计/聚合写入/COMMIT失败全回滚。旧AppendAudit不能吞掉新增字段。已有无材料身份操作保持兼容；nil typed sink不消费出箱且不允许新材料写入。

认证payload首次reserved提交时由Version 1转为Version 2。新代码仍读取普通v1；常规写入保留v2；v1内出现材料/永久claim或未知版本均拒绝。固定R36旧代码`ecb7cd405a0630d814580d1a6123d3e9a56c3e97`会拒绝v2，避免其反序列化/回写丢掉新字段。**创建材料后不能直接降级旧auth程序**；必须走审查后的升级/恢复兼容路径，不能删Version或claim恢复可用性。实际SQL形状不变，公共迁移与接受该新格式仍需总控审查。旧数据库快照回退仍必须依靠外部quarantine/恢复连续性，不由v2独自解决。

## 真实验证与剩余

Windows完整auth、可选Xray及架构测试通过；实际SQLite组合将中央生成UUID接至R35 WithClientExport，校验密文落库、真实身份/显式grant、同Tx审计、导出后重开防重放、审计失败不交付、BEGIN前会话撤销及固定旧代码拒绝v2且字节原样保留。typed完整审计probe为隔离测试表，不能冒充DS已接收新公共schema；当前用户/配置/插件修订、任务/确认/profile resolver仍明确fixture。

本机Ubuntu WSL2非root UID1000原生auth 67/SQLite 15顶层PASS、无skip；精确源/二进制/日志哈希见`progress/codex/LINUX-XRAY-MATERIALS-EVIDENCE.json`。源码安全扫描不能覆盖外部Xray发布物；两份已知FAIL发布物均未运行或安装。真实持久用户服务/应用配置与禁用部署、QR、正式reveal政策/Host/路由/页面、实际Xray互通、双架构安装及生产准入仍未验收，AC-23保持NOT_ACCEPTED。
