# Server Fleet v0.9 — G2 接口与通信协议设计交付

**阶段：G2 已启动并产出基础候选契约；还没有通过接口冻结（G2 Gate）。** 本包以原 G1 已批准完整项目包为基线，不修改既有 151 项接口、已批准的 ADR、Xray 需求或 HTML 原型。

## 读档建议

1. `ServerFleet-Functional-Requirements-v0.9.md`：产品功能基线。
2. `architecture/ARCHITECTURE-BASELINE.md`：已批准的总体方案。
3. **`g2/README-G2.md`**：这轮新增全部文件的总索引。
4. `g2/G2-CORE-CONTRACT-RULES.md`：统一协议约定与边界。
5. `g2/G2-FREEZE-AND-GAP-REGISTER.md`：尚不能宣称已冻结的事项。
6. `g2/G2-NEGATIVE-TEST-PLAN.md`：后端真实测试门槛。
7. `g2/G2-VALIDATION-REPORT.md`：静态检查范围和未完成项。

## 现有工件与候选工件的关系

| 类型 | 文件 | 状态 |
|---|---|---|
| 现行产品规范 | `ServerFleet-Functional-Requirements-v0.9.md` | 产品已批准 |
| 现行架构 | `architecture/*Accepted.md` | G1 已批准，WASM Runtime 未锁定 |
| **原始主 API** | `ServerFleet-API-Contract-v0.9.yaml` | 原始 119 路径/151 操作，未更改 |
| **G2 增量接口** | `g2/G2-Core-OpenAPI-candidate.yaml` | 29 条路径/34 操作候选 |
| **G2 组合接口** | `g2/ServerFleet-API-Contract-v0.9-G2-merged-candidate.yaml` | 147 条路径/185 操作候选 |
| **Agent Protobuf** | `g2/proto/serverfleet/agent/v1/agent.proto` | WSS 承载的 Protobuf 消息候选 |
| 原始 Xray 接口 | `Plugin/xray-manager/Xray-Manager-OpenAPI.yaml` | 原 19 路径/23 操作，未更改 |

**重要：** 后续 AI 开发 Agent 可以依据此包建立工程脚手架、准备 Mock、编写单元和合同测试，但**不得把组合候选 OpenAPI 视为全部已冻结**，更不得以这些定义代替 mTLS、执行权限和身份验证等真实后端测试。
