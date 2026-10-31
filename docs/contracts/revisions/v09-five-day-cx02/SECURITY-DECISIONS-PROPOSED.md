# CX02 新实施决策候选（不是历史ADR补写）

2026-10-30；codex-02；状态 PROPOSED / 未批准。总控已确认仓库与11个历史ZIP均没有完整PRIV/WC/PKI-OPS/UPDATE，不能提供其本地位置。现有Accepted AI/PRIV解释附录仅覆盖明确条款。本页为未来独立决策的具体输入，不声称替代缺失历史全文，不因测试PASS自动批准。

## P01 身份和凭据最小实施边界

建议采用auth/identity.go的候选参数：本机受控setup 32随机字节、10分钟单用、原子创建永久initialized；PBKDF2-HMAC-SHA256 600000轮/32字节随机盐/32字节结果；密码12–1024字节（仍需真实1C1G抗DoS/延迟评审）；TLS同源会话Cookie __Host-sf_session，Secure/HttpOnly/Strict/Path=/且无Domain；8小时绝对/30分钟空闲；用户名与可信来源IP分别5次/5分钟；CSRF独立随机值。主体与节点授权分开，管理员无默认wildcard。

适用限制：PRIV补证/替代决定、MFA启用政策及受控安装秘密交付未批准前不接真实登录。若部署要求MFA，现有password-only组件不足，不能忽略secondFactorCode成功登录。失效、epoch、initialized、审计出箱及限速由DS事务持久化；空表/恢复不能重新开setup。轮换/撤销提交失败不交付成功。Bearer未接受限目的/节点上限/撤销求交前保持禁用。

安全凭据建议采用auth私有AES-256-GCM组件，对象/owner/用途/revision/keyID认证绑定；只做经授权的服务内使用，不开放普通明文GET；密钥由受保护安装环境注入并与业务数据库分离；CA/恢复私钥不进入通用凭据库。DS适配必须维护每key累计使用量，达到GCM全局<2^32次上限前轮换；重新启动不清零使用量。当前组件无仓储/API，因此不能称AC-19通过。

## P02 Agent执行和特权

建议唯一任务入口负责当前用户/AI/插件权限、目标快照、审批、任务持久化、审计与每节点再次校验；Agent只接受程序注册的类型化动作，参数schema+路径边界+能力二次验证，在启动副作用前持久executionId/完整offer摘要。未知结果不自动重跑，取消不等于回滚。默认普通并发5、root/修改首1后5、未启动TTL15分钟保持既有条款。

尚未给出可批准的任意Shell沙箱/网络/宿主凭据隔离设计；因此本轮执行器拒绝root/Shell/插件能力，不能把其他动作名映射到任意argv绕过。AI任意root Shell即使full也需真实管理员Web逐项审批。启用这种能力需要独立安全差异、沙箱/横向网络限制证据和完整PRIV适用核对。当前回调执行器/去重测试不构成该许可。

## P03 更新签名与恢复

建议release.v1域分离签名覆盖固定版本、组件、平台/架构、发布单调序列、字节数与SHA256；发布公钥通过独立可信安装配置pin，不接受同源自报key。保持私有staging对象，安装前同一对象复验，拒绝替换后路径重开TOCTOU。实际安装必须在统一任务授权/审批/审计后，保存真实health结果；不可逆迁移不描述为可自动回滚。签名格式/根轮换/降版授权/具体health协议仍待UPDATE决策接收。

恢复只输出quarantine核对计划：保持Root SPKI连续、独立权威撤销序列/世代不倒退、保留撤销并集、使所有旧会话/审批失效；在途未知、过期不派发，未启动待重新授权。仅有备份内的“已吊销列表”不足以证明最新状态。离线解密与材料真实性、旧主控隔离/fencing、恢复事务和Linux实际演练尚未实现；计划组件永远不能清quarantine。不能由HTTP布尔claimed fenced触发放行。

## P04 新契约和PoC接收项

auth-session.patch.json是在内存副本上应用的独立RFC6902候选，不改原始OpenAPI。服务内Identity不是HTTP DTO，适配仍按既有userId/displayName/roles模型，不能将subjectId/administrator结构直接回传。me的CSRF恢复只修改CurrentIdentityResult新schema，不污染其他Identity使用处。Cookie名/opaque Bearer/初始化204为明确迁移，旧客户端需单独处理，既有nodes operationId/parameters/responses保持。

隔离WSS/wazero实验使用独立reports模块和固定依赖，根go.mod/sum/锁哈希保持；Windows循环握手/内存/无限循环终止只能算局部PoC。生产WSS依赖先交S04精确patch；WASM Host的持久生命周期、限额、多租户/1C1G/Linux证明不足，不锁生产runtime；Eino框架未选择。以上所有组件的持久来源/事务接缝/真实端点消费仍需独立owner接收。
