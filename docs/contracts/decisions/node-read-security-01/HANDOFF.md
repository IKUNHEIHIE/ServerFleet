# READ-SEC-01 · 本地交接

会话 `codex-01`；审查人 `codex-review-01`；2026-10-29。

- 实际工作目录：`D:\admin\desktop\codex\myproject\ks.worktrees\docs--read-security-evidence-cx01`。
- 实际分支：`docs/read-security-evidence-cx01`。
- 起始HEAD：`7ef78771da410a06d62a8605058e67a2f066ceb5`，开始时工作区干净。
- 最终本地HEAD：本地提交后在交接回复报告40位提交号，并输出 `git rev-parse HEAD`。
- 仅新增 [SOURCES.md](SOURCES.md)、[SECURITY-ACCEPTANCE.md](SECURITY-ACCEPTANCE.md)、本HANDOFF。未修改旧代码、候选、原文/批准状态、共享配置或其他作者任务。旧N03-A目录停止保留。

## 安全事实与交接边界

1. D05/D06/D07、D01–D04及P1-C10仍OPEN，G2未冻结。AUTH-01B/N03-A接收的是离线函数/用例，r03接收的是评审工件和D08检查接线；不代表认证、HTTP、缓存或存储方案获批。
2. 可信subject和当前可信策略/库存来源仍由未来调用方绑定；字符串格式、KnownNodes/ReadGrants、函数端口或私有scope不能证明身份和来源当前性。新用例有真实空库存预检/共同范围/错误无部分列表/复制隔离证据，旧Visible及原限制探针保持原样。
3. 来源坏ID的内部InvalidCheck不能直接推出HTTP400；D06需要区分失败阶段/来源，再批准公开映射。本文未新增wrapper、HTTP mapper或错误接口，也没有自行选择500/503/404等最终表现。
4. 旧scope是时点快照，新准备才观察当前策略。跨请求生命周期、凭据撤销、来源缓存、策略/库存/输出的原子绑定没有因离线通过而完成。no-store仍为候选，真实跨主体缓存/来源切换验收UNTESTED。
5. PRIV/WC/PKI-OPS/UPDATE完整原文未收录；S05已批准解释不能替代其原文。缺失条款与条件适用性列于SOURCES，交负责人从授权原归档补证；没有搜索项目外私人归档或编写不存在的条款。

供DeepSeek READ-PLAN-01总体方案引用的是SOURCES的来源分层和SECURITY-ACCEPTANCE的A01–A16安全事实/验收缺口，不是另一套产品选项。未读取其在编文件、改写方案、发送消息或启动其他Agent；Gemini仅引用本分支已有复审记录，不读其未提交返修。

## 本轮实际命令与输出

| 实际命令/核对 | 实际输出与范围 |
|---|---|
| Get-Location；git branch --show-current；git rev-parse HEAD；git status --short | 与上述目录/分支/起点一致，初始无变更 |
| Get-Content、rg -n（本分支原始来源/索引、批准记录、候选、代码、直接测试及已提交复审证据） | 已定位S01–S32；核对32处短句及其适用性，不以源字符串当运行实现 |
| rg --files docs -g '*PRIV*' -g '*WC*' -g '*PKI*' -g '*UPDATE*' | 仅有ADR-AI-PRIV-001-Approval-Interpretation-Accepted.md；与原索引“未收录”说明相符，不等于取消旧ADR效力 |
| rg生产目标范围内的会话/凭据/缓存头检索（具体命令见下） | 唯一session匹配为group.go关于不授权AI sessions的注释，没有发现这些目标范围中的登录/凭据/缓存头实现；与health主入口核对共同支持“尚未接线”，不是仅凭无匹配推导安全 |
| npm ci | PASS，127包；原Buf postinstall许可提醒保留，未修改许可或依赖 |
| npm run tools:install -- actionlint | PASS，只为本轮policy检查安装固定actionlint1.7.12；未安装/改写生产能力 |
| npm run check:policy | PASS；69 original files and 3 byte-exact candidates；8项工具测试通过；Web/Go边界、分支规则、actionlint通过。只含工具/结构检查，不是运行认证验收 |
| 下列内联JS经node --input-type=module执行 | PASS：3文档、52本地链接、76次行范围检查（含32处短句复核）、32原文片段、16验收项；历史探针36/33断言JSON一致，r03哈希与复审记录一致；没有新增测试文件或模拟用例实现 |
| git diff --check；git diff --cached --check及暂存范围核对 | PASS：仅本目录三个新增Markdown，既有跟踪文件无改动 |

本轮没有重跑auth/nodes业务测试、r03契约lint/负向运行、完整check/security或Web/浏览器验收。S24–S32的33/36断言、Go212/805、Web151及漏洞扫描结果均为已提交的**历史复审证据**；不是本轮新跑结果。历史W05组件4PASS/1FAIL保持其未接收交付语境，不虚构本轮FAIL或将其作为当前主线漏洞结论。

本轮静态重算r03候选SHA-256为 `dcb499b108fc6eab67412e08833100f9cb13c93d843863661faab56535180cfd`；这是与已提交复审记录的字节比对，不是新跑契约或运行验收。policy原始输出留在忽略的reports/check-policy.json、source-integrity.log、tooling-tests.log等；引用核对输出见本表及本次工具记录。

```powershell
rg -n 'session|Cookie|Bearer|JWT|subjectId|csrf|Cache-Control|no-store' apps/controller internal/auth internal/nodes apps/web/src/App.tsx -g '*.go' -g '*.tsx' -g '!*_test.go'
```

### 可复核的只读文档检查

设置项目固定Node环境后，将下面唯一javascript代码块通过 `node --input-type=module` 执行（本轮从本文件提取该代码块并经stdin运行）。检查仅验证本地链接/行号/原文片段、来源编号及已提交JSON，不测试或批准安全行为。

```javascript
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createHash } from "node:crypto";
import path from "node:path";
const root = process.cwd(), dir = path.resolve(root, "docs/contracts/decisions/node-read-security-01");
const files = ["SOURCES.md", "SECURITY-ACCEPTANCE.md", "HANDOFF.md"];
const read = file => readFileSync(file, "utf8");
const fence = String.fromCharCode(96).repeat(3);
let links = 0, ranges = 0, quotes = 0;
function target(from, href) {
  const [name, fragment] = href.split("#"), file = path.resolve(path.dirname(from), name);
  assert.ok(file.startsWith(root + path.sep), "reference escaped workspace");
  const lines = read(file).split(/\r?\n/);
  if (!fragment) return lines.join("\n");
  const m = /^L(\d+)(?:-L?(\d+))?$/.exec(fragment);
  assert.ok(m, "unexpected reference fragment: " + href);
  const start = Number(m[1]), end = Number(m[2] ?? m[1]);
  assert.ok(start >= 1 && end >= start && end <= lines.length, "bad line range: " + href);
  ranges++;
  return lines.slice(start - 1, end).join("\n");
}
for (const name of files) {
  const file = path.join(dir, name), body = read(file).replace(new RegExp(fence + "[\\s\\S]*?" + fence, "g"), "");
  for (const m of body.matchAll(/\[[^\]]*\]\(([^)]+)\)/g)) { target(file, m[1]); links++; }
}
const source = read(path.join(dir, "SOURCES.md")), ids = new Set();
for (const row of source.split(/\r?\n/)) {
  const m = /^\| (S\d{2}) \|.*?\]\(([^)]+)\) \| `([^`]+)` \|/.exec(row);
  if (!m) continue;
  assert.ok(!ids.has(m[1]), "duplicate source ID"); ids.add(m[1]);
  assert.ok(target(path.join(dir, "SOURCES.md"), m[2]).includes(m[3]), "quote mismatch: " + m[1]); quotes++;
}
assert.equal(quotes, 32);
const acceptance = read(path.join(dir, "SECURITY-ACCEPTANCE.md"));
for (const m of acceptance.matchAll(/\bS\d{2}\b/g)) assert.ok(ids.has(m[0]), "unknown source ID");
for (const m of acceptance.matchAll(/S(\d{2})[–-]S(\d{2})/g)) for (let n = +m[1]; n <= +m[2]; n++) assert.ok(ids.has("S" + String(n).padStart(2, "0")));
assert.equal((acceptance.match(/^\| A\d{2} /gm) ?? []).length, 16);
const json = file => JSON.parse(read(path.resolve(root, file)));
const auth = json("docs/development/reviews/evidence/auth01b-review-probe.json");
const nodes = json("docs/development/reviews/evidence/n03a-review-probe.json");
assert.equal(auth.status, "PASS"); assert.equal(auth.assertions, 36);
assert.equal(nodes.status, "PASS"); assert.equal(nodes.assertions, 33);
const review = json("docs/development/reviews/N03-A-RESULTS.json");
assert.equal(review.testedCodeHead, "0ec62214d206af71c4dffb81bdc033fde4eec062");
assert.equal(review.checks.combinedCheck.goTopLevelPassed, 212); assert.equal(review.checks.combinedCheck.goSubtestsPassed, 805);
for (const id of ["D05", "D06", "D07"]) assert.ok(review.openDecisions.includes(id));
const sha = createHash("sha256").update(readFileSync("api/openapi/revisions/g2-r03/nodes-read-candidate.yaml")).digest("hex");
assert.equal(sha, review.checks.g2r03CandidateSha256);
console.log(JSON.stringify({ status: "PASS", files: files.length, localLinks: links, lineRangeChecks: ranges, sourceQuotes: quotes, acceptanceItems: 16, historicalProbeAssertions: [36, 33], historicalEvidenceOnly: true, candidateSha256: sha }));
```

## 未决、未运行与停止

核心证据缺口为：完整安全ADR适用原文、D05可信身份/凭据/撤销决定与真实绑定、D06分来源错误与防枚举/兼容映射、D07请求生命周期/来源当前性/撤权输出边界、HTTP到浏览器的缓存和来源切换。详见A01–A16；待决定项PENDING，未运行的真实认证/HTTP/存储/缓存/浏览器均UNTESTED。

本轮只读来源/证据核验和文档/政策检查。未运行服务器、登录、凭据验证、真实节点、数据库、云、执行、Linux/race或GitHub CI；不以检查通过宣称原子性、实时撤权或生产安全。引用原始包及r03是候选依赖，不触发批准变更。本地提交后立即停止，交codex-review-01复审；不合并main、不推送、不扩展到下一任务。
