# 两套Xray模板与实际目标版本实验

可选plugins/xray-manager新增config.go，不由核心import。只为锁定26.6.1支持vless-tls-vision、vless-reality-vision；不猜兼容矩阵、不选浮动latest。已按Context7与该tag实际infra/conf/vless.go核对users/xtls-rprx-vision/decryption=none。两种均RAW，前者TLS1.3与证书受管路径，后者REALITY target/serverNames/shortIds与逐实例key引用。Stats/用户稳定统计email开启，管理API固定127.0.0.1，未加任意进程/Shell/SSH调用。

WithTemplate要求当前预检facts和mandatory admission；拒绝未接管已有实例、端口冲突、插件禁用、revision变更、30秒前的facts、证书域名/期限/路径不符、REALITY未确认fallback风险/目标TLS1.3不可达/空shortId。facts布尔值不是凭据，必须由Controller唯一任务绑定真实读预检、actor、node、scope、pluginRevision和plan；禁止从浏览器JSON接收为事实。没有已有实例的自动覆盖。

MaterialResolver按instance+purpose+ref向程序注册的凭据服务借用，仅接受canonical v4 UUID / 32字节X25519 privateKey。配置只送受控Agent暂存/目标版本-test/部署回调，非GET导出/模型/普通日志。返回缓冲清零，但Go字符串/编码器临时分配不承诺全部内存擦除；回调必须遵守取消。平台公共Credential kind新增与实例绑定尚需契约/安全接收，当前未给其他模块通用解密入口。

独立本机证据命令：固定Node执行`tests/poc/v09/platform/run-xray.mjs`。官方v26.6.1 Windows64 archive SHA256 `0a1ee8c8a45e11e865c725db195429c79b52ee960407065089c5be586f343248`，来自GitHub该release asset digest；解包只取xray.exe，额外固定exe SHA256 `9bd2d873d78b5c39303ab16db630a131f9f33836e98d96455f80a8092fecdcd5`，执行前重验。只有下载公开测试工件，不接云/VPS/真实凭据；二进制在忽略reports，不打入核心发行。

实际两模板均通过Xray run -test并启动专属子进程，只在回环动态端口检查inbound与API后终止自己进程。TLS测试只对本机临时证书路径做Windows位置映射；REALITY target使用保留invalid域，无外部目标通信。此证据不是Linux/双架构、VLESS客户端完整数据流、外部target TLS可达性、安装/回滚或正式WASM插件准入。没给SF_XRAY_LOCAL_TEST_BINARY的通用go test明确SKIP该项，不按普通单测PASS推定外部二进制验证。

尚需实现/接收：插件持久生命周期、Host隔离与能力接线、真实Agent预检/部署/健康、配置历史恢复、custom-json安全审查、用户服务/受控导出、Stats采集与持久对账。原始FR与API未改，本候选不冻结G2。

上游核对来源：[固定Release](https://github.com/XTLS/Xray-core/releases/tag/v26.6.1)、[固定VLESS解析源](https://github.com/XTLS/Xray-core/blob/v26.6.1/infra/conf/vless.go)、[官方REALITY说明](https://xtls.github.io/en/config/transports/reality.html)。
