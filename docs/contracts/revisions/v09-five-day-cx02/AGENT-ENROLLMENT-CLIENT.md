# Agent初始注册客户端（PROPOSED）

R31 / CX-03、08；apps/agent/enrollment_client.go，私有newAgentEnrollmentClient(identity, config)与EnrollOnce(ctx, transientToken)。main/安装器仍未启用。身份须由已有Prepare显式建立；客户端不会生成、重置、替换私钥、选择nodeId、自动批准、循环轮询或自动重试。

独立安装输入提供固定无路径HTTPS Origin、一个自签Root公开证书及当前profile/信任/隔离回调，Root SPKI必须匹配本地意图pin。标准x509 RootCAs/域名/期限验证，TLS1.3，VerifyConnection再核对已验证链Root；不使用系统Root、InsecureSkipVerify、返回证书TOFU、环境代理或URL凭据。必须使用DNS名（SNI），每次新连接，HTTP无Cookie jar/客户端证书、关闭跳转/压缩/GetBody/keepalive；没有Idempotency-Key或应用自动重试。

只向配置Origin的既有POST /api/v1/agent/enroll交本地相同CSR、requestId、实际runtime.GOOS与瞬时token；不落盘token，缓冲尽力clear且错误均固定无URL/响应体/秘密。输入/网络/响应总10秒、握手/拨号3秒、响应头16KiB/5秒、响应体96KiB；不能强制取消不合作的Current回调。令牌文本、Go/http/运行时内部拷贝无完全擦除保证。

成功状态仅接受R30候选最小六字段profile：pending/denied只有state+expiresAt；issued必须另有nodeId/leaf/chain/generation；严格大小写/重复/null/未知字段/尾随值与uint64整数解析。禁止Set-Cookie/压缩/非200/错误媒体类型，服务端临时agentEndpoint/reasonCode扩展在此最小profile亦拒绝，绝不采用响应端点。与更宽候选契约的兼容性需总控同步，不能擅自挂载；独立差异agent-enrollment.patch.json仍待接收。

pending是观测结果，无issued.json；denied返回明确errAgentEnrollmentDenied，不是成功安装；issued经本地Accept重验配对私钥/Root/身份/EKU/期限/最低generation后不可覆盖持久化。当前门在请求前、响应后与保存后检查；末次失效或写结果不明会报unconfirmed并保留原意图/材料，不能为“重试成功”重新生成身份。借用证书仍需WithCertificate当前门与Controller每次叶指纹映射，不把签发当执行授权。

本机真实TLS验证同CSR两次明确调用pending→issued、环境代理零请求、无浏览器凭据、重开本地证书可校验；TLS错误域名、跳转目的地零请求、取消、响应篡改/越界/状态混用/过期及当前门失效负向均PASS。此处服务端签发为明确fixture，不冒称真实EnrollmentService管理员/SQLite/WSS全链。Ubuntu非root原生Agent21顶层PASS，见LINUX-ENROLLMENT-CLIENT-EVIDENCE.json；继续真实服务组合。没有真实外部凭据或生产连接，Windows元数据仍如实windows，现有Controller linux-only时正确拒绝。
