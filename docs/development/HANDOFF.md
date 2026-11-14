# 后续 Agent 开发交接

更新日期：2026-11-14。接手现有项目，先按以下顺序阅读：

1. 根 [AGENTS](../../AGENTS.md) 与 [当前状态](CURRENT-STATE.md)：任务范围、代码缺口、发布和未验收范围。
2. [功能说明](../product/FEATURES.md)、[API](../api/README.md)：现有行为和应保留的界面语义。
3. [项目结构](PROJECT-STRUCTURE.md)、[模块归属](MODULE-OWNERSHIP.md)、[依赖规则](DEPENDENCY-RULES.md)。
4. [工具链](TOOLCHAIN.md)、[集成清单](INTEGRATION-CHECKLIST.md)；安装任务读 [deployment](../../deployment/README.md)。
5. 涉及架构决策时读 [批准 ADR 来源与替代关系](../architecture/README.md)。

## 开工与判断依据

从远程当前 main 建独立分支/工作目录，先检查 head 和本地变更。本次核对的代码基线是 `c6204762c7fb285c475b619886474351c26dca31` / v0.9.2，**只用于定位证据，不要求后续回退到它**。main 前进后核对差异并刷新 CURRENT-STATE。

当前用户请求决定任务。任务板用于记录，旧 OPEN/READY/IN PROGRESS 不自动授权新会话执行；历史生产主机和凭据授权不继承。记录任务 ID、唯一会话、目录、验证与停止条件，只有明确要求并行才启动其他 Agent。同一 GitHub 账号不构成独立 APPROVE。

判断顺序：当前请求与批准约束 → 当前源码/配置及增量契约 → 现行功能/API/部署文档 → 精确提交的证据 → 历史计划/原型。实现与批准要求冲突时记录差异并按任务修复，不能改写历史制造一致。

## 前端重构须知

- React 正式页面已接入 Controller，不按旧 P1 说明改回 Mock-only。入口 `apps/web/src/app/routes.tsx`，API/DTO 在 `apps/web/src/lib/api/`，加载状态在 `apps/web/src/lib/data/`。
- 会话、Origin/CSRF、节点资源权限、独立审批、版本冲突、UNKNOWN、加载/空/失败/缺观测值均属行为契约。管理员无节点通配；“已提交”不等于执行成功。
- 路由索引用于定位，没有一份已全字段核验并冻结的统一 OpenAPI。逐功能核对 HTTP DTO、增量候选、客户端与测试；缺口补独立修订，不猜字段。
- 纯视觉调整保留业务契约；新增/修改/删除交互同步产品/API。Xray、受限 Bearer 的未装配状态不得展示为已可用。

## 交付

按影响验证。纯文档不运行完整 CI，旧绿灯不能算本次通过；需要轮询时十分钟一次。PR 按 expected head 正常合并，结果补评论，不为记录绿灯反复编辑 PR 说明。

完成后更新 CURRENT-STATE、现行功能/API 和任务板；历史记录保留。区分已实现、条件装配、实机未验收、未实现，不报无依据完成率，不凭 Release 存在宣称全部产品验收。
