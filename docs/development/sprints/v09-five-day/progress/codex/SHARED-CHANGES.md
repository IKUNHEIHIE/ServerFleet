# 共享变更请求 / codex-02

R30—33补充候选（REQUESTED）：agent-enrollment.patch.json精确RFC6902修订与兼容测试，保持原始YAML字节；EnrollmentService构造必填before+commit，EnrollmentStore.Update/SQLBackend.InTx回调新增可信txCtx，让commit使用同Tx只读auth reader。详见ENROLLMENT-HTTP.md、ENROLLMENT-WIRING.md、AGENT-REGISTRATION-SQL.md。所有本分支调用方已同步，旧三参数/旧callback签名不能编译，无默认放行。Linux真实注册/SQLite/审计回滚/身份重开/mTLS/零节点权限/显式grant吊销证据已交，但未接收正式角色/PKI/profile/Node业务组合；生产回退是不挂载监听器，保留持久材料与隔离。此增量无公共文件/根依赖/DS源码修改，不作为G2冻结或RecordTx接收证据。

2026-10-30；状态 REQUESTED，未经接收不修改公共文件或主线消费端。CX-01 无共享文件改动，无新增依赖。

## S01 OpenAPI 认证迁移（API owner）

2026-10-30总控澄清：8a7dd470已含98f6d94d依赖登记，但S01—S04和DS共享申请均未接收；不得假设RecordTx或扩展Request存在。四份完整安全ADR在仓库及11个历史ZIP均缺失；继续独立实现，具体依赖项保持BLOCKED。

S01目标为**独立契约修订候选**，原始受字节保护的 api/openapi/g2-merged-candidate.yaml 不得直接改写。下面 diff 仅用于描述在独立副本上的精确语义差异，待总控安排修订目录和固定生成器接收；不手改 gen。早期本申请将原文件写作目标不准确，现予纠正，实际从未修改该文件。

```diff
@@ components.securitySchemes.cookieSession
-      name: sf_session
+      name: __Host-sf_session
@@ components.securitySchemes.bearerAuth
-      bearerFormat: JWT
+      bearerFormat: opaque-restricted-token
@@ paths./api/v1/nodes.get.security
       - bearerAuth: []
+      - cookieSession: []
@@ paths./api/v1/auth/bootstrap.post.responses
-        '200':
-          description: 成功
-          content:
-            application/json:
-              schema:
-                $ref: '#/components/schemas/LoginResult'
+        '204':
+          description: 初始化已提交；使用普通登录取得会话
```

还需受保护 CSRF 重载交付：建议现有 me 响应增加 csrfToken，no-store、同源读取；需 GM/HTTP owner 决定精确 schema。不新增平行路由。Bearer 签发/求交端口未批准前关闭兼容，不能仅改 security 标签就声称可用。兼容影响：旧 Cookie 名不双读，JWT 不自动兼容，bootstrap 200→204 为显式破坏性差异；未获接收不开相关运行时路由。回退：不接线现有 health 以外入口。

## S02 storage 事务接缝（DS owner）

**更新：原通用RecordTx申请由auth专用SQL适配替代（仍待接收）**。已核对DS提交bb8954988fe7b2f832edf291803529d7745e4f0a的InTx/Queryer方案；精确适配与两条业务建表语句在`internal/auth/identity_sql.go`，不要求DS新增KV。`IdentitySQLBackend.Read`映射一条QueryRow.Scan(payload,ever_initialized)，InTx映射DS单一事务Read/Exec/RowsAffected；事务回调一次，取消回滚。总控注册迁移编号，本包不抢占编号。真实WAL/重启/并发/回滚证据见RESUMED.md；隔离实验不代表公共接收。下文原申请保留追溯，不再作为实施前置。

文件候选 `internal/storage/records.go`（本包不写入）；精确新增接口建议：

```go
type RecordTx interface {
    Read(namespace, key string) ([]byte, error)
    Write(namespace, key string, value []byte) error
}
func (db *DB) View(ctx context.Context, fn func(RecordTx) error) error
func (db *DB) Update(ctx context.Context, fn func(RecordTx) error) error
```

namespace 仅组合根绑定的 auth 命名空间；Read 返回复制；无记录独立 sentinel；Update 串行写且回调只执行一次，ctx/回调/提交失败回滚；不能把任意 HTTP namespace 传入。DS 可提出领域仓储替代差异；在精确实现返回前不假设 DB/RecordTx 已存在。业务表和迁移编号由总控审查；没有本包抢占编号。测试要求：并发初始化仅一提交、取消回滚、重启撤销不复活、提交错误不交付凭据。回退为不启用身份模块。

## S03 唯一 tasks 请求扩展（DS+契约 owner）

**后续具体补充（仍未接收）**：审批完整任务绑定、PreparePlan、AISessionID持久和消费签名见APPROVAL-WIRING.md；工作流SubmitTx/WorkflowBinding见WORKFLOW-WIRING.md。旧S03下方字段建议是历史讨论输入，PlanDigest必须服务器计算，不得接受客户端自报摘要。DS现有Command尚缺executionId/schema/timeout/privilege/expiry/scope等转换Agent Offer的必要字段；不得只按taskId/action/params构造默认root/approval证明。未经接收继续在各自模块和隔离测试实施，不在main组合。

文件 `internal/tasks/ports.go`，保留 Request 的原字段，建议精确增量：

```diff
 type Request struct {
     SubjectID string
     ActionID string
     NodeIDs []string
+    CanonicalParametersJSON []byte
+    IdempotencyKey string
+    PlanDigestSHA256 string
+    ScopeRevision uint64
+    ApprovalID string
+    Source string
+    PluginID string
 }
```

这些字段必须由可信统一入口构建，不能当客户端 claims 放行；SubjectID 不自行证明身份。Authorizer/审批/审计失败在持久调度前关闭，每节点派发再查当前权限、插件状态、AI范围。审批一次性绑定操作者、摘要、目标快照、世代及期限，由任务事务原子消费；普通5、root/修改1→5、未启动15分钟 TTL、UNKNOWN不重跑。新字段零值不能默认授权：旧调用仍可编译但新执行入口拒绝不完整请求。需要 DS 对持久提交/审批消费事务做精确回提；原 Submitter 不能承载完整受控执行时不以无参数任务替代。测试/回退：旧离线测试继续，新增越权/重放/撤权负向；未经接收不派发。

## S04 WSS 依赖及发布（精确 patch 已提供，仍 REQUESTED）

当前根go.mod仍只有protobuf。独立精确diff见 `docs/contracts/revisions/v09-five-day-cx02/S04-WSS-DEPENDENCY.patch`：coder/websocket v1.8.15，两个校验和值来自固定Go模块下载/校验；该版本无模块依赖，Go最低1.23，与当前1.27.3兼容。调用方将为transport的WSS适配与Agent客户端；不影响现有Sender/Proto字段。接收时应与WSS生产import一起集成，否则go mod tidy -diff会正确报告多余依赖。回退为不接WSS，保留health/Agent离线入口；不能降级ws或跳TLS验证。

已通过本目录隔离WSS+mTLS+二进制Protobuf PoC：native主动连接、frame上限、文本/节点不符/吊销/Origin拒绝。测试脚本将版本和sum只写自己忽略的reports子模块，根go.mod/sum/package-lock前后SHA256相同。Context7先resolve再query，并对固定版本本地源码核对API；依赖漏洞扫描含测试也PASS。没有将PoC源import到根工程，没有修改根配置或锁。

wazero v1.12.0仅在同一隔离PoC验证128KiB内存上限、未注册Host拒绝、50ms无限循环终止、随后正常模块返回42；其x/sys v0.44.0也仅隔离锁定。Linux/1C1G/完整Host ABI及持久生命周期未验收，**不申请自动采用生产WASM运行时**。Eino隔离试验另记，不加入根依赖，不等同真实BYOK/POC-06。全部第三方版本和sum保存在tests/poc/v09/platform对应锁文本；根无新增依赖。

## S05 中央Xray材料与完整事务审计（PROPOSED，R37）

精确auth候选见`XRAY-CENTRAL-MATERIALS.md`及`internal/auth/xray_materials.go`；`IdentitySQLStatements.AppendXrayMaterialAudit func(context.Context, []XrayMaterialAuditEvent) error`必须映射同一真实Tx，保留scope/instance/inbound/user/reference/plan/revision/result，不可转成丢字段的旧事件。无sink时新材料写入明确拒绝；当前父ctx不能继承以前事务的审计能力。现有SQL表形状和公共迁移编号未改。

兼容变化：首次持久reserved将auth payload切到v2；新实现兼容v1并保留v2，固定R36旧writer实际拒绝v2且未改原字节。接收方必须协调新auth部署与降级/恢复准入；不可清除永久claim或改版本绕开。回退只能保持未启用材料或使用审查过的恢复流程。调用方为受控plugin task及单用户客户端导出，已在隔离SQLite真实cipher/fence/审计组合通过；持久用户/配置/profile与正式Host/任务/schema/页面仍待接收，无新增公开credential kind/API/Proto/锁/核心可选插件依赖。
