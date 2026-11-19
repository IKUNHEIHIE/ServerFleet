# 内部 TLS 与公网证书操作简化

任务 CERTIFICATE-GUIDE-20261119，会话 codex-certificate-guide-20261119-01。工作树 /home/admin/sf-certificate-workflow-20261119，分支 codex/simplify-certificate-workflow-20261119，基线 c6840505370fc3dafca5c64f5ffc58d11f40f0e9。此轮范围为 awskr 测试站、同机 Agent 和获准的三个验收域名。

内部 TLS 选择服务组、下发内容并提交，从同页查看逐节点结果。没有内部签发器时明确提示。有效期在逐节点身份的可选设置中调整。

公网 HTTPS 选择安装节点、输入域名，按进度提交生成申请、签发及安装。页面根据当前受控连接和域名匹配 DNS 区域，保留原任务审批和回执核验。批准使用服务端提供的任务计划摘要。公开链自动带入安装，手动操作和续签仍可展开。测试站同机 Agent 增加独立网站证书目录，私钥由 Agent 写入。
