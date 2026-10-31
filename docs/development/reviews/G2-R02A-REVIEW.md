# G2-R02A · 节点只读契约独立复审

日期：2026-10-29（Asia/Shanghai）；审查任务G2-R02A-REVIEW；审查人codex-review-01；起始adce532。精确提交`8165057044d9525f5e7e22e669e1c708486c22ea`，作者codex-01，分支contract/nodes-read-cx01，起始0fef8ba。只读核对作者工作区干净，未改作者源码或历史。

## 决定与接收边界

**ACCEPT REVIEW ARTIFACTS**：接收两个既有GET的r03隔离候选、来源/操作/字段矩阵、兼容与待决说明、真实契约负向检查和离线边界探针。26个新增文件全部在获准的api/openapi/revisions/g2-r03、docs/contracts/revisions/g2-r03、tests/contracts/g2r03内；作者没有改旧候选、r02、端口、生成物、依赖或生产实现。

没有发现阻止本轮评审工件接收的新问题。**不批准D01–D07、运行时profile、HTTP接线或G2冻结**；P1-C10仍OPEN。真实客户端、认证/错误迁移与数据源原子性没有因为静态检查通过而得到验收。

DeepSeek的TASK-01B和Gemini的W05不变，继续原adce532基线和任务。没有读取/修改这两位未提交代码、切换其分支或发送消息。第六批原三项交付现均完成审查，其契约项仅按上述候选范围接收。

## 关键审查依据

- 原Merged已存在GET /api/v1/nodes和listNodesForTransfer；r03保留路径、operationId、onlineOnly参数、items包裹及必填id/name/online。详情保留nodeId/displayName/status/revision及原字段类型。只包含这两个GET，不能同Merged拼接加载为第二套重复路由。
- Core与Merged的差异已用实际来源/hash核对；列表原有CPU/内存但无采样时间。r03仅提议可选富化字段，未用假revision、响应时间或online=false构造权威状态/新鲜度。真假0/100、nullable、旧无时间形状均有实际工具样例。
- online/status一致性、新增字段/错误/缓存头、不可见与不存在统一404均明确待批；保留列表Bearer与详情Cookie OR Bearer、Error与Problem的原差异，不偷偷沿用r02运行时header/profile。Web不能据此直接用Cookie调用旧列表。
- 可选新增字段并非无条件兼容：原开放NodeSummary允许扩展名碰撞，新status/revision/sampledAt约束会收窄这类旧扩展；真实工具展示旧形状通过、新形状拒绝，文档明确需要客户端/产品批准。
- 来源矩阵与候选由固定生成器重算，测试同时深比较原属性/required/参数/认证/错误媒体，并让实际Redocly拒绝坏refs、重复operationId、缺路径参数及16个错误载荷；没有用测试自写DTO验证器冒充工具。
- Go测试实际调用当前auth/nodes，确认纯拒绝过滤、混合故障中止，同时记录两个现存限制：Visible空库存不调用策略读取；每节点读取可能来自不同快照。这些PASS是局限复现，不是缺口已关闭，更不证明真实HTTP鉴权。

## 总控接收的共享差异（D08）

总控审查并批准作者提交的具体patch：根check:contracts增加r03显式入口；Web candidate-adapter只修正两处来源说明，函数、类型、候选映射行为不变。改动在独立integration/batch06-contract-review目录完成，没有修改Gemini正在工作的目录或任务。

注释改变了来源文件字节，因此先保存作者原provenance，再调用原固定`build-candidate.mjs --write`重算。只改变provenance的webAdapter哈希：5eebba4c… → 026f71ec…；其他来源、MATRIX及候选YAML未变。候选SHA-256仍为`dcb499b108fc6eab67412e08833100f9cb13c93d843863661faab56535180cfd`。作者原provenance归档在本报告evidence，不改写其提交。共享技术提交为`7c361324f8d85d3ad9d9660138d13cfa64c0b2a6`，这是组合验证的代码HEAD。

P1-C10及r03 README/DECISIONS同步集成处理状态；D08工具/注释协调完成，D01–D07仍OPEN。原VALIDATION/RESULTS/patch是作者交付时的记录，保留，不把过去“未接线”的陈述伪改成作者已完成集成测试，也不重复应用已处理的patch。

## 总控实际复验

固定Go1.27.3、Node24.22.1/npm11.19.1及原锁定Redocly2.72.0/yaml2.9.1/Buf1.75.0。作者精确提交先独立复验，接线/注释/重算后再执行组合检查。

| 检查 | 结果 |
|---|---|
| 作者精确提交：node tests/contracts/g2r03/run.mjs | PASS，2路径/2操作；JS14/14；Go4个顶层+5个子测试；候选hash与作者一致 |
| 真实负向检查 | PASS（预期拒绝）：3种结构缺陷、16个坏载荷逐例有schema诊断，共32项错误诊断；这些是测试成功，不是候选FAIL |
| 共享patch应用性检查 | 作者精确提交上PASS；总控按审查过的具体差异处理，没有盲目应用额外内容 |
| 固定生成器重算 | PASS，只有Web注释来源hash变化，候选/MATRIX无diff |
| 独立集成npm ci / npm run tools:install | PASS，127包/固定4个Go工具；复用已验证的本地file模块缓存，未改依赖/脚本许可或关闭校验 |
| npm run check（已经实际接入r03） | PASS：policy/contracts/go/web/git；r03再跑14项JS及真实Go边界；全包Go162顶层/600子测试、0失败；Web151项 |
| npm run check:security | PASS，npm0漏洞、govulncheck无漏洞 |
| 旧资料/契约/生成物 | PASS，69原始文件与3原候选字节完整性；原OpenAPI、Buf兼容与生成一致性、r02检查保持通过 |

完整摘要见G2-R02A-RESULTS.json；evidence/g2r02a-review-*保存组合check/security/r03结果、真实负向诊断、Go边界、原provenance及生成记录。作者首次harness误判及原始FAIL仍保留在其交付证据；最终真实JSON pointer校验未删负向用例、未降低规则。

## 待决与后续

D01旧客户端兼容、D02状态权威、D03版本来源/精度、D04采样形状/时效、D05身份/凭据、D06错误映射、D07空集合预检/列表策略与库存原子性仍待批准及真实验证。尤其内部Node没有revision/platform/agentVersion，不能把它强转公开NodeDetail；空库存预检与共同策略视图也不能靠端口字符串或缓存旧允许补齐。

仅给Codex分配AUTH-01B：在auth内新增离线读取预检/不可变范围，见CODEX-AUTH-01B-WORK-PACKAGE.md。它不改原实时Authorizer或nodes，不实现身份/HTTP/存储原子性，不独立关闭D07。其他两位继续原任务。

没有运行真实API、Cookie/Bearer认证、WSS/mTLS、存储/任务/执行、真实客户端缓存或生产性能测试；GitHub/Linux race未运行。交付本报告、main接收与Codex新目录后停止，不启动其他Agent、不推送远程。
