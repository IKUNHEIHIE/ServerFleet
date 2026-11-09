# R14 节点本地网站私钥与公开CSR候选

V09-CLOUD-CODE-COMPLETE-01 / chatgpt-takeover-01。UNTESTED；按用户要求集中编码后统一验收。原候选/Proto/gen/原始资料及已接收SQL/hash不改，不宣称G2或完整ACME/发布验收。

新增CSR task创建/已完成公开回执GET，以及现有TaskOffer schema 1中的显式certificates.csr.create。管理员仍须目标节点资源；修改类、真人审批、当前审批者/提交者权限、计划/参数/连接和节点状态检查有效。AI/plugin、Root Shell、多节点、假node及一般任意queued执行不获准。CheckDispatch仅处理精确待派发节点的已有冻结任务，不替代running-only CheckExecution，不放宽queued网关工件上传。

Agent仅在显式-certificate-root下注册能力；Linux普通UID、0700目录，必须与-file-root彼此不相包含，0600单链接文件/独占密钥引用，不允许通过普通文件worker读网站私钥。只生成P-256公钥CSR，域名/单SAN/签名/hash/节点/引用严格绑定，私钥不进入Controller或传输；已有/部分引用拒绝，不重建或覆盖UNKNOWN。阶段中断保留私有证据。

Controller保存公开CSR需当前running task/节点/执行ID/未截断stdout第一帧、域名/引用及CSR签名/hash核验；transport owner提供持久端口，公开回执与audit同事务。缺typed回执的成功终态归UNKNOWN；GET只在统一任务成功完成且当前读权限成立后返回公共数据。新增cx/0015_agent_certificate_csr，预计正式34项。正式Web提供登记/审批提示/回执读回，不把CSR生成称作证书签发或网站上线。

ACME新增IssueDNS01WithCSR纯公开CSR入口，验证必须在任何ACME/DNS副作用之前；只使用账户Signer签ACME请求，不借用Controller签网站CSR。旧Issuer入口仍保留，完整公开签发→节点证书安装→Nginx/HTTP链及实际provider接线继续实现。

新Controller-local任务hash单独域隔离并绑定权威/source/approvalRequired；已有节点hash字节算法保持，旧批准计划不重算。集中验收需真实Controller/Agent WSS、无能力/错误私有目录/未知或重复引用/路径逃逸/AI/无审批/排队撤权/错误执行ID或CSR/缺回执/取消重启UNKNOWN及Web实际终态；尚未运行这些检查。
