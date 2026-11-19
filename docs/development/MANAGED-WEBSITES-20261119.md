# 主控网站证书交付回执

- 任务 MANAGED-WEBSITES-20261119，会话 codex-managed-websites-20261119-01。
- 基线 `da35754`，交付源码 `a1e733e705e8dbaba58bfe9bcf430ee73effa6e6`，测试站版本 `0.9.3-demo-managed-websites-20261119`。
- 2026-11-19 16:52 CST 部署至 awskr 测试站。部署范围为 Controller、网页和同机 Agent。源码提交保存在 awskr 的任务分支。

## 功能

输入域名，确认首次签发。主控自动匹配 DNS 区域并签发。同域名使用一张证书，网站私钥加密存储。
按名称搜索、多选节点或选中全部搜索结果，再确认下发。当前已确认的目标接收后续版本，Agent 验证证书和私钥后切换当前文件。
出现不确定结果时保留原版本并显示任务状态。现有内部 TLS、历史证书和任务保留，旧续签计划已停止。

## 验证

| 项目 | 结果 |
| --- | --- |
| 无节点时主控首次签发与自动续签 | PASS。隔离 CA，签发、续签各产生一个订单 |
| Agent 证书版本切换与私钥匹配 | PASS。隔离证书验签、文件权限和旧版本拒绝覆盖 |
| 网页 | PASS。内部下发及新域名、快捷选择用例共 2 项，生产构建通过 |
| PostgreSQL | PASS。独立实例迁移通过，测试站新增迁移 `cx/0025_managed_websites` |
| 接口索引和构建来源 | PASS。生成索引核对成功，主控与 Agent 均来自记录的精确提交 |
| 测试站 | PASS。五项就绪、五节点在线、同机 Agent 协商 `certificates.website.install`，四条旧订单摘要一致 |
| 实机新公网订单及多节点下发 | UNTESTED。本轮实机核对部署与能力，申请和安装使用隔离材料验证 |

## 部署工件

- Controller SHA-256 `224889a962fac588c9df099e8d52cba2bee1fdc8799b32c04bd349bb53fd1ce8`
- Agent SHA-256 `ba60aecf4a240313c0dc14f7eef5ec2c13d11e21fed351f66d6c0764de21c0e5`
- 网页首页 SHA-256 `3532fd7798e0d9542013f270ffe59ffa9d98e3d65873aee00c0e7ea50faca6d2`

节点运行新版 Agent 并启用证书目录即可参与下发。业务进程按自身配置加载证书文件。
