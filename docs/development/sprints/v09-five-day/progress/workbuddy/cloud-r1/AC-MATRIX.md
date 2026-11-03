# V09-CLOUD-WB-01 · AC-01—24 映射（本任务视角）

共享矩阵 `docs/development/sprints/v09-five-day/ACCEPTANCE.json` 由主控维护，本任务**不改**它。
本文件只记录：本轮实测触及了哪一条、提供什么证据、**状态是否因此改变**。

结论先行：**没有任何一条因本任务而可改为 ACCEPTED。** 全部保持 `NOT_ACCEPTED`。

| AC | 标题 | 本轮触及 | 证据 | 状态 |
|---|---|---|---|---|
| AC-01 | 节点接入、身份与生命周期 | 间接（专项内真实 enroll/审批/mTLS） | P05/P06 | NOT_ACCEPTED（专项前提，非验收） |
| AC-02 | 普通分组与自动分类 | 否 | — | NOT_ACCEPTED |
| AC-03 | 文件互传 | 否 | — | NOT_ACCEPTED |
| AC-04 | 服务组 | 否 | — | NOT_ACCEPTED |
| AC-05 | 批量任务 | 否 | — | NOT_ACCEPTED |
| AC-06 | Nginx 网关 | 否（本容器无 nginx，网关用例未执行） | — | NOT_ACCEPTED |
| AC-07 | DNS 管理 | 否 | — | NOT_ACCEPTED |
| AC-08 | 公网证书与内部 TLS | 否 | — | NOT_ACCEPTED |
| AC-09 | 监控告警与 QQ/SMTP | 否 | — | NOT_ACCEPTED |
| AC-10 | AI 会话资源授权 | 否 | — | NOT_ACCEPTED |
| AC-11 | AI 三级审批 | 否 | — | NOT_ACCEPTED |
| AC-12 | 插件平台 | 否 | — | NOT_ACCEPTED |
| AC-13 | 更新中心 | **是（Agent 组件本地签名更新一条链路）** | P10—P19：页面 check/plan/确认/勾选/202、异人审批、真实安装、计划阶段拒绝、真实回滚、重启持久 | **NOT_ACCEPTED**：Controller 组件升级、在线可信发行源、分批/金丝雀均未实现 |
| AC-14 | 工作流、用户权限与审计 | 间接（异人审批与审计事件） | P14、P17 | NOT_ACCEPTED |
| AC-15 | 服务发布向导 | 否 | — | NOT_ACCEPTED |
| AC-16 | 服务可用性监控 | 否 | — | NOT_ACCEPTED |
| AC-17 | 安全批量执行 | 否 | — | NOT_ACCEPTED |
| AC-18 | 节点恢复与主控灾备 | **是（仅前置条件探测，不是恢复）** | R06—R13：备份校验 200/verified；预览 eligible=false（2 项产品侧关闭）；创建作业 501；作业读回 404 | **NOT_ACCEPTED**：实际恢复、新权威、旧主控 fence、旧信任代次拒绝全部 UNTESTED |
| AC-19 | 安全凭据 | 间接（0600 私有配置、无秘密进日志） | P01/P08/R02 | NOT_ACCEPTED |
| AC-20 | 流量配额与到期提醒 | 否 | — | NOT_ACCEPTED |
| AC-21 | Xray 安装 | 否（派发要求 Xray 保持关闭） | — | NOT_ACCEPTED |
| AC-22 | Xray 配置 | 否 | — | NOT_ACCEPTED |
| AC-23 | Xray 用户与受控导出 | 否 | — | NOT_ACCEPTED |
| AC-24 | Xray 流量 | 否 | — | NOT_ACCEPTED |

## 检查点范围（实际，不外推）

| 检查点 | 覆盖 | 不覆盖 |
|---|---|---|
| CP-2 | Agent 组件本地签名更新：签名校验 → 冻结计划 → 异人审批 → 安装 → 健康 → 失败拒绝 → 真实回滚 → 重启持久 | Controller 组件升级、在线发行、分批/金丝雀、执行中断恢复 |
| CP-3 | B06 前置条件：备份一致性、备份校验、预览检查项、创建作业、作业读回、两条负向 | 实际恢复执行、新权威、旧主控 fence、旧信任代次拒绝 |

B03（Xray）、B04（发布）、B05（更新）、B06（恢复）**均继续 OPEN**。
