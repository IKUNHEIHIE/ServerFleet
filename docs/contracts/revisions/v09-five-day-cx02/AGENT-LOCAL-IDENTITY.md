# Agent本地私钥、CSR与证书接收（候选）

CX-03/08；apps/agent的私有agentIdentityStore，不挂载main/安装器，不含注册HTTP或WSS拨号、不自动登记节点或批准接入。独立绝对目录须Linux0700、记录/私钥0600；os.Root、常规文件/Lstat/SameFile及大小限制，无跨目录路径或symlink追随。Root SPKI指纹与最低trust generation来自独立可信安装输入，拒绝空值/非规范pin；不从返回证书自我信任。

Prepare明确生成本地P256 key/PKCS8、空subject/SAN/extension的签名CSR与随机requestId；先O_EXCL永久enrollment.json公共意图，再O_EXCL私钥文件及Sync，重新加载验证key/CSR公钥和签名。重复同意图保留同request/CSR/key，不允许部分写入、损坏、缺key、旧已签发记录导致自动重置或重新登记；reinstall/rotation必须另有已批准流程，不能以同名覆盖。现有Agent升级打开原身份即可借用，不得为版本字符串变化悄悄生成新身份。

Request只交CSR、requestId、独立pin、准备时的版本/实际runtime.GOOS与瞬时256位令牌；令牌不落盘，fmt打印已在transport.AgentEnrollmentInput显式脱敏，JSON仍保留出站API所需token字段。原Controller候选只支持linux；本机Windows请求如实声明windows、会被该Controller拒绝，未伪造Linux平台通过验收。完整注册网络还须固定HTTPS/服务器名/Root pin/禁止跳转与浏览器凭据来源，并取得真实admin决定；本组件不补默认批准。

Accept只保存approved响应中已验证的证书：严格仅CERTIFICATE PEM链/无垃圾或私钥block/重复证书、配对本地key、CN精确nodeId/唯一subject、仅digitalSignature+clientAuth、无SAN/CA扩权、期限与结果一致且剩余>1分钟、证书总有效期≤24小时+1分钟、最低generation及自签Root SPKI匹配，使用唯一pinned Root做完整x509链/时间校验。issued.json不可覆盖；同一响应可幂等重取，新证书/身份/换代不能借此偷偷覆盖。响应generation字段本身不构成独立签名或当前启用证明。

WithCertificate必须注入当前可信profile/trust/revocation gate，校验已持久key/CSR/链/字段后借给程序注册TLS消费者，消费后再次gate；缺gate关闭。消费者不能保留私钥或重入同一store；API没有原始私钥/通用导出，CA/恢复私钥不在此。内存字节尽力清除，Go crypto对象/运行时副本不保证完全擦除。该门不替代session.Current、当前Root/撤销权威或Controller每次按leaf fingerprint映射；签发不等于当前启用/执行授权。

真实临时文件/CSR/重开保持、请求token不落盘/打印脱敏、错误Root/公钥/节点/权限/期限/世代/私钥混入PEM/损坏与缺依赖负向已测；实际本机TLS1.3+mTLS以本地新私钥/签发证书成功，Controller当前撤销后新TLS握手拒绝。测试CA/签发/映射和gate明确fixture，不是假记真实管理员批准/注册网络/Node存储/安装或Linux双架构环境。缺PKI-OPS新决策、轮换/信任包/独立最新撤销、Linux文件所有者/ACL/断电证明仍保持BLOCKED/UNTESTED。
