# 初始 Agent 注册 HTTP（PROPOSED）

R30 / CX-03、08；`internal/transport/enrollment_http.go`。既有 POST `/api/v1/agent/enroll` 的私有候选 handler，未挂载 main、未开放监听器。直接调用真实 EnrollmentService，不实现第二套令牌/审批或节点数据库。

程序接缝：NewEnrollmentHTTP(service, EnrollmentHTTPConfig)。Authority、ServerName、数值有界的并发/全局/对端IP分钟预算，以及 Current/Rejected 回调必须显式提供；没有默认启用 profile 或假审计。Current须验证实际监听器、当前PKI权威和恢复隔离，提交前与输出前重查。管理员签发令牌/决定仍使用已有服务的独立当前授权闸，不能由此匿名接口自行批准。

仅直接 TLS1.3、精确配置 Host/SNI、无客户端证书的初始来源。Cookie/Authorization/Proxy-Authorization/Origin/Referer/Sec-Fetch-Site/X-CSRF-Token出现即拒绝，空值也拒绝；不信任转发IP头。固定路径/POST/无查询，严格六个字符串JSON字段，不接受重复/大小写别名/null/未知字段/压缩/尾随值，32KiB上限、10秒上下文与实际请求读取期限；不支持读取期限的包装器关闭。部署须配置服务器连接/头/写期限及TLS握手预算，不能拿handler预算充当完整抗滥用系统。

响应保留既有 pending/issued/denied：内部approved映射issued，rejected/expired映射denied，签发后将leaf与chain分开。`agent-enrollment.patch.json`是独立RFC6902候选：platform改必填，结果新增expiresAt/trustGeneration与issued条件要求；受保护YAML、生成代码与Proto未改。已有additionalProperties:false客户端会拒绝新字段，旧无platform客户端也不兼容，需总控同步契约/客户端/生成和监听器验收后才可接线。平台值仍由现有服务只接受linux，不伪造本机Windows元数据；generation不是最新授权凭据。

Rejected只接收服务器生成的请求引用和固定原因码，无token/CSR/请求体/IP/私钥；必须接真实有界持久/聚合拒绝审计适配，错误或panic报503。2秒取消要求适配器配合，Go无法强制停止不合作回调；每个匿名拒绝同步写库也可能放大负载，生产聚合/背压策略尚待审查。当前每进程固定分钟配额、受限IP映射与并发槽只保护合法来源后续工作，不构成分布式限流、安装profile或1C1G证据。

真实本机TLS测试调用同一EnrollmentService：pending无证书、管理员决定提交失败仍pending、明确决定后issued且CSR恶意SAN不被复制。审批Gate/内存仓储/拒绝审计在该测试中明确为fixture，非真实人类登录、Node持久注册或统一审计验收。来源/解析/预算/回拨/隔离/输出前失效负向通过；Linux组件证据单列。缺PKI正式决策、共享事务/节点接线、独立最新撤销与生产TLS profile仍阻止正式挂载。
