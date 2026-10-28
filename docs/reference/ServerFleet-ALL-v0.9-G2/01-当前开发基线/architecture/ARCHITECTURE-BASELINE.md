# Server Fleet — G1 已批准架构基线（供非技术项目所有者和开发 Agent）

**项目状态**：产品需求 v0.9 已收口；G1 架构方向获批；PoC 尚未执行；G2 API/Protobuf 尚未冻结；真实 Controller、Agent、Xray 插件尚未交付。

## 一句话方案

一台 Go 主控管理多个主动连接的 Go Agent；React 管理台调用主控 HTTP API；主控统一检查用户、节点、工具、审批和任务；所有可选插件只通过受控能力扩展，不独立控制 Agent。主控只执行运维管理，不替网站或代理节点转发用户业务流量。

## 当前决策表

| 层 | 已批准方向 | 状态/边界 |
|---|---|---|
| Controller | Go 模块化单体、SQLite WAL 存储、内部有界任务调度 | 已批准，需低配压力验证 |
| Agent | Go Linux amd64/arm64、主动出站 mTLS、Protobuf 通信 | 已批准，协议 G2 补齐 |
| Web | React + TS + Vite + shadcn/ui（静态部署） | 已批准 |
| API/事件 | REST/OpenAPI + SSE；Agent Protobuf | 技术方向批准，接口尚未冻结 |
| 插件 UI | JSON/YAML 声明式，主控组件统一渲染 | 已批准 |
| 插件后端 | 受限 WASM 优先进行 PoC；失败再由新 ADR 比较受限子进程 | **未锁定特定运行时** |
| Nginx | 原有站点安全接管，或有权限时全新安装；健康验证后人工启用 | 已批准，PoC 待验证 |
| AI | 可关闭的主控内嵌 AI + BYOK，Eino ADK 优先 PoC | **框架尚未锁定**，完全访问不绕强制审批 |
| DNS/证书 | 内置 Cloudflare，ACME 与私有 CA 分离，本地网站私钥 | 保留已批准 ADR |
| Xray | 唯一获批的第一版可选插件，按需安装和启用 | 插件已批准，**未发布任何可执行插件** |

## 明确不引入（当前阶段）

Kubernetes、Redis、Kafka、多活主控、把 Go 原生 plugin 作为热加载方案、未经批准的任意第三方本机可执行插件、插件自定义浏览器 JavaScript、主控承载用户网站流量、对任意 Shell 做可靠预演/原子回滚。

## 对并行开发的边界

1. 可以先行建立 Monorepo、前端 UI 静态壳、OpenAPI/Protobuf 的草案工具链、CI 与测试框架；不要在 G2 前任意冻结临时接口。
2. **所有修改性操作必须先有服务端安全权限与统一任务通道**；禁止前端模拟直接被当成真实后端安全实现。
3. 需要 WASM 资源占用、Nginx 安装、Xray 模板、Agent 断线与恢复等隔离 PoC 的结论才可以宣布最终运行方案。
4. 任何开发 Agent 不能擅自使用旧 PLUGIN-D2 编译期插件架构，或仅支持已有 Nginx 的旧 DEPLOY-D2 来完成已批准产品范围。
