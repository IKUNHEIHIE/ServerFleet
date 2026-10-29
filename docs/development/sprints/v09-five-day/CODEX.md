# V09-5D-CX · 安全与主控/Agent平台复合任务

会话 codex-02；分支 `feature/v09-platform-cx02`；目录 `D:\admin\desktop\codex\myproject\ks\.worktrees\feature--v09-platform-cx02`；审查 codex-review-01+相关安全/契约/模块owner。精确基线使用总控本次派发SHA，并核对分支/HEAD/目录；旧codex-01设计目录不得复用。

先读根AGENTS、README、架构/来源索引、MODULE-OWNERSHIP、DEPENDENCY-RULES、TASK-BOARD、[共同计划](PLAN.md)、现行v0.9需求/AC矩阵、G1/PoC计划和已接收认证/HTTP/Web提案。用户已授权完整v0.9五日复合实施，不再以旧“仅3份设计”或W07待批为由停止；G1安全/执行/插件约束仍适用。

## 允许写入

- `internal/auth/**`、`internal/transport/**`、`internal/plugins/**`、`apps/controller/**`、`apps/agent/**`。
- 新领域目录 `internal/ai/**`、`internal/workflows/**`、`internal/updates/**`、`internal/recovery/**`；可选包 `plugins/xray-manager/**`（不得由核心import）。新增包import边须先交总控登记，不默认突破现有依赖图。
- `tests/integration/v09/platform/**`、`tests/poc/v09/platform/**`、`docs/contracts/revisions/v09-five-day-cx02/**`、`docs/development/sprints/v09-five-day/progress/codex/**`、自己忽略的`reports/_review/v09-platform/`。
- 禁止修改DS/GM业务目录、公共配置/锁/生成/API/Proto/迁移台账及原始资料。共享变更按PLAN的具体patch机制；模块公开ports跨owner变化也先协调。真实凭据/生产/VPS/云不在本次授权中。

## 连续子任务

| ID | 实施目标与可验收结果 |
|---|---|
| CX-01 | 首3小时交**可单独接收的接口/依赖检查点**：先列登录/节点/任务/SSE、可信主体/策略错误、Agent受控命令/结果、storage仓储语义的操作级差异与失败例；首个提交仅候选接缝/精确共享patch，便于总控独立contract接收。再实现平台基础，不等全量185操作才开始代码；扩展操作随对应功能检查点补齐，不绕过审查 |
| CX-02 | 管理员受控初始化、密码/登录防护、Cookie/CSRF/Origin、登出/过期/撤销、受限Bearer迁移与服务器主体；RBAC/显式资源grants、凭据安全存储与轮换、Tgate/世代观察边界；与DS接通持久身份/会话仓储。不能以NodePolicy已知subject自认证，不能默认管理员全节点 |
| CX-03 | Agent安装/短期接入凭证、审批与身份、主动WSS+mTLS+Protobuf、心跳/重连/轮换/吊销/重装、去重和连接对账；控制/事件由组合回调交DS，不反向import领域。先隔离PoC/安全门槛，真实连接未准入时不启用 |
| CX-04 | Agent侧受控执行/取消/结果与日志；Controller统一授权/审批/任务/审计链；与DS调度组合。明确Shell与白名单操作区别，root/AI审批不豁免，未知结果不自动重跑；普通5、修改类1→5、未启动15分钟TTL等保持已批准语义 |
| CX-05 | AI BYOK/可关闭接入、消息/SSE、资源零授权/服务组成员快照、三级审批、过期/撤权/重放负向；自动化工作流触发/条件/动作/人工审批/通知均复用统一任务。Eino优先PoC，未获证据不得锁定生产框架；不得自造执行旁路 |
| CX-06 | 插件来源/签名/清单/声明式UI能力、按需安装启停/升级卸载及禁用后拒绝；WASM生命周期/限额/隔离PoC。只开发可选Xray：双架构安装/安全接管、两套VLESS模板/自定义JSON、目标版本预检/配置历史恢复、用户/受控导出、用户/入站/实例流量对账。Host与Xray需分发行，不编入核心、不恢复六个旧演示插件 |
| CX-07 | 更新中心校验/健康检查/失败处理、备份与离线恢复平台、信任连续/旧主控隔离/撤销状态与任务不复活；与DS存储快照及GM页面联调。缺恢复密钥/正式安全条款不得声称可恢复，私钥不泄露 |
| CX-08 | 独占Controller组合入口/中间件/路由注册、Agent入口与安装打包接缝；接收DS业务模块并组合，不在apps入口另写业务数据库。给总控构建/安装/运行/升级/恢复步骤和发布所需具体patch，完成平台相关PoC及安全回归 |

## 交付和验证

每个可运行增量提交，不做“五天后一个大commit”。检查点记录在自己progress目录的`STATUS.md`/`HANDOFF.md`/`SHARED-CHANGES.md`：子项状态、SHA、精确调用、命令输出、依赖、风险。先交CX-01，继续CX-02/03等无依赖项；接口受阻不能用放行mock代替。

验证至少包含：空/双/失效凭据与身份源故障、跨主体/跨节点越权、会话/策略撤销、审批重放/过期/root强制确认、错误证书/CA/吊销/重连重复命令、部分日志/未知结果、插件禁用/签名/限额/逃逸、更新被篡改/恢复旧撤销记录、AI/工作流间接越权。Agent实际执行、Xray双架构与PoC-01/02/03/04/06/08要真实证据，测试double只记模拟层。

按PLAN阶段目标连续推进；库/API用Context7；所有FAIL/BLOCKED/UNTESTED如实登记。范围内常规修复直接做。全部子项交精确HEAD与证据后停止，或确无独立工作时说明硬阻塞；不自行合并main/推送/重写历史/启动其他Agent。本轮五日目标不是绕过G2及安全准入的许可。
