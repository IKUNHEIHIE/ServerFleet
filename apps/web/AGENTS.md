# Web 开发边界

继承根 AGENTS，先读 [交接](../../docs/development/HANDOFF.md)、[功能](../../docs/product/FEATURES.md)和[API](../../docs/api/README.md)。

正式 React 页面已接入 Controller；旧 P1 的 Mock-only/不扩展业务阶段限制已不适用。按当前任务修改，入口 routes.tsx、会话/导航、共享样式、依赖/锁与其他任务协调；不覆盖他人变更。

只调用 Controller REST/SSE，不直连 Agent/数据库，不在前端持有私钥或秘密。测试 Mock 只作测试，不导入生产。既有 API/DTO 与处理器交叉核对，未冻结候选不能单独充当全部字段契约。

保留会话/CSRF/Origin、资源授权、审批、冲突和 UNKNOWN 语义；加载/空/失败/缺失观测值明确展示，操作需可访问名称。界面重构不能把“已提交”当成功或用管理员身份跳过节点权限。

行为变化同步产品/API 文档；代码按影响运行 check:web/check:policy 及接口相关检查，纯文档只做文档核对，不运行完整 CI。
