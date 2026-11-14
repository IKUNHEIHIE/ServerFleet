# Server Fleet UI v2.0 — NEUROX 风格浅色主题重构记录

## 1. 重构目标与边界

本次重构面向 **Server Fleet** 正式前端（`apps/web`，React 19 + Vite 8 + Tailwind CSS v4 + shadcn/ui 基础原语），以 **NEUROX** 的布局层次、KPI 动态卡片、拓扑连线与交互编排为视觉参考，落地现代、年轻、精致的 **Light Theme（浅色主题）SaaS 运维平台**。

严格遵守工程约束：
- **零后端改动**：未修改任何 Go 后端代码、SQLite Schema、OpenAPI/Protobuf 契约或 API 请求/响应结构。
- **零业务删减与零假数据**：保留全部 18 个核心/插件业务路由与会话态视图，所有页面数据、审批流、mTLS/PKI 证书链与 RBAC 权限校验均 100% 驱动自真实 API 与服务端契约。
- **无障碍与性能保护**：所有动画优先使用 `transform` / `opacity` 与 SVG 矢量描边，完整支持 `prefers-reduced-motion: reduce` 降级。

---

## 2. 统一设计系统（Design Tokens — Light Theme）

在 `apps/web/src/index.css` 与 `apps/web/index.html` 中建立统一的 NEUROX Light Theme 设计变量与字体体系（`Plus Jakarta Sans`、`Space Grotesk`、`JetBrains Mono`）：

| Token | 色值 | 用途 |
| :--- | :--- | :--- |
| `--background` | `#F6F7FB` | 全局冷白/浅灰底色与微妙网格点阵 |
| `--card` | `#FFFFFF` | 卡片容器表面与高光毛玻璃层 |
| `--primary` | `#6658E8` | 品牌主强调色（蓝紫科技色） |
| `--primary-light` | `#F0EEFF` | 品牌浅色高亮底衬与选中背景 |
| `--foreground` | `#1C2030` | 主要标题与正文深灰文字 |
| `--muted-foreground` | `#737B8C` | 辅助说明与次要指标文字 |
| `--border` | `#E8EAF1` | 卡片、表格与分割线细腻边框 |
| `--success` | `#169B68` | 在线、健康、已验证状态 |
| `--warning` | `#D89B2B` | 降级、待审批、额度预警状态 |
| `--destructive` | `#DE5555` | 离线、失败、吊销与危险操作 |
| `--info` | `#3984E7` | 执行中、派发中、信息提示状态 |

---

## 3. 新增 Aceternity UI 与动态交互组件

在 `apps/web/components.json` 中注册 `@aceternity` 官方 Registry，并在 `apps/web/src/components/aceternity/` 下构建完整的浅色主题适配组件集：

- `Spotlight`（`spotlight.tsx`）：用于 Dashboard Hero 横幅、AI 运维工作空间与登录页的柔和局部聚光灯背景。
- `SparklesCore`（`sparkles.tsx`）：轻量级 SVG 粒子星光特效，用于 AI 运维工作区与登录品牌区。
- `BackgroundBeams`（`background-beams.tsx`）：SVG 动态光束路径，用于登录/引导屏幕与 404 状态。
- `BackgroundGradient`（`background-gradient.tsx`）：柔和渐变光晕容器，用于重点卡片与表单外框。
- `GlowingEffect`（`glowing-effect.tsx`）：支持鼠标跟随径向光效与悬停高亮边框的卡片特效，广泛用于各业务页 KPI 统计卡与审批模式卡。
- `MovingBorder`（`moving-border.tsx`）：流动渐变边框，用于高优先级入口。
- `CardHoverEffect`（`card-hover-effect.tsx`）：卡片矩阵共享悬停高亮背景。
- `BentoGrid` / `BentoGridItem`（`bento-grid.tsx`）：不对称 Bento 仪表盘网格布局。
- `WobbleCard`（`wobble-card.tsx`）：3D 视差微倾卡片容器。
- `TextGenerateEffect`（`text-generate-effect.tsx`）：AI 输出文本逐词渐显动画。
- `TracingBeam`（`tracing-beam.tsx`）：左侧渐变追踪光束时间线，用于任务执行步骤流与 AI Agent 工具调用轨迹。
- `AnimatedTooltip`（`animated-tooltip.tsx`）：弹簧物理微交互浮动提示。
- `Meteors`（`meteors.tsx`）：轻量流星背景点缀。
- `AnimatedNumber`（`animated-number.tsx`）：基于 `requestAnimationFrame` 的 KPI 数字平滑滚动与过渡组件。

---

## 4. 全局布局与业务页面改造清单

1. **全局导航与命令面板（`shell.tsx`, `command-palette.tsx`, `routes.tsx`）**：
   - 可折叠侧边栏（`268px` ↔ `76px`），带 `motion` `layoutId` 活动导航胶囊指示器与分组图标。
   - 顶栏集成 `Ctrl + K` / `Cmd + K` Raycast 风格全局命令面板（复用路由与 RBAC 权限过滤）、实时通知中心抽屉、会话状态胶囊与主控写锁横幅。
2. **认证与会话屏（`auth-pages.tsx`, `session-screens.tsx`）**：
   - 双栏非对称登录/首次引导布局，左侧搭载 `BackgroundBeams` + `SparklesCore` 与 PKI/RBAC 架构摘要卡片。
3. **概览仪表盘（`overview-page.tsx`）**：
   - `Spotlight` 顶部概览横幅、4 张 `GlowingEffect` + `AnimatedNumber` KPI 卡片、NEUROX `SyncDonut` 在线率环形图、Recharts 实时在线节点 CPU/内存双柱状图、控制面快捷矩阵。
4. **节点管理（`nodes-page.tsx`, `node-list.tsx`, `node-detail.tsx`, `node-status-badge.tsx`, `metric-display.tsx`）**：
   - 脉冲呼吸在线状态徽章、动态渐变 CPU/内存/磁盘进度条、批量选择悬浮操作栏、节点详情抽屉与完整详情页升级。
5. **普通分组与服务组（`groups-page.tsx`, `service-groups-page.tsx`）**：
   - 缩进式分组层级树、服务组健康度进度条与版本快照标识、成员管理抽屉支持拖拽节点增删。
6. **网关管理与一键发布（`gateways-page.tsx`, `publishing-page.tsx`）**：
   - 基于真实路由、网关与服务组数据的 3 列 **SVG 动态网络拓扑画布（`GatewayTopologyCanvas`）**，带 `animate-neurox-dash` 流量连线动画；一键发布提供 3 阶段流水线步骤条。
7. **任务管理（`tasks-page.tsx`, `task-detail-page.tsx`）**：
   - 4 张任务状态 KPI 卡、任务详情 4 阶段生命周期指示器、SSE 实时执行进度条、带 `TracingBeam` 的深色终端流式日志面板。
8. **AI 运维工作空间（`ai-pages.tsx`, `agent-run-panel.tsx`）**：
   - 顶部 `Spotlight` + `SparklesCore` 工作区横幅与每日预算进度条、3 种审批模式（每次审批 / 自动审批 / 完全访问）高亮卡片、支持将节点与服务组直接拖入资源授权区或 AI 聊天输入框、现代化气泡对话流、思考脉冲指示器与 `TracingBeam` 工具调用/审批卡片轨迹。
9. **证书、插件与 Xray、文件互传、监控、DNS、工作流、更新、备份、用户、审计、系统设置**：
   - 证书管理新增 **PKI 三级信任链可视化架构图**（`Offline Root CA → Online Intermediate CA → Per-Node Leaf Identity / CSR`）。
   - 插件中心与 Xray 节点管理器新增 4 维状态 KPI 卡片与声明式标签页。
   - 文件互传升级双栏拖拽上传按钮、目录文件/文件夹图标、跨节点传输任务实时进度条。
   - 监控告警、DNS 管理、自动化工作流、更新中心、备份恢复、用户与权限、审计日志、系统设置全面统一 `PageHeader`、`Badge`、`GlowingEffect` 统计卡片与表格交互反馈。
