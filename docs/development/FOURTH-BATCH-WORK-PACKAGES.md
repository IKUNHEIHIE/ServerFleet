# 第四批任务：有界帧解码、节点授权接线、只读节点详情

日期：2026-10-29；总控codex-review-01。基线为第三批审查集成后的main，开始时记录实际HEAD。本单分配任务，不自动启动Agent；三项可人工并行派发，各自完成本地提交后停止。

## 共同约束

读AGENTS、README、架构索引、模块归属、依赖规则、TASK-BOARD、本任务单及第三批报告。每项使用下列独立分支/工作目录；不在已完成目录叠加新任务。不得改其他任务文件、推送、合并main或启动其他Agent。

共享保护集合S：根/模块AGENTS、现有ports.go、全部依赖清单/锁、API/Proto/gen、Buf/Redocly、CI/scripts、依赖规则、数据库公共结构、原始资料和已批准ADR。均不可修改，不加依赖。需要公共接口或来源冲突时提交具体差异给总控，不自行改候选或基线。只有N02下文明确列出的两个既有测试适配文件获本次协调修改授权。

全部为离线实现和合成测试；不连接真实Controller/Agent、数据库、云或真实节点，不提供生产默认放行mock，不批准G2冻结、运行时profile或真实执行。

## T01-B · 有界原始帧解码候选

| 项目 | 分配 |
|---|---|
| 唯一会话 / 审查 | Codex codex-01 / 总控独立通信与安全审查 |
| 分支 / Worktree | feature/frame-decoder-cx01 / `D:\admin\desktop\codex\myproject\ks.worktrees\feature--frame-decoder-cx01` |
| 允许写入 | internal/transport内新增解码/错误/测试/说明；现有T01-A测试和校验规则保留，最多增加必要的局部调用接缝，不改ports.go |
| 目标 | 在现有ValidateEnvelope前增加实际可调用的离线原始字节入口，先限制输入长度，再有界解析/解码，最后调用原语义校验。避免只检查proto.Size(decoded)却漏掉实际帧大小 |
| 输入与限制 | 调用方显式传入上下文/时钟/限制；原始字节上限、递归/结构工作限额必须为正且有界，无隐藏生产默认。扫描和Protobuf解码两阶段均不得先无界递归/分配。错误只暴露固定分类，不回显原始帧/参数 |
| 重复字段候选规则 | 本轮局部保守规则：Envelope已知单值元数据字段1–7重复，以及oneof字段20–32出现多次（同种/异种均含）拒绝，不能默许last-one-wins。正常payload内既有repeated字段仍允许。规则写成明确候选假设，不改wire或宣布冻结 |
| 兼容边界 | 合法未知扩展字段不能一律当作非法；未知payload-only仍由现有语义入口拒绝。畸形tag/长度/截断、异常嵌套必须受限拒绝，不panic。不能声称能识别所有部分字段拼接：Protobuf无独立消息边界，此入口仍依赖上游“一帧一个Envelope”约定 |
| 输出/安全 | 失败不给可用消息，输入不被修改；成功只表示结构/语义检查，不证明mTLS身份、序号单调、防重放、任务关联、授权或执行安全。不新增WSS连接、握手状态机、重试/执行或持久去重 |
| 验收 | 直接调用解码入口；13种现有有效消息真实编码后仍通过；原始大小恰好/超界、含未知字段的超大wire、重复元数据/同种和异种oneof/两条完整Envelope拼接、畸形长度/递归边界、typed nil/语义失败及无消息输出。限额为合成注入值；禁止只在测试里另写防护算法 |
| 命令 | go test -count=1 -cover ./internal/transport/...；npm run check:go；npm run check:policy；npm run check:contracts；npm run check:security |

使用现有protobuf依赖，先按项目规则查当前文档；不手改生成代码。记录候选接收规则和未覆盖的原始编码歧义，不把“解码通过”映射成执行准入。

## N02 · nodes接入授权错误分类

| 项目 | 分配 |
|---|---|
| 唯一会话 / 审查 | DeepSeek deepseek-01 / 总控安全与节点审查 |
| 分支 / Worktree | feature/nodes-auth-errors-ds01 / `D:\admin\desktop\codex\myproject\ks\.worktrees\feature--nodes-auth-errors-ds01` |
| 允许写入 | internal/nodes内可见性实现/测试/局部说明；**明确批准**同步tests/testkit/auth.go和auth_test.go的拒绝sentinel兼容，其他共享/跨模块文件不改 |
| 目标 | nodes.Visible/VisibleFilter/VisibleClassify消费已合入auth的errors.Is分类：仅明确auth.ErrDenied可跳过节点；ErrInvalidCheck、ErrPolicyUnavailable、取消/期限和未知错误必须中止并返回错误，不返回部分成功列表或分类计数 |
| 调用规则 | 继续固定nodes.read、逐节点目标、原顺序和输入只读；不要为方便批量授权改变端口或缓存允许。主体空白可拒绝，但不得把带首尾空白的不同主体静默trim成已授权身份；身份来源仍由可信上游绑定 |
| 测试适配 | 旧tests/testkit.ErrDenied与auth.ErrDenied需兼容errors.Is（保留既有引用的源兼容）；mock只能放测试。新增真实NodePolicyAuthorizer组合测试，不只使用自定义fake。auth实现本身不改 |
| 负向验收 | 全拒绝/混合授权只显示允许节点；包装ErrDenied仍过滤；第一次或最后一次策略失败、已允许一个节点后失败、未知错误、非法请求、取消/期限都返回错误且无部分结果。覆盖两个wrapper。撤权下次读取生效，统计不泄露被拒节点；现有取消回归保留 |
| 边界 | 不加HTTP、持久化、审计写入、角色/动作或AI/root权限；不把原始异常接入对外响应或日志。本轮只修正内部调用语义，不制定公共错误payload |
| 命令 | go test -count=1 -cover ./internal/nodes/... ./tests/testkit/...；npm run check:go；npm run check:policy；npm run check:security |

本次语义变更有意废止“授权后端失败也成功返回部分列表”的旧断言。应改成真实拒绝/不可用分类测试，不能删掉失败覆盖来取得通过；交接列出调用者兼容变化和剩余接线风险。

## W03 · 只读节点详情与列表选择

| 项目 | 分配 |
|---|---|
| 唯一会话 / 审查 | Gemini gemini-01 / 总控契约、安全及UI审查 |
| 分支 / Worktree | feature/web-node-details-gm01 / `D:\admin\desktop\codex\myproject\ks\.worktrees\feature--web-node-details-gm01` |
| 允许写入 | apps/web/src/features/nodes内详情组件、与NodeListPanel组合的局部包装、测试和说明 |
| 目标 | 用当前已提供的NodeListItemView显示选中节点的名称/ID/状态/平台/Agent版本/最近在线及CPU/内存质量；从列表点击或键盘激活选择，可明确关闭。使用简单非模态详情区，不引入弹窗库 |
| 选择边界 | 仅存选中ID，每次从调用方本轮ready节点集合解析当前对象；不得缓存旧节点对象。加载、失败或选中ID已从来源移除时立即隐藏旧详情，不自动改选其他节点。来源恢复时只能使用本轮当前记录；本地搜索不改变授权范围 |
| 数据边界 | 不增加未冻结的IP/地区/凭据/连接诊断字段，不从浏览器推断关机或身份验证状态；未知继续未知。复用已有状态/指标/时间展示判定，保留offline无值、有效0/100、invalid和stale可见时间；无机器日期读取或新默认采样策略 |
| 交互与验收 | 详情区有可访问名称，选择/关闭可用键盘；验证换节点、同ID数据更新、来源移除、ready→loading/error/empty、关闭、不误显示旧数据；现有搜索/筛选/键盘和指标回归仍通过；渲染与选择均不访问网络 |
| 禁止 | App/main/全局CSS/共享UI/依赖及S；不接真实API、Agent、终端/执行、编辑、删除、重装、吊销、批量任务或权限管理；不伪造可操作按钮 |
| 命令 | npm run check:web；npm run check:policy |

详情只展示当前上游提供的可见集合；隐藏旧详情不是后端授权实现。不把局部ViewModel当作冻结Controller DTO。

## 交付与停止

每项交接：实际目录/分支、起始HEAD和最终commit、文件清单、命令与实际输出、至少一个失败场景、候选依赖/兼容影响、FAIL/BLOCKED/UNTESTED及剩余风险。工具/网络失败如实保留，不能降门槛或改锁文件规避。只创建本地提交，交总控审核后停止；不合并main、不推送、不自行开始下一任务。
