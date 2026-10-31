# Server Fleet — G2 接口与通信协议设计交付

**基线**：Server Fleet v0.9 / docs-r2 + G1 已批准架构。**当前 G2 为候选契约，尚未冻结**；真正 Controller、Agent、插件都尚未交付。

## 核心阅读顺序

1. `../ServerFleet-Functional-Requirements-v0.9.md`：产品范围唯一现行基线。
2. `../architecture/ARCHITECTURE-BASELINE.md` 和 G1 Accepted ADR：必须遵守的技术原则。
3. `G2-CORE-CONTRACT-RULES.md`：通用规则、认证/授权、任务、审计、Agent 状态。
4. `G2-Core-OpenAPI-candidate.yaml`：仅新增 G2 基础接口的独立设计候选。
5. `ServerFleet-API-Contract-v0.9-G2-merged-candidate.yaml`：与原 119 路径组合后的**评审版本**。
6. `proto/serverfleet/agent/v1/agent.proto`：WSS+mTLS 中的 Protobuf Envelope 草案。
7. `AGENT-TRANSPORT-AND-STATE-MACHINE.md`：消息语义、幂等与重连规则。
8. `G2-FREEZE-AND-GAP-REGISTER.md`、`G2-NEGATIVE-TEST-PLAN.md`、`G2-VALIDATION-REPORT.md`：尚未冻结/验证的内容。

## 不可替代的原始文件

- `../ServerFleet-API-Contract-v0.9.yaml`：**原 119 路径 / 151 操作**，原文件不被覆盖。
- `../Plugin/xray-manager/Xray-Manager-OpenAPI.yaml`：原 19 路径 / 23 操作，原文件不被覆盖。
- `../ServerFleet-v0.9.html`：原型不变，模拟功能不等于后端安全实现。

G2 专注认证/RBAC、Agent、任务、审计等基础闭环；DNS、证书、Xray 和网关的完整协议与真实 PoC 仍需后续明确。这一包不能交给开发 Agent 当作**已冻结的全部 API**，允许据此建立工程脚手架和合同测试框架。
