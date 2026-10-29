# 第五批独立审查：T01-C / N02-R1 / W04

日期：2026-10-29（Asia/Shanghai）；任务BATCH05-REVIEW；审查人codex-review-01；基线189112e。依据用户提供的精确提交和第五批任务单。不启动其他Agent、不改作者源码/历史、不推送或访问真实节点。

## 决定

| 任务 | 精确HEAD | 决定 |
|---|---|---|
| T01-C / codex-01 | `b118aa6a73ac6eddcb32c126bb9b64b89496bcab` | ACCEPT：离线连接对账与入站序列候选；仅internal/transport新增3个文件 |
| N02-R1 / deepseek-01 | `fdeb3e6f18ea1930a257eacbf986a4119d2602b1` | ACCEPT：N02-R01关闭；接收原ea7ed5c及追加返修，原历史保留 |
| W04 / gemini-01 | `8e95c3cbb8bfffc497449cc2728c4dd04fe45eee` | CHANGES REQUESTED：1个P2，W04-R01；未合并main |

三个作者工作区无未提交跟踪文件变更；总控探针只在忽略的reports/_review运行。N02按共同祖先合入，保留main已有T01-B/W03，不能把旧分支缺少后续文件的两树diff误当作删除提案。独立集成代码HEAD为`043b3fde140ee5b5f41ac2e18aad2dc3abc5396d`，只包含通过的两项及历史N02；W04不在接收组合中。

## N02-R01关闭证据

返修只允许精确ErrDenied及最终通向它的普通单链包装过滤；任何多分支错误均保守中止，已知致命分类优先，未知/过深链不作拒绝。返回原错误并给nil结果，保留内部errors.Is/As；ctx检查和真实NodePolicyAuthorizer组合仍在。testkit拒绝sentinel与auth.ErrDenied兼容，主体不再静默trim。

重跑第四批相同独立探针：ErrDenied分别与策略不可用、非法检查、取消、期限、未知故障组合，Visible/VisibleFilter/VisibleClassify共15个断言**全部PASS**，原来15个FAIL关闭。作者进一步覆盖顺序、包装/嵌套、自定义Is、多分支和普通纯拒绝；没有删掉失败覆盖。该修复接收后，第三批登记的“授权故障却成功返回空/部分列表”接线缺口也关闭；真实HTTP/身份/策略存储仍未接线。

## W04-R01 · P2：重复选择跳过聚焦，残留请求延迟抢焦点

位置：W04的`node-list-workspace.tsx:80–82`及无依赖聚焦effect。鼠标选中A后，焦点回到A行并按Enter；selectedId仍是A，React跳过相同状态更新，写入focusDetailNext这个ref本身不会触发提交，因此effect不能及时执行，焦点仍在列表行。若用户接着聚焦搜索框，后续来源数据刷新触发父组件更新，残留标志才被消费，焦点被移入详情。

独立Vitest配置只在reports加载当前真实组件、原固定fixture及原测试setup，未改生产文件或原测试。两个新增断言实际FAIL（退出1）：重复激活后详情未获焦点；转向搜索后后台刷新又抢走焦点。147项原测试通过不覆盖这个组合。问题是一次用户意图没有被即时处理/清理，并非两个独立缺陷。

返修要求：相同ID的Enter/Space及重复键盘选择也能立即聚焦；关闭、失效或用户移动焦点后，不残留等待后台更新执行的旧请求。用事件驱动的最小修复，保留非模态Tab/关闭返回/动态来源隐藏/指标行为，不用定时器反复focus或强制全局重渲染规避。原提交保留，追加W04-R1；两个独立用例纳入回归并补实际浏览器重复选择/刷新场景。

作者留存tab-check脚本/JSON和420/1280截图已只读检查：有真实浏览器CDP Tab→行→Enter→详情→Tab关闭→离开→Escape返回的记录，截图呈现独立合成数据组件。此记录不覆盖重复选择/后台刷新，也不是总控重跑的浏览器结果；本轮不以它关闭上述失败。原浏览器和预览工件保留，未由总控执行或清理。

## T01-C关键审查

绑定由可信调用者显式提供，按值保存，未从帧建绑定或自动重绑。Receive复用有界解码/语义检查，精确对账node/connection及方向，严格递增允许跳号，比较与提交在同一mutex内；失败不消费水位。最大uint64不作加一，避免回绕。句柄复制共享私有状态，Close终止所有别名；已持锁Receive可先线性化提交的边界已说明，不伪称能撤销业务。零值/nil未绑定默认拒绝，失败无可用消息，错误不含ID。

这些只是单内存连接的接纳规则，不证明mTLS、跨实例/重启防重放、全局连接ID唯一、任务去重、资源授权或审批。没有Sender/真实通信/执行接线，候选参数和协议未冻结。

## 总控实际复验

固定Go1.27.3、Node24.22.1/npm11.19.1。模块检查在作者精确提交上执行；组合检查只含T01-C/N02-R1，前端沿用已接收W03。

| 检查 | 结果 |
|---|---|
| T01-C：go test -count=1 -cover ./internal/transport/... | PASS，98.2% |
| T01-C：go test -count=20 ./internal/transport -run '^TestConnectionConcurrent' | PASS，32同序号/递增竞争/关闭竞争各重复20轮 |
| 总控实际ConnectionGuard探针 | PASS，12个断言；含失败不消费水位、32竞争1成功31拒绝、最大序号、句柄关闭和新旧连接隔离 |
| N02-R1：go test -count=1 -cover ./internal/nodes/... ./tests/testkit/... | PASS，nodes95.7%，两个testkit100% |
| N02原15个失败断言重跑 | PASS，15/15，N02-R01 CLOSED |
| W04：npm run check:web | PASS，147项原测试、类型检查、构建；不是本次main接收证据 |
| W04独立重复激活/刷新探针 | **FAIL，2项断言失败，退出1**；W04-R01 OPEN，未合并 |
| 独立组合npm ci / tools:install | PASS，127包/固定4个Go工具；复用已验证的本地file模块缓存，不关闭校验或改锁 |
| 独立组合npm run check | PASS，policy/contracts/go/web/git；Go144顶层/581子测试、0失败；Web129项 |
| 独立组合npm run check:security | PASS，npm0漏洞、govulncheck无漏洞 |
| 原始资料/契约/生成物 | PASS，69原始文件及3原候选字节完整性、OpenAPI/Buf兼容/生成一致性及r02显式检查；公共合同/锁未改 |

证据见BATCH-05-RESULTS.json和evidence/batch05-*；原N02失败保留在第四批证据。本轮W04完整FAIL日志、JUnit及外置探针/配置源码以文本归档，不删除失败，不混入常规测试包。

## 新登记的资料范围差异与后续

P1-C10已登记：Core无节点集合操作，但原Merged/r02已有GET /api/v1/nodes（listNodesForTransfer/NodeSummary）；旧模型有online布尔及可空CPU/内存却无采样时间，新NodeDetail有五状态/revision且无同形指标。不能依据Web局部Core适配说明断言所有候选都无列表/指标，也不能静默替换已有GET。下一批G2-R02A对齐来源和只读候选，仍不批准运行时profile或接口冻结。

下一批：G2-R02A契约准备、TASK-01A目标快照/未启动等待窗口、W04-R1焦点返修，见SIXTH-BATCH-WORK-PACKAGES.md。新任务用新目录，W04-R1继续原分支。GitHub/Linux race、真实API/身份/存储/WSS/任务/执行、1C1G性能仍未验收；App仍为开发骨架。交付审查、合入通过项和任务目录后停止，不自动启动其他会话。
