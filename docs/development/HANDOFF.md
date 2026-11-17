# 后续 Agent 开发交接

## 2026-11-18 AI 推理流与现成 Agent（任务分支，未部署）

AI-AGENT-SDK-20261118；feature/ai-agent-sdk-20261118，基于 main d71a2f0。
新增模型 Markdown 渲染、公开完整推理字段与摘要/计划分离、
Responses/Chat Completions 函数调用兼容及 Eino ADK v0.9.23 装配。
按用户要求侧重功能样例验证，复用现有执行底座；
[实现、验证与边界](AI-AGENT-SDK-20261118.md)、
[框架接入范围](../architecture/AI-AGENT-ADK-20261118.md)。
此前测试站仍运行 fc0416a，本轮未自动部署；最终提交与验证以 PR 回执为准。

更新日期：2026-11-18。接手现有项目，先按以下顺序阅读：

1. 根 [AGENTS](../../AGENTS.md) 与 [当前状态](CURRENT-STATE.md)：任务范围、代码缺口、发布和未验收范围。
2. [功能说明](../product/FEATURES.md)、[API](../api/README.md)：现有行为和应保留的界面语义。
3. [项目结构](PROJECT-STRUCTURE.md)、[模块归属](MODULE-OWNERSHIP.md)、[依赖规则](DEPENDENCY-RULES.md)。
4. [工具链](TOOLCHAIN.md)、[集成清单](INTEGRATION-CHECKLIST.md)；安装任务读 [deployment](../../deployment/README.md)。
5. 涉及架构决策时读 [批准 ADR 来源与替代关系](../architecture/README.md)。

## 最新测试站部署

2026-11-18 北京时间 00:35 已按本次用户授权部署 main fc0416ae4410f1858a494b78a8d29487a155a712，
手工版本 0.9.3-ai-continuity-20261118，包含 PR #55/#56。
先读 [部署回执](DEPLOY-AI-20261118.md)；下方 AI/PG 原任务“未部署”是历史状态。
用户本次明确不做备份；没有更新 Agent、签名安装分发或正式 Release。
后续部署仍按后续用户任务决定，不自动继承本次操作授权。

## AI 体验增量

当前任务目录 awskr /home/admin/sf-ai-memory-20261117，
分支 fix/ai-memory-continuity-20261117，基于已合并 PR #55 的 main b5f2f98。
先核对 [AI 改进记录](AI-IMPROVEMENTS-20261117.md) 与最终 PR 回执；
旧 PostgreSQL 任务及原未提交目录保留，不自动部署线上。
公开思考摘要与工具回执可展示；原始推理、供应商秘密与旧范围资源授权不可继承。


## PostgreSQL 维护增量

任务分支 fix/postgres-maintenance-20261117 基于 main 3b32b1f；
原 awskr 未提交现场保留。先核对 [维护基线与验证](POSTGRES-MAINTENANCE-20261117.md)，
不要将旧目录的 HEAD 或线上二进制直接等同于 GitHub main。
备份为可移植应用快照，PostgreSQL 导入只允许空目标；
Supervisor/恢复激活使用受策略约束的私有 DSN 文件，持续 fence 与审批仍有效。

## 开工与判断依据

从远程当前 main 建独立分支/工作目录，先检查 head 和本地变更。当前维护分支为 fix/postgres-maintenance-20261117，基于 main 3b32b1f51b1d9f9f862fcbc9b896153f5eaebbc7。
PR #54 已合并；其改动是本任务起点。历史基线 71dbca3 仅用于追溯，不要求后续回退到它。main 前进后核对差异并刷新 CURRENT-STATE。

当前用户请求决定任务。任务板用于记录，旧 OPEN/READY/IN PROGRESS 不自动授权新会话执行；历史生产主机和凭据授权不继承。记录任务 ID、唯一会话、目录、验证与停止条件，只有明确要求并行才启动其他 Agent。同一 GitHub 账号不构成独立 APPROVE。

判断顺序：当前请求与批准约束 → 当前源码/配置及增量契约 → 现行功能/API/部署文档 → 精确提交的证据 → 历史计划/原型。实现与批准要求冲突时记录差异并按任务修复，不能改写历史制造一致。

## 前端重构须知

- React 正式页面已接入 Controller，不按旧 P1 说明改回 Mock-only。入口 `apps/web/src/app/routes.tsx`，API/DTO 在 `apps/web/src/lib/api/`，加载状态在 `apps/web/src/lib/data/`。
- 会话、Origin/CSRF、节点资源权限、独立审批、版本冲突、UNKNOWN、加载/空/失败/缺观测值均属行为契约。仅首个 Bootstrap 管理员对当前节点具有读取、编辑、暂停、删除的跨节点权限；其他用户和管理员仍需逐节点授权；“已提交”不等于执行成功。
- 路由索引用于定位，没有一份已全字段核验并冻结的统一 OpenAPI。逐功能核对 HTTP DTO、增量候选、客户端与测试；缺口补独立修订，不猜字段。
- 文件互传前端按 `file-pane.tsx`（单栏）/`transfer-history.tsx`（历史弹窗）/`files-page.tsx`（页面骨架）拆分；逻辑仍只走 Controller REST，保留待确认上传与 UNKNOWN 语义。
- 纯视觉调整保留业务契约；新增/修改/删除交互同步产品/API。Xray、受限 Bearer 的未装配状态不得展示为已可用。

## 交付

按影响验证。纯文档不运行完整 CI，旧绿灯不能算本次通过；需要轮询时十分钟一次。PR 按 expected head 正常合并，结果补评论，不为记录绿灯反复编辑 PR 说明。

完成后更新 CURRENT-STATE、现行功能/API 和任务板；历史记录保留。区分已实现、条件装配、实机未验收、未实现，不报无依据完成率，不凭 Release 存在宣称全部产品验收。
