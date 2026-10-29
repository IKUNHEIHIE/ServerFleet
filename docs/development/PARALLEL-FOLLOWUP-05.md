# READ-PLAN-01-R1 / W06复审后的收口任务

日期：2026-10-29；总控codex-review-01。DeepSeek的方案及返修接收为PROPOSED文档，RP01-R01/R02关闭；总控只澄清公开可选字段省略与局部VM可null的区别，没有改变schema或批准新范围。Codex的READ-SEC-01此前已接收。两位本任务完成停止，等待后续明确分配，不启动认证/HTTP/legacy VM或其他新实现。

Gemini的99cd663报告有19项实际浏览器PASS及6张截图，保留为历史证据；因同ID重复激活被关后重开替代、迟到失败/卸载未覆盖，W06尚未完成且报告未合入main。不是已复现生产缺陷，也不重新打开已关闭的W05隔离问题。详见reviews/FOLLOWUP-04-REVIEW.md。

## W06-R1 · 补齐指定浏览器边界与准确交接

| 项目 | 分配 |
|---|---|
| 任务 / 会话 / 审查 | W06-R1 / gemini-01 / 总控UI与安全审查 |
| 原目录 / 分支 | `D:\admin\desktop\codex\myproject\ks\.worktrees\chore--web-node-browser-qa-gm01` / chore/web-node-browser-qa-gm01 |
| 精确起点 | `99cd6635b3da4739a4e2b30816bb9749d14419ae`；保留提交/原19项结果、脚本与截图，追加返修。无须同步main，不reset/rebase或覆盖原工件 |
| 必读 | AGENTS、README、架构索引、模块归属/依赖、TASK-BOARD、本任务单、FOLLOWUP-04-REVIEW；本单在主目录已提交，原目录没有时按精确主线提交只读核对 |
| 唯一跟踪写入 | 原apps/web/src/features/nodes/BROWSER-ACCEPTANCE.md。预览/驱动/截图/trace只写忽略reports/_preview或reports/_review的新w06-r1子目录；不改生产组件/测试/App/配置/依赖/候选/共享文件 |
| W06-R01：同ID与关闭按钮 | 原C6 Escape会清空selectedId，C7 Space只是重开，不能证明alreadySelected分支。真实浏览器中让详情保持挂载、同一节点保持选中，以实际Tab/Shift+Tab返回该行，分别Enter和Space再次激活，记录动作前详情/行ID和实际activeElement、动作后焦点立即到详情。两种按键各自保持已选中前提，不先关闭/切换/刷新。再实际键盘激活关闭按钮，证明详情消失、焦点返回当前来源行。不得用element.focus/JS.click伪造键盘路线，也不能只等待后台刷新后观察焦点 |
| W06-R02：迟到失败与卸载 | 可控旧请求忽略abort：来源A→B且B成功后旧A失败；同key无效load→恢复有效后旧请求失败。分别记录旧signal已abort、旧失败被处理且不覆盖新数据/提示/计数/详情、不抢新焦点。另在pending时卸载真实NodeLoader，记录signal已abort、卸载后旧请求分别迟到成功/失败均不复活内容且无未处理拒绝；使用两个独立pending实例覆盖两种结局，不能对已settled Promise重复settle。仅卸载验收组件，不关闭整个页面来代替验证 |
| 首提交/结构化证据 | 延续真实NodeLoader/现有组件与合成fixture、固定时钟/受控Promise。保留已有A→B、A→null→A、同key失效恢复的用例；输出实际布局阶段首提交完整文本/节点计数/详情状态，不只保存firstCommitLen或最终截图。为新增操作保留完整前后activeElement、选中/显示ID、请求/abort/settle顺序、浏览器错误/未处理拒绝观察。不要把父组件观察器说成覆盖所有子组件独立更新 |
| 原报告校正 | C7原PASS保留为“关闭后Space重开”，不能追改成原来已测同ID重复。19/19仅是原已执行集合，不代表全部任务覆盖；新/未执行项逐项PASS/FAIL/BLOCKED/UNTESTED。报告和结果JSON统一Enter实际keyDown带text机制；截图/trace用实际完整绝对路径，不用省略号。注明原记录ddf8c5b和返修验证代码HEAD、工具版本/启动命令/端口、失败及局限 |
| 操作边界 | 仅localhost独立预览/独立浏览器资料，无真实账号/Controller/API/节点/生产/VPS/云或远程；不新增依赖。结束只停本任务创建的进程，保留原工件，不影响其他服务。不以本次证明认证/RBAC/真实HTTP或跨主体安全 |
| 验证 / 停止 | 实际浏览器记录、npm run check:web、npm run check:policy、git diff --check；库/工具用法按适用Context7/技能说明。若发现组件失败，只保留最小复现/截图并交总控，不顺带修组件。仅提交报告返修，交新HEAD/文件/命令输出/证据绝对路径后停止；不合并main/推送/启动其他Agent |

完成W06-R1并通过复审后，由总控结合已接收方案和安全证据整理具体负责人选择包。当前不请求抽象批准，不以方案接收或浏览器合成测试批准D01–D07/P1-C10、Cookie/Bearer、R1-A/R1-B、运行时profile或G2冻结。
