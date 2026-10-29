# READ-AUTH-DESIGN-01 · 本地交接

2026-10-29（Asia/Shanghai）；会话codex-01；审查人总控codex-review-01及安全/契约负责人。

| 项目 | 实际记录 |
|---|---|
| 工作目录 | `D:\admin\desktop\codex\myproject\ks.worktrees\docs--node-read-auth-design-cx01` |
| 分支 | `docs/node-read-auth-design-cx01` |
| 起始HEAD | `4196b2e8ab9c389ae0830bf56f905d9bafa00d05`；开始时git status --short无输出 |
| 代码/资料对照 | `0569c1a5ae01ea7715b53a979a3dbb0af50b6b3c`；到起始HEAD，internal/apps/api/proto/原始资料/契约修订无差异 |
| 最终HEAD | 在本地提交成功后以交接消息中`git rev-parse HEAD`的精确40位结果为准；本提交文档不内嵌自身哈希，避免自引用 |
| 方向/批准状态 | A1+B1已确认；详细设计PROPOSED；D01–D07/P1-C10 OPEN；G2未冻结 |
| 写入范围 | 仅本目录新增[DESIGN](DESIGN.md)、[ACCEPTANCE](ACCEPTANCE.md)、[HANDOFF](HANDOFF.md)；无代码/契约/共享文件修改 |

## 交付及建议

DESIGN给出可信身份→当前策略→N03-A责任链、Cookie与受限Bearer选择/拒绝矩阵、生命周期及参数、同源/CSRF流程、初始化/恢复/ADR适用性、HTTP/Web语义输入表与具体兼容差异。ACCEPTANCE给出AU01–AU34的输入/受控时序、可观察结果、未来运行层次和责任，全部未来运行UNTESTED。

推荐服务端不透明会话，固定HTTPS同源、__Host-sf_session、服务端空闲/绝对到期和当前撤销校验；Bearer仅受限非浏览器用途，不将JWT标签当选型。源读取/输出当前性闸门、世代及存储事务是未来需求；没有声称现有顺序读取解决撤权与输出原子性。没有读其他作者未提交内容，也未把语义需求写成DeepSeek/Gemini承诺。

## 本轮命令与实际输出

所有Git/文件命令在上表目录运行；仅公开安全文档通过Context7/浏览只读查询。未安装依赖/生成工具，不借主目录缓存写入。固定运行时由主目录env.ps1只读提供，`node --version`实际输出`v24.22.1`。

| 命令/核验 | 实际输出及解释 |
|---|---|
| Get-Location；git branch --show-current；git rev-parse HEAD；git status --short | 路径/分支/HEAD与上表一致；status为空；退出0 |
| git diff --exit-code 0569c1a5ae01ea7715b53a979a3dbb0af50b6b3c HEAD -- internal apps api proto docs/reference docs/contracts/revisions | 无输出，退出0；只说明这些范围相同，不表示真实安全测试通过 |
| 阅读AGENTS/README/架构索引/模块归属/依赖规则/TASK-BOARD/共同与本任务单；rg --files后读取来源与复审 | 已读当前已提交选择包/旧方案及验收/16缺口/r03三个矩阵文档/W06-R1和真实auth/nodes源码；不访问其他工作目录 |
| 早期文件定位重试 | 猜测的IMPLEMENTATION-PLAN.md/ACCEPTANCE-PLAN.md不存在，读取退出1；改用rg --files定位实际PROPOSAL.md/ACCEPTANCE.md。一次Windows含*的路径rg报告os error 123；该检索未作为证据，改用目录文件名检索。无文件被修改 |
| rg --files docs，文件名匹配PRIV/WC/PKI/UPDATE | 仅命中现行AI/PRIV解释附录；结合R06原索引，完整四份ADR未收录；不能推导从未批准或全都阻塞正常会话设计 |
| Context7 resolve_library_id → query_docs两次 | 官方/owasp/cheatsheetseries；分别查会话生命周期和CSRF；首个查询含框架片段，未选框架，另直接核对E01–E03原网页/章节；无项目内容或秘密发送外部 |
| 文档只读核验（下节） | 退出0；实际JSON：`{"status":"PASS","files":3,"localLinks":38,"lineRanges":31,"sourceQuotes":22,"acceptanceItems":34,"runtimeAcceptance":"UNTESTED","baselineContentEqual":true}` |
| git diff --check；git diff --cached --check | 均无输出、退出0；新增文件暂存后再次检查，非仅检查旧文件 |
| 新增文件白名单 | 初次PowerShell期望数组拼接表达式写错，检查退出1：`Staged file whitelist mismatch`，未提交；实际暂存列表确为下列3个A。改用逐文件ForEach-Object构造期望行后退出0：`PASS: exactly three allowed added Markdown files; no other tracked or untracked changes`；未放宽规则/改文件范围 |
| git diff --exit-code（既有跟踪文件/暂存后的工作差异） | 暂存前后均无输出、退出0；既有文件未改，暂存后没有遗漏的工作差异 |
| 本地commit后git rev-parse HEAD / git status --short | 精确HEAD交接消息报告；干净状态以最终实际输出为准 |

本任务未运行check:policy、完整check/security、auth/nodes业务测试、真实认证/HTTP/存储/代理/浏览器、race/性能/远程CI。任务单将policy设为可选、纯文档不新增镜像测试；本轮选择引用/原文/范围/diff检查，不把历史PASS算本轮通过。没有登录、签发凭据、真实秘密、生产/VPS/云、实现、推送/合并或Agent消息操作。

实际暂存文件输出为：

```text
A	docs/contracts/decisions/node-read-auth-01/ACCEPTANCE.md
A	docs/contracts/decisions/node-read-auth-01/DESIGN.md
A	docs/contracts/decisions/node-read-auth-01/HANDOFF.md
```

候选依赖仅为本基线的`api/openapi/g2-merged-candidate.yaml`（列表/详情认证、已有auth操作、CSRF及LoginResult/Session）、`docs/contracts/revisions/g2-r03/{DECISIONS,COMPATIBILITY,MATRIX}.md`与原始G2核心规则；来源/精确行及效力均在DESIGN。未修改/发布候选，不采用r02运行时profile；没有引入库、依赖或生成物。

## 可复跑的只读文档核验

从仓库根用PowerShell提取下方JavaScript后，以固定Node运行（运行时可只读加载主目录scripts/env.ps1）。它只检查本轮三文档的本地Markdown链接、范围、R01–R22原句、AU01–AU34唯一性/UNTESTED和基线文件一致性；不发网络请求，不替代人工语境/效力核验或未来运行验收。

实际运行方式（内存执行，不新增脚本文件）：

```powershell
. 'D:/admin/desktop/codex/myproject/ks/scripts/env.ps1'
$readAuthHandoffPath = 'docs/contracts/decisions/node-read-auth-01/HANDOFF.md'
$readAuthCheckSource = [regex]::Match((Get-Content -LiteralPath $readAuthHandoffPath -Raw), '(?ms)^```javascript\r?\n(.*?)^```\r?$').Groups[1].Value
$readAuthCheckSource | node --input-type=module
git diff --check
git diff --cached --check
git diff --exit-code
git diff --cached --name-status
git status --porcelain=v1 --untracked-files=all
```

```javascript
import fs from 'node:fs';
import path from 'node:path';
import { execFileSync } from 'node:child_process';
const dir = 'docs/contracts/decisions/node-read-auth-01';
const names = ['DESIGN.md', 'ACCEPTANCE.md', 'HANDOFF.md'];
const root = process.cwd();
let localLinks = 0, lineRanges = 0, sourceQuotes = 0;
const fence = String.fromCharCode(96).repeat(3);
const clean = s => s.replace(new RegExp('^' + fence + '[^\\n]*\\n[\\s\\S]*?^' + fence + '[ \\t]*$', 'gm'), '');
const fail = s => { throw new Error(s); };
for (const name of names) {
  const file = path.join(root, dir, name);
  const md = clean(fs.readFileSync(file, 'utf8'));
  for (const m of md.matchAll(/\[[^\]]+\]\(([^)]+)\)/g)) {
    if (/^https?:\/\//.test(m[1])) continue;
    const [rel, hash] = m[1].split('#');
    const target = path.resolve(path.dirname(file), decodeURIComponent(rel));
    const within = path.relative(root, target);
    if (within.startsWith('..') || path.isAbsolute(within) || !fs.existsSync(target)) fail('bad link: ' + m[1]);
    localLinks++;
    if (hash) {
      const range = /^L(\d+)(?:-L(\d+))?$/.exec(hash);
      if (!range) fail('unsupported local anchor: ' + hash);
      const lines = fs.readFileSync(target, 'utf8').trimEnd().split(/\r?\n/);
      const a = +range[1], b = +(range[2] || range[1]);
      if (a < 1 || b < a || b > lines.length) fail('bad range: ' + m[1]);
      lineRanges++;
    }
  }
  if (name === 'DESIGN.md') {
    const rows = md.split(/\r?\n/).filter(x => /^\| R\d{2} \|/.test(x));
    if (rows.length !== 22) fail('source row count');
    rows.forEach((row, i) => {
      if (!row.startsWith('| R' + String(i + 1).padStart(2, '0') + ' |')) fail('source sequence');
      const ref = /\]\(([^)]+)#L(\d+)-L(\d+)\)/.exec(row);
      const quote = /\| `([^`]+)` \|/.exec(row);
      if (!ref || !quote) fail('source syntax');
      const source = fs.readFileSync(path.resolve(path.dirname(file), ref[1]), 'utf8').split(/\r?\n/).slice(+ref[2] - 1, +ref[3]).join('\n');
      if (!source.includes(quote[1])) fail('quote mismatch: R' + (i + 1));
      sourceQuotes++;
    });
  }
  if (name === 'ACCEPTANCE.md') {
    const rows = md.split(/\r?\n/).filter(x => /^\| AU\d{2} \/ /.test(x));
    if (rows.length !== 34) fail('acceptance row count');
    rows.forEach((row, i) => {
      if (!row.startsWith('| AU' + String(i + 1).padStart(2, '0') + ' / ') || !row.includes('UNTESTED')) fail('acceptance state/sequence');
    });
  }
}
const unchanged = execFileSync('git', ['diff', '--name-only', '0569c1a5ae01ea7715b53a979a3dbb0af50b6b3c', '4196b2e8ab9c389ae0830bf56f905d9bafa00d05', '--', 'internal', 'apps', 'api', 'proto', 'docs/reference', 'docs/contracts/revisions'], { encoding: 'utf8' }).trim();
if (unchanged) fail('code/reference baseline changed');
console.log(JSON.stringify({ status: 'PASS', files: names.length, localLinks, lineRanges, sourceQuotes, acceptanceItems: 34, runtimeAcceptance: 'UNTESTED', baselineContentEqual: true }));
```

人工核验另检查来源状态/语境、A1+B1已确认与细节PROPOSED、原Cookie名/Bearer-only/现有生命周期操作及CSRF结构、内部坏ID的成因、缺失ADR按适用性列举。外部依据只支持机制，不批准数值/接口；旧W05状态以最新W06-R1复审更新，不改历史材料。

## 待总控组合审查、证据缺口与风险

1. 完整PRIV直接相关但未收录，需授权归档补证；WC/PKI-OPS/UPDATE分别按HTTPS密钥持有、恢复和更新是否实际涉及补适用条款，不把四份完整实现都设为A1的新增前置。
2. Cookie名称、列表新增Cookie、非JWT不透明Bearer、bootstrap不自动建会话、刷新后CSRF交付、Bearer管理范围是明确兼容差异；本轮未改候选。未知旧客户端/JWT签发方兼容、真实认证与凭据签发均无证据。
3. credentialEpoch/授权世代、持久撤销与恢复单调性、受限Token求交适配、Tgate尚无实现；顺序读取不能消除撤销到输出的窗口，D07需明示边界。当前类型也不能强制禁止旧scope/身份结果跨请求复用。
4. 同源代理/TLS配置、会话参数、登录验证器/KDF/第二因素/限速仍须安全复审。HttpOnly不防同源XSS操作，Lax不防同站恶意子域；严格来源要求有客户端兼容成本。
5. no-store不删除已显示内容；跨标签/休眠/远程撤销未被观察时存在旧显示窗口。真实Web世代/首提交/缓存、迟到Cookie/响应及身份切换须共同验收，fixture历史PASS不覆盖它们。

停止条件：仅提交这三份新增Markdown，交精确本地HEAD后停止，等待总控复审；不自行同步其他设计、追加实现或关闭D项。
