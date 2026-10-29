# READ-SEC-01 · 安全来源与证据独立复审

日期：2026-10-29（Asia/Shanghai）；任务READSEC01-REVIEW；总控会话codex-review-01；起始main `ddf8c5beaee24694e687ad3166db32597ef60df1`。作者codex-01，原分支docs/read-security-evidence-cx01，起始7ef7877，精确HEAD `f3cdf73d83590b686a61327905a4c45d0b8e94d5`。作者跟踪工作区干净；未改其源码、文档或提交历史。

## 接收决定

**ACCEPT SECURITY EVIDENCE**：仅接收docs/contracts/decisions/node-read-security-01/的SOURCES、SECURITY-ACCEPTANCE、HANDOFF三个新增Markdown文件。没有发现阻止本轮文档接收的新问题。没有代码/候选/原始资料/批准状态/依赖/共享配置修改，也没有新增认证、HTTP mapper、存储或执行实现。

来源层级区分准确：G0产品约束、明确Accepted及适用范围、未批准G2候选、源码可见事实、历史执行证据分别标识；没有因为文件名含已批准就冻结其草案接口，也没有拿旧登记否定较新批准。对AI/Agent特定约束说明适用条件，不把AI审批或Agent注册令牌期限套作浏览器会话规则。

缺失PRIV/WC/PKI-OPS/UPDATE完整原文明确为“未收录/仅引用”，不是从未批准或失效；S05解释性附录不冒充完整原文。相关证书/恢复/更新事项按是否涉及该能力列条件，不强迫首条只读实现所有功能，不搜索项目外归档或凭据。总控核对原来源/替代索引、G1批准、AI/PRIV附录、BOOT批准段及历史登记，来源/效力描述与原文相符。

## 实现与验收边界

16个安全验收项覆盖D05可信主体/凭据/撤销、D06错误来源/枚举/敏感信息/混合故障/取消、D07空库存/共同范围/旧scope复用/TOCTOU/计数/复制/缓存/来源切换。每项给出来源、当前证据、缺口和未来负责模块，没有把历史离线或静态PASS升格为真实身份/HTTP/浏览器/存储证明，也没有另列产品选项或自行关闭D项。

特别核验：来源坏ID可经真实ReadVisibleInventory→CheckNode返回auth.ErrInvalidCheck，不能只按sentinel推断HTTP400；内部Denied在主体与单节点等不同阶段也有不同意义。文档未擅定最终500/503/404映射或增加wrapper。旧scope保持捕获集合、新调用重新准备、来源当前性与跨请求生命周期仍靠可信组合落实；顺序两次来源读取不等于策略/库存/输出原子性。成功深复制和局部key不构成实时撤权或认证保证。

文档以作者7ef7877基线引用FOLLOWUP02的4PASS/1FAIL，明确为历史未接收交付而非当前主线新漏洞。**当前main已接收W05及两次返修，两个问题已关闭，5个独立组件用例通过；真实浏览器W06仍未验收**。保留作者历史语境，不改写其原证据或要求同步分支；本报告明确这项主线状态更新。16项表中的真实认证/HTTP/缓存/存储等缺口仍然有效。

## 总控实际核验

独立integration/readsec01-review先登记任务，再按共同祖先合入精确文档提交，组合HEAD为`240453f6a342cad79dae2ae2ccd28947820fd92d`。相对原main只增加作者3份文档及总控登记；原Web/Go/契约/生成物保留，之后仅增加复审/任务板/README和证据文档。

总控先阅读检查HANDOFF的只读核验脚本，再提取到忽略reports/_review运行。作者精确目录和独立组合各运行一次，均得到：3文档、52本地链接、76次行区间检查、32原文短句、16验收项PASS；原始32处引文确实位于其链接区间。另核对源语境/效力而非仅信任机械检查。实际文件名检索仅命中已批准AI/PRIV解释性附录，与原索引缺失说明一致，不能据此推导旧ADR无效。

脚本核对的历史auth36/nodes33断言及候选hash只是比对已提交证据，不是本轮重新运行这两份业务探针。作者也如实只做文档/policy检查，没有宣称新跑完整check/security或真实认证。总控合并门槛所需的npm ci/tools:install/check/security结果另外按本次执行归档，不能与作者历史数字混用。

固定Go1.27.3、Node24.22.1/npm11.19.1，未增加依赖或改变许可/检查。

| 检查 | 结果 |
|---|---|
| 作者精确目录及独立组合的只读引用/短句检查 | 两次PASS：3文档、52链接、76行区间、32原文片段、16验收项；历史证据JSON/hash一致 |
| 原文效力/语境人工复核与缺失ADR文件核验 | PASS：明确区分批准/候选/历史/适用范围；缺失原文不虚构、未升格为失效或批准 |
| 组合npm ci / npm run tools:install | PASS，127包/4个固定工具；复用本地file模块缓存，未改许可/依赖或关闭校验 |
| 组合npm run check | PASS，policy/contracts/go/web/git；Go212顶层/805子测试、0失败，Web179项 |
| 组合npm run check:security | PASS，npm0漏洞、govulncheck无漏洞 |
| 原资料/契约/生成一致性 | PASS，69原文件/3原候选、OpenAPI/Buf及r02/r03显式检查；r03候选hash未变 |

摘要/原始输出见READ-SEC-01-RESULTS.json及evidence/readsec01-review-*。真实身份、HTTP、浏览器、策略库存/输出事务、WSS/mTLS、任务/审批/执行、Linux race、GitHub CI、1C1G性能均未在本轮验收。G2、D01–D07及P1-C10保持OPEN。

归档阶段首次staged diff检查因提取脚本文本末尾额外空行FAIL，未生成最终复审提交；主线已先快进到通过全量检查的组合240453f。只规范化这份派生归档文本的末尾空白后重查并补交复审记录；作者文档、原始输出、代码/候选未改变，也未降低检查规则。初次失败摘要见evidence/readsec01-review-staging-retry.json。

## 收口安排与停止

READ-SEC-01完成接收，Codex原任务停止，保留目录/报告，不分配新的实现或继续扩展文档。DeepSeek继续原READ-PLAN-01-R1修正来源/onlineOnly兼容，Gemini继续原W06真实浏览器验收；本轮未读取两位未提交内容，不改变任务/基线/目录，不启动/代发消息。

等两项交付到齐并完成复审后，再由总控将修正方案与本安全证据组合成负责人可审查的具体选择包；Cookie/凭据、R1-A/R1-B、legacy VM、错误/撤权/缓存与真实接线仍未获批，不以本轮文档接收替代批准。完成本轮证据和main接收后停止，无远程推送或云/节点操作。
