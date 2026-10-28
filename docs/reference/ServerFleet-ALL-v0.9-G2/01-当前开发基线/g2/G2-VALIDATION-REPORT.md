# G2 静态验证报告

- 校验脚本：`g2/scripts/validate_g2.py`
- 测试状态：**静态检查通过（STATIC_CHECKS_PASS_ONLY）**；没有测试任何真实 Controller、Agent、Xray 或 Nginx 操作。

| 项目 | 结果 |
|---|---:|
| 原始主 API | 119 路径 / 151 操作 / 132 schemas |
| G2 独立基础接口候选 | 29 路径 / 34 操作 |
| G2 组合候选 | 147 路径 / 185 操作 / 171 schemas |
| 原 Xray 独立接口 | 19 路径 / 23 操作（不变） |
| 新增独立 URL 路径 | 28（`/api/v1/tasks` 复用原 URL 的 GET） |

### 已实测的静态内容

- YAML 可解析，全部 `$ref` 路径在各自文件内解析正常。
- 路径变量有声明，`operationId` 唯一。
- 组合候选中所有原 151 项操作的内容和现有 132 个 Schema 与原始 API 一致。
- 合并文件的旧全局 `security` 未更改；新的 Cookie/Bearer 只在新增端点显式提供，公开登录/初始化和 Agent 注册单独处理。
- Protobuf 文件做了初步结构/消息存在性检查，**不是 Proto 编译验证**。

### 尚未测试、不能声称“通过”

1. `buf lint` 与 `buf breaking`、`protoc` / Go / TypeScript 代码生成与往返序列化。
2. 外部 OpenAPI 3.1 规范校验器、生成客户端和服务器 stub 的编译。
3. HTTPS、CSRF、RBAC、一次性审批和 mTLS 的端到端负向安全测试。
4. Agent 连通、重连、执行 ID 去重、SSE 断流、状态同步。
5. SQLite WAL 写入性能、1C1G 内存，以及 WASM/Xray/Nginx 的八项 G1 PoC。

**结论：允许开始 M0 的合同工具链和工程脚手架准备；还不满足 G2 Gate 完整冻结，也不能交付生产主控/Agent。**
