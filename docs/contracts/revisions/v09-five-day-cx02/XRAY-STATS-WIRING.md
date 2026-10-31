# Xray Stats读取、精确计数与真实TLS/Vision实验（候选）

`DecodeStatsJSON`处理固定版本StatsService非复位读取结果，不直连节点/Agent、不import数据库、gRPC或Xray-core SDK。输入上限1MiB、UTF-8/嵌套/元素有界，拒绝重复/大小写别名/尾随文档、重复counter、负数/溢出/浮点不精确值。int64字符串/JSON整数精确保留，2^53+1不走float64。已出现counter的protobuf默认value可为0；缺失任一方向则Available=false，不把缺失填成观测0。

StatsBinding来自当前受管配置的内部userId→email/inboundId→tag映射，禁止任意通配前缀或浏览器自报。按user、inbound、instance分视图，instance只汇总登记的业务inbounds；排除管理API、不加user+inbound/outbound，重复base与溢出拒绝。模板ID也补拒绝>>>分隔符注入。采样SegmentID/At须来自真实受控进程/启动观察；数值回退或JSON自身不能证明新进程，不能用时间戳猜“成功重启”。

成功读取才调用decoder；实际读取失败保持Unavailable并交Accumulate形成缺口。Accumulate首样本只建基线、进程区段变化/计数复位不虚构中间用量、保留历史总计；Source完整性与每维展示均独立，不作为VPS账单。当前函数/状态可组合，尚未接生产Host、当前权限、Node collector、Controller时序存储或页面。

## 实际Windows局部证据

固定官方26.6.1二进制、本机真实VLESS+TLS+Vision客户端/服务端、临时CA及真实verify信任，不使用allowInsecure；通过客户端SOCKS向测试HTTP目标发送请求/读取响应。仅为此fixture添加精确回环HTTP目标redirect/finalRules允许，保留管理API防护，不接公网/外部节点。一次采样测得user上/下行45208/28829字节，inbound与instance各46773/28947（实际运行可因TLS开销略变），三个视图不相加。

真实StatsService由固定CLI `api statsquery -s 127.0.0.1:<apiPort> -pattern ''`读取，默认reset=false（固定上游命令/help与源码已核对）；重复读取没有清零。停止API后查询明确失败，重启实际Xray进程、再次传输、采样新PID/启动区段，历史总计保持且首个新段增量未知。测试不是生产持久历史、Linux双架构、REALITY客户端或完整POC-04。

**重要：功能实验后，额外govulncheck二进制扫描发现该26.6.1发布物55项符号级漏洞（exit3）。上述是历史功能证据，不能当作可发布版本。** 固定源/证据见XRAY-ARTIFACT-SECURITY.md；旧包禁止发布，runner新增先扫描再执行的门槛，安全FAIL不改成PASS。核心与隔离module的源码扫描PASS不覆盖这个外部Go1.26.3二进制。

## 尚缺的共享接缝

当前Agent Proto的MetricsPoint.value=float64不能无损承载全部int64字节计数；TaskOutput只是日志，不能偷作遥测/文件数据通道。本包不会将Stats JSON伪装stdout、拆高低位藏进指标或调用插件直连Agent。S03须接收有界、版本化、当前node/execution/action/plugin/config/process绑定的结构化只读结果/遥测端口，重复/陈旧/换进程都需审查；公共Proto/生成/兼容由总控单写。未接收前，本机真实CLI实验与纯mapper继续独立，不声称生产采集已经接通。

同时复核当前上游TLS/file字段后补拒绝masterKeyLog和未授权dir能力，防止自定义配置写TLS秘密日志或公开目录；未知扩展仍需正式OS/Host边界审查，最小JSON审查不是沙箱。
