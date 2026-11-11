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

## 正式控制台同源托管与核心构建包

本分支新增 Controller `-web-dir /绝对路径/web`，仅管理HTTPS模式。它加载已构建index.html/assets，不依赖fixturehost、Vite开发服务器或浏览器代理。Web与原REST/SSE/Agent入口共用TLS服务；仅GET/HEAD构建资产可读，未知API不会回退成HTML，API身份/Origin/CSRF与恢复隔离门不变。

web目录及assets目录0700、所有文件0600并由当前进程用户所有；拒绝链接/隐藏/源代码树/未知根文件/丢失入口脚本。仅限index.html、可选favicon.ico及assets内受限类型的平面构建文件；单文件8MiB、总32MiB、最多1024文件。启动时读成不可变快照，后续替换文件不会发布新内容。响应带CSP（脚本/连接仅同源）、nosniff、no-referrer、no-store，HTTPS带HSTS。构建包脚本自动设置权限；直接使用Vite dist前需人工核对/设置这些权限。

固定Go1.27.4/Node24.22.1/npm11，依赖按锁文件准备后，在干净已提交的Linux工作区运行：

```sh
node scripts/build-release.mjs --version=0.9.0 --sequence=1 --output=/absolute/private-parent/new-candidate
```

output必须是仓库外的新目录，父目录已存在且不能通过别名/符号链接绕回仓库。不复用旧打包副本、不打入.tools/cache/reports/数据库/私钥/Token/Xray可执行文件。输出linux-amd64和linux-arm64各自的controller、agent、web，以及包含源SHA/实际工具版本/全部文件字节数与SHA-256的manifest.json；仅交叉构建，不等于ARM实际运行或硬1C1G验收。

`agent.manifest.json`是待离线签名的规范ReleaseManifest，签名消息仍为`serverfleet.release.v1\0`+原始JSON字节；不自动生成信任键/签名、不自动灌入inbox。Agent健康版本与默认入网版本、Controller `-version`均写入同一明确构建版本。失败保留build-status.json的INCOMPLETE状态及部分输出；成功必须有绑定源SHA与manifest摘要的BUILT_UNSIGNED_UNVERIFIED正向回执，不能安装/签名；成功也明确UNSIGNED/UNVERIFIED，不代表发布认证或可信在线发行。没有自动安装/systemd启用/网络部署步骤。正式生产访问与云资源不在当前授权中。

## 2026-11-09 核心优先包新增入口

上述2026-11-08边界保留为当时检查点。集中编码分支已新增普通/Root受控命令、节点执行端续传、独立Controller/远程Agent监督更新和实际拥有进程的恢复fence/激活；当前仍待整包集中验收，不以历史绿灯证明。实际配置、部署模式和未覆盖环境见[五项核心接线](CORE-PRIORITY-STARTUP.md)。构建输出现在为Controller和Agent都生成规范待签名manifest；不会自动签名或部署。
