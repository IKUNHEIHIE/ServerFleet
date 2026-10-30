# 插件配置密文历史与一次性恢复源（PROPOSED）

R34 / CX-06；internal/plugins/history.go、history_sql.go，平台持有有界历史/加密/持久claim，不将文件系统、SQL、通用密钥或运行时交给插件，不导入可选Xray。当前只允许xray-manager，按PluginID+NodeID+InstanceID绑定独立行，schema/接口与Host能力仍待总控接收，未启用任何WASM ABI或发布物。

程序接缝：NewHistoryVault(store,scope,activeKey,keys,before,commit)，Save、List、WithRestoreSource、Close；SQLHistoryStore通过Read/InTx/Exec适配，无database/sql import。HistorySchemaStatements只给本域建表候选，不分配公共迁移号；HistorySeed返回显式新受信实例INSERT及参数，不执行或替换。缺行/表/损坏不当作空历史，不自动重置/跨实例复制。

HistoryOperation冻结actor/sole task/action、scope、pluginRevision、currentConfigRevision、expectedHistoryRevision、source version/content SHA/target version。before/commit均必填；HistoryCheck.Stage由服务构造，prepare/read/consume验证当前真实主体、唯一任务/完整审批/插件签名启用/节点与实例grant、配置修订和兼容性；commit使用可信txCtx的同Tx只读权限reader。observe/commit-finalize只核验同一任务的当前结果与权限，不用执行前旧配置修订反复否认已知新结果，也不再次执行。Stage不能从Web提供后直接放行，字符串不是身份/批准证明，回调须服从5秒取消。

Save记录受控任务给的原始JSON配置（至多1MiB），仅做UTF8/对象/JSON与明显PEM私钥标记的基础拒绝，不替代ReviewCustomJSON、路径授权或目标版本-test。VersionID=source TaskID，CAS递增，重复拒绝；历史至多16条/整体24MiB/永久claim及出箱各10000，不自动逐出旧记录或允许未知任务重放。容量耗尽明确拒绝，保留/归档政策需单独审查；不是无限历史或完整用户业务。List只给摘要、版本、长度与时间，双当前门，不给UUID/REALITY私钥/原始配置。

配置按每条新256位随机salt用固定Go1.27.3 crypto/hkdf.Key(SHA256)从专用32字节数据密钥派生独立AES256 key，再用NewGCMWithRandomNonce封装。域分离AAD绑定scope、全部版本元数据、KeyID、salt；跨节点/实例、改摘要/版本/KeyID/nonce/ciphertext均不可借用。每次封装派生独立key，避免依赖会在失败/数据库回退时丢失的全局随机nonce计数；随机salt碰撞概率依赖crypto/rand。Keyring仅可信组合注入，最多16个版本，复制后借用、Close尽力clear；无环境/导出接口、不复用CA/恢复/登录密钥。旧密钥缺失关闭，未实现自动重包/密钥退休或正式离线恢复密钥流程，Go crypto/运行时内存不保证完整擦除。

WithRestoreSource在解密/回调前同Tx永久记reserved及审计。回调成功且当前门/最终提交成功仅记**delivered**，表示源已交付，绝非Xray部署成功；目标-test、唯一任务/审批、Agent暂存/部署/health/回退仍属真实受控流程。错误/panic/撤权/取消/无法确定最终提交记unknown或保留reserved；同一TaskID永久拒绝再借，重开仍如此，新任务也必须有新的明确审批/对账，不能自动换TaskID逃避未知结果。消费者不得保留字节、重入vault或作为通用下载/模型/Host秘密读取，缓冲返回尽力清零。

SQL写入/claim/全字段审计出箱同一事务。AppendAudit可选，nil真实保留出箱，不宣称到达统一审计；非nil必须同Tx完整保存Scope/actor/task/action/result/version/digest/time，失败全回滚。DS现有审计summary不能容纳所有字段，不能静默丢弃；需独立审计扩展或可追溯全字段域记录接收。本轮SQLite测试同Tx写实际私有全字段audit probe并故障回滚，明确不是正式DS审计schema/新Host能力获批。

Windows模块/architecture PASS2.438s/1.857s，staticcheck PASS；内存事务fixture验证CAS/当前门/未知结果/密文篡改/实例隔离/丢回调假空成功拒绝等六项。实际SQLite十三项（新增一项历史）PASS17.557s，Agent21 PASS/真实Linux-only注册另SKIP；历史测试实际文件写入/数据库两次重开，已完成/未知claim不重复。Ubuntu UID1000原生plugins11/SQLite13顶层PASS/无skip，见LINUX-PLUGIN-HISTORY-EVIDENCE.json精确源/二进制/日志。该历史证据不执行Xray，两份已知漏洞发布物仍禁止安装/发布；实际Xray业务配置历史页面/Host适配/目标预检/恢复部署未验收，继续受控出口等独立工作。
