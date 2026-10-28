# G2-C01～C07 评审与待决事项

状态：**候选评审完成，批准待定，Gate 未通过**。G2-Cxx 对应现行 `g2/G2-FREEZE-AND-GAP-REGISTER.md`，不是工程记录中同号的 P1-Cxx。需求/已批准ADR优先于任何本轮推荐；未收录ADR不推定内容。

| 项目 | 事实/风险 | 本轮处理 | 未决/批准人 |
|---|---|---|---|
| G2-C01 API基础 | 旧Error、XrayError、Problem、无错误体并存；成功码/分页不统一；组revision0有效；幂等主键含body会掩盖换参冲突 | 新profile统一HTTP Problem；显式错误状态绑定；保留逐操作成功码/分页；明确幂等查找键和摘要分离、revision别名；185项矩阵 | profile选择机制、24hTTL/敏感响应重放、事务/持久化、逐域分页/202迁移及ETag强一致性待总控+安全/资源owner批准 |
| G2-C02 认证 | 旧151 Bearer/JWT和新增Cookie/Bearer混用，CSRF仅可选参数无法表达AND；login/bootstrap未约定恢复CSRF的完整生命周期 | profile内Cookie写分支强制CSRF，保留4特殊流程独立证明；取消JWT格式假设，要求响应Cookie/CSRF；无签发API | 初始token来源/单用、Origin策略、登录限流、idle/absoluteTTL、CSRF刷新/轮换、Bearer签发撤销和端到端失效均未验证；安全owner审批 |
| G2-C03 RBAC/资源 | User/Role/Grant是候选DTO；auth.Check只有SubjectID/Action/NodeIDs。不能从Role名或resource-grants推定AI会话授权 | 文档保留默认零资源、服务组冻结/新增排除/移除失效、scopeRevision独立；隔离拒绝样例含撤权与AI full-access root人工审批 | 多角色合并/冲突、逐动作逐资源联合校验（禁止跨角色资源scope与action做笛卡尔放大）、管理员降权、全量撤权与旧审批防重放待独立安全审查 |
| G2-C04 节点生命周期 | 新NodeDetail把online/offline/suspended/revoked放同enum，连通性与身份状态容易混淆；现有Reader只有ID/Name；轮换/重装合同不足 | 给暂停/吊销增加Node资源并发前提；保留证书身份为权威，消息node_id不授权；不发明轮换/重装接口 | 生命周期与在线状态分离模型、暂停恢复/证书轮换/重新安装的审批/旧证书拒绝、CSR持有证明与丢响应重取待通信+安全owner；相关PKI原ADR须补齐 |
| G2-C05 任务 | 旧六状态与新十二状态不同；临时Submitter没有审批/快照/摘要/状态能力；旧advance/stop与新resume/cancel语义不完全同义 | 新profile同源状态枚举、jobId/taskId相等；旧客户端扩展状态需升级；明确900秒只限未启动，unknown不重跑；SSE410/stream.gap | 状态聚合/迟到结果审计、审批与派发原子校验、SSE水位/保留窗口、快照与撤权竞态、持久去重/恢复隔离均需真实测试；任务+安全/通信owner |
| G2-C06 审计 | AuditEvent有脱敏字符串，但schema无法阻止字符串中泄密；无修改/删除审计API。Recorder仅Append最小Event | 保留只读查询、按权限过滤分页、脱敏白名单方向；隔离字段拒绝样例；不承诺SQLite防篡改 | 事件/拒绝覆盖清单、关键写入与审计一致性、审计写失败如何阻断高风险修改、保留/清理权限、完整性/恢复证据待audit+storage+安全owner |
| G2-C07 Agent Proto | 原Proto能编译但原Buf根lint失败/Go导入路径缺失；P1仅加go_package和format，已有Go生成；零值/未知enum解码可成功 | 语义token对照、原descriptor WIRE、既有FILE基线/生成一致性、13 oneof往返与畸形wire检查；见PROTO-REVIEW | TS生成/跨语言、WSS+mTLS身份/限流/版本/持久去重/重连/恢复/1C1G真实PoC均未执行；通信+安全owner，不批准Proto语义冻结 |

## 已批准约束与仍属推荐的区别

已批准且不可放松：Go模块化单体；Agent主动WSS+mTLS/Protobuf；Web仅Controller REST/SSE；统一任务授权/审批/审计；普通5、root/修改1→5、未启动离线15分钟；AI零授权/冻结快照；完全访问不能免除AI任意root Shell真人管理员审批；CA/恢复私钥/原始Token不直出；Xray是唯一已批准可选插件，不进核心Release；WASM/Eino未锁定。

本轮**仅推荐**：显式profile头、24h幂等保留、SameSite=Strict、资源`"sf-rN-<64hex>"`标签、混合凭据400、旧状态无法表示时409升级、所有受保护写方法要求幂等键、SSE410和fetch适配。这些需要总控/安全/受影响owner确认，不能以测试PASS反推已批准。

## 原记录冲突映射与来源

| P1记录 | 对应本轮处理 |
|---|---|
| P1-C01 | G2-C07与PROTO-REVIEW：只审查M0-P1-r1，不再修改proto/生成配置/基线 |
| P1-C02 | G2-C02/C03与COMPATIBILITY：新旧认证、scope/会话撤销与CSRF生命周期 |
| P1-C03 | G2-C05：任务完整状态与唯一权威记录，非六状态降级 |
| P1-C04 | G2-C01：Error/Problem、成功码、分页、expectedRevision/ETag逐项兼容 |
| P1-C05 | 公开仅/tasks，历史/jobs示例不形成第二队列；业务update/jobs等查询路径保留，不能误删 |
| P1-C06 | 文献以Functional Requirements v0.9与G1 Accepted/替代索引为准；历史Release-History v0.7标题及旧“待修订”描述不覆盖较新批准，不改原文 |
| P1-C07 | PRIV/WC/PKI-OPS/UPDATE原ADR详细文件仍未独立收录，不能凭引用猜测实现；需要总控补归档核验 |

只读来源（相对仓库根，原始文件未改）：

- `docs/reference/ServerFleet-ALL-v0.9-G2/01-当前开发基线/ServerFleet-Functional-Requirements-v0.9.md`：§2/4/5/10及FR-0803；普通分类和服务组独立，目标与授权快照各有提交/授权时间点。
- 同目录 `architecture/G1-DECISIONS-Accepted.md`、`ARCHITECTURE-BASELINE.md`、`SOURCE-AND-SUPERSESSION-INDEX.md`：架构、历史替代及缺ADR事实。
- `architecture/ADR-AI-PRIV-001-Approval-Interpretation-Accepted.md` §1～2；`history/08-ADR-TASK-001-批量任务调度与执行引擎-已批准.md` §3～8/13；历史BOOT安全引导约束。
- `g2/G2-CORE-CONTRACT-RULES.md` §2～7、`AGENT-TRANSPORT-AND-STATE-MACHINE.md` §1～6、`G2-NEGATIVE-TEST-PLAN.md`：候选方向和未执行负向门槛。
- 原主API、Core/Merged/Xray工作候选、P1 Proto/Go生成物/兼容基线、临时ports.go及 `docs/contracts/P1-CANDIDATE-REVIEW.md`。

## 总控需决定的顺序

1. 审查profile是否适合未来部署与前端SSE；旧契约保留期限、支持探测和拒绝降级机制。
2. 认证token/CSRF/session生命周期、资源并发域/强ETag、幂等原子存储及敏感结果重放策略。
3. 任务迁移/聚合与旧客户端扩展状态拒绝；AI授权变更、审批/派发原子校验和恢复核对。
4. 补齐缺失ADR，再设计节点轮换/重装、审计持久化和传输语义门禁。
5. 另行分配真实隔离安全PoC、TS/跨语言检查；满足门槛后才讨论冻结。当前不启动这些工作。
