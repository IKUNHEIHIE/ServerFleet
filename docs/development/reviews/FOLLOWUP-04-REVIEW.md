# READ-PLAN-01-R1 / W06独立复审

日期：2026-10-29（Asia/Shanghai）；任务FOLLOWUP04-REVIEW；总控会话codex-review-01；起始main `69d6d431281512dec6c5847b8fe2d06e1ce97b48`。独立integration/followup04-review先登记任务，再审查精确交付与忽略目录中对应证据；未改作者目录/提交/原证据，未启动或代发其他Agent消息。

## 决定

| 任务 | 精确交付 | 结论 |
|---|---|---|
| READ-PLAN-01-R1 / deepseek-01 | `4da769c166c0cf0eabc8970a536f077374ad43a4` | ACCEPT_WITH_CLARIFICATION：原RP01-R01/R02关闭；接收原方案/返修3份文档，总控补公开字段省略与局部null的事实澄清。方案仍PROPOSED，不批准实施 |
| W06 / gemini-01 | `99cd6635b3da4739a4e2b30816bb9749d14419ae` | CHANGES_REQUESTED：W06-R01/R02两个P2是指定覆盖缺口，报告未合入main；保留原19项作者浏览器PASS及6张截图，不据此宣布完整验收，也未复现新生产缺陷 |

两位跟踪工作区干净。DeepSeek从98a1f6b累计只新增3份范围内Markdown，保留d0709fc原提交；Gemini从ddf8c5b只新增BROWSER-ACCEPTANCE.md，生产组件/测试/配置/依赖未改。合并采用共同祖先，未把旧分支缺少的新主线文件当成删除。已接收W05-R01/R02保持CLOSED。

## READ-PLAN-01-R1接收与总控澄清

作者已区分Node中已有Status/Metrics.ObservedAt与真正缺失的revision/platform/agentVersion，说明状态权威、优先级和共同采样语义仍待D02/D04；平台/Agent版本不再是详情非空前置。r03五状态一致性仍为提案，未升格为已批准事实。RP01-R01原问题关闭。

onlineOnly布尔query及默认false已进入R1-A、D01、Controller拆分、COMPAT-01/新增COMPAT-02。方案明确先认证/策略预检→库存与授权可见集合→onlineOnly→字段投影，覆盖省略/false/true、隐藏在线节点、过滤成空不绕预检/吞策略失败；坏query解析/HTTP码仍待批准。RP01-R02关闭。

独立逐条核对**68处引用、25个文件**，历史引用按98a1f6b，新增N03-A/FOLLOWUP03审查引用按本轮起始已提交main读取。路径/范围均有效；另手工核对实际字段、原Merged/r03参数、批准状态及N03-A/W05的历史与当前语境，未以机械引用检查代替语义审查。Q3改为状态权威方向，不再隐含永久移除产品能力；现阶段只是待批方向，详细状态来源/优先级仍须具体方案，不能将Q3泛化为D02已冻结。

返修仍有一处事实表述不一致：ACCEPTANCE的FIELD-01称公开可选platform/agentVersion可为null，HANDOFF及部分概括混用“省略/null”；r03实际是可省略的string，局部Web VM才可null。总控在**集成目录**将这些措辞明确区分，并重申详情缺必填status/revision不得成功。未改schema、可选性或产品选择，不让作者再做一轮纯文字返修。主体澄清提交`a1cd54ae15a524e757004b1b526f8be1371274c7`；最后收口同时澄清Q1同一短语和追加总控接收注，均为文档。原4da769c保留，不能把澄清归为作者原交付已正确。

方案的“W06尚未执行”对应其ddf8c5b核对阶段；当前已读到浏览器记录但整项未接收，HANDOFF接收注明确最新状态。已接收READ-SEC-01的32来源/16缺口仍有效；它按7ef7877引用W05历史FAIL也不代表现主线W05仍失败。

R1-A先legacy列表、R1-B后富化/详情、Cookie方向、错误映射均仍PROPOSED。没有批准认证/CSRF/凭据生命周期、HTTP/存储/字段来源或新Web VM。Q1–Q3是方案选项，**不是足以自动批准D01–D07的完整技术决定**；结合安全A01–A16及W06-R1复审后才形成具体负责人选择包。

## W06证据审查

已读取临时驱动browser-qa.mjs、preview.tsx/HTML/Vite配置、w06-results.json、原日志及常规检查输出，人工查看全部6张截图。作者在独立Chrome资料上通过CDP Input.dispatchKeyEvent/MouseEvent发键盘/鼠标，未使用element.focus或JS.click冒充按键；浏览器记录Chrome/156.0.8078.49。Enter在驱动和报告中实际用keyDown带text，但结果JSON的keyMechanism仍概括为rawKeyDown，补验时需同步，不能混写。

原19项结果均为作者已执行集合PASS，截图支持0/100、陈旧/缺失、1280焦点环及420卡片内横滚的观察。布局阶段观察器在父级来源更新提交时记录文本，C12/C15/C16的断言读取该记录，早于被动effect；不过最终JSON只存firstCommitLen等摘要，补验需保留原始提交文本/状态以利复核。观察器不是所有子组件独立提交的完整追踪。当前端口8461/9461未见监听，本轮没有启动浏览器或预览进程。

**总控本轮没有独立重跑浏览器**，上述是对作者真实驱动、结果、截图和生产事件处理代码的证据审查；不能写成总控19/19实跑。审查证据以followup04-author-w06-*原样归档，18项复制哈希见followup04-evidence-manifest.json，原记录/截图/驱动均保留。

### W06-R01 · P2 · 关闭后重开不能覆盖保持选中的同ID重复激活

位置：作者BROWSER-ACCEPTANCE.md的C6/C7及结论；对应驱动C6→C7顺序。

C6先Escape关闭详情，生产NodeListWorkspace.handleClose明确setSelectedId(null)，然后C7才Space打开同一行。此时触发的是未选中→新选中，**没有进入alreadySelected且详情仍挂载的重复激活分支**。报告却将C7说明为“重复选择仍立即聚焦详情”。这是W04曾专门返修且本任务明确要求的边界，不能用关后重开替代。此外实际脚本只Escape关闭，没有激活关闭详情按钮，报告结论却包括“Escape/关闭返回来源行”。

要求：详情保持打开且同ID保持选中，真实Tab/Shift+Tab返回来源行，分别Enter和Space重复激活，核对立即聚焦详情而非等待另一次刷新；再实际键盘激活关闭按钮并检查详情消失/返回来源行。保存动作前后详情存在、节点ID和activeElement，不用程序focus伪造路线。原C7的PASS保留并准确更名为关后Space重开。此项是证据覆盖不足，**不是判定当前生产重复激活仍失败**。

### W06-R02 · P2 · 缺少迟到失败与pending卸载的浏览器记录

位置：作者BROWSER-ACCEPTANCE.md的C14/C17、局限与结论；对应驱动所有旧请求settle调用、预览的组件生命周期。

指定任务包括迟到成功/错误不覆盖与卸载abort。驱动C14/C17只给旧请求success，没有旧请求failure；C9/C11只测试当前请求错误。预览没有卸载NodeLoader的操作，关闭Chrome不是在仍存活页面中观察pending组件卸载。报告未将这些指定场景逐项列为UNTESTED，却以19/19和“迟到响应不覆盖”收口；不能据此完成W06。

要求：补来源切换后旧拒绝、同key失效恢复后旧拒绝；pending卸载后分别迟到成功/失败（两个独立请求），实际记录signal abort、页面/新来源状态不被覆盖、无后台抢焦点和未处理拒绝。保留顺序与完整状态/首提交证据。已接收组件测试中的对应PASS仍有效，但不能充当此项实际浏览器运行。

## 实际验证

技术组合HEAD：`a1cd54ae15a524e757004b1b526f8be1371274c7`；后续只加上述收口文档/证据/任务状态，没有运行时代码差异。主线运行时apps/internal/api/proto/gen/scripts/依赖与起始69d6d43相同；Gemini报告未参与组合。

| 检查 | 结果 |
|---|---|
| READ-PLAN精确引用/事实审查 | PASS，68处/25文件；字段与onlineOnly原文、历史/当前接收状态已核；公开null表述由总控澄清 |
| W06作者证据 | 已执行19/19 PASS、6截图；指定覆盖缺口W06-R01/R02 OPEN；不是总控独立浏览器实跑 |
| npm ci / npm run tools:install | PASS，127包、4个固定Go工具；本地file模块缓存，不关闭校验，不启用未批准install scripts |
| npm run check | PASS，policy/contracts/go/web/git；Go212顶层/805子测试，0失败；Web179项，0失败 |
| npm run check:security | PASS，npm audit 0漏洞、govulncheck无漏洞 |
| 来源/契约/生成一致性 | PASS，69原文件、3原候选；r02/r03显式检查、OpenAPI/Proto/生成/负向检查保留；r03候选SHA256仍dcb499b108fc6eab67412e08833100f9cb13c93d843863661faab56535180cfd |

原始输出、独立引用核验脚本、Go JSON/Web JUnit及来源/契约结果见evidence/followup04-*，摘要见FOLLOWUP-04-RESULTS.json。原作者FAIL/PASS及被退回报告未删除，未把未执行项写成PASS，未删测试或放宽规则。

归档检查如实记录：首次staged diff检查因作者w06.log的C13空details后有一个行尾空格而FAIL，尚未提交或推进main。该日志改为followup04-author-w06-w06.log.base64.txt无损编码存档，解码SHA256与作者原文件一致；其他原工件未改，未裁剪原日志或放宽检查。重试staged检查及最终check:policy通过，详见followup04-archive-check.json/followup04-final-policy.log。

## 后续与停止

DeepSeek READ-PLAN-01-R1及Codex READ-SEC-01完成停止，不再自动分配纯辅助实现。Gemini在原W06目录从99cd663追加W06-R1，唯一跟踪改动仍为报告；忽略新子目录保存补验。具体范围见PARALLEL-FOLLOWUP-05.md。没有要求同步main、重写历史、修生产组件、接真实数据源或启动其他Agent。

待W06-R1复审后汇总可供负责人决定的具体方案；当前不请求抽象授权、不自行启动R1-A/R1-B。D01–D07/P1-C10仍OPEN、G2未冻结；真实登录/身份/RBAC/HTTP/缓存、策略库存输出原子性、WSS/mTLS、任务审批/执行、Linux race、GitHub CI及1C1G性能均未验收。
