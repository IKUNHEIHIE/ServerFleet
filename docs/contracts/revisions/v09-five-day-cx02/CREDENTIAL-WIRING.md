# 凭据平台接线候选（未接收）

`auth.NewCredentialService`注入32字节密钥组与activeKey，无默认密钥；构造拒绝重复密钥材料别名。auth自有SQL聚合持久密文/AAD（ID、owner、purpose、revision、keyID）、变更暂存、每key加密预算、服务调用防重放闸。真实密钥只由获准安装配置提供，不传聊天、Git、日志。CA/恢复私钥和平台登录秘密不属于此库。

提供CreateIdempotent、List、References、StageRotation、ApplyRotation、Revoke、UseForService。Create/Stage要求当前管理员Cookie+CSRF；轮换/撤销/使用要求Controller生成的CredentialOperation，gate验证唯一tasks的当前状态、操作者、权限、审批、源、action、changeID、预期revision与服务端摘要。gate为nil拒绝。调用方不得把请求JSON中的这些事实直接当证明。域内不调Agent/SSH，不自造任务调度器。

加密前独立提交key预算，失败也消耗；每key最多2^32-1次，恢复可能回退预算时必须换新key。轮换10分钟暂存密文，任务参数仅含changeId，任务成功准入再CAS替换；旧密钥须保留到所有记录重封，当前实现不声称全库自动重封/供应商验证。创建/轮换状态unverified，不伪造lastValidatedAt。

UseForService只借出缓冲给程序注册的业务回调，不提供通用HTTP/AI/Host解密。先审计intent并永久预留operationId，再调用；回调缓冲清零，错误/恐慌/超时/调用后撤权/审计失败均未知，不自动重跑。仅调用后再次通过gate及凭据revision/有效性核对才记录成功。持久防重放记录上限10000，耗尽失败关闭；未来与tasks历史归档协议需审查，不可用定时删除恢复可重放性。回调须遵守30秒取消，不能宣称能中断不合作的第三方函数。

HTTP可选配置`IdentityHTTPConfig.Credentials`必须绑定同一个IdentityService。已实现GET credentials（既有数组）、POST credentials（201 metadata，Idempotency-Key）、GET references（明确items数组候选）。创建receipt仅摘要非秘密参数，重放解密常量时间比较当前密文；撤销/轮换后原请求不复活。读取先缓冲再CommitGate。旧references契约只有ok，拟以items完善，需GM/DS审查。

尚未挂载rotate/revoke HTTP：既有契约是202统一异步任务，DS现有节点任务没有正式接收的Controller本地目标/secret-stage引用/凭据引用验证端口。需要总控接收目标类型与action/changeId参数schema，再将StageRotation→唯一tasks.Submit，worker→ApplyRotation/Revoke；不得返回虚构jobId或同步成功冒充202。引用源nil/失败报503，不能返回假空列表。

新记录随auth迁移一起登记，不占公共编号、不改原OpenAPI/生成/依赖。本候选不替代缺失完整PRIV/UPDATE原文，不冻结G2。单元测试与真实SQLite重启证据见progress/codex/RESUMED.md，非真实供应商、任务审批全链或页面验收。
