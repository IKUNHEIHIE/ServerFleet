# R30：异常／UNKNOWN 人工处置完整闭环

2026-11-11。增量基线为已合入R29的main `5e9acae8`；不改历史候选或已批准ADR，不表示G2冻结。产品页面、API适配、Controller和领域服务同步变更。

| 类型 | 产品变化 | 接口与实现 |
| --- | --- | --- |
| 新增 | 结构化核验证据、跨轮次不可覆盖台账 | `record_evidence`；`GET /operations/incidents/{id}/evidence?after`；三类必需证据24h、风险控制证据 |
| 新增 | 当前来源／逐目标／后续任务汇总与事实快照 | `assessment`、`reconcile`、`observationDigest`；快照15min、来源原始观察与当前事实分开展示 |
| 新增 | 后续任务阻断、风险接受和明确独立复核 | `checks`、`reviewAcknowledged`；提交和复核均重读当前事实；待执行／不可读阻断结案，未知不可确认成功 |
| 新增 | 结案依据变化后自动重新核查 | 有界轮转监视、`needsRecheck`、`cycle`；原结果及历史证据保留，不自动提交任务 |
| 新增 | 备份上传异常直接创建只读核验任务 | `POST /operations/incidents/{id}/verify-backup`；统一 `backups.remote.verify`，返回已准入任务及 `linked`，CAS失败不隐藏任务 |
| 修改 | 异常列表筛选与稳定分页 | `state/sourceKind/taskId/cursor/limit`、`nextCursor`；默认active/50，最多100；空可见页可有下一页 |
| 修改 | 关联任务限定原资源，展示其实际当前结果 | 当前任务读权限＋创建时间＋资源范围＋Controller业务域；备份上传须同一远端上传记录的核验 |
| 修改 | 来源修订变化仍可追溯旧记录 | 同owner/plan/action身份及当前读权限，保留原source；新收集按kind/id去重，旧重复保留 |
| 修改 | 接管／释放／重新打开／恢复失效旧轮次 | 追加cycle，旧证据保留；旧提案补齐证据和快照后重新提交 |
| 删除 | 无 | 不删除既有业务能力、历史迁移、测试或证据 |

[独立OpenAPI候选](../../../api/openapi/revisions/v09-r30/incidents-closure-candidate.yaml)列出6个路径、6个操作；实际前缀为 `/api/v1`。安全会话名为 `__Host-sf_session`，HTTPS／CSRF／当前管理员／当前资源权限和运行权威由后端执行。证据引用不抓取外部内容，不接收私钥、带凭据或签名的链接；只返回最少必要任务事实，不复制名称／参数／输出／私人AI内容。

与R29读取字段保持增量兼容；列表默认改为待处置分页，老客户端需适配游标，不再假定一次返回所有条目。R29提交结论／复核请求缺少新证据约束时会拒绝；这是安全收口。新增迁移 `cx/0023_incident_closure_evidence` 使全组合共43项，原42迁移、SQL和摘要不改；无需重置数据库。新增表后不能让旧Controller接管写入；回退须先停机并按备份恢复流程处理，不手工删迁移台账。无依赖／锁／Proto／生成代码／Runtime变更。

使用步骤、结论准入矩阵、失败恢复和边界见 [INCIDENT-CLOSURE](../../development/INCIDENT-CLOSURE.md)。最终精确head的CI结果在PR评论记录，早期R29绿灯不覆盖此增量。真实节点、真实备份存储、真人双管理员浏览器和故障演练仍由用户联调，不冒称生产验收。
