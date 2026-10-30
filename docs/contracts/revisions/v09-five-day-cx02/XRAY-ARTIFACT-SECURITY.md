# 外部Xray发布物安全门槛（候选）

2026-10-30，codex-02。先前固定26.6.1只是兼容性实验目标，不是已批准生产版本。额外执行固定govulncheck1.9.0 `-mode=binary` 后，官方Windows amd64发布物扫描exit3，55项符号级发现，涉及7个模块及Go标准库。原始日志在本目录忽略报告`reports/_review/v09-platform/xray-binary-security-r21.log`，不能用根npm check:security/隔离module源码扫描PASS覆盖。

- 来源：https://github.com/XTLS/Xray-core/releases/tag/v26.6.1
- ZIP SHA256：0a1ee8c8a45e11e865c725db195429c79b52ee960407065089c5be586f343248
- EXE SHA256：9bd2d873d78b5c39303ab16db630a131f9f33836e98d96455f80a8092fecdcd5
- Go build info：go1.26.3；主模块(devel)，扫描对主模块的精确版本覆盖另有限制。
- 示例发现：GO-2026-6809（crypto/x509）、GO-2026-6617（net/http及x/net）、GO-2026-4970（os.Root），另有x/crypto SSH等模块发现。二进制扫描无法证明具体配置路径可达/可利用，不能夸大为55个已复现攻击；也不因当前模板未用某协议就自动豁免。

处置：该包不得作为Server Fleet发布/安装目标；保留历史功能证据，新增runner扫描前置，非零直接停止执行，不关扫描/删记录/降门槛。没有安装到实际节点，核心入口和插件runtime依旧未启用，不是线上事故。

GitHub连接工具已只读核对正式发布v26.10.29（2026-10-29发布、非prerelease）。公开HTTPS下载并核对固定asset SHA256后，静态扫描同样 **FAIL / exit3 / 3项符号级发现**：GO-2026-6866、GO-2026-6860（x/net v0.61.0，修复版本v0.62.0），GO-2026-5932（x/crypto v0.58.0，停止维护的openpgp，无修复版本）。Go build info为go1.27.3、主模块(devel)。未执行该新版二进制，未把它替换为模板目标。

- 新版ZIP SHA256：fce33e81cccf50e7aab572b175519a50b5e0f9d2f5c8d1ed73012ea5c9ad4d58
- 新版EXE SHA256：07a332b714a3c0eee047a22a9c0ea749b64df33e4e1ff6d28a8c7f9ef943ce10
- 原始日志：reports/_review/v09-platform/xray-v26.10.29/binary-security.log。
- 复查入口：`node tests/poc/v09/platform/audit-xray-release.mjs 26.10.29`（或26.6.1）。只允许两份精确pin，校验归档/EXE、只读build info、扫描并保存static-audit.json；非零保持FAIL，永不启动EXE。新下载有超时/64MiB界限/独立partial，失败不污染正常缓存。

两份工件均不作为可发布/安装目标；没有默认豁免或自行重编译的“官方包”。后续候选必须先静态扫描、再核版本/API/两模板/真实流量与负向，才可更新本包兼容目标；不追浮动latest、不借此冻结G2/Host/profile或伪造官方签名。Linux两架构尚未扫描/安装验证，不能外推本次Windows结果。

扫描方法和限制依据[固定govulncheck1.9.0文档](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck@v1.9.0)。最终生产准入还需要发行来源/签名/双架构/安装/当前策略与任务审批证据；单次扫描无已知漏洞不等于全面安全。
