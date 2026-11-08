# 正式程序启动装配（2026-11-08候选）

本页记录产品可用入口与真实边界；实现候选以PR精确head接收为准。固定Go/Node/工具、普通用户与私有目录要求仍见TOOLCHAIN；不使用旧Windows路径、代理、运行库或签名材料。

## 文件与后台清理

Controller的-file-deployment保持原可信节点根映射。用户上传同名策略从页面选择并冻结；重新选择原文件续传时须读取策略/目标/摘要绑定，修改当前选择不会修改旧会话。skip表示未覆盖旧文件，rename显示实际路径，UNKNOWN不自动重放。

Controller默认接通本服务DNS cleaner，每30秒最多处理10条到期的已知pending/retrying。每条操作采用原所有者当前cleanup与DNS权限，后台审计SourceSystem，不创建拥有权限的cleanup-service账号。只匹配TXT记录的ID、FQDN、值；明确未执行的失败可有界重试。超时/连接未知/远端成功而本地提交失败等不确定错误持久UNKNOWN，过期lease也仅隔离、不再删除。隔离/quarantine阻止后台循环，取消仍优先。可用-disable-cleanup-retries关闭后台扫描；管理员HTTP重试仍有原权限门。

## 可信本地签名Agent更新

Controller新增-update-deployment /绝对私有目录/update.json，仅管理HTTPS模式可用。配置文件0600、父目录0700、当前用户所有、禁止符号链接/硬链接；真正更新部署仍要求普通用户Linux与其原有全部身份、进程所有权、签名、当前权限、schema漂移门。配置不创建目录、不生成签名/私钥、不替换生产文件、不接收HTTP可执行路径。

JSON version=1，必须含以下字段（camelCase）：directory（slot/active所在私有目录）、inbox（现有DirectoryReleaseSource格式的签名发行文件）、stageDir（三个目录独立）、keyId、nodeId、identityDir、controllerOrigin（HTTPS Origin）、rootCertificate（PEM路径）、publicKeys（key ID到base64 Ed25519公钥）、candidateVersion（固定三段版本）、initial（ReleaseManifest完整字段）、minimumSequence、maxBytes（最大256MiB）、schemaVersion和schemaFingerprint（受审sqlite_master SHA-256）。未知字段/重复键/后附JSON/不安全路径/不支持组件拒绝启动。程序注入数据库/审计/任务/管理员/运行门/当前节点连接校验；配置不能提供替代回调放行。

initial必须对应已签名inbox及真实active程序；Start会核对实际签名/字节并建立已安装出处，按绑定身份启动并监督自己拥有的Agent；遗留update-lock/reservation、其他进程、schema漂移与健康失败遵从原有拒绝/UNKNOWN/回滚逻辑。不是把一份JSON写好就算验收通过。

该入口装配已有本地签名inbox的Agent组件更新，不是可信在线发行、Controller自更新或全舰队canary/batch发布。恢复的UPDATE原文只明确Agent范围，Controller更新需求仍须依据现行需求协调，不能凭本装配宣布完成。B05完整证据仍OPEN。

## 待交付

Standalone发布路由检查器/完整装配、node-local网站私钥/CSR与ACME组合、完整更新批次、加密一致备份/恢复CLI和新权威fence等继续实施。用户负责手动验证；真实外部/物理双机/ARM运行/硬1C1G未在当前环境自动证明。当前只映射UID0，普通用户实际程序测试需原生CI，不降低门槛。
