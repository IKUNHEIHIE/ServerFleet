# 节点加载容器 · 真实浏览器与键盘验收（W06）

- 会话：gemini-01；工作目录 `D:\admin\desktop\codex\myproject\ks\.worktrees\chore--web-node-browser-qa-gm01`
- 分支：`chore/web-node-browser-qa-gm01`；验证代码 HEAD：`ddf8c5beaee24694e687ad3166db32597ef60df1`
- 浏览器：HeadlessChrome `156.0.8078.49`（revision `29eb03535d5f2f4f899b95f22a92413ffde5284e`），Windows x64
- 页面入口：`http://127.0.0.1:8461/index.html`（临时静态服务，仅监听 127.0.0.1）
- 报告范围：只用真实 `NodeLoader` + 现有展示组件 + 合成 fixture（`FIXED_NOW` 固定时钟、可控 Promise）。
  不接真实 Controller/HTTP/账号，不代表首条只读闭环已验收。

## 环境与机制

| 项目 | 实际值 |
|---|---|
| 预览构建 | `npx vite build --config reports/_preview/vite.preview.config.ts`（临时配置，产物在 `reports/_preview/dist`） |
| 服务 | 任务内 Node `http` 服务，`127.0.0.1:8461`，脚本结束即 `server.close()` |
| 浏览器驱动 | Chrome DevTools Protocol（`--remote-debugging-port=9461`，独立 `--user-data-dir`） |
| 按键机制 | `Input.dispatchKeyEvent`：Tab/Shift+Tab/Escape/Space 用 `rawKeyDown`+`keyUp`；**Enter 用带 `text:"\r"` 的 `keyDown`**（实测 `rawKeyDown` 只触发自定义 `onKeyDown`，不会激活原生按钮） |
| 鼠标机制 | `Input.dispatchMouseEvent`（`mousePressed`/`mouseReleased`），非脚本 `.click()` |
| 首提交观察 | 预览内 `useLayoutEffect` 在**布局阶段**记录 `textContent`，早于被动 effect；断言用该记录，而不是最终 DOM |
| 结构化记录 | `reports/_review/w06/w06-results.json`（含每例结果、activeElement、tab 顺序、截图清单） |

**没有**使用 `element.focus()`、jsdom 或触发 JS `.click()` 冒充真实按键/点击。

## W06 原用例与结果（19/19 PASS）

真实 activeElement 记录来自 `window.__qa.focus()`，与 `Input.*` 事件同一页面上下文。

| 编号 | 场景 | 结果 | 实际观察 |
|---|---|---|---|
| C1 | 首次加载 | PASS | `hasLoading=true`、无表格，正文含“正在加载节点” |
| C2 | 成功后展示 | PASS | 表格 + `共 8 个节点` + `aws-tokyo-01` |
| C3 | 固定时钟指标质量 | PASS | 同屏含真实 `0%` 与 `陈旧 · 采样于 2026-10-29 02:41 UTC` |
| C4 | 真实 Tab 顺序 | PASS | `<INPUT>` 搜索 → `<SELECT>` 状态 → `<TR>` 首行（禁用的“清空筛选”被跳过） |
| C5 | Enter 激活行 | PASS | 焦点 `<SECTION>`，heading=`节点详情` |
| C6 | Tab / Shift+Tab / Escape | PASS | Tab→`<BUTTON>关闭详情`；再 Tab→`<BODY>`（可离开，无陷阱）；Shift+Tab 返回按钮；Escape→`<TR>` 且详情消失。**本项只验证 Escape，未激活关闭按钮**（关闭按钮的键盘激活见 R1 的 C22） |
| C7 | 关闭后 Space 重开 | PASS | Escape 关闭 → Space 重新打开同一行，焦点 `<SECTION>`。这是“未选中→新选中”，**不是重复激活**；重复激活见 R1 的 C20/C21 |
| C8 | 真实鼠标点击行 | PASS | 详情出现，焦点仍在 `<TR>`（指针不搬动焦点） |
| C9 | 失败安全提示 | PASS | `role=alert` 含固定文案“节点数据暂时无法加载，请稍后重试。”，无异常原文 |
| C10 | 鼠标点“重新加载” | PASS | 当帧 `hasLoading=true`、无表格/计数/详情 |
| C11 | 键盘 Tab+Enter 重试 | PASS | 焦点落到 `<BUTTON>重新加载`，Enter 后进入 loading |
| C12 | A→B 首个提交 | PASS | 首次提交文本含“正在加载节点”，不含 `aws-tokyo-01`、`共 8 个节点`，无表格 |
| C13 | B 成功 | PASS | 只显示 B 的 `gcp-singapore-01`，无 A 的节点 |
| C14 | 旧请求迟到成功 | PASS | 仍显示 B；旧请求 `signal.aborted=true`；焦点留在搜索框（未被抢走） |
| C15 | A→null→A | PASS | null 期 `alert`、无表格；恢复首个提交含“正在加载节点”，不含旧节点/计数/`节点详情` |
| C16 | 同 key 无效 load → 恢复 | PASS | 同上：`alert` 后恢复首个提交即 loading，旧详情不复活 |
| C17 | pending 期间失效 | PASS | 旧请求 `aborted=true`；迟到成功不覆盖恢复后的 loading |
| C18 | 详情打开时切换来源 | PASS | 切换前焦点 `<SECTION>`，切换后 `<BODY>`，400ms 后仍 `<BODY>`（无后台抢焦点） |
| C19 | 窄屏 420px | PASS | `htmlScrollWidth=420`（页面不横滚）、表格容器 `client=386 / scroll=801`（卡片内滚动） |

## 视觉检查（可复核截图）

绝对路径（均在 git 忽略的 `reports/_review/w06/`）：

| 截图 | 内容 | 结论 |
|---|---|---|
| `D:\admin\desktop\codex\myproject\ks\.worktrees\chore--web-node-browser-qa-gm01\reports\_review\w06\loading-state.png` | 首次加载 | 骨架/提示清晰，无表格 |
| `...\reports\_review\w06\error-state.png` | 失败 + 重试按钮 | 固定文案，按钮可见可用 |
| `...\reports\_review\w06\focus-row-1280.png` | 1280，键盘聚焦行 | 行有可见 focus ring；全 7 列可见；陈旧时间、0%/100%、离线破折号可读 |
| `...\reports\_review\w06\focus-detail-1280.png` | 1280，详情聚焦 | 详情卡片有 focus ring；字段三列排布，无裁切 |
| `...\reports\_review\w06\wide-1280.png` | 1280，详情打开 | 表格 + 详情同屏可读 |
| `...\reports\_review\w06\narrow-420.png` | 420 窄屏 | 卡片在视口内、右边界可见；表格在卡片内横向滚动；长名称不折行、不重叠 |

## 复现步骤

```bash
# 1) 构建临时预览（产物在 reports/_preview/dist，不提交）
npx vite build --config reports/_preview/vite.preview.config.ts
node -e "const fs=require('fs');const s=fs.readFileSync('reports/_preview/dist/reports/_preview/preview.html','utf8');fs.writeFileSync('reports/_preview/dist/index.html', s.replaceAll('../../assets/','./assets/'))"

# 2) 运行真实浏览器验收（脚本自行起 127.0.0.1:8461 静态服务与 Chrome，结束即关闭）
node reports/_review/w06/browser-qa.mjs

# 3) W06-R1 补验（127.0.0.1:8462 / CDP 9462，输出到 reports/_review/w06r1）
npx vite build --config reports/_review/w06r1/vite.preview.config.ts
node -e "const fs=require('fs');const p='reports/_review/w06r1/dist/reports/_review/w06r1/preview.html';fs.writeFileSync('reports/_review/w06r1/dist/index.html', fs.readFileSync(p,'utf8').replaceAll('../../assets/','./assets/'))"
node reports/_review/w06r1/browser-qa-r1.mjs

# 4) 常规检查
npm run check:web
npm run check:policy
```

## W06-R1 补充验收（2026-10-29）

审查指出原 C6/C7 与 C14/C17 的覆盖缺口（同 ID 重复激活、关闭按钮键盘激活、迟到失败、pending 卸载）。
本节在同一隔离预览的**新临时目录** `reports/_review/w06r1` 补测，并复跑原 19 项；原 `reports/_review/w06`
的结果与截图保持不动，作为历史证据。

### 覆盖边界

| 范围 | 内容 | 本轮结果 |
|---|---|---|
| 原 19 项（C1–C19） | 断言不变；C7 更名为“关闭后 Space 重开”，C6 注明只验证 Escape | 新目录复跑，全部 PASS |
| 新增 R01（C20–C22） | 同 ID 保持选中时的真实键盘重复激活（Enter / Space）；键盘激活关闭按钮后焦点返回 | 新增，PASS |
| 新增 R02（C23–C25） | 来源切换后旧请求迟到失败；同 key 失效恢复后旧拒绝；pending 卸载 + 迟到成功/失败 | 新增，PASS |

本轮共 **25/25 PASS**（原 19 + 新增 6）。

### 新增用例与实际记录

| 编号 | 场景 | 结果 | 实际记录 |
|---|---|---|---|
| C20 | 详情保持打开、`selectedRowId=aws-tokyo-01`，Shift+Tab 回来源行后 **Enter** 重复激活 | PASS | 打开后 `<SECTION>`(节点详情) → Shift+Tab `<TR>`（详情仍在、选中仍在）→ Enter 后 `<SECTION>`，详情仍存在 |
| C21 | 同一保持选中状态下，Shift+Tab 回行后 **Space** 重复激活 | PASS | `<TR>` → `<SECTION>`(节点详情) |
| C22 | **键盘激活关闭按钮**：Tab 到按钮后 Enter / Space 各一次（两次独立重开） | PASS | Enter：`<BUTTON>关闭详情` → `<TR>` 且详情消失；重开后 Space：同样 `<TR>` 且详情消失 |
| C23 | 来源切换（B 旧请求仍 pending → 切回 A 成功）后**旧请求迟到失败** | PASS | `aborted=true`；`hasAlert=false`；仍显示 `aws-tokyo-01`；焦点仍在 `<INPUT>` |
| C24 | 同 key 失效恢复后**旧请求迟到失败** | PASS | 仍 `hasLoading=true`、`hasAlert=false`、无表格 |
| C25 | **pending 卸载**：两个独立请求分别迟到成功 / 迟到失败 | PASS | 卸载时 `aborted=true`；卸载后与两次迟到 settle 后 `hasTable/hasAlert/hasDetail` 均为 false；未处理拒绝计数 `0 → 0 → 0` |

### 首提交原文（布局阶段记录，完整保留）

C12 / C15 / C16 的首个提交文本均为：
`搜索节点状态筛选全部状态在线离线已暂停已吊销未知清空筛选 筛选只作用于当前已加载的节点，不改变任何授权范围。节点列表 加载中 正在加载节点…`（67 字符），
不含 `aws-tokyo-01`、`共 8 个节点` 或 `节点详情`。完整字符串与每例状态见
`reports/_review/w06r1/w06r1-results.json` 的 `traces`（`firstCommitC12/C15/C16`、`r01`、`r01Close`、
`lateFailureSwitch`、`lateFailureSameKey`、`unmount`）。

### 证据（绝对路径，均在 git 忽略的 reports 下）

- 驱动：`D:\admin\desktop\codex\myproject\ks\.worktrees\chore--web-node-browser-qa-gm01\reports\_review\w06r1\browser-qa-r1.mjs`
- 预览：`...\reports\_review\w06r1\preview.tsx`、`preview.html`、`vite.preview.config.ts`
- 记录：`...\reports\_review\w06r1\w06r1-results.json`、`w06r1.log`
- 新增截图：`r01-repeat-enter-1280.png`（重复激活后详情聚焦）、`r01-close-return-1280.png`（关闭后行聚焦）、
  `r02-late-failure-1280.png`、`r02-unmounted.png`
- 复跑截图：`loading-state.png`、`error-state.png`、`focus-detail-1280.png`、`focus-row-1280.png`、
  `wide-1280.png`、`narrow-420.png`
- 服务端口：预览 `127.0.0.1:8462`、CDP `9462`；脚本结束即关闭（未见监听残留）

### 机制说明修正

W06 的 `w06-results.json` 曾把按键机制概括为 `rawKeyDown`；实际 **Enter 使用 `keyDown` + `text:"\r"`**（`rawKeyDown`
不会激活原生按钮）。本轮 `keyMechanism` 已按实际写明；原文件保留不改。

## 局限与未验证（UNTESTED）

- **UNTESTED**：真实 Controller/HTTP/登录、真实账号与跨主体授权；本页无任何真实网络请求。
- **UNTESTED**：屏幕阅读器（NVDA/JAWS/VoiceOver）实际播报；Headless Chrome 不能代表辅助技术栈。
- **UNTESTED**：Firefox/Safari 的 Tab 顺序与 `:focus-visible` 表现（仅 Chrome 156 实测）。
- **UNTESTED**：真实操作系统级别的高对比度/缩放/触摸输入。
- 本轮（含 R1）均为**作者在 headless Chrome 实跑**并由脚本记录，不是总控独立复跑；总控复审需自行重跑或核验驱动。
- C18 观察：来源切换时工作区按 `key` 重建，焦点回落 `<body>`（不残留旧详情、也不自动跳到新行）。
  这是现有约定下的行为，**不是缺陷**，但“切换后把焦点放到安全位置”的可用性改进需要产品决策，本项不修改组件。
- 本报告不证明真实 HTTP 闭环、身份/RBAC、性能或跨用户隔离能力。

## 结论

已接收的 `NodeLoader` 组合在真实浏览器中：键盘可达并可见；Enter/Space 在**同 ID 保持选中**时也能立即聚焦详情；
键盘激活“关闭详情”后详情消失且焦点回到来源行；Escape 同样返回；失败提示安全且鼠标/键盘重试可用；
来源切换、A→null→A、同 key 失效恢复的**首个提交**即隐藏旧节点/计数/详情（布局阶段记录为证）；
迟到成功与**迟到失败**都不覆盖新结果、不抢焦点；pending 卸载会 abort 且迟到 settle 不产生渲染变化或未处理拒绝；
420/1280 下可读且无页面级横向滚动。
