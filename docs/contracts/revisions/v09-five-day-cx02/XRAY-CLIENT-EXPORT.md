# 单用户受控客户端导出（PROPOSED）

R35 / CX-06；可选plugins/xray-manager/export.go，WithClientExport(ctx,request,read,admit,borrow,deliver)。未挂载既有client-export POST或新增GET，不由核心import。只实现vless-uri与json（outbounds配置段，不创建本机监听器或覆盖客户端）；qrcode仍拒绝并保持未完成，不伪装二维码内容/完整订阅门户。

request由可信Controller产生，包含当前真人Web主体、相同recipient、scope/user、requestId、可选approvalId、至多2分钟期限与格式。当前已应用配置/用户reader提供固定node/instance/inbound/user、凭据引用+修订、配置/用户/插件修订、版本化模板、连接地址/SNI/端口及REALITY公共验证参数；用户/插件必须启用、观察不超过30秒。禁AI/system等源、跨收件主体、失效/未来观察、未知版本/模板、不规范IP/DNS/IPv6 zone、无修订/控制字符/错误public key与shortId。观察值/Source字符串不是身份或批准，admit必须重查真实认证/CSRF与机密reveal权限、节点/实例授权、确认/适用审批及插件当前可信启用/版本；没有默认放行。

冻结全部稳定字段（不含每次观察时刻）计算服务端域分离digest。借用只允许当前用户的canonical v4 UUID，不借服务端REALITY/TLS私钥；REALITY pbk来自已登记公共验证材料，不能由返回私钥临时推断。URI按上游提案使用type=tcp、Vision flow、TLS/REALITY与SNI，REALITY含fp=chrome/pbk/sid；IPv6加括号、标签转义、无重复query、不含allowInsecure/管理API/服务端key路径。JSON仅固定26.6.1兼容outbound字段；已扫描发布物仍禁止执行/安装，formatter测试不构成真实客户端互通或二进制准入证明。

borrow必须是中央凭据服务的当前、持久一次性借用/确认/审计适配：只有其回调后授权及审计提交成功、再次读取的用户/凭据/配置/插件仍与冻结值相同，才调用一次deliver。错误/panic、回调丢失/重复、撤权/轮换、输出结果不明都失败/UNKNOWN，不自动重试；保留的借用回调在函数返回后失效。输出缓冲尽力clear，fmt隐藏client metadata；Go URL/JSON字符串和运行时拷贝不能保证完全擦除。deliver只能供当前受保护响应，须no-store/同源Cookie+CSRF/安全输出处理，不能交普通日志、AI或另一个收件人。

**具体未接收接缝**：auth当前通用CredentialService仅支持已有四种kind，不接受Xray UUID/REALITY；本轮未把其他种类伪装Xray凭据，也没有绕过中央库另造秘密来源。Read/admit/borrow测试明确double，真实实例绑定Xray材料/凭据借用及用户持久服务继续实施，正式typed Host与安全政策/审计/路由/页面仍未接收。单次跨重启语义只有真正中央fence接通后才可验收，不由格式函数自己新建队列或临时map证明。

Windows三个新增顶层测试PASS（含URI/TLS/REALITY/JSON/撤权/轮换/审计失败/双回调/源与地址负向），完整可选包/architecture及staticcheck PASS，精确日志export-r35-final.log。首次编译FAIL多余strings import，移除后通过，未删除失败例。Ubuntu非root原生可选包11顶层PASS/3真实Xray二进制测试SKIP，LINUX-XRAY-EXPORT-EVIDENCE.json记录精确源/二进制/日志；未执行任何已知不安全发布物。

参考：[XTLS分享链接提案](https://github.com/XTLS/Xray-core/discussions/716)（2026-10-30查阅，type=tcp/pbk兼容名称与flow、SNI等）、[官方传输说明](https://xtls.github.io/en/config/transport.html)。当前上游文档可能与26.6.1不同，不将浮动文档当固定版本JSON测试证据；本功能唯一需求仍为受保护FR-PLUGIN-XRAY-001与原client-export契约。
