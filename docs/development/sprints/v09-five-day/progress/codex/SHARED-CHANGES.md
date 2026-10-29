# 共享变更请求 / codex-02

2026-10-30；状态 REQUESTED，未经接收不修改公共文件或主线消费端。CX-01 无共享文件改动，无新增依赖。

## S01 OpenAPI 认证迁移（API owner）

文件 api/openapi/g2-merged-candidate.yaml；需总控固定工具重新生成检查工件，不手改 gen。

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

## S04 WSS 依赖及发布（后续精确 patch）

当前 go.mod 只有 protobuf。先完成 transport 自有 mTLS 身份验证和离线门禁；WebSocket 库版本、go.sum 的完整 patch 要在选型文档查询与隔离 PoC 后提供。此项未给精确锁差异，不要求总控批准任意版本或 runtime。WASM/Eino 同理，未选生产 runtime；不添加依赖、不伪称 PoC 通过。
