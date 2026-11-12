# R39：provisioned 证书链与 raw 安装

## 修改

- `LoadProvisioned` 的尾链传 Root DER，签发器构造拒绝重复/乱序/未以固定自签根结尾的链。新签发和续期均输出完整唯一链。
- 入网与续期响应遇到确认的历史 Leaf/当前 Intermediate/同 Intermediate 缓存时，验证原 DER/签名/serial/fingerprint/expiry 后，仅重组公开 PEM 响应。身份/原签发记录和审批保持，失效/撤销/过期令牌门不变。
- `GET /api/v1/installations/catalog` 的既有 `installerScript.url/sha256` 可指向本版 raw 入口；类型/鉴权/空配置 `available:false` 不变，不新增业务 HTTP 路由或字段。
- 控制台安装命令为新版本传入公开的临时 bootstrap CA，旧命令行为兼容；命令从不含 enrollment token。raw 初始脚本使用系统 HTTPS 信任；程序仍签名校验和 Root pin。

## 新增 CLI/脚本入口

- `deployment/raw-install.sh` 模式：download/setup/update-distribution/controller/agent。agent 接收原生安装器参数；controller 验证公开签名包后使用显式组件绑定。
- `deployment/install.sh --bootstrap-ca-base64`：仅用于引导程序 HTTPS 的公开 CA；`--component` 绑定 Go installer 组件。
- `controller install -component controller|agent` 可选，与 config 不同则在修改目录/网络安装前拒绝；未传保持原 API。
- `setup.py fetch` 在线验证发行 API、摘要、tag 和 raw 字节并导出离线材料；`prepare --checksums/--checksums-sha256` 保留独立首次信任；`update` 仅提升已有向导分发的版本/sequence，保留旧文件；`install-controller` 引导现有原生安装协议。

没有新增数据库迁移/Proto字段/依赖/执行通道。部署前提、失败回退和未做实机验收范围见 deployment/RAW-INSTALL.md 与 QUICK-SETUP.md；不改写已批准 ADR 或历史候选。
