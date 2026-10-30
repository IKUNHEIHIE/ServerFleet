# 外部Xray发布物安全门槛（候选）

2026-10-30，codex-02。先前固定26.6.1只是兼容性实验目标，不是已批准生产版本。额外执行固定govulncheck1.9.0 `-mode=binary` 后，官方Windows amd64发布物扫描exit3，55项符号级发现，涉及7个模块及Go标准库。原始日志在本目录忽略报告`reports/_review/v09-platform/xray-binary-security-r21.log`，不能用根npm check:security/隔离module源码扫描PASS覆盖。

- 来源：https://github.com/XTLS/Xray-core/releases/tag/v26.6.1
- ZIP SHA256：0a1ee8c8a45e11e865c725db195429c79b52ee960407065089c5be586f343248
- EXE SHA256：9bd2d873d78b5c39303ab16db630a131f9f33836e98d96455f80a8092fecdcd5
- Go build info：go1.26.3；主模块(devel)，扫描对主模块的精确版本覆盖另有限制。
- 示例发现：GO-2026-6809（crypto/x509）、GO-2026-6617（net/http及x/net）、GO-2026-4970（os.Root），另有x/crypto SSH等模块发现。二进制扫描无法证明具体配置路径可达/可利用，不能夸大为55个已复现攻击；也不因当前模板未用某协议就自动豁免。

处置：该包不得作为Server Fleet发布/安装目标；保留历史功能证据，新增runner扫描前置，非零直接停止执行，不关扫描/删记录/降门槛。没有安装到实际节点，核心入口和插件runtime依旧未启用，不是线上事故。

GitHub连接工具已只读核对当前正式发布v26.10.29（2026-10-29发布、非prerelease），只作为下一个验证候选；二进制下载工具不支持，公开release下载会另核固定asset SHA256。必须先静态扫描、再核版本/API/两模板/真实流量与负向，才可更新本包兼容目标；不追浮动latest、不借此冻结G2/Host/profile或伪造官方签名。尚未给出该候选扫描结论。

扫描方法和限制依据[固定govulncheck1.9.0文档](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck@v1.9.0)。最终生产准入还需要发行来源/签名/双架构/安装/当前策略与任务审批证据；单次扫描无已知漏洞不等于全面安全。
