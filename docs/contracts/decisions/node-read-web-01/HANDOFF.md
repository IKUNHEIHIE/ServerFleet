# READ-WEB-DESIGN-01(-R1) · 交接

## 1. 任务与身份

| 项 | 值 |
|---|---|
| 任务 | READ-WEB-DESIGN-01（首次交付）+ READ-WEB-DESIGN-01-R1（限定返修） |
| 唯一会话 | gemini-01 |
| 审查人 | 总控 codex-review-01（UI / 契约 / 安全） |
| 工作目录 | `D:\admin\desktop\codex\myproject\ks\.worktrees\docs--node-read-web-design-gm01` |
| 分支 | `docs/node-read-web-design-gm01` |
| 首次起始 HEAD | `4196b2e8ab9c389ae0830bf56f905d9bafa00d05` |
| 首次交付提交 | `63b0feb8dffad7dc88252dd7dd786b80c7e54e7c`（保留，未改写） |
| 返修起点 | `63b0feb8dffad7dc88252dd7dd786b80c7e54e7c` |
| 最终提交 | 本文件随 R1 提交产生；精确哈希在交接回复中给出（Git 无法在文件内自引用本提交号） |

未同步 main、未 rebase/reset、未清理旧工件；未读其他会话未提交内容；未访问真实节点/凭据/云。
返修依据：`D:\admin\desktop\codex\myproject\ks\docs\development\READ-DESIGN-01-REPAIR.md:33-35`（主目录，本轮未同步）。

## 2. 交付文件（始终只有这 3 个）

| 文件 | 行数（R1 后） | 内容 |
|---|---|---|
| `docs/contracts/decisions/node-read-web-01/DESIGN.md` | 426 | legacy DTO→局部 VM 规则、online × 指标矩阵、重复 ID/坏行边界、加载接缝与复用、页面/计数/文案、错误语义分层、键盘与窄屏、R1-B 迁移点、未定项 |
| `docs/contracts/decisions/node-read-web-01/ACCEPTANCE.md` | 118 | Web 验收项（含 R1 新增 WERR-03/WSES-06/07/WDTO-05/WCOMPAT-01）+ 返修覆盖映射 + UNTESTED/BLOCKED |
| `docs/contracts/decisions/node-read-web-01/HANDOFF.md` | 本文件 | 交接记录、修正位置、核验输出与风险 |

未新增/修改任何 TSX、TS、fetch、App、测试、mock、fixture、预览或依赖；未启动浏览器或静态服务。

## 3. R1 修正位置（对照返修项）

| 返修项 | 修正位置 | 修正内容 |
|---|---|---|
| **WD01-R01**（403 不得当认证失效） | DESIGN §6.1、§6.3、**新增 §6.4**、§5.4；ACCEPTANCE **WERR-01/03、WSES-06/07** | 401 由认证层判定并清会话；403 只清本列表/授权视图并递增**展示世代**，保持会话、不跳登录、不宣称服务端登出；503 与**身份来源故障**保持会话且不得提示重新登录；任何错误态先清旧行/计数 |
| **WD01-R02**（离线不得显示无时间的历史百分比） | DESIGN §3.2、**新增 §3.3.1**、§5.2；ACCEPTANCE **WDTO-03** | `LegacyMetric` 增加 `unavailable("offline")`，`value` 分支显式 `sampledAt: null`；`online=false` 一律 `—`+「离线，无当前读数」，行仍展示；`online=true` 的 0/100/普通值保留并标「无采样时间」；给出 online × 数值交叉矩阵 |
| 澄清：重复 ID / 部分坏行 | DESIGN **新增 §3.3.2**、§3.3 表、§5.3；ACCEPTANCE **WDTO-01/05、WERR-02、WCOUNT-01** | 身份/权威字段异常（id/online）与重复 id → **整次失败**；展示字段（name/region/指标）行内降级；**撤销原 `dropped` 口径**，不再有“部分成功” |
| 澄清：泛型抽取的重新验证 | DESIGN §4.2、**新增 §4.3 验证责任表**；ACCEPTANCE **新增 WCOMPAT-01**、§4.1 | 抽取 `useLoadState<T>` 后必须重跑富化用例（实测 **28** 个，非旧稿 26）并重做浏览器冒烟；历史 W06 证据只覆盖抽取前实现 |
| 澄清：跨标签范围 | DESIGN §9；ACCEPTANCE §6 | 明示与认证设计存在范围差异：不实现 BroadcastChannel/`pageshow`/`visibilitychange`，**不声称已完成跨标签隔离**，列为 UNTESTED |

保留的正确部分：独立 legacy 类型、不伪造 `status/revision/sampledAt`、不强转富化 VM、局部搜索不当授权、
首提交世代/迟到结果、A1 无详情时不假造行交互、真实 HTTP/浏览器明确 UNTESTED。

## 4. 核验命令与实际输出

```text
$ git rev-parse --abbrev-ref HEAD
docs/node-read-web-design-gm01
$ git rev-parse HEAD            # R1 前（返修起点）
63b0feb8dffad7dc88252dd7dd786b80c7e54e7c
$ git status --porcelain        # 起始时为空
```

加载器用例实数核对（`apps/web` 下运行）：

```text
$ npx vitest run src/features/nodes/node-loader.test.tsx
Test Files  1 passed (1)
      Tests  28 passed (28)
$ grep -c "  it(" src/features/nodes/node-loader.test.tsx
28
```

→ 原稿写的“26 个用例”已更正为 **28**，并写入 ACCEPTANCE §4.1 与 WCOMPAT-01。

引用核对（脚本解析三份文档中的路径/行号引用；排除本设计**提案中的新文件名**，裸文件名按
`apps/web/src/features/nodes/<name>` 解析）：

```text
引用总数: 42 | 缺失: none | 越界: none
```

`npm run check:policy`（固定工具链经 gitignore 的 `.tools` 目录联接）：

```text
PASS: 69 original files and 3 byte-exact candidates
# pass 8 / # fail 0
PASS: Web production import and Agent WebSocket boundaries
PASS: branch docs/node-read-web-design-gm01, 1 commit title(s); remote PR not available locally
ok  serverfleet.local/serverfleet/tests/architecture
> actionlint: -shellcheck=（无输出）
exit=0
```

`git diff --cached --check` 无输出；完整日志在忽略目录 `reports/_review/read-web-design/`。

## 5. 依赖与兼容影响

| 依赖 | 用途 | 状态 |
|---|---|---|
| `api/openapi/g2-merged-candidate.yaml:90-112,6639-6678` / `api/openapi/revisions/g2-r02/merged-candidate.yaml:12905-12944` | 旧列表操作与 NodeSummary（可空 cpu/memory 且无时间） | 候选，未冻结 |
| `api/openapi/revisions/g2-r03/nodes-read-candidate.yaml:9-10,20-35,41-49,193-196,209-217,217-302` | 新字段、legacyUntimed 示例、Cookie/Bearer 差异 | 候选，D02–D04 OPEN |
| `docs/contracts/decisions/node-read-01/*` | A1+B1 与 R1-A/R1-B 范围 | 已接收但整体 PROPOSED |
| 已接收 W05/W06 及复审 | 隔离/竞态规则来源 | 已接收（合成 fixture） |

兼容影响：本轮**不改任何生产代码**。后续若采纳“抽 `useLoadState<T>`”会改动已接收的 `node-loader.tsx`
（对外行为不变，但需变更评审）；备选“复制 legacy loader”不改已接收文件，代价是两套竞态逻辑。

## 6. 未定 / 阻塞 / 风险

| 项 | 类型 | 影响 |
|---|---|---|
| 列表是否接受会话 Cookie、双凭据拒绝规则 | **BLOCKED**（D05/D06 + 契约批准） | 未批准前无法真实接线；不假设 Cookie 已支持，不设计降级 |
| 401/403/503 的真实映射与身份来源故障语义 | **BLOCKED**（D06 + 认证设计） | 本设计只给分层原则与固定文案；不臆造状态码 |
| 跨标签页/`pageshow`/`visibilitychange` 同步 | 待与认证设计对齐 | 不实现、不声称完成；其他标签页可能短暂显示旧主体数据 |
| 后端对坏/重复 ID 的实际行为 | 待 D01 探测 | 若后端整次拒绝，与本设计同向；若返回部分合法，Web 仍整次失败（不提供部分成功） |
| `region/provider` 是否首版展示、是否双计数 | 可裁剪/未定 | 不影响类型与隔离规则 |
| 真实 HTTP / 认证 / 浏览器 / 屏幕阅读器 | UNTESTED | 本任务未执行也不声称已验证 |

## 7. 停止声明

R1 修正、矩阵同步与澄清均已完成；三份文档、引用核对、`check:policy` 与 diff 检查均已执行，
仅创建本地提交；未合并 main、未推送、未启动其他 Agent、未改写历史。等待总控复审。
