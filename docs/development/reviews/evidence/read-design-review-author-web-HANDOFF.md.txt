# READ-WEB-DESIGN-01 · 交接

## 1. 任务与身份

| 项 | 值 |
|---|---|
| 任务 | READ-WEB-DESIGN-01（A1+B1 下的 Web 具体设计，**只写设计**） |
| 唯一会话 | gemini-01 |
| 审查人 | 总控 codex-review-01（UI / 契约 / 安全） |
| 工作目录 | `D:\admin\desktop\codex\myproject\ks\.worktrees\docs--node-read-web-design-gm01` |
| 分支 | `docs/node-read-web-design-gm01` |
| 起始 HEAD | `4196b2e8ab9c389ae0830bf56f905d9bafa00d05` |
| 起始状态 | `git status --porcelain` 为空（干净） |
| 最终提交 | 本文件随唯一一次提交产生；精确哈希在交接回复中给出（Git 无法在文件内自引用本提交号） |

本轮没有读取或修改其他会话的未提交内容，没有访问真实节点/Controller/凭据/云。

## 2. 交付文件（仅此 3 个）

| 文件 | 行数 | 内容 |
|---|---|---|
| `docs/contracts/decisions/node-read-web-01/DESIGN.md` | 329 | legacy DTO→局部 VM 的逐字段规则、类型为何不能复用、加载接缝、页面/计数/文案、会话 key 与失效、错误/缓存、键盘与窄屏、R1-B 迁移点、未定项 |
| `docs/contracts/decisions/node-read-web-01/ACCEPTANCE.md` | 99 | 24 个 Web 验收项（DTO/计数/onlineOnly/会话失效/竞态/错误/缓存/键盘/窄屏/HTTP），含历史证据边界与 UNTESTED |
| `docs/contracts/decisions/node-read-web-01/HANDOFF.md` | 本文件 | 交接记录 |

未新增/修改任何 TSX、TS、fetch、App、测试、mock、fixture、预览或依赖；未启动浏览器或静态服务。

## 3. 核验命令与实际输出

```text
$ git rev-parse --abbrev-ref HEAD
docs/node-read-web-design-gm01
$ git rev-parse HEAD
4196b2e8ab9c389ae0830bf56f905d9bafa00d05
$ git status --porcelain
（空）
```

引用核对（脚本解析三份文档中的 `path:line[-line]` 引用，检查文件存在与行号不越界）：

```text
带行号引用数: 13
缺失路径: none
越界行号: none
```

`npm run check:policy`（通过主目录固定工具链，`.tools` 以目录联接接入，属 gitignore）：

```text
PASS: 69 original files and 3 byte-exact candidates
# pass 8 / # fail 0
PASS: Web production import and Agent WebSocket boundaries
PASS: branch docs/node-read-web-design-gm01, 1 commit title(s); remote PR not available locally
ok  serverfleet.local/serverfleet/tests/architecture  2.336s
> actionlint: -shellcheck=（无输出，退出 0）
exit=0
```

完整输出：`reports/_review/read-web-design/check-policy.log`（忽略目录，不提交）。

`git diff --cached --check` 无输出（无空白/冲突标记）。

## 4. 设计要点（便于复审定位）

1. **不伪造**：旧 DTO 缺 `status/revision/sampledAt`，因此新增 `LegacyNodeRow`，
   类型里没有这些字段；不存在 `as NodeListItemView`，也不把 `online=false` 猜成 offline/suspended/revoked。
2. **可复用 vs 必须新建**：`NodeLoader` 的状态机可复用但类型绑死富化 VM → 建议抽 `useLoadState<T>`；
   列/状态徽标/指标单元格/详情/面板均绑定富化字段 → 新建 legacy 版本；`matchesQuery` 只需放宽为结构类型。
3. **首提交隔离**：key = `sessionEpoch:subjectRef:onlineOnly`，沿用 W05-R2 的「精确 key + 可用性」世代规则；
   会话失效/切换主体/切换 onlineOnly 都必须先换 key 再渲染。
4. **计数口径**：只数当前已加载的可见行；隐藏节点、被丢弃行都不计入；不显示服务端总数。
5. **指标**：无 `sampledAt` 的旧数值显示数值 +「无采样时间」，不写「陈旧/新鲜」；0/100 保留，越界/非有限值不夹取。
6. **键盘**：首版无详情 → 行不可聚焦（不伪造 Enter/Space）；R1-B 接入详情时再启用 W03/W04 契约并重测。

## 5. 候选依赖与兼容影响

| 依赖 | 用途 | 状态 |
|---|---|---|
| `api/openapi/g2-merged-candidate.yaml:90-112,6639-6678` | 旧列表操作与 NodeSummary | 候选，未冻结 |
| `api/openapi/revisions/g2-r02/merged-candidate.yaml:12905-12944` | 同形旧 DTO 交叉核对 | 候选 |
| `api/openapi/revisions/g2-r03/nodes-read-candidate.yaml:9-10,20-35,41-49,193-196,209-217,217-302` | 新字段、legacyUntimed 示例、Cookie/Bearer 差异 | 候选，D02–D04 OPEN |
| `docs/contracts/revisions/g2-r03/DECISIONS.md:7-12` | D02–D04 未决 | — |
| `docs/contracts/decisions/node-read-01/*` | A1+B1 选择与 R1-A/R1-B 范围 | 已接收但整体 PROPOSED |
| 已接收 W05/W06 及复审 | 隔离/竞态规则的来源 | 已接收（合成 fixture） |

兼容影响：本轮**不改任何生产代码**。若后续实施采纳“抽 `useLoadState<T>`”，会改动已接收的
`node-loader.tsx`（对外行为不变，但需变更评审）；备选“复制 legacy loader”不改已接收文件，
代价是两套竞态逻辑，需在实施任务中明确取舍。

## 6. 未定 / 阻塞 / 风险

| 项 | 类型 | 影响 |
|---|---|---|
| 列表操作是否接受会话 Cookie、双凭据拒绝规则 | **BLOCKED**（D05/D06 + 契约批准） | 未批准前无法真实接线；本设计不假设 Cookie 已支持，也不设计降级 |
| 错误映射（401/403/503/400）与错误体 | **BLOCKED**（D06） | 设计只给固定文案与分类原则，不臆造状态码 |
| 跨标签页登出同步 | 未定 | 首版可能短暂显示旧标签页数据；不声称 Web 能独自证明撤权 |
| `region/provider` 是否首版展示 | 可裁剪 | 依赖产品确认；不影响类型 |
| 计数是否需要“全部可见/在线”双计数 | 未定 | 首版只给已加载计数 |
| r03 新字段若提前出现 | 风险 | 首版忽略；若服务端提前输出，Web 不会据其推导状态（避免第二权威） |
| 真实 HTTP / 认证 / 浏览器 / 屏幕阅读器 | UNTESTED | 本任务未执行，也不声称已验证 |

## 7. 停止声明

已完成三份文档、引用核对、`check:policy` 与 diff 检查，仅创建本地提交；
未合并 main、未推送、未启动其他 Agent、未改写历史。等待总控复审。
