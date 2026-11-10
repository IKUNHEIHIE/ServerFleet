# R15 节点本地公开证书链安装及发布接缝

V09-CLOUD-CODE-COMPLETE-01，UNTESTED。整包编码后集中验收；旧候选、Proto/gen、已接收SQL/hash不改，未获G2冻结或B04/B06/发布认证。

- 新chain-tasks POST/GET：基于实际已完成CSR任务、当前管理员/节点资源、修改类计划/审批/当前派发复权。公开链限制8KiB、最多6个规范CERTIFICATE块；不接受私钥/CSR/其他隐藏材料。链的SHA、公开CSR SHA、域名/引用、CSR任务ID全部进入冻结计划。
- 通用task string值仍限1024字节。只有明确certificates.chain.install、SourceWeb、单目标、强制审批/修改类且通过完整public-chain解析的六字段载荷使用有界16KiB专用端口，不扩大普通命令/密钥/任意参数上限。
- Agent -certificate-root继续与file-root隔离；-certificate-ca只能由可信启动指定公开CA，否则用系统roots。Agent在写文件前独立检查本地CSR签名/hash、实际私钥匹配、精确单SAN域名、链/ServerAuth/有效期和普通UID/0700/0600权限。文件名由叶证书SHA派生，独占不可覆盖；已存在相同实际字节只做只读对账，不同/部分/未知文件不重写。执行中断分类UNKNOWN。
- Agent仅输出公开摘要/到期时间；Controller验证正确execution/第一stdout帧/计划/节点/实际typed回执，audit与transport owner事务同提交。没有回执的成功帧记UNKNOWN；GET不以队列/ACK/输出文字冒称安装完成。
- 追加cx/0016_node_certificate_chains，预计正式35项。正式Web提供公开证书导入、任务审批提示、实际回执与发布certificateId=nodecert-<taskId>，不接收或持久化用户私钥。
- Gateway私有启动文档可增加certificateRoots:[{nodeId,directory}]；该目录必须与Agent实际证书根一致。动态nodecert-命名空间由已成功安装任务、当前读权限/节点绑定及未过期回执解析，路径由私有启动根+受限引用+叶SHA派生，不从HTTP接收文件路径。每个gateway必须属于该证书节点；禁止把一个节点私钥槽跨节点使用。预置旧证书接口仍保留。混合节点多网关需要独立证书映射，不能以同一nodecert蒙混通过。
- 发布探针对该新来源额外比对实际TLS叶证书SHA，并继续验证当前连接/任务/路由token/HTTP实际字节。安装、重载与HTTPS健康仍分开取证，不能据此称全链通过。

集中验收需涵盖实际CLI/WSS、坏链/隐藏私钥/错key/SAN/过期/不受信根、权限变化/错误CSR任务或执行ID/缺typed回执/取消重启UNKNOWN、同槽不覆盖、跨节点拒绝、实际nginx -t/reload及TLS叶/响应字节、旧节点DTO/预置证书兼容。尚未执行。
