# 第三批独立审查：AUTH-01A / A01 / W02

日期：2026-10-29（Asia/Shanghai）；审查人：总控codex-review-01；任务BATCH03-REVIEW；基线a103c5f。审查依据用户给出的精确提交及THIRD-BATCH-WORK-PACKAGES.md。没有启动其他Agent、重写作者历史、推送或访问真实节点/云。

## 接收决定

| 任务 | 精确提交 | 结论 |
|---|---|---|
| AUTH-01A / codex-01 | `8e875705b2e218baeaf8235c5d7d0a6e1681fad2` | 通过；接收离线节点读取策略、错误分类及测试。仅新增internal/auth内4个文件，ports.go不变 |
| A01 / deepseek-01 | `d99d7efbe0ccd84a828b2413ec2c7ca099c66e95` | 通过；接收事件校验、受限查询视图和明确的测试内存适配。仅新增internal/audit内7个文件，ports.go不变 |
| W02 / gemini-01 | `5c6ceff8d689a81d0a186f692a83b8dd833e29de` | 通过；接收局部搜索/状态筛选及包装组件。8个文件均在features/nodes内，App/共享UI/依赖不变 |

本轮没有发现需要阻止上述离线任务接收的新问题。三个作者工作区审查时均无未提交的跟踪文件变更；未改作者源码。AUTH实际工作目录已由作者按用户指定移动至`D:\admin\desktop\codex\myproject\ks.worktrees\feature--auth-policy-cx01`，总控只核对并同步任务板。

集成在独立`.worktrees/integration--batch03-review`完成，无冲突。集成代码HEAD为`d1d7f11211c062efcb4eeaf6164821d44d3aa115`（含三个no-ff合入）；通过组合检查后才接收main。审查文档/任务单的后续提交不改变被测业务代码。作者分支保留。

## 关键审查依据

AUTH-01A：只有明确的nodes.read精确授权可以通过；空目标、通配、未知主体/动作、混合越权目标均拒绝。读取器每次取当前策略，没有旧允许兜底；读取失败与权限拒绝分别可用errors.Is判断，错误不含原始策略异常。取消/期限优先于最后读取给出的允许结果。适配器复制目标slice；策略map由可信读取器提供不可变快照。成功不代表认证、AI/root权限、审批或后续派发许可。

A01：Action由可信调用者的显式允许列表控制，Result是有限代码域；字符串长度、UTF-8、控制/格式字符、时间边界受检，错误不回显输入。View复制事件，查询复制投影，时间窗为[Since,Until)，时间倒序且同时间保持追加顺序；分页有界。相同RequestID与完全相同事件均不会去重丢失。MemoryRecorder仅位于testkit，没有生产组合根接线。视图依赖已校验记录和上游读取授权，不是秘密识别器或持久审计日志。

W02：搜索仅对传入集合的名称/ID作裁剪、大小写归一和子串匹配，按状态组合过滤，保持输入顺序/对象；不发请求。来源为空与筛选无命中分开，加载/失败不显示旧节点或成功计数。新增NodeList可选emptyContent和description=null保持默认调用行为。真实0/100、陈旧采样时间、离线无值及行键盘操作有回归；筛选不能替代Controller授权。

## 实际复验

使用固定Go1.27.3、Node24.22.1/npm11.19.1；以下结果是总控重新执行，不仅转述作者报告。

| 检查 | 结果 |
|---|---|
| AUTH：`go test -count=1 -cover ./internal/auth/...` | PASS，94.6% |
| A01：`go test -count=1 -cover ./internal/audit/...` | PASS，audit/testkit均100.0% |
| W02：`npm run check:web` | PASS，100项测试，类型检查/构建通过 |
| 撤权与最后读取取消/期限用例：`go test -count=20 ./internal/auth -run 'TestCancellationDuringFinalPolicyReadHasPriority\|TestRevocationAndReaderFailureAfterPriorAllow'` | PASS，重复20轮 |
| 独立调用实际auth/audit实现 | PASS，21个断言；含混合目标拒绝、撤权、源失败、取消、时间纳秒边界、32并发同请求事件、投影隔离、取消不写入 |
| 独立调用实际filterNodes等函数 | PASS，6个场景；组合过滤、中文、空白、顺序/引用不变和空结果计数 |
| 独立集成：`npm ci`、`npm run tools:install` | 最终PASS；安装127个npm包、构建固定4个Go工具；工具缓存恢复过程见下 |
| `npm run check` | PASS：policy/contracts/go/web/git全部通过；Go99个顶层测试、402个子测试，0失败；Web100项 |
| `npm run check:security` | PASS；npm found 0 vulnerabilities，govulncheck No vulnerabilities found |
| 原始资料/公共合同 | 69份原始文件与3份原候选字节校验通过；OpenAPI/Buf兼容与生成一致性、r02显式合同检查通过；无公共接口改动 |

摘要与复验输出归档于BATCH-03-RESULTS.json及evidence/batch03-*。独立Go探针运行于reports/_review，避免把临时main包混入go test ./...。证据中的探针源码保留为文本，不成为生产/常规测试包。

### 工具安装中的实际失败与恢复

首次tools:install失败：proxy.golang.org DNS无法解析。第二次显式GOPROXY=off使用本机模块缓存，仍因Go查询模块deprecation元数据失败。这两次均未当作成功。

随后使用主目录已有固定版本模块缓存作为本地file代理，并显式指定该GOMODCACHE；在本集成目录重新构建4个固定工具成功。未关闭GOSUMDB、module verify、静态检查、依赖扫描或依赖脚本限制，未改锁文件。完整check和security以此环境实际通过；npm安全扫描和govulncheck均获得成功结果。保留三次安装日志，网络故障不是业务代码失败。

## 已知接线缺口与停止条件

- nodes.Visible仍把除ctx外的所有授权错误过滤掉，因此策略不可用可能表现为成功空/部分列表。这是第二批已登记的调用方迁移缺口，AUTH-01A按任务范围只提供分类和建议，没有擅改nodes。本轮独立任务接收不等于这条链路已完成；第四批N02只允许明确ErrDenied作为过滤，其余错误必须中止，禁止半截成功结果。
- 认证/可信身份来源、策略持久化与授权后撤权原子性、真实执行审批均未实现；audit没有持久化/读取权限/防篡改保证；Web仍没有真实API或App接入。
- 原始wire长度/解码分配与重复oneof防护尚未完成，第四批T01-B仅做有界离线解码候选，不接网络或执行。
- GitHub CI、Linux race、浏览器实际Tab导航/视觉验收、真实节点及1C1G性能均UNTESTED。组件测试不替代这些验收；没有宣告G2冻结。

下一步为FOURTH-BATCH-WORK-PACKAGES.md的T01-B/N02/W03，各自独立目录，可人工同时派发。交付本报告、main接收和下一批工作目录后停止；不自动启动或发送其他会话消息。
