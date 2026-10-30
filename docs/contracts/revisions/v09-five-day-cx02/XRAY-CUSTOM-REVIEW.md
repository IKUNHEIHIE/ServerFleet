# Xray自定义JSON最小安全审查（候选）

实现 `plugins/xray-manager/custom.go` 的 `ReviewCustomJSON(raw,CustomPolicy)`，不引入核心Release或根依赖。锁定26.6.1，输入≤1MiB、深度≤32、值节点≤100000、入/出站各≤128；拒绝重复/大小写别名字段、尾随第二文档、无效UTF-8、未审顶层模块和重复tag。解码副本只用于审查，原始字节不改写，摘要绑定实际输入；输出只有摘要/数量/固定warning code，不返回配置或秘密。

CustomPolicy必须来自当前受控预检和文件授权，不能直接取浏览器/model字段。已识别file字段以及日志access/error只允许当前实例目录内的精确授权文件；拒绝跨实例/非规范路径/环境变量、include/command/script/confdir和显式ext文件引用。文件检查不代替目标进程OS沙箱或穷尽Xray所有扩展字段；未经审查的新模块/资产/Unix socket/metrics部署profile保持拒绝。原配置不被静默“修好”，需要作者自行提交符合边界的修订。

管理API支持两种受限布局：直接回环IP:port，或一个严格的回环dokodemo-door入站及首条专用路由。只允许StatsService/HandlerService。所有普通代理流向在自定义规则之前必须有管理端口到blackhole的拒绝规则，不允许再由其他规则/出站/tag/loopback/balancer绕回API。主控实际部署还须满足本机执行器/Host的独立准入，不能把这个检查当作任意root配置执行许可。

后续R20补严出站在routing之后覆盖目标的情况：例如freedom.redirect可把一个原本不匹配管理端口的请求改送本机API。审查还会拒绝出站端点/redirect/URL引用管理端口（含DNS别名、前导零及结构化address+port）；不靠DNS解析猜测安全，不静默重写。其他redirect/finalRules产生明确EGRESS_POLICY_OVERRIDE_REQUIRES_REVIEW风险码。依据固定26.6.1上游[Freedom配置](https://github.com/XTLS/Xray-core/blob/v26.6.1/infra/conf/freedom.go)及[出站处理](https://github.com/XTLS/Xray-core/blob/v26.6.1/proxy/freedom/freedom.go)。实际二进制-test接受该危险redirect语法，而本安全审查拒绝；测试没有启动危险配置。这是输入准入负向，不是已部署生产漏洞或完整OS隔离证明。

两套标准模板同时补入`sf-block`，专用本机API入站先路由，再拒绝所有普通代理目标管理端口及回环目标。只绑定127.0.0.1不足以隔离能访问本机网络的代理客户端。阻断该管理端口也影响代理访问远端相同端口，这是模板的明确安全限制；自定义配置不得悄悄移除保护。没有修改用户提交的custom-json。

固定warning包括自定义管理员审查、目标版本实际-test和流量可用性未证实；REALITY转发、allowInsecure、debug日志、TUN系统网络变化额外提示。唯一任务提交必须绑定原始摘要、全部明确风险确认、当前actor/node/plugin/revision与受控文件资料，并在目标机器用目标二进制实际-test。Review成功不等于配置有效/已发布/已启动/权限通过；未输出fake jobId或成功健康状态。

依据：Context7先resolve/query `/xtls/xray-core/v26.6.1`，检索结果较浅后核对固定上游[APIConfig](https://github.com/XTLS/Xray-core/blob/v26.6.1/infra/conf/api.go)、[配置装配](https://github.com/XTLS/Xray-core/blob/v26.6.1/infra/conf/xray.go)、[日志文件选项](https://github.com/XTLS/Xray-core/blob/v26.6.1/infra/conf/log.go)及固定官方二进制。没有浮动latest或选择新的生产运行时。

真实Windows实验在`run-xray.mjs`原有固定artifact/exe哈希下：两模板-test及启动继续通过；两份自定义配置实际-test通过；最低审查可接受的不存在协议被实际Xray-test拒绝，证明没有以本地审查替代目标校验。实际本机SOCKS→私有API负向：直接API确实响应HTTP/2，再经受保护代理不能得到API响应。仅使用本机回环及测试进程，非完整VLESS客户端/Linux/插件Host/部署历史证据。

首轮真实模板-test失败：额外IPv4-mapped CIDR被Xray26.6.1明确拒绝（104前缀超其转换后的32位上限）。保留失败日志，改为受支持127/8和::1/128；管理端口拒绝同时覆盖IPv4/IPv6映射访问，不降低安全要求。历史/回滚、持久用户/受控导出、Stats真实采集和Linux安装继续未完成。
