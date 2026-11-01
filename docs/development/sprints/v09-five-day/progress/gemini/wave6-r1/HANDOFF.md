# 交接说明（gemini-06 · Wave 6-R1）

## 运行

```sh
npm run check:web                 # typecheck + 39 文件/338 项 + build
node tests/browser/v09/run-acceptance.mjs                  # 16/16（负向/退化，未动）
node tests/browser/v09/run-live-controller-acceptance.mjs  # 70/70（负向/退化，未动）
node tests/browser/v09/run-positive-success-acceptance.mjs [--skip-build] [--require-complete]
                                  # 26 步：20 PASS / 6 BLOCKED / 0 FAIL（当前基线预期）
                                  # --require-complete：存在 BLOCKED 时退出码 1（已实测）
```

正向套件需要 WSL 默认发行版（编排细节同 wave6 HANDOFF：交叉编译 linux/amd64、
localpki 格式预置 CA、`-agent-pki-dir` 必须显式指向、前台 exec 保持会话）。

## 本轮新增要点

- **AI 模型设置**：`#/ai` 页面「AI 模型设置（当前用户）」面板——owner CAS
  （expectedRevision）、凭据下拉只列 ai_api_key+ai 作用域元数据（引用/状态/修订号）、
  409 冲突诚实重读、非管理员只读；秘密值只在「设置 → 凭据」提交一次，绝不回显。
- **显式生成**：AI 会话页每条用户消息「生成回复」→ POST …/messages/{messageId}/generate
  （无请求体）→ 渲染完整脱敏助手消息；失败按 AI_SOURCE_UNAVAILABLE /
  NOT_AUTHORIZED / GENERATION_OUTCOME_UNKNOWN / MODEL_REVISION_CONFLICT 诚实显示；
  页面明确标注"不是浏览器逐 token 流"。
- **P02** 现在验证真实 `location.reload()` 后 /auth/me 的实际 csrfToken 与写锁状态。
- **P13/P14**：真实失败任务页面（异人批准 → Agent CAPABILITY_REJECTED → failed）
  与撤权隔离（403 不清认证）。
- **B 组全部结构化**：每个 BLOCKED 都先建立了真实前置结构（双在线 Agent/SHA 夹具/
  合成凭据+owner CAS 设置/服务组/真实备份），精确引用服务端原始响应；接线后重跑
  同一脚本即按各步终态断言翻转。

## 已知缺口（登记于 SHARED-CHANGES，均非测试域可修）

1. operator 角色资源授权间歇 403（状态真值正确）——总控 Go 域核查；P13 以 BLOCKED
   承载并附身份存储 dump。
2. BYOK endpoint 固定 api.openai.com + 默认配额拒绝——需 CX 注入 patch。
3. 网关接管审批（409 takeover_approval_required）后继流程无契约。
4. 任务日志/输出查询端点、executions 审计字段、密钥状态端点、常驻连接字段——同前。

## 边界

未改 Go/OpenAPI/Proto/锁文件/CI/`apps/web/public`；未操作 awskr；全部秘密为合成值
且不入 Git/日志；awskr 既有服务未触碰。
