# 第四批独立审查：T01-B / N02 / W03

日期：2026-10-29（Asia/Shanghai）；审查任务BATCH04-REVIEW；审查人codex-review-01；起始ff66dea。依据用户精确提交、第四批任务单和现行架构边界；没有启动其他Agent或改写作者历史。

## 决定

| 任务 | 精确HEAD | 决定 |
|---|---|---|
| T01-B / codex-01 | `3488197265c1280e85330ff1bac477133b03e96d` | ACCEPT：有界离线帧解码候选；只新增internal/transport内3个文件，原T01-A/ports/Proto/gen不变 |
| N02 / deepseek-01 | `ea7ed5c99c85c28dc0797fcaeaa2ba1da9bef9f0` | CHANGES REQUESTED：1个P2问题，未合入main；常规测试通过不能关闭该问题 |
| W03 / gemini-01 | `64465c4bab86a6d2abd473afc6840604af6a73d1` | ACCEPT：局部只读详情及列表选择；11个文件均在features/nodes内，不改App/共享UI/依赖 |

作者三个工作区均无未提交的跟踪文件变更。审查只在忽略的reports/_review中建立总控探针；未改作者源码。通过的两项在独立integration/batch04-review工作目录组合，代码HEAD为`1b67e2fffd905cded10a4cdfeea5081ba241bcf9`；组合与安全检查通过后接收main。N02提交不在本次main历史中。

## N02-R01 · P2：多原因错误可掩盖致命授权失败

位置：N02提交的`internal/nodes/visibility.go:52–54`。直接按`errors.Is(authErr, auth.ErrDenied)`跳过，会把`errors.Join(auth.ErrDenied, auth.ErrPolicyUnavailable)`也当作普通拒绝。若前一个节点已允许，函数返回成功的部分列表；VisibleFilter和VisibleClassify沿用此错误结果。

独立探针在同一精确提交上分别组合ErrDenied与ErrPolicyUnavailable、ErrInvalidCheck、context.Canceled、context.DeadlineExceeded、未知错误。每种覆盖Visible及两个wrapper，共15个断言全部失败：error=nil、结果不是nil；列表保留第一个已允许节点。这是一个错误分类缺陷的15种复现，不是15个独立问题。ctx保持有效用于检验从Authorizer返回的取消分类；旧实现对返回的取消/期限会先传播，本次判断顺序会吞掉它们。

任务单已要求策略不可用、非法输入、取消/期限和未知错误必须中止。Authorizer端口没有“错误互斥”的约束；仅在HANDOFF要求组合根不使用errors.Join，不能代替本函数的失败处理。即使当前NodePolicyAuthorizer不产生多原因错误，其他合法端口实现或包装器仍会触发。

返修要求：只有纯拒绝可以过滤；包含其他原因的错误必须中止并返回nil结果，保留内部errors.Is/As可识别性。可保守拒绝所有多分支错误；不要靠修改auth端口、放松任务约束或口头限定调用者规避。增加正反顺序、外层包装、嵌套join和三个入口的实际实现测试；普通ErrDenied及单链包装拒绝仍须按原规则过滤。保留原提交，在原任务分支追加N02-R1提交。

证据：evidence/batch04-n02-joined-error-probe.log及其go.txt源码；预期fatal/nil断言实际退出1，明确保留FAIL。模块已有测试通过，未删除或篡改。

## 通过项的关键审查

T01-B：实际len(raw)在扫描/解码分配前受限；显式栈扫描已知message及未知group，共享全局字段预算、入栈前深度检查，未知bytes保持不透明。protobuf解码显式RecursionLimit/禁lazy，再调用原ValidateEnvelope。重复顶层元数据1–7及oneof20–32拒绝；合法repeated/未知扩展兼容，失败始终无消息输出。128层是实现能力上界，未当作生产默认或协议冻结。不能识别所有不重叠部分编码拼接的局限已明确。没有新增网络/真实身份/重放或执行准入。

W03：只存选中ID，每次从本轮ready来源重新解析；loading/error/来源移除时同步隐藏详情，同ID更新取新记录，没有旧对象缓存或自动改选。搜索过滤不扩大授权；恢复时仅取当前集合。指标和状态组件抽取供列表/详情共用，保留0/100、陈旧时间、离线无值。非模态区有可访问名称和关闭按钮；真实浏览器Tab、焦点落点与关闭返回位置未验收，已列入W04；本轮不宣称已完成视觉/读屏验收。

## 总控实际复验

固定Go1.27.3、Node24.22.1/npm11.19.1。各精确作者提交先执行模块检查；组合检查只含T01-B/W03，不含被退回N02。

| 检查 | 结果 |
|---|---|
| T01-B：go test -count=1 -cover ./internal/transport/... | PASS，98.0% |
| T01-B：go test ./internal/transport -run '^$' -fuzz '^FuzzDecodeEnvelopeFrame$' -fuzztime=5s -parallel=2 | PASS，36,980次执行，无panic/输入修改/失败消息泄露；非穷举证明 |
| 总控帧探针，直接调用DecodeEnvelopeFrame | PASS，10个断言：实际大小、重复字段/oneof、完整拼接、截断、group深度、未知bytes保留及语义失败无消息 |
| N02：go test -count=1 -cover ./internal/nodes/... ./tests/testkit/... | PASS，nodes95.3%，两个testkit100%；不覆盖已发现的多原因错误 |
| N02独立多原因错误探针 | **FAIL，退出1，15个断言失败**；N02-R01未关闭，不合并 |
| W03：npm run check:web | PASS，129项测试、类型检查和构建通过 |
| 独立组合：npm ci / npm run tools:install | PASS，127个npm包、固定4个Go工具；复用第三批已验证的本地file模块缓存，未改依赖或关闭校验 |
| 独立组合：npm run check | PASS，policy/contracts/go/web/git五组；Go113顶层/469子测试，0失败；Web129项 |
| 独立组合：npm run check:security | PASS，npm0漏洞，govulncheck无漏洞 |
| 原始资料/公共合同 | PASS，69原始文件和3原候选字节完整性；OpenAPI/Proto兼容、生成一致性及r02显式检查；公共契约与锁文件不变 |

完整摘要见BATCH-04-RESULTS.json；check/security/JUnit、probe/fuzz输出及源码文本归档evidence/batch04-*。全量通过仅证明本次接收的组合，不得表述为N02已通过。

## 未验证与交接

G2未冻结；真实WSS/mTLS/吊销、序列/连接身份绑定、任务审批/持久去重、生产审计/存储、HTTP/App接线、Linux race/GitHub CI、浏览器视觉/真实Tab和1C1G性能均未完成。main仍保留N02之前的旧节点可见性行为，第三批登记的授权不可用会表现为成功空/部分列表的接线缺口尚未关闭；此模块暂不接真实API。

第五批为T01-C（离线连接对账/入站序列候选）、N02-R1（本问题返修）、W04（键盘焦点/预览验收）。N02-R1继续原分支，其余新建工作目录；见FIFTH-BATCH-WORK-PACKAGES.md。交付审查/合入通过项和任务目录后停止，不自动派发或启动会话，不推送远程。
