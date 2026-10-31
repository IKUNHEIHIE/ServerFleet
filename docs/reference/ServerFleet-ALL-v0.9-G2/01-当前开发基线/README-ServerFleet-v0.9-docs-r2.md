# Server Fleet v0.9 / docs-r2 + G1 已批准架构 — 当前统一入口

**产品基线：** v0.9，已批准的功能范围保持不变。  
**文档修订：** docs-r2（2026-10-29），勘误与开发前收口；不构成 v1.0 正式软件发布。  
**G1 状态：** 2026-10-29 六项架构方向已批准；WASM、低配性能和安全运行证据仍待 PoC，G2 接口尚未冻结。
**工程状态：** 交互 HTML + 功能规范 + OpenAPI **设计草案**；没有完整可运行的 Controller/Agent，也没有真实 Xray 插件发行包。

## 阅读优先级（开发 Agent 必须遵守）

1. [现行产品功能需求规范](./ServerFleet-Functional-Requirements-v0.9.md)：唯一全局产品功能范围来源。
2. [Xray 唯一详细功能规范](./Plugin/xray-manager/FR-PLUGIN-XRAY-001.md)：该插件的业务细节、拒绝条件与验收规则；全局鉴权规则仍受第 1 项约束。
3. [产品验收与追踪矩阵](./docs/PRODUCT-ACCEPTANCE-MATRIX-v0.9.md)：逐功能检验用户可见行为、失败与越权场景；辅助索引，不另行扩大需求。
4. [主 OpenAPI 设计草案](./ServerFleet-API-Contract-v0.9.yaml) + [Xray 独立 OpenAPI](./Plugin/xray-manager/Xray-Manager-OpenAPI.yaml)：当前设计表达，**未完成实现级契约冻结**。
5. [G1 已批准架构决策](./architecture/G1-DECISIONS-Accepted.md)、[当前架构基线](./architecture/ARCHITECTURE-BASELINE.md)：插件与网关有正式替代 ADR，AI/PRIV 有解释性补充；旧 ADR 未冲突条款仍有效。
6. [架构决策替代索引](./architecture/SOURCE-AND-SUPERSESSION-INDEX.md) 与 [原冲突审查历史](./docs/ADR-REVIEW-REGISTER-v0.9.md)：原登记表是 G0 时点的快照，不代表当前审批状态。
7. [接口覆盖缺口](./docs/CONTRACT-GAP-REGISTER-v0.9.md) 与 [阶段/Gate 方案](./docs/MILESTONE-AND-GATE-PLAN-v0.9.md)：下一轮待决事项；不是新增已批准需求或已完成设计。
8. [本次勘误审计](./docs/DOCS-R2-AUDIT.md) 和 [docs-r2 文档变更记录](./docs/DOCS-R2-CHANGELOG.md)：校验方法与限制。

## 已批准与未批准的边界

- 当前唯一获批的**可选官方插件**：Xray 节点管理器（1 项，**未实现、不可真实安装**），不包含已删除的六个旧示例插件。
- 继续批准并保留既有全部核心功能和 v0.8 六项：发布向导、可用性监控、安全批量任务、节点/主控恢复、凭据管理、流量配额与到期提醒。
- 插件仍**按需安装**，不进入核心 Release；插件中心显示目录条目与实际安装/启用状态是不同概念。
- AI 的三级审批不扩张资源权限；原安全 ADR 对 AI 任意 root Shell 的强制管理员确认仍在有效状态，除非经独立批准修改。
- **G1 已批准**：网关新增安装与旧 ADR 已对齐；插件按需发行已替代编译期内置；AI 根权限与完全访问限制已澄清。
- **仍未最终冻结**：插件 WASM 运行时（须 PoC）、完整 Agent Protobuf、认证/权限基础 API、AI 具体框架与低配性能。

## 交付目录

| 文件 | 权威用途 | 状态 |
|---|---|---|
| `ServerFleet-Functional-Requirements-v0.9.md` | 当前产品规范 | docs-r2 已整理 |
| `Plugin/xray-manager/FR-PLUGIN-XRAY-001.md` | Xray 独立正式需求 | 原文保留 |
| `ServerFleet-API-Contract-v0.9.yaml` | 管理 API 草案 | **原始字节保持不变** |
| `Plugin/xray-manager/Xray-Manager-OpenAPI.yaml` | Xray API 草案 | **原始字节保持不变** |
| `Plugin/index.json` + `Plugin/xray-manager/manifest.yaml` | 插件目录设计占位 | 非可执行发布物 |
| `ServerFleet-v0.9.html` | v0.9 单文件模拟前端 | 原文保留；不代表线上已部署 v0.9 |
| `CHANGELOG-v0.9.md` / `ServerFleet-Release-History-v0.2-v0.9.md` | 产品版本历史 | 原文保留 |
| `docs/*` | 审查与开发交接资料 | docs-r2 新增 |
| `AGENTS.md` | 多开发 Agent 的文档优先级与禁止事项 | docs-r2 新增 |

**在线演示说明：** `https://serverfleet-preview.vercel.app` 是之前使用过的地址，但 docs-r2 未验证它当前提供的具体版本。本包内 v0.9 HTML 是明确的本地原型基线。

**历史来源：** `architecture/history/` 提供本次检索到的五份原始 ADR 只读快照；其余未收录旧 ADR 仍须从原项目归档核对。本包内新 `architecture/*.md` 是 G1 有效修订，原 G0 冲突登记和 G1 提案仅作为历史。

## 本次 G1 正式批准的新增文档（2026-10-29）

- [G1 决策批准记录](./architecture/G1-DECISIONS-Accepted.md)（六项决策全部批准）
- [插件 ADR v0.3](./architecture/ADR-PLUGIN-001-v0.3-Accepted.md)（旧编译期插件后端方案被替代；WASM 仍需 PoC）
- [网关 ADR v0.3](./architecture/ADR-DEPLOY-001-v0.3-Accepted.md)（安全接管与全新安装两条路径）
- [AI/PRIV 正式解释补充](./architecture/ADR-AI-PRIV-001-Approval-Interpretation-Accepted.md)
- [PoC 验收计划](./architecture/G1-PoC-Acceptance-Plan.md) 与 [G2 开始条件](./architecture/G2-Entry-Checklist.md)

**G1 批准不代表技术 PoC 或正式后端开发已经完成。产品文档与 OpenAPI/HTML 原始内容均未改动。**
