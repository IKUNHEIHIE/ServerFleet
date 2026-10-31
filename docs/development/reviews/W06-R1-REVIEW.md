# W06-R1复审与只读阶段收口

日期：2026-10-29（Asia/Shanghai）；任务W06R1-REVIEW；总控codex-review-01；起始main `157549e711e018a33e54584373b9c5136d1cf5d3`；独立目录integration/w06r1-review先登记任务再审查。

## 决定

接收Gemini gemini-01精确交付 **`2c1c8bcbfbd71443ae4caed96ad4905e0c820e97`** 及保留的原99cd663报告，范围仅apps/web/src/features/nodes/BROWSER-ACCEPTANCE.md。**W06-R01/R02 CLOSED**，依据是作者修正后的证据与总控3项独立浏览器补验共同成立，不是原25项断言自动证明全部前提。接收时总控澄清C24真实来源、记录代码HEAD、原文/路径及证据来源，没有改生产组件、测试、配置、依赖、候选或作者目录/工件。

作者跟踪工作区干净，从99cd663仅改1份允许报告，原W06/W06-R1忽略目录分别保存，历史未改写。作者报告的R1结果代码HEAD为99cd663，最终2c1c8bc仅改报告；代码与已接收W05一致。原两个W05隔离问题保持CLOSED。本次仍是合成来源的隔离组件验收，真实认证/HTTP/cache/跨主体安全未完成。

## W06-R01关闭

已读R1驱动/预览/结果JSON，人工检查新增4张截图，并核对生产NodeListWorkspace的激活/关闭处理。C20先打开详情，Shift+Tab回仍选中行，beforeRepeat同时记录hasDetail=true和aria-current行名称，再Enter回详情；C21没有先关闭，继续Shift+Tab→Space回详情。驱动使用真实CDP Input键盘事件，未用element.focus或JS.click伪造路线。C22分别真实键盘Enter/Space激活关闭按钮，两次均回行且详情消失。原C7改称“关闭后Space重开”，未把历史证据追改为重复激活。

selectedRowId这个预览字段实际提取选中行的显示名称aws-tokyo-01，非模型ID；结合真实行内容node-online-01、aria-current、详情同一名称及驱动连续无切换/关闭的前提核对，并未把名称当授权或请求身份。C20/C21覆盖了已选同ID分支，W06-R01关闭。

## W06-R02关闭及总控独立补验

作者C23补了跨来源旧请求迟到失败；C25在仍存活页面中卸载NodeLoader，两个独立pending请求分别迟到成功/失败，页面不恢复内容、错误数组0→0→0。首提交C12/C15/C16的实际完整字符串已经存进traces，不再只有长度。作者25个断言均PASS，记录/截图可读，但仍有以下证据前提需澄清：

- C24先产生scope-b旧请求，再broken(scope-a)→a(scope-a)，实际跨key，不能单独证明“同key失效恢复后旧拒绝”。
- Preview在空依赖effect里建立snapshot回调，sourceKind捕获初始a，后续不能证明来源；snapshot.body最多500字符。请求key、DOM状态和原始首提交文本需分别使用，不能将这个字段当权威。
- C25驱动检查并保存了第一次卸载abort，第二次没有独立记录abort断言；这不等于生产没有中止，但证据仍可更明确。

总控直接在自己的忽略reports/_review/w06close中复制作者预览源码/临时配置，按Context7核对的Vite自定义配置方式重新构建，真实导入当前生产NodeLoader和已有合成fixture；没有复用作者预构建bundle、修改作者工件或给生产加入fixture。仅127.0.0.1:8463临时静态服务，使用Computer Use新建后台IAB页的受限CDP开发能力操作合成来源/Promise并观测实际DOM，没有真实账号/Controller/API。

浏览器观测UA为Chrome/156.0.0.0。代码HEAD `ed6b80c`（只有任务登记变化，业务代码与起始main相同）；操作以Runtime.evaluate调用显式QA接缝，**这些是来源/生命周期开发补验，不冒充原生键盘输入或总控重跑作者25项**。作者键盘证据仍按上节独立核对。

| 总控独立用例 | 实际前提、动作与结果 |
|---|---|
| S1：真正同key的旧拒绝 | 初始request0 scope-a未settle→broken仍scope-a→a恢复生成独立request1 scope-a；先检查两个key相同、0已abort，再拒绝0。新页面仍loading、无表格/详情/alert，错误数组空；随后1成功显示8节点。PASS |
| S2：卸载后旧成功 | 新request2 scope-b未settle→只卸载组件，页面/QA仍存在；实际检查2已abort，再settle success，表格/详情/alert不恢复，错误为空。PASS |
| S3：独立卸载后旧拒绝 | 重新挂载生成独立request3，先检查pending且未abort，再卸载并检查3已abort，随后settle failure。无表格/详情/alert，错误/未处理拒绝0，焦点仍body。PASS |

11步实际状态、全请求key/abort/settle顺序、焦点和错误数组见evidence/w06r1-reviewer-supplemental-results.json，截图见w06r1-reviewer-same-key-old-failure.png和w06r1-reviewer-unmounted-late-failure.png；操作前后只读观测，不依赖错误的sourceKind。总控复现步骤见w06r1-reviewer-procedure.md。3项补齐所缺前提，W06-R02关闭。

作者原25项/首提交JSON与驱动/预览/10截图均原样归档；新增4截图本轮人工检查，其余6张是原W06场景在R1的复跑工件，原W06六图已在FOLLOWUP04人工核验。未宣称本轮逐张重查所有复跑截图。27项来源到归档的复制SHA256见w06r1-evidence-manifest.json。原W06原记录与前次失败结论保留，报告C24明确标回其真实跨key前提，总控补验另列。

父级useLayoutEffect仅观察其来源变化的提交，不覆盖所有子组件独立更新；首提交证据属于局部fixture，不能推导HTTP/身份/授权/cache或跨主体隔离。屏幕阅读器、Firefox/Safari、系统高对比度等UNTESTED保留。

## 组合验证

合入作者报告后技术组合HEAD **`4907caf49caf3e0558afa1f9c7f9fd25a59b9285`**；后续仅审查/证据/状态/负责人选择文档。固定Go1.27.3、Node24.22.1；先npm ci、固定tools:install，再完整check及security。当前无新的生产代码/公共契约差异，只新增/澄清Markdown报告。

| 检查 | 真实结果 |
|---|---|
| 作者证据 | 25/25断言PASS；实际范围和C24前提已澄清；不是总控25项重跑 |
| 总控浏览器 | 3/3独立补验PASS，11步结构化记录，2截图；只做来源/生命周期边界 |
| npm ci / npm run tools:install | PASS，固定依赖和4Go工具，本地file模块缓存，未关闭校验/批准额外install scripts |
| npm run check | PASS，policy/contracts/go/web/git；Go212顶层/805子测试，0失败；Web179，0失败 |
| npm run check:security | PASS，npm audit 0漏洞、govulncheck无漏洞 |
| 原始资料/契约/生成 | PASS，69原文件/3原候选；r02/r03显式检查及负向/生成校验；r03候选SHA256不变dcb499b108fc6eab67412e08833100f9cb13c93d843863661faab56535180cfd |

独立预览构建通过；Computer Use页已关闭，只中止本任务启动的静态服务（exec会话中断退出不当作测试FAIL），8463及作者8462/9462未见监听。未停止其他服务。摘要见W06-R1-RESULTS.json，全量输出见evidence/w06r1-*；没有新增运行时测试代码、删除测试、放宽检查或改写原证据。

## 负责人选择与停止

READ-PLAN-01-R1、READ-SEC-01及W06-R1三份材料现已接收；三个作者当前任务完成停止，不再自动安排辅助实现。总控将已评审证据汇总到 **docs/contracts/decisions/node-read-01/OWNER-CHOICES.md**：选择首版A1先兼容列表/A2列表+详情，以及B1浏览器会话Cookie/B2先Bearer-only；推荐A1+B1。详细代价、下一轮三位设计任务的允许目录/交付/停止条件、方向确认的作用均列在包中。

负责人尚未选择；本轮没有新开发分支/工作目录、启动/消息派发或实现授权。方向确认后才登记具体设计任务，设计审查再形成可批准的契约/安全决定；不能把推荐或方向选择升格为D01–D07已批准。G2未冻结、P1-C10仍OPEN；真实身份/策略库存来源/HTTP/cache/数据库和输出原子性、通信mTLS、任务审批执行、Linux race、线上CI/1C1G性能均未验收。
