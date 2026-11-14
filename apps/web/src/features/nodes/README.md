# 节点前端模块

更新：2026-11-14。正式页面已接入 App 与 Controller API；旧 W01—W05 的“不接入 App/只用 Mock/等 G2 才接网络”是早期阶段限制，已不适用。开发先读 [交接](../../../../../docs/development/HANDOFF.md)、[功能](../../../../../docs/product/FEATURES.md)、[API](../../../../../docs/api/README.md)。

## 当前入口

| 文件 | 职责 |
|---|---|
| nodes-page.tsx | 节点列表页，调用 nodesApi，组合当前列表/适配与状态 |
| node-add-page.tsx / installation-panel.tsx | 令牌、审批、可信安装目录与安装命令 |
| node-detail-page.tsx | 节点详情、身份、能力、诊断及配额等接口呈现 |
| node-list-workspace.tsx / node-list-panel.tsx / node-list.tsx | 列表/筛选/选中与展示组件 |
| node-detail.tsx | 列表内只读详情组件，与正式详情页不同 |
| node-loader.tsx | 可注入 NodeSource 的加载组件，不代表整个模块没有网络 |
| rich-adapter.ts / legacy-adapter.ts / candidate-adapter.ts | 已知形状适配；当前 HTTP 字段以 DTO/处理器核对 |
| view-model.ts / metric-value.tsx / node-status-badge.tsx | 指标质量与状态展示 |
| test/ 与 *.test.ts(x) | 合成用例，禁止生产导入 |

App 挂载见 apps/web/src/app/routes.tsx；REST 客户端/DTO 见 apps/web/src/lib/api。不要根据旧候选单独猜测端点字段，也不通过删除旧适配器掩盖兼容问题。

## 展示与安全语义

- loading/error/empty 与真实 ready 分开；旧响应/旧作用域不覆盖新请求，撤权不继续显示旧数据。
- 管理员无隐式节点通配；筛选和隐藏不是权限控制。缺权限、没有节点、筛选无结果按当前可获得事实解释，不伪造统计。
- fresh/stale/invalid/missing 保持区别，真实 0% 有效，越界不夹取，未上报不补零。时效策略由调用方注入，不能把前端阈值当 Agent 协议采样周期。
- 错误不展示秘密或原始供应商正文；“已批准/已提交”不等于在线或执行成功。
- 键盘选择/关闭焦点回落、可访问名称、加载和失败状态在重构后保留。真实浏览器验收与 jsdom 测试分别记录，不以历史浏览器记录宣称新 UI 已验收。

代码变化按影响运行根 check:web/check:policy 及接口专项；纯文档不运行完整 CI。UI 行为新增/修改/删除同步功能/API 文档；生产 Xray/Bearer 未装配边界不能仅靠界面绕过。
